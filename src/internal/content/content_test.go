package content

import (
	"encoding/json"
	"os"
	"testing"
)

// testdata/images.json: bodies and the <img> tags expected from them, as the
// former Ruby implementation (libxml2) wrote them. Edit it by hand when the
// output is meant to change.
func TestImageTagsMatchRuby(t *testing.T) {
	data, err := os.ReadFile("testdata/images.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct{ Content, Images string }
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("no cases")
	}
	for _, c := range cases {
		if got := ImageTags(c.Content); got != c.Images {
			t.Errorf("ImageTags(%q)\n got %q\nwant %q", c.Content, got, c.Images)
		}
	}
}
