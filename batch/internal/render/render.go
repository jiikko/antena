// Package render builds the static pages. It is a byte-for-byte port of
// the former Ruby implementation's HtmlRenderer; the expected output is
// testdata/pages (render_golden_test.go).
package render

import (
	_ "embed"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/jiikko/antena/batch/internal/catalog"
	"github.com/jiikko/antena/batch/internal/content"
	"github.com/jiikko/antena/batch/internal/state"
)

//go:embed style.html
var style string

var tokyo = time.FixedZone("+09:00", 9*60*60)

// Input is the catalog with every post attached to its site (Attach).
type Input struct {
	Catalog *catalog.Catalog
	Posts   []*state.Post
	Prefix  string
	Now     time.Time

	sitePosts map[int][]*state.Post
	postSite  map[*state.Post]*catalog.Site
}

// Attach links posts to sites. A post of a site missing from the catalog is
// an error (the Ruby runner raises the same).
func Attach(c *catalog.Catalog, posts []*state.Post, prefix string, now time.Time) (*Input, error) {
	in := &Input{Catalog: c, Posts: posts, Prefix: prefix, Now: now,
		sitePosts: map[int][]*state.Post{}, postSite: map[*state.Post]*catalog.Site{}}
	for _, p := range posts {
		site := c.SiteByID(p.SiteID)
		if site == nil {
			return nil, fmt.Errorf("post id=%d references unknown site id=%d", p.ID, p.SiteID)
		}
		in.postSite[p] = site
		in.sitePosts[site.ID] = append(in.sitePosts[site.ID], p)
	}
	return in, nil
}

// Pages returns S3 keys relative to the prefix and their HTML.
func Pages(in *Input) (map[string]string, error) {
	r := &renderer{in}
	vip := in.Catalog.CategoryBySlug("vip")
	if vip == nil {
		return nil, fmt.Errorf("category slug=vip is required for the index page")
	}
	out := map[string]string{"index.html": r.categoryPage(vip)}
	for _, cat := range in.Catalog.Categories {
		out[cat.Slug] = r.categoryPage(cat)
	}
	for _, site := range in.Catalog.EnabledSites() {
		posts := append([]*state.Post(nil), in.sitePosts[site.ID]...)
		sort.SliceStable(posts, func(i, j int) bool { return posts[i].ID > posts[j].ID })
		pageCount := int(math.Ceil(float64(len(posts)) / 50.0))
		for page := 1; page <= pageCount; page++ {
			key := fmt.Sprintf("sites/%d", site.ID)
			if page > 1 {
				key = fmt.Sprintf("sites/%d/%d", site.ID, page)
			}
			out[key] = r.sitePage(site, posts, page, pageCount)
		}
	}
	return out, nil
}

type renderer struct{ in *Input }

// byCreatedDesc orders by created_at, newest first. Ruby's sort_by is not
// stable and leaves ties unordered; ties are broken here by id, newest first.
func byCreatedDesc(posts []*state.Post) []*state.Post {
	out := append([]*state.Post(nil), posts...)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i].CreatedAt, out[j].CreatedAt
		if !a.Equal(b) {
			return a.After(b)
		}
		return out[i].ID > out[j].ID
	})
	return out
}

func first(posts []*state.Post, n int) []*state.Post {
	if len(posts) > n {
		return posts[:n]
	}
	return posts
}

