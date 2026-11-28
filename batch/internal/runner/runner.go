// Package runner is one batch run: lock, read the state, fetch the feeds,
// prune, write the state, write the pages. The steps and their order are in
// migration-next/state-format.md ("1回の実行で記事状態にすること").
package runner

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sort"
	"time"

	"github.com/jiikko/antena/batch/internal/catalog"
	"github.com/jiikko/antena/batch/internal/feed"
	"github.com/jiikko/antena/batch/internal/render"
	"github.com/jiikko/antena/batch/internal/state"
	"github.com/jiikko/antena/batch/internal/store"
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
	for _, k := range keys {
		if err := r.Store.Write(ctx, r.Store.Key(k), []byte(pages[k]), "text/html; charset=utf-8"); err != nil {
			return err
		}
	}
	fmt.Fprintf(r.Log, "migration-next uploaded %d pages under s3://%s/%s/ and %d posts to s3://%s/%s\n",
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
		r.warn(fmt.Sprintf("WARN migration-next: s3://%s/%s does not exist; starting from an empty post set", r.Store.Bucket, r.Store.StateKey()))
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
	for _, site := range sites {
		if site.RSSURL == "" {
			continue
		}
		body, err := r.Fetch(ctx, site.RSSURL)
		var entries []feed.Entry
		if err == nil {
			entries, err = feed.Parse(body, r.Now())
		}
		if err != nil {
			r.warn(fmt.Sprintf("WARN %s (site_id=%d): feed error %T: %v", site.Name, site.ID, err, err))
			continue
		}
		inserted := 0
		for _, e := range entries {
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
		return io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	}
}
