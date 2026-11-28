package render

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jiikko/antena/batch/internal/catalog"
	"github.com/jiikko/antena/batch/internal/state"
)

// Compares pages rendered from a real post state with the Ruby
// implementation's pages for the same input. The state holds third-party
// content, so it is not in the repo: ANTENA_LIVE_DIR points at a directory
// with posts.json, sites.yml, now.txt and ruby-pages/ and ruby-pages-preview/
// (one file per page key, "/" replaced by "_"), written by the Ruby renderer.
func TestLiveStateMatchesRuby(t *testing.T) {
	dir := os.Getenv("ANTENA_LIVE_DIR")
	if dir == "" {
		t.Skip("ANTENA_LIVE_DIR is not set")
	}
	read := func(name string) []byte {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	cat, err := catalog.Load(read("sites.yml"))
	if err != nil {
		t.Fatal(err)
	}
	st, err := state.Load(read("posts.json"))
	if err != nil {
		t.Fatal(err)
	}
	now, err := time.Parse(time.RFC3339, strings.TrimSpace(string(read("now.txt"))))
	if err != nil {
		t.Fatal(err)
	}
	for prefix, sub := range map[string]string{"": "ruby-pages", "migration-preview": "ruby-pages-preview"} {
		in, err := Attach(cat, st.Posts, prefix, now)
		if err != nil {
			t.Fatal(err)
		}
		pages, err := Pages(in)
		if err != nil {
			t.Fatal(err)
		}
		files, err := os.ReadDir(filepath.Join(dir, sub))
		if err != nil {
			t.Fatal(err)
		}
		if len(files) != len(pages) || len(files) == 0 {
			t.Errorf("%s: %d Ruby pages, %d Go pages", sub, len(files), len(pages))
		}
		for key, html := range pages {
			want, err := os.ReadFile(filepath.Join(dir, sub, strings.ReplaceAll(key, "/", "_")))
			if err != nil {
				t.Errorf("%s: %v", sub, err)
				continue
			}
			if html != string(want) {
				i := firstDiff(html, string(want))
				lo := max(0, i-80)
				t.Errorf("%s/%s differs at byte %d\n go:   %q\n ruby: %q", sub, key, i, html[lo:min(len(html), i+120)], string(want)[lo:min(len(want), i+120)])
			}
		}
	}
}
