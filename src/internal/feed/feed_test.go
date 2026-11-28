package feed

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jiikko/antena/src/internal/content"
)

type expectedEntry struct {
	Title       *string `json:"title"`
	URL         string  `json:"url"`
	Summary     *string `json:"summary"`
	PublishedAt *string `json:"published_at"`
	Images      string  `json:"images"`
}

func compare(t *testing.T, name string, got []Entry, want []expectedEntry) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s: %d entries, want %d", name, len(got), len(want))
		return
	}
	for i, w := range want {
		g := got[i]
		var pub *string
		if g.PublishedAt != nil {
			s := g.PublishedAt.UTC().Format("2006-01-02T15:04:05.000000Z07:00")
			pub = &s
		}
		if !eq(g.Title, w.Title) || g.URL != w.URL || !eq(g.Summary, w.Summary) || !eq(pub, w.PublishedAt) {
			t.Errorf("%s[%d]:\n got title=%s url=%q summary=%s published=%s\nwant title=%s url=%q summary=%s published=%s",
				name, i, show(g.Title), g.URL, show(g.Summary), show(pub), show(w.Title), w.URL, show(w.Summary), show(w.PublishedAt))
		}
		if images := content.ImageTags(g.Content); images != w.Images {
			t.Errorf("%s[%d] images:\n got %q\nwant %q", name, i, images, w.Images)
		}
	}
}

func eq(a, b *string) bool { return (a == nil && b == nil) || (a != nil && b != nil && *a == *b) }
func show(s *string) string {
	if s == nil {
		return "nil"
	}
	return `"` + *s + `"`
}

// testdata/feeds.json: synthetic feeds and the entries the former Ruby
// implementation (Feedjira) read from them. Edit it by hand when the output
// is meant to change.
func TestSyntheticFeedsMatchRuby(t *testing.T) {
	data, err := os.ReadFile("testdata/feeds.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name    string           `json:"name"`
		Now     string           `json:"now"`
		XML     []byte           `json:"xml_base64"`
		Entries *[]expectedEntry `json:"entries"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("no cases")
	}
	for _, c := range cases {
		now, err := time.Parse(time.RFC3339, c.Now)
		if err != nil {
			t.Fatal(err)
		}
		got, err := Parse(c.XML, now)
		if c.Entries == nil {
			if err == nil {
				t.Errorf("%s: want an error (Ruby returned nil), got %d entries", c.Name, len(got))
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", c.Name, err)
			continue
		}
		compare(t, c.Name, got, *c.Entries)
	}
}

// Recorded real feeds and the entries the former Ruby implementation read
// from them (third-party content, kept only in the Mac's tmp/; src/README.md).
// ANTENA_FEED_FIXTURES overrides the place.
func TestRecordedFeedsMatchRuby(t *testing.T) {
	dir := os.Getenv("ANTENA_FEED_FIXTURES")
	if dir == "" {
		dir = "../../../tmp/feed-record-20261127"
	}
	metas, _ := filepath.Glob(filepath.Join(dir, "*.meta.json"))
	if len(metas) == 0 {
		t.Skipf("no recorded feeds in %s", dir)
	}
	for _, metaPath := range metas {
		base := metaPath[:len(metaPath)-len(".meta.json")]
		var meta struct {
			RecordedAt string `json:"recorded_at"`
		}
		readJSON(t, metaPath, &meta)
		var raw []map[string]any
		readJSON(t, base+".expected.json", &raw)
		want := make([]expectedEntry, 0, len(raw))
		for _, r := range raw {
			e := expectedEntry{URL: r["url"].(string)}
			if v, ok := r["title"].(string); ok {
				e.Title = &v
			}
			if v, ok := r["summary"].(string); ok {
				e.Summary = &v
			}
			if v, ok := r["published_at"].(string); ok {
				p, err := time.Parse(time.RFC3339Nano, v)
				if err != nil {
					t.Fatal(err)
				}
				s := p.UTC().Format("2006-01-02T15:04:05.000000Z07:00")
				e.PublishedAt = &s
			}
			c, _ := r["content"].(string)
			e.Images = content.ImageTags(c)
			want = append(want, e)
		}
		body, err := os.ReadFile(base + ".xml")
		if err != nil {
			t.Fatal(err)
		}
		now, err := time.Parse(time.RFC3339Nano, meta.RecordedAt)
		if err != nil {
			t.Fatal(err)
		}
		got, err := Parse(body, now)
		if err != nil {
			t.Errorf("%s: %v", filepath.Base(base), err)
			continue
		}
		if len(want) == 0 {
			t.Errorf("%s: no expected entries", filepath.Base(base))
		}
		compare(t, filepath.Base(base), got, want)
	}
}

func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatal(err)
	}
}
