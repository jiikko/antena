// Package content cleans feed bodies before they are stored and extracts the
// image tags the site pages show from them.
package content

import (
	"regexp"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Same steps and order as FeedEntries.cleaned_content in the Ruby
// implementation. Ruby's /m (dot matches newline) is (?s) here.
var cleanups = []*regexp.Regexp{
	regexp.MustCompile(`(?s)<script[^>]*>.*</script>`),
	regexp.MustCompile(`(?s)<img src="[^"]*" width=.1. height=.1[^>]*>`),
	regexp.MustCompile(`(?s)<!--[^-]*-->`),
	regexp.MustCompile(`(?s)<a href=.http://2ch-c.net/[^>]*>[^<]*</a>`),
	regexp.MustCompile(`<br( /)?>`),
	regexp.MustCompile(`(?s)<a [^>]*>続きを読む</a>`),
	regexp.MustCompile(`&nbsp;`),
	regexp.MustCompile(`&gt;`),
}

// Clean returns the HTML stored as a post's content. content wins over
// summary when both are given (nil means absent, like Ruby's nil).
//
// The former Ruby implementation parsed the result with Nokogiri and stored
// its to_html; this keeps the cleaned markup as written and only cuts out the
// removed elements, so the bytes differ. Readers use only the <img> tags of
// content (docs/state-format.md), which ImageTags reads the same from both. Building a tree here (x/net/html.Parse) was not used: HTML5 tree
// construction moves images (foster parenting out of tables, <image> becomes
// <img>) where libxml2's HTML parser does not.
func Clean(contentHTML, summary *string) string {
	var text string
	switch {
	case contentHTML != nil:
		text = *contentHTML
		for _, re := range cleanups {
			text = re.ReplaceAllLiteralString(text, "")
		}
	case summary != nil:
		text = *summary
	}
	return removeElements(text)
}

// removeElements cuts out [id=related-title], .automatic-related,
// [id=article_mid] and .kijinai, from the start tag to the matching end tag
// (to the end of the input when it is not closed). libxml2 also closes some
// elements implicitly (a <p> at the next block), where it removes less.
func removeElements(doc string) string {
	z := html.NewTokenizer(strings.NewReader(doc))
	var out strings.Builder
	inRaw := false
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			out.Write(z.Raw()) // an unterminated tail is kept as written
			return out.String()
		}
		raw := string(z.Raw())
		if tt == html.TextToken && inRaw {
			// libxml2 parses the children of these elements as markup.
			out.WriteString(removeElements(raw))
			inRaw = false
			continue
		}
		inRaw = false
		if tt != html.StartTagToken && tt != html.SelfClosingTagToken {
			out.WriteString(raw)
			continue
		}
		name, hasAttr := z.TagName()
		tag := string(name)
		a := atom.Lookup(name)
		if !matchesRemoved(z, hasAttr) {
			out.WriteString(raw)
			inRaw = (tt == html.StartTagToken && markupInRawText[a]) || (tt == html.SelfClosingTagToken && rawTextTags[a])
			continue
		}
		if tt == html.SelfClosingTagToken || voidElements[tag] {
			// The tokenizer still reads what follows a self-closing raw text
			// tag as its text; libxml2 reads it as markup after an empty element.
			inRaw = tt == html.SelfClosingTagToken && rawTextTags[a]
			continue
		}
		for depth := 1; depth > 0; {
			switch z.Next() {
			case html.ErrorToken:
				return out.String()
			case html.StartTagToken:
				if n, _ := z.TagName(); string(n) == tag {
					depth++
				}
			case html.EndTagToken:
				if n, _ := z.TagName(); string(n) == tag {
					depth--
				}
			}
		}
	}
}

// markupInRawText are the elements the tokenizer returns as raw text but
// libxml2 parses as markup. Script and style stay raw in both.
var markupInRawText = map[atom.Atom]bool{
	atom.Noscript: true, atom.Iframe: true, atom.Noembed: true, atom.Noframes: true,
	atom.Title: true, atom.Textarea: true, atom.Xmp: true, atom.Plaintext: true,
}

// rawTextTags are all the elements whose content the tokenizer returns as
// text, including after a self-closing tag (<iframe/>, <script/>).
var rawTextTags = map[atom.Atom]bool{
	atom.Script: true, atom.Style: true, atom.Noscript: true, atom.Iframe: true, atom.Noembed: true,
	atom.Noframes: true, atom.Title: true, atom.Textarea: true, atom.Xmp: true, atom.Plaintext: true,
}

func isSelfClosing(z *html.Tokenizer) bool {
	raw := strings.TrimRight(string(z.Raw()), " \t\n\r\f")
	return strings.HasSuffix(raw, "/>")
}

func isXMLSpace(r rune) bool { return r == ' ' || r == '\t' || r == '\n' || r == '\r' }

var voidElements = map[string]bool{
	"area": true, "base": true, "br": true, "col": true, "embed": true, "hr": true, "img": true, "input": true,
	"link": true, "meta": true, "param": true, "source": true, "track": true, "wbr": true,
}

func matchesRemoved(z *html.Tokenizer, hasAttr bool) bool {
	matched := false
	seen := map[string]bool{}
	for hasAttr {
		var k, v []byte
		k, v, hasAttr = z.TagAttr()
		key := string(k)
		if seen[key] {
			continue // the first of duplicated attributes counts
		}
		seen[key] = true
		switch key {
		case "id":
			if string(v) == "related-title" || string(v) == "article_mid" {
				matched = true
			}
		case "class":
			// Nokogiri's ".kijinai" splits on XML whitespace only (normalize-space).
			for c := range strings.FieldsFuncSeq(string(v), isXMLSpace) {
				if c == "automatic-related" || c == "kijinai" {
					matched = true
				}
			}
		}
	}
	return matched
}

