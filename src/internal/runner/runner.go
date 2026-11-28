// Package runner is one batch run: lock, read the state, fetch the feeds,
// prune, write the state, write the pages. The steps and their order are in
// docs/state-format.md ("1回の実行で記事状態にすること").
package runner

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"runtime/debug"
	"sort"
	"sync"
	"time"

	"github.com/jiikko/antena/src/internal/catalog"
	"github.com/jiikko/antena/src/internal/feed"
	"github.com/jiikko/antena/src/internal/render"
	"github.com/jiikko/antena/src/internal/state"
	"github.com/jiikko/antena/src/internal/store"
)

type Runner struct {
	Store     *store.Store
	SitesFile string
	Fetch     func(ctx context.Context, url string) ([]byte, error)
	Now       func() time.Time
	Log       io.Writer // progress lines (stdout)
	Err       io.Writer // WARN lines (stderr)
}

func (r *Runner) warn(msg string) { fmt.Fprintln(r.Err, msg) }

func (r *Runner) Execute(ctx context.Context) error {
	// The other progress lines come only after the first feed is fetched; this
	// one marks the start, so the run's own time can be read from the log.
	fmt.Fprintf(r.Log, "antena started: pages under s3://%s/%s/, state s3://%s/%s\n",
		r.Store.Bucket, r.Store.Prefix, r.Store.Bucket, r.Store.StateKey())
	lock := store.NewLock(r.Store, r.warn, store.DefaultStaleAfter, r.Now)
	ok, err := lock.Acquire(ctx)
	if err != nil || !ok {
		return err
	}
	defer lock.Release(ctx)

	sites, err := os.ReadFile(r.SitesFile)
	if err != nil {
		return fmt.Errorf("sites YAML does not exist: %s", r.SitesFile)
	}
	cat, err := catalog.Load(sites)
	if err != nil {
		return err
	}
	st, err := r.loadState(ctx)
	if err != nil {
		return err
	}
	if _, err := render.Attach(cat, st.Posts, r.Store.Prefix, r.Now()); err != nil {
		return err
	}
	r.fetchNewPosts(ctx, st, cat)
	prune(st, r.Now())

	// Save first so a later page upload failure can recover from the same post set.
	data, err := st.JSON(r.Now())
	if err != nil {
		return err
	}
	if err := r.Store.Write(ctx, r.Store.StateKey(), data, "application/json; charset=utf-8"); err != nil {
		return err
	}
	in, err := render.Attach(cat, st.Posts, r.Store.Prefix, r.Now())
	if err != nil {
		return err
	}
	pages, err := render.Pages(in)
	if err != nil {
		return err
	}
	keys := make([]string, 0, len(pages))
	for k := range pages {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if err := firstError(inParallel(len(keys), func(i int) error {
		return r.Store.Write(ctx, r.Store.Key(keys[i]), []byte(pages[keys[i]]), "text/html; charset=utf-8")
	})); err != nil {
		return err
	}
	fmt.Fprintf(r.Log, "antena uploaded %d pages under s3://%s/%s/ and %d posts to s3://%s/%s\n",
		len(pages), r.Store.Bucket, r.Store.Prefix, len(st.Posts), r.Store.Bucket, r.Store.StateKey())
	return nil
}

// loadState starts from an empty post set only when the key is missing. A
// file that exists but cannot be read fails the run, so the existing posts
// are never replaced by an empty set.
func (r *Runner) loadState(ctx context.Context) (*state.State, error) {
	data, err := r.Store.ReadIfExists(ctx, r.Store.StateKey())
	if err != nil {
		return nil, err
	}
	if data == nil {
		r.warn(fmt.Sprintf("WARN antena: s3://%s/%s does not exist; starting from an empty post set", r.Store.Bucket, r.Store.StateKey()))
		return &state.State{}, nil
	}
	return state.Load(data)
}

func (r *Runner) fetchNewPosts(ctx context.Context, st *state.State, cat *catalog.Catalog) {
	titles := map[string]bool{}
	urls := map[string]bool{}
	for _, p := range st.Posts {
		if p.Title != nil {
			titles[*p.Title] = true
		}
		if p.URL != nil {
			urls[*p.URL] = true
		}
	}
	nextID := st.NextID()
	// Sites in id order: ids are given in this order, and pages sort by id.
	sites := cat.EnabledSites()
	sort.SliceStable(sites, func(i, j int) bool { return sites[i].ID < sites[j].ID })
	// Feeds are fetched concurrently, but their entries are taken in the site
	// order above: which post wins a duplicate title and which id each post
	// gets must not depend on which feed answered first.
	entries := make([][]feed.Entry, len(sites))
	errs := inParallel(len(sites), func(i int) error {
		if sites[i].RSSURL == "" {
			return nil
		}
		body, err := r.Fetch(ctx, sites[i].RSSURL)
		if err != nil {
			return err
		}
		entries[i], err = feed.Parse(body, r.Now())
		return err
	})
	for i, site := range sites {
		if site.RSSURL == "" {
			continue
		}
		if err := errs[i]; err != nil {
			r.warn(fmt.Sprintf("WARN %s (site_id=%d): feed error %T: %v", site.Name, site.ID, err, err))
			continue
		}
		inserted := 0
		for _, e := range entries[i] {
			if urls[e.URL] || (e.Title != nil && titles[*e.Title]) {
				continue
			}
			now := r.Now().UTC()
			url, content := e.URL, e.Content
			st.Posts = append(st.Posts, &state.Post{
				ID: nextID, SiteID: site.ID, Title: e.Title, URL: &url, Summary: e.Summary, Content: &content,
				PublishedAt: e.PublishedAt, CreatedAt: now, UpdatedAt: &now,
			})
			urls[e.URL] = true
			if e.Title != nil {
				titles[*e.Title] = true
			}
			nextID++
			inserted++
		}
		fmt.Fprintf(r.Log, "%s: +%d\n", site.Name, inserted)
	}
}

// maxParallel bounds concurrent feed fetches and page uploads. With
// maxFeedBytes it also bounds the feed bodies held at once (8 x 16 MiB
// against the job's 512 MiB).
const maxParallel = 8

// inParallel runs fn(0..n-1) with at most maxParallel at a time and returns
// each call's error by index. Every call runs even after one fails, as the
// sequential loop it replaced would not have, so fn must be safe to run after
// another call's failure.
//
// A panic in fn becomes that index's error: a panic in a goroutine would end
// the process without running the caller's defers, which release the S3 lock
// (Execute), and the lock would block every run for an hour.
func inParallel(n int, fn func(i int) error) []error {
	errs := make([]error, n)
	sem := make(chan struct{}, maxParallel)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer func() { <-sem; wg.Done() }()
			defer func() {
				if p := recover(); p != nil {
					errs[i] = fmt.Errorf("panic: %v\n%s", p, debug.Stack())
				}
			}()
			errs[i] = fn(i)
		}()
	}
	wg.Wait()
	return errs
}

