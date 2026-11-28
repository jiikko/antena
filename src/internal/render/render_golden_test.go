package render

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/jiikko/antena/src/internal/catalog"
	"github.com/jiikko/antena/src/internal/state"
)

// testdata/: a fixed input and the pages expected from it. The pages were
// first produced by the former Ruby implementation, and the Go port was made
// to match them byte for byte. UPDATE_GOLDEN=1 rewrites them from the Go
// output; use it only when the output is meant to change, and read the diff.
const golden = "testdata"

func TestGoldenPages(t *testing.T) {
	// pages/: live keys; pages-preview/: links under /preview and noindex.
	for prefix, sub := range map[string]string{"": "pages", "preview": "pages-preview"} {
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
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		for key, file := range manifest {
			if err := os.WriteFile(filepath.Join(golden, sub, file), []byte(pages[key]), 0o644); err != nil {
				t.Fatal(err)
			}
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