func (r *renderer) categoryPage(cat *catalog.Category) string {
	now := r.in.Now
	enabled := cat.EnabledSites()
	siteIDs := map[int]bool{}
	for _, s := range enabled {
		siteIDs[s.ID] = true
	}
	from := now.Add(-2 * 24 * time.Hour)
	var fresh []*state.Post
	for _, p := range r.in.Posts {
		if siteIDs[p.SiteID] && !p.CreatedAt.Before(from) && !p.CreatedAt.After(now) {
			fresh = append(fresh, p)
		}
	}
	sort.SliceStable(fresh, func(i, j int) bool { return fresh[i].ID > fresh[j].ID })
	fresh = first(fresh, 150)

	var cards strings.Builder
	for _, site := range enabled {
		var items strings.Builder
		for _, p := range first(byCreatedDesc(r.in.sitePosts[site.ID]), 4) {
			items.WriteString(`<li class="post_by_site">` + r.link(p.TitleString(), p.URLString(), linkOpts{external: true, tracking: true}) + `</li>`)
		}
		cards.WriteString(`<li class="content"><div class="sitetitle">` + r.link(site.Name, site.PagePath(1), linkOpts{}) + `</div><ul>` + items.String() +
			`<li class="post_by_site" style="text-align:right">` + r.link("続きを見る", site.PagePath(1), linkOpts{}) + `</li></ul></li>`)
	}

	var rows strings.Builder
	var previousDay string
	for _, p := range fresh {
		local := p.CreatedAt.In(tokyo)
		day := local.Format("2006-01-02")
		if day != previousDay {
			rows.WriteString(fmt.Sprintf(`<tr><td><b><ins>%d/%d</ins></b></td></tr>`, int(local.Month()), local.Day()))
			previousDay = day
		}
		site := r.in.postSite[p]
		desktop := r.link(p.TitleString(), p.URLString(), linkOpts{external: true, tracking: true})
		mobile := r.link(truncate(p.TitleString(), 40)+" ("+site.Name+")", p.URLString(), linkOpts{external: true, tracking: true})
		rows.WriteString("<tr>\n" +
			`  <td width="50px">` + local.Format("15:04") + "</td>\n" +
			`  <td class="mawarikomi"><div class="visible_pc">` + desktop + `</div><div class="visible_sp"><p style="height:70px">` + mobile + "</p></div></td>\n" +
			`  <td><div class="visible_pc">` + r.link(site.Name, site.URL, linkOpts{external: true}) + `</div><div class="visible_sp"></div></td>` + "\n" +
			"</tr>\n")
	}

	body := `<div id="base"><div class="container">` + "\n" +
		`  <div class="left_column">` + r.categoryMenu(cat) + "\n" +
		`    <div id="top"><div id="headline_block"><table id="headline_body"><tbody>` + rows.String() + `</tbody></table></div>` + "\n" +
		`      <div id="main"><ul id="sites">` + cards.String() + `</ul></div>` + "\n" +
		"    </div>\n" +
		"  </div>\n" +
		`  <div class="right_column">` + r.recentSidebar() + "</div>\n" +
		"</div></div>\n"
	return r.document("ぺろぺろあんてな", body, 0, "")
}

func (r *renderer) sitePage(site *catalog.Site, sitePosts []*state.Post, page, pageCount int) string {
	start := (page - 1) * 50
	end := start + 50
	if end > len(sitePosts) {
		end = len(sitePosts)
	}
	var posts []*state.Post
	if start < len(sitePosts) {
		posts = sitePosts[start:end]
	}
	cat := site.Category
	var items strings.Builder
	for _, p := range posts {
		images := ""
		if p.Content != nil {
			images = content.ImageTags(*p.Content)
		}
		date := p.CreatedAt.In(tokyo).Format("2006/1/2")
		items.WriteString(`<li class="tag_post">` + "\n" +
			`  <div class="hidden" data="post-content">` + images + "</div>\n" +
			`  <div class="tag_post_image"></div>` + "\n" +
			`  <div class="tag_post_body"><p><div>` + r.link(p.TitleString(), p.URLString(), linkOpts{external: true}) + `</div><div class="text-right">` + r.link(site.Name, site.URL, linkOpts{external: true}) + "</div></p>\n" +
			`    <div class="scraped-images"></div><div class="category"><span>カテゴリ：</span>` + r.link(cat.Name, categoryPath(cat.Slug), linkOpts{}) + "</div>\n" +
			`    <div><span>サイト(当サイト内へのリンク)：</span>` + r.link(site.Name, site.PagePath(1), linkOpts{}) + `</div><div class="tag_post_tags"></div><div class="text-right">` + h(date) + "</div>\n" +
			"  </div>\n" +
			"</li>\n")
	}

	var same strings.Builder
	for _, peer := range cat.EnabledSites() {
		name := peer.Name
		if name == "" {
			name = "登録名なし"
		}
		same.WriteString("<li>" + r.link(fmt.Sprintf("%s(%d)", name, len(r.in.sitePosts[peer.ID])), peer.PagePath(1), linkOpts{}) + "</li>")
	}
	pagination := r.pagination(site, page, pageCount)
	body := `<div id="base">` + r.categoryMenu(cat) + `<div class="container">` + "\n" +
		`  <div class="left_column"><p style="margin-bottom:10px">` + r.link("カテゴリ:"+cat.Name, categoryPath(cat.Slug), linkOpts{}) + ` -&gt; ` + h(site.Name) + "の記事一覧</p>\n" +
		"    <h1>" + r.link(site.Name, site.URL, linkOpts{external: true}) + " の記事</h1>\n" +
		"    " + pagination + "<ul>" + items.String() + "</ul>" + pagination + "\n" +
		"  </div>\n" +
		`  <div class="right_column">` + r.recentSidebar() + `<h3 class="top-buffer">同一カテゴリのサイト</h3><ul class="list_in_side">` + same.String() + "</ul></div>\n" +
		"</div></div>\n" +
		siteScript
	canonical := ""
	if page == 1 {
		canonical = r.publicPath(site.PagePath(1))
	}
	return r.document(h(site.Name)+"の記事一覧 | ぺろぺろあんてな", body, page, canonical)
}

