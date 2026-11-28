package content

import (
	"encoding/json"
	"os"
	"testing"
)

// cases.json is the Ruby implementation's output (spec/fixtures/golden/images/build.rb).
func TestImageTagsMatchRuby(t *testing.T) {
	data, err := os.ReadFile("../../../spec/fixtures/golden/images/cases.json")
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
