// Package catalog reads config/sites.yml: the fixed categories and sites.
package catalog

import (
	"fmt"
	"sort"
	"strconv"

	"gopkg.in/yaml.v3"
)

type Category struct {
	ID    int
	Name  string
	Slug  string
	Sites []*Site // sorted by (position, id)
}

func (c *Category) EnabledSites() []*Site {
	var out []*Site
	for _, s := range c.Sites {
		if s.Enabled {
			out = append(out, s)
		}
	}
	return out
}

type Site struct {
	ID       int
	Name     string
	URL      string
	RSSURL   string
	Category *Category
	Position int
	// Any value other than the boolean false enables the site (Ruby: data["enable"] != false).
	Enabled bool
}

func (s *Site) PagePath(page int) string {
	if page <= 1 {
		return fmt.Sprintf("/sites/%d", s.ID)
	}
	return fmt.Sprintf("/sites/%d/%d", s.ID, page)
}

type Catalog struct {
	Categories []*Category // file order
	Sites      []*Site     // file order
	bySlug     map[string]*Category
	byID       map[int]*Site
}

func (c *Catalog) CategoryBySlug(slug string) *Category { return c.bySlug[slug] }
func (c *Catalog) SiteByID(id int) *Site                { return c.byID[id] }

func (c *Catalog) EnabledSites() []*Site {
	var out []*Site
	for _, s := range c.Sites {
		if s.Enabled {
			out = append(out, s)
		}
	}
	return out
}

func Load(data []byte) (*Catalog, error) {
	var root map[string]any
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("sites YAML could not be read: %w", err)
	}
	if root == nil {
		root = map[string]any{}
	}
	catsData, ok := root["categories"]
	if !ok {
		return nil, fmt.Errorf("sites YAML has no categories")
	}
	sitesData, ok := root["sites"]
	if !ok {
		return nil, fmt.Errorf("sites YAML has no sites")
	}
	catRows, err := rows(catsData)
	if err != nil {
		return nil, fmt.Errorf("invalid sites YAML: %w", err)
	}
	siteRows, err := rows(sitesData)
	if err != nil {
		return nil, fmt.Errorf("invalid sites YAML: %w", err)
	}

	c := &Catalog{bySlug: map[string]*Category{}, byID: map[int]*Site{}}
	catByID := map[int]bool{}
	for _, row := range catRows {
		id, err := requiredInt(row, "id")
		if err != nil {
			return nil, fmt.Errorf("invalid sites YAML: %w", err)
		}
		name, err := requiredString(row, "name")
		if err != nil {
			return nil, fmt.Errorf("invalid sites YAML: %w", err)
		}
		slug, err := requiredString(row, "slug")
		if err != nil {
			return nil, fmt.Errorf("invalid sites YAML: %w", err)
		}
		cat := &Category{ID: id, Name: name, Slug: slug}
		if c.bySlug[slug] != nil || catByID[id] {
			return nil, fmt.Errorf("duplicate id or slug in sites YAML")
		}
		c.bySlug[slug] = cat
		catByID[id] = true
		c.Categories = append(c.Categories, cat)
	}
	for _, row := range siteRows {
		slug, err := requiredString(row, "category")
		if err != nil {
			return nil, fmt.Errorf("invalid sites YAML: %w", err)
		}
		cat := c.bySlug[slug]
		if cat == nil {
			return nil, fmt.Errorf("site id=%v references unknown category %q", row["id"], slug)
		}
		id, err := requiredInt(row, "id")
		if err != nil {
			return nil, fmt.Errorf("invalid sites YAML: %w", err)
		}
		name, err := requiredString(row, "name")
		if err != nil {
			return nil, fmt.Errorf("invalid sites YAML: %w", err)
		}
		position := 0
		if v, ok := row["position"]; ok && v != nil {
			if position, err = toInt(v); err != nil {
				return nil, fmt.Errorf("invalid sites YAML: position: %w", err)
			}
		}
		enabled := true
		if b, ok := row["enable"].(bool); ok && !b {
			enabled = false
		}
		site := &Site{
			ID: id, Name: name, URL: optionalString(row["url"]), RSSURL: optionalString(row["rss_url"]),
			Category: cat, Position: position, Enabled: enabled,
		}
		if c.byID[id] != nil {
			return nil, fmt.Errorf("duplicate id or slug in sites YAML")
		}
		c.byID[id] = site
		c.Sites = append(c.Sites, site)
		cat.Sites = append(cat.Sites, site)
	}
	for _, cat := range c.Categories {
		sort.SliceStable(cat.Sites, func(i, j int) bool {
			a, b := cat.Sites[i], cat.Sites[j]
			if a.Position != b.Position {
				return a.Position < b.Position
			}
			return a.ID < b.ID
		})
	}
	return c, nil
}

func rows(v any) ([]map[string]any, error) {
	list, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("expected a list, got %T", v)
	}
	out := make([]map[string]any, 0, len(list))
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("expected a mapping, got %T", item)
		}
		out = append(out, m)
	}
	return out, nil
}

func requiredInt(row map[string]any, key string) (int, error) {
	v, ok := row[key]
	if !ok {
		return 0, fmt.Errorf("key not found: %q", key)
	}
	return toInt(v)
}

// requiredString mirrors Ruby's data.fetch(key).to_s: the key must exist, nil becomes "".
func requiredString(row map[string]any, key string) (string, error) {
	v, ok := row[key]
	if !ok {
		return "", fmt.Errorf("key not found: %q", key)
	}
	return optionalString(v), nil
}

func optionalString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	default:
		return fmt.Sprint(t)
	}
}

// toInt mirrors Ruby's Integer(): integers and integer strings only.
func toInt(v any) (int, error) {
	switch t := v.(type) {
	case int:
		return t, nil
	case string:
		n, err := strconv.Atoi(t)
		if err != nil {
			return 0, fmt.Errorf("invalid value for Integer(): %q", t)
		}
		return n, nil
	default:
		return 0, fmt.Errorf("can't convert %T into Integer", v)
	}
}