const siteScript = `<script>
  document.addEventListener("DOMContentLoaded", function() {
    document.querySelectorAll(".tag_post").forEach(function(tagPost) {
      var source = tagPost.querySelector("[data=post-content]").innerHTML;
      var images = new DOMParser().parseFromString(source, "text/html").querySelectorAll("img");
      var target = tagPost.querySelector(".scraped-images");
      images.forEach(function(img) { var a = document.createElement("a"); a.href = img.getAttribute("src"); a.target = "_blank"; var copy = document.createElement("img"); copy.src = img.getAttribute("src"); a.appendChild(copy); target.appendChild(a); });
    });
  });
</script>
`

func (r *renderer) categoryMenu(current *catalog.Category) string {
	var links strings.Builder
	for _, cat := range r.in.Catalog.Categories {
		active := ""
		if cat.Slug == current.Slug {
			active = "active"
		}
		links.WriteString("<li>" + r.link(cat.Name, categoryPath(cat.Slug), linkOpts{class: active}) + "</li>")
	}
	return `<ul id="menu">` + links.String() + "</ul>"
}

func (r *renderer) recentSidebar() string {
	var items strings.Builder
	for _, p := range first(byCreatedDesc(r.in.Posts), 15) {
		title := strings.ReplaceAll(p.TitleString(), "ｗｗｗｗ", "")
		items.WriteString("<li>" + r.link(title, p.URLString(), linkOpts{external: true}) + " (" + h(r.in.postSite[p].Category.Name) + ")</li>")
	}
	return `<h3 class="top-buffer">全カテゴリの新着記事</h3><ul class="list_in_side little-font">` + items.String() + "</ul>"
}

func (r *renderer) pagination(site *catalog.Site, page, pageCount int) string {
	if pageCount <= 1 {
		return ""
	}
	links := make([]string, 0, pageCount)
	for n := 1; n <= pageCount; n++ {
		if n == page {
			links = append(links, fmt.Sprintf(`<span aria-current="page">%d</span>`, n))
		} else {
			links = append(links, fmt.Sprintf(`<a href="%s">%d</a>`, h(r.publicPath(site.PagePath(n))), n))
		}
	}
	return `<nav class="pagination" aria-label="ページ">` + strings.Join(links, " ") + "</nav>"
}

func (r *renderer) document(title, body string, page int, canonical string) string {
	// Preview copies must not compete with the live pages in search results.
	robots := ""
	if r.in.Prefix != "" || page >= 2 {
		robots = `<meta content="noindex,follow" name="robots">`
	}
	if canonical != "" {
		canonical = `<link href="` + h(canonical) + `" rel="canonical">`
	}
	return `<!DOCTYPE html><html><head><title>` + title + `</title><meta name="viewport" content="width=device-width,initial-scale=1"><meta charset="utf-8">` + robots + canonical + style + "</head>\n" +
		`<body><div class="navbar"><div id="nav"><div id="nav_left">` + r.link("ぺろぺろあんてな", "/index.html", linkOpts{class: "site_name"}) + `</div></div><br class="clearfix"></div>` + body + "</body></html>\n"
}

type linkOpts struct {
	external, tracking bool
	class              string
}

func (r *renderer) link(label, href string, o linkOpts) string {
	path := href
	if !o.external {
		path = r.publicPath(href)
	}
	attrs := []string{`href="` + h(path) + `"`}
	if o.external {
		attrs = append(attrs, `target="_blank"`)
	}
	if o.tracking {
		attrs = append(attrs, `data-ob="true"`)
	}
	if o.class != "" {
		attrs = append(attrs, `class="`+h(o.class)+`"`)
	}
	return "<a " + strings.Join(attrs, " ") + ">" + h(label) + "</a>"
}

func (r *renderer) publicPath(path string) string {
	base := ""
	if r.in.Prefix != "" {
		base = "/" + r.in.Prefix
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return base + path
}

// Unprefixed: link() adds the preview prefix.
func categoryPath(slug string) string {
	if slug == "" {
		return "/index.html"
	}
	return "/" + slug
}

var escaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#39;")

// h is Ruby's CGI.escapeHTML.
func h(s string) string { return escaper.Replace(s) }

// truncate counts characters like Ruby's String#length.
func truncate(s string, length int) string {
	r := []rune(s)
	if len(r) <= length {
		return s
	}
	return string(r[:length-1]) + "…"
}
