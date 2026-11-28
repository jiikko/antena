// Package state reads and writes the post state (posts.json). The format is
// specified in docs/state-format.md.
package state

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"time"
)

// TimeLayout is Ruby's Time#iso8601(6): microseconds, "Z" for UTC, the
// parsed offset otherwise.
const TimeLayout = "2006-01-02T15:04:05.000000Z07:00"

type Post struct {
	ID          int
	SiteID      int
	Title       *string // nil: no title (JSON null)
	URL         *string
	Summary     *string
	Content     *string
	PublishedAt *time.Time
	CreatedAt   time.Time
	UpdatedAt   *time.Time
}

func (p *Post) TitleString() string {
	if p.Title == nil {
		return ""
	}
	return *p.Title
}

func (p *Post) URLString() string {
	if p.URL == nil {
		return ""
	}
	return *p.URL
}

type State struct {
	Posts []*Post
}

func (s *State) NextID() int {
	max := 0
	for _, p := range s.Posts {
		if p.ID > max {
			max = p.ID
		}
	}
	return max + 1
}

// Load fails on anything that is not a readable state, so a caller never
// replaces existing posts with an empty set.
func Load(data []byte) (*State, error) {
	var raw any
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("posts state could not be read: %w", err)
	}
	var rows []any
	switch t := raw.(type) {
	case []any:
		rows = t
	case map[string]any:
		v, ok := t["posts"]
		if !ok {
			return nil, fmt.Errorf("posts state could not be read: key not found: \"posts\"")
		}
		list, ok := v.([]any)
		if !ok {
			return nil, fmt.Errorf("posts state could not be read: posts is %T", v)
		}
		rows = list
	default:
		return nil, fmt.Errorf("posts state could not be read: top level is %T", raw)
	}
	st := &State{Posts: make([]*Post, 0, len(rows))}
	for _, r := range rows {
		row, ok := r.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("posts state could not be read: post is %T", r)
		}
		p, err := postFromRow(row)
		if err != nil {
			return nil, fmt.Errorf("posts state could not be read: %w", err)
		}
		st.Posts = append(st.Posts, p)
	}
	return st, nil
}

func postFromRow(row map[string]any) (*Post, error) {
	id, err := requiredInt(row, "id")
	if err != nil {
		return nil, err
	}
	siteID, err := requiredInt(row, "site_id")
	if err != nil {
		return nil, err
	}
	p := &Post{ID: id, SiteID: siteID}
	if _, ok := row["title"]; ok {
		p.Title, err = optionalString(row["title"])
	} else {
		p.Title, err = optionalString(row["name"])
	}
	if err != nil {
		return nil, err
	}
	if p.URL, err = optionalString(row["url"]); err != nil {
		return nil, err
	}
	if p.Summary, err = optionalString(row["summary"]); err != nil {
		return nil, err
	}
	if p.Content, err = optionalString(row["content"]); err != nil {
		return nil, err
	}
	if p.PublishedAt, err = optionalTime(row["published_at"]); err != nil {
		return nil, err
	}
	if p.UpdatedAt, err = optionalTime(row["updated_at"]); err != nil {
		return nil, err
	}
	v, ok := row["created_at"]
	if !ok {
		return nil, fmt.Errorf("key not found: \"created_at\"")
	}
	created, err := optionalTime(v)
	if err != nil {
		return nil, err
	}
	if created == nil {
		return nil, fmt.Errorf("post id=%d has no created_at", id)
	}
	p.CreatedAt = *created
	return p, nil
}

func requiredInt(row map[string]any, key string) (int, error) {
	v, ok := row[key]
	if !ok {
		return 0, fmt.Errorf("key not found: %q", key)
	}
	switch t := v.(type) {
	case json.Number:
		if n, err := t.Int64(); err == nil {
			return int(n), nil
		}
		// Ruby's Integer(12.7) truncates a float.
		f, err := t.Float64()
		if err != nil || math.IsInf(f, 0) || math.IsNaN(f) {
			return 0, fmt.Errorf("invalid %s: %s", key, t)
		}
		return int(f), nil
	case string:
		var n int
		if _, err := fmt.Sscanf(t, "%d", &n); err != nil || fmt.Sprint(n) != t {
			return 0, fmt.Errorf("invalid value for Integer(): %q", t)
		}
		return n, nil
	default:
		return 0, fmt.Errorf("can't convert %T into Integer", v)
	}
}

func optionalString(v any) (*string, error) {
	switch t := v.(type) {
	case nil:
		return nil, nil
	case string:
		return &t, nil
	default:
		return nil, fmt.Errorf("expected a string, got %T", v)
	}
}

func optionalTime(v any) (*time.Time, error) {
	switch t := v.(type) {
	case nil:
		return nil, nil
	case string:
		if t == "" {
			return nil, nil
		}
		parsed, err := ParseTime(t)
		if err != nil {
			return nil, err
		}
		return &parsed, nil
	default:
		return nil, fmt.Errorf("expected a time string, got %T", v)
	}
}

// ParseTime reads the ISO 8601 times both implementations write.
func ParseTime(s string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05Z0700", "2006-01-02 15:04:05Z07:00", "2006-01-02 15:04:05 -0700"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unreadable time %q", s)
}

func formatTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.Format(TimeLayout)
}

func (s *State) JSON(exportedAt time.Time) ([]byte, error) {
	type row struct {
		ID          int     `json:"id"`
		SiteID      int     `json:"site_id"`
		Title       *string `json:"title"`
		URL         *string `json:"url"`
		Summary     *string `json:"summary"`
		Content     *string `json:"content"`
		PublishedAt any     `json:"published_at"`
		CreatedAt   string  `json:"created_at"`
		UpdatedAt   any     `json:"updated_at"`
	}
	posts := make([]row, 0, len(s.Posts))
	for _, p := range s.Posts {
		posts = append(posts, row{
			ID: p.ID, SiteID: p.SiteID, Title: p.Title, URL: p.URL, Summary: p.Summary, Content: p.Content,
			PublishedAt: formatTime(p.PublishedAt), CreatedAt: p.CreatedAt.Format(TimeLayout), UpdatedAt: formatTime(p.UpdatedAt),
		})
	}
	doc := struct {
		SchemaVersion int    `json:"schema_version"`
		ExportedAt    string `json:"exported_at"`
		Posts         []row  `json:"posts"`
	}{1, exportedAt.UTC().Format(TimeLayout), posts}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}
