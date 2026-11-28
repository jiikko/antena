package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jiikko/antena/batch/internal/s3fake"
	"github.com/jiikko/antena/batch/internal/state"
	"github.com/jiikko/antena/batch/internal/store"
)

const sitesYAML = `categories:
- {id: 1, name: VIP, slug: vip}
sites:
- {id: 2, name: two, url: "https://two.example/", rss_url: "https://two.example/rss", category: vip, position: 1}
- {id: 1, name: one, url: "https://one.example/", rss_url: "https://one.example/rss", category: vip, position: 2}
- {id: 3, name: off, url: "https://off.example/", rss_url: "https://off.example/rss", category: vip, enable: false}
`

func rss(items ...string) []byte {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?><rss version="2.0"><channel><title>t</title>`)
	for _, it := range items {
		b.WriteString(it)
	}
	b.WriteString(`</channel></rss>`)
	return []byte(b.String())
}

func item(title, url string) string {
	return fmt.Sprintf(`<item><title>%s</title><link>%s</link></item>`, title, url)
}

type env struct {
	t     *testing.T
	fake  *s3fake.Server
	feeds map[string][]byte
	now   time.Time
	log   bytes.Buffer
	err   bytes.Buffer
	sites string
}

func newEnv(t *testing.T) *env {
	fake := s3fake.New()
	t.Cleanup(fake.Close)
	dir := t.TempDir()
	sites := filepath.Join(dir, "sites.yml")
	if err := os.WriteFile(sites, []byte(sitesYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	return &env{t: t, fake: fake, feeds: map[string][]byte{}, now: time.Date(2026, 11, 28, 1, 0, 0, 0, time.UTC), sites: sites}
}

func (e *env) run(prefix string) error {
	vars := map[string]string{"MIGRATION_NEXT_PREFIX": prefix, "MIGRATION_NEXT_ALLOW_PUBLIC": "true"}
	st, err := store.FromEnv(func(k string) string { return vars[k] }, func(k string) (string, bool) { v, ok := vars[k]; return v, ok }, e.fake.Client())
	if err != nil {
		return err
	}
	r := &Runner{Store: st, SitesFile: e.sites, Now: func() time.Time { return e.now }, Log: &e.log, Err: &e.err,
		Fetch: func(_ context.Context, url string) ([]byte, error) {
			if body, ok := e.feeds[url]; ok {
				return body, nil
			}
			return nil, errors.New("connection refused")
		}}
	return r.Execute(context.Background())
}

func (e *env) posts(key string) []*state.Post {
	o, ok := e.fake.Object(key)
	if !ok {
		e.t.Fatalf("no %s", key)
	}
	st, err := state.Load([]byte(o.Body))
	if err != nil {
		e.t.Fatal(err)
	}
	return st.Posts
}

func TestRunWritesLiveKeysAndDedupsAcrossRuns(t *testing.T) {
	e := newEnv(t)
	e.feeds["https://one.example/rss"] = rss(item("a", "https://one.example/a"), item("b", "https://one.example/b"))
	e.feeds["https://two.example/rss"] = rss(item("c", "https://two.example/c"), item("a", "https://two.example/same-title"))
	e.feeds["https://off.example/rss"] = rss(item("off", "https://off.example/x"))
	if err := e.run(""); err != nil {
		t.Fatal(err)
	}
	posts := e.posts("state/posts.json")
	var got []string
	for _, p := range posts {
		got = append(got, fmt.Sprintf("%d:%d:%s", p.ID, p.SiteID, p.URLString()))
	}
	// Sites in id order; a title already seen (from site 1) is skipped; disabled sites are not fetched.
	want := []string{"1:1:https://one.example/a", "2:1:https://one.example/b", "3:2:https://two.example/c"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("posts = %v, want %v", got, want)
	}
	if !strings.Contains(e.err.String(), "WARN migration-next: s3://prpr-antena/state/posts.json does not exist") {
		t.Fatal(e.err.String())
	}
	for _, k := range []string{"index.html", "vip", "sites/1", "sites/2"} {
		if o, ok := e.fake.Object(k); !ok || o.ContentType != "text/html; charset=utf-8" {
			t.Errorf("page %s: %v %v", k, ok, o.ContentType)
		}
	}
	if _, ok := e.fake.Object("locks/migration-next-run.lock"); ok {
		t.Error("lock not released")
	}
	if !strings.Contains(e.log.String(), "one: +2\n") || !strings.Contains(e.log.String(), "two: +1\n") ||
		!strings.Contains(e.log.String(), "migration-next uploaded 4 pages under s3://prpr-antena// and 3 posts to s3://prpr-antena/state/posts.json") {
		t.Fatal(e.log.String())
	}

	e.feeds["https://one.example/rss"] = rss(item("new", "https://one.example/new"), item("a", "https://one.example/a"))
	e.now = e.now.Add(30 * time.Minute)
	if err := e.run(""); err != nil {
		t.Fatal(err)
	}
	posts = e.posts("state/posts.json")
	if len(posts) != 4 || posts[3].ID != 4 || posts[3].URLString() != "https://one.example/new" {
		t.Fatalf("second run: %d posts, last %+v", len(posts), posts[len(posts)-1])
	}
}

// A run under a page prefix never reads, writes or locks the live state.
func TestRunUnderPrefixUsesItsOwnStateAndLock(t *testing.T) {
	e := newEnv(t)
	e.fake.Put("state/posts.json", "live")
	e.feeds["https://one.example/rss"] = rss(item("a", "https://one.example/a"))
	if err := e.run("go-preview"); err != nil {
		t.Fatal(err)
	}
	if o, _ := e.fake.Object("state/posts.json"); o.Body != "live" {
		t.Fatal("live state changed")
	}
	for _, k := range e.fake.Puts {
		if !strings.HasPrefix(k, "go-preview/") && k != "state/go-preview/posts.json" && k != "locks/go-preview/migration-next-run.lock" {
			t.Errorf("wrote %s", k)
		}
	}
	for _, k := range e.fake.Gets {
		if k == "state/posts.json" || k == "locks/migration-next-run.lock" {
			t.Errorf("read %s", k)
		}
	}
	if len(e.posts("state/go-preview/posts.json")) != 1 {
		t.Fatal("preview state")
	}
}

func TestRunStopsOnUnreadableState(t *testing.T) {
	for name, setup := range map[string]func(*env){
		"denied":  func(e *env) { e.fake.GetStatus["state/posts.json"] = 403 },
		"broken":  func(e *env) { e.fake.Put("state/posts.json", "{broken") },
		"no time": func(e *env) { e.fake.Put("state/posts.json", `{"posts":[{"id":7,"site_id":1,"created_at":null}]}`) },
	} {
		e := newEnv(t)
		setup(e)
		if err := e.run(""); err == nil {
			t.Errorf("%s: want an error", name)
		}
		for _, k := range e.fake.Puts {
			if k == "state/posts.json" {
				t.Errorf("%s: state overwritten", name)
			}
		}
	}
}

func TestRunWarnsAndSkipsFailingFeeds(t *testing.T) {
	e := newEnv(t)
	e.feeds["https://one.example/rss"] = []byte(`<feed xmlns="http://www.w3.org/2005/Atom"></feed>`)
	if err := e.run(""); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(e.err.String(), "WARN one (site_id=1): feed error") || !strings.Contains(e.err.String(), "WARN two (site_id=2): feed error") {
		t.Fatal(e.err.String())
	}
}

// Sites with more than 50 posts lose posts created 5 days or more ago.
func TestPrune(t *testing.T) {
	now := time.Date(2026, 11, 28, 0, 0, 0, 0, time.UTC)
	var posts []*state.Post
	add := func(site, n int, age time.Duration) {
		for i := 0; i < n; i++ {
			posts = append(posts, &state.Post{ID: len(posts) + 1, SiteID: site, CreatedAt: now.Add(-age)})
		}
	}
	add(1, 40, time.Hour) // site 1: 51 posts, 11 at exactly 5 days
	add(1, 11, 5*24*time.Hour)
	add(2, 30, 6*24*time.Hour) // site 2: 50 posts, none pruned
	add(2, 20, time.Hour)
	st := &state.State{Posts: posts}
	prune(st, now)
	count := map[int]int{}
	for _, p := range st.Posts {
		count[p.SiteID]++
	}
	if count[1] != 40 || count[2] != 50 {
		t.Fatalf("after prune: %v", count)
	}
}

func TestStateJSONRoundTrip(t *testing.T) {
	e := newEnv(t)
	e.feeds["https://one.example/rss"] = rss(`<item><title>t &amp; &lt;u&gt;</title><link>https://one.example/q?a=1&amp;b=2</link><description>s</description><pubDate>Fri, 27 Nov 2026 10:00:00 +0900</pubDate></item>`)
	if err := e.run(""); err != nil {
		t.Fatal(err)
	}
	o, _ := e.fake.Object("state/posts.json")
	var doc map[string]any
	if err := json.Unmarshal([]byte(o.Body), &doc); err != nil {
		t.Fatal(err)
	}
	p := doc["posts"].([]any)[0].(map[string]any)
	if doc["schema_version"].(float64) != 1 || p["title"] != "t & <u>" || p["url"] != "https://one.example/q?a=1&b=2" ||
		p["published_at"] != "2026-11-27T01:00:00.000000Z" || p["created_at"] != "2026-11-28T01:00:00.000000Z" {
		t.Fatalf("%v", p)
	}
	if strings.Contains(o.Body, `\`+"u0026") { // encoding/json's HTML escaping of "&"
		t.Fatal("HTML-escaped JSON: " + o.Body)
	}
}