func firstError(errs []error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// prune drops, for each site with more than 50 posts (disabled sites
// included), every post created 5 days or more ago.
// Every post's site is in the catalog (render.Attach checked it).
func prune(st *state.State, now time.Time) {
	cutoff := now.Add(-5 * 24 * time.Hour)
	count := map[int]int{}
	for _, p := range st.Posts {
		count[p.SiteID]++
	}
	kept := st.Posts[:0]
	for _, p := range st.Posts {
		if count[p.SiteID] > 50 && !p.CreatedAt.After(cutoff) {
			continue
		}
		kept = append(kept, p)
	}
	st.Posts = kept
}

// HTTPFetch is Net::HTTP.get_response as the Ruby runner uses it: redirects
// are not followed, the status is not checked (the body is parsed as it is),
// gzip is decoded, and connecting and waiting for the response each have a
// 60 second limit.
func HTTPFetch() func(ctx context.Context, url string) ([]byte, error) {
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: 60 * time.Second}).DialContext,
			TLSHandshakeTimeout:   60 * time.Second,
			ResponseHeaderTimeout: 60 * time.Second,
		},
		// Net::HTTP has no total limit; this only bounds a feed that keeps trickling.
		Timeout: 10 * time.Minute,
	}
	return func(ctx context.Context, url string) ([]byte, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "Ruby")
		req.Header.Set("Accept", "*/*")
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		return readFeedBody(resp.Body, url)
	}
}

// maxFeedBytes is far above the largest feed seen (about 0.3 MB in 2026-11);
// it bounds memory, see maxParallel.
const maxFeedBytes = 16 << 20

// readFeedBody fails on a body over maxFeedBytes instead of truncating it:
// a truncated feed still parses up to the cut and would add part of its items.
func readFeedBody(body io.Reader, url string) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(body, maxFeedBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxFeedBytes {
		return nil, fmt.Errorf("feed body of %s is over %d bytes", url, maxFeedBytes)
	}
	return data, nil
}