// ImageTags is PostRecord#only_image_tags_by_content: every <img> in document
// order, serialized like libxml2 2.12's HTML serializer, except tags whose
// serialization contains width="1". testdata/images.json holds the Ruby
// output this must equal.
func ImageTags(doc string) string {
	var out strings.Builder
	collectImages(doc, &out)
	return out.String()
}

func collectImages(doc string, out *strings.Builder) {
	z := html.NewTokenizer(strings.NewReader(doc))
	var raw atom.Atom
	for {
		switch z.Next() {
		case html.ErrorToken:
			// A tag cut off at the end of the input: libxml2 still reads it.
			if rest := string(z.Raw()); len(rest) > 4 && strings.EqualFold(rest[:4], "<img") && !strings.Contains(rest, ">") {
				collectImages(rest+">", out)
			}
			return
		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := z.TagName()
			a := atom.Lookup(name)
			raw = 0
			switch {
			case a == atom.Img:
				if tag := serializeImg(z, hasAttr); !strings.Contains(tag, `width="1"`) {
					out.WriteString(tag)
				}
			case markupInRawText[a]:
				raw = a
			case rawTextTags[a] && isSelfClosing(z):
				// <script/>: libxml2 reads what follows as markup.
				raw = a
			}
		case html.TextToken:
			if raw != 0 {
				// Raw, not Text: Text unescapes <textarea> and <title>, which
				// would turn "&lt;img&gt;" into a tag libxml2 never sees.
				collectImages(string(z.Raw()), out)
				raw = 0
			}
		default:
			raw = 0
		}
	}
}

func serializeImg(z *html.Tokenizer, hasAttr bool) string {
	withValue := attrHasValue(z.Raw())
	var b strings.Builder
	b.WriteString("<img")
	seen := map[string]bool{}
	for i := 0; hasAttr; i++ {
		var key, val []byte
		key, val, hasAttr = z.TagAttr()
		k := string(key)
		if seen[k] {
			continue // libxml2 keeps the first of duplicated attributes
		}
		seen[k] = true
		b.WriteString(" " + k)
		if i < len(withValue) && !withValue[i] {
			continue
		}
		v := encodeEntities(string(val))
		if k == "src" || k == "href" || k == "action" {
			v = uriEscape(strings.TrimLeft(v, " \t\n\r"))
		}
		b.WriteString("=" + quoted(v))
	}
	b.WriteString(">")
	return b.String()
}

// attrHasValue scans a raw start tag the way the HTML tokenizer splits
// attributes and reports, per attribute in order, whether it had "=". The
// tokenizer gives "" for both <img ismap> and <img ismap="">; libxml2 writes
// the first as a bare name.
func attrHasValue(raw []byte) []bool {
	isSpace := func(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' }
	i := 1 // after '<'
	for i < len(raw) && !isSpace(raw[i]) && raw[i] != '/' && raw[i] != '>' {
		i++ // tag name
	}
	var out []bool
	for i < len(raw) {
		for i < len(raw) && (isSpace(raw[i]) || raw[i] == '/') {
			i++
		}
		if i >= len(raw) || raw[i] == '>' {
			break
		}
		// Attribute name: a leading '=' belongs to the name.
		i++
		for i < len(raw) && !isSpace(raw[i]) && raw[i] != '/' && raw[i] != '>' && raw[i] != '=' {
			i++
		}
		for i < len(raw) && isSpace(raw[i]) {
			i++
		}
		if i >= len(raw) || raw[i] != '=' {
			out = append(out, false)
			continue
		}
		out = append(out, true)
		i++
		for i < len(raw) && isSpace(raw[i]) {
			i++
		}
		if i < len(raw) && (raw[i] == '"' || raw[i] == '\'') {
			q := raw[i]
			i++
			for i < len(raw) && raw[i] != q {
				i++
			}
			i++
		} else {
			for i < len(raw) && !isSpace(raw[i]) && raw[i] != '>' {
				i++
			}
		}
	}
	return out
}

// encodeEntities is libxml2's attribute text encoding for HTML documents.
var entityEncoder = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\r", "&#13;")

func encodeEntities(s string) string { return entityEncoder.Replace(s) }

// uriEscape is xmlURIEscapeStr(value, "\"#$%&+,/:;<=>?@[\\]^`{|}").
func uriEscape(s string) string {
	const keep = "-_.!~*'()" + "\"#$%&+,/:;<=>?@[\\]^`{|}"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || strings.IndexByte(keep, c) >= 0 {
			b.WriteByte(c)
			continue
		}
		const hex = "0123456789ABCDEF"
		b.WriteByte('%')
		b.WriteByte(hex[c>>4])
		b.WriteByte(hex[c&15])
	}
	return b.String()
}

// quoted is xmlBufWriteQuotedString.
func quoted(s string) string {
	if !strings.Contains(s, `"`) {
		return `"` + s + `"`
	}
	if !strings.Contains(s, "'") {
		return "'" + s + "'"
	}
	return `"` + strings.ReplaceAll(s, `"`, "&quot;") + `"`
}
