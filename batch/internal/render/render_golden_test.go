package render

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/jiikko/antena/batch/internal/catalog"
	"github.com/jiikko/antena/batch/internal/state"
)

const golden = "../../../spec/fixtures/golden"

// The pages for the fixed golden input must equal the Ruby implementation's
// output byte for byte (spec/fixtures/golden/README.md).
func TestGoldenPages(t *testing.T) {
	// pages/: live keys; pages-preview/: links under /migration-preview and noindex.
	for prefix, sub := range map[string]string{"": "pages", "migration-preview": "pages-preview"} {
		t.Run(sub, func(t *testing.T) { checkGolden(t, prefix, sub) })
	}
}

func checkGolden(t *testing.T, prefix, sub string) {
	pages := renderGolden(t, prefix)
	var manifest map[string]string
	readJSON(t, filepath.Join(golden, sub, "manifest.json"), &manifest)

	keys := make([]string, 0, len(pages))
	for k := range pages {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	want := make([]string, 0, len(manifest))
	for k := range manifest {
		want = append(want, k)
	}
	sort.Strings(want)
	if len(keys) != len(want) {
		t.Fatalf("page keys = %v, want %v", keys, want)
	}
	for i := range keys {
		if keys[i] != want[i] {
			t.Fatalf("page keys = %v, want %v", keys, want)
		}
	}
	for key, file := range manifest {
		expected, err := os.ReadFile(filepath.Join(golden, sub, file))
		if err != nil {
			t.Fatal(err)
		}
		if got := pages[key]; got != string(expected) {
			t.Errorf("%s differs from %s/%s at byte %d", key, sub, file, firstDiff(got, string(expected)))
		}
	}
}

func renderGolden(t *testing.T, prefix string) map[string]string {
	t.Helper()
	sites, err := os.ReadFile(filepath.Join(golden, "input/sites.yml"))
	if err != nil {
		t.Fatal(err)
	}
	cat, err := catalog.Load(sites)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(golden, "input/posts.json"))
	if err != nil {
		t.Fatal(err)
	}
	st, err := state.Load(data)
	if err != nil {
		t.Fatal(err)
	}
	now, _ := time.Parse(time.RFC3339, "2026-11-27T12:00:00+09:00")
	in, err := Attach(cat, st.Posts, prefix, now)
	if err != nil {
		t.Fatal(err)
	}
	pages, err := Pages(in)
	if err != nil {
		t.Fatal(err)
	}
	return pages
}

// The Ruby implementation reads migration-next/templates/style.html; the copy
// embedded here must stay the same while both exist.
func TestStyleMatchesRubyTemplate(t *testing.T) {
	ruby, err := os.ReadFile("../../../migration-next/templates/style.html")
	if err != nil {
		t.Fatal(err)
	}
	if string(ruby) != style {
		t.Fatal("batch/internal/render/style.html differs from migration-next/templates/style.html")
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

func firstDiff(a, b string) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return min(len(a), len(b))
}
