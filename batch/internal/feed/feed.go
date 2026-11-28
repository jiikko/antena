// Package feed turns an RSS / RDF body into post candidates the way the Ruby
// implementation does with Feedjira 3.2 (FeedEntries.parse). Only the RSS
// family is read (Feedjira's RSS, ITunesRSS and RSSFeedBurner parsers, which
// share RSSEntryUtilities); Atom and JSON feeds return ErrUnsupported and the
// runner skips the site.
//
// The element rules follow SAXMachine: per item, the first occurrence of each
// configured element wins; text of unconfigured nested elements is appended
// to the enclosing configured element; no whitespace is stripped.
package feed

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/html/charset"
	"golang.org/x/text/encoding/charmap"

	"github.com/jiikko/antena/batch/internal/content"
)

var ErrUnsupported = errors.New("unsupported feed format")

// Entry is one post candidate, in feed order.
type Entry struct {
	Title       *string
	URL         string
	Summary     *string
	Content     string // cleaned HTML document
	PublishedAt *time.Time
}

// Parse returns the entries of body that have a URL and are not published
// after now.
func Parse(body []byte, now time.Time) ([]Entry, error) {
	kind, err := detect(body)
	if err != nil {
		return nil, err
	}
	items := parseItems(body)
	var out []Entry
	for _, it := range items {
		url := it.url(kind == feedBurner)
		if it.published == nil {
			it.published = it.updated // FeedEntryUtilities#published: @published ||= @updated
		}
		if it.published != nil && now.Before(*it.published) {
			continue
		}
		if url == "" {
			continue
		}
		out = append(out, Entry{
			Title: it.title, URL: url, Summary: it.summary,
			Content: content.Clean(it.content, it.summary), PublishedAt: it.published,
		})
	}
	return out, nil
}

type kind int

const (
	plainRSS kind = iota
	feedBurner
)

var (
	atomFeed   = regexp.MustCompile(`<feed[^>]+xmlns\s?=\s?["'](http://www\.w3\.org/2005/Atom|http://purl\.org/atom/ns#)["'][^>]*>`)
	docsAtom   = regexp.MustCompile(`<id>https?://docs\.google\.com/.*</id>`)
	rssOrRDF   = regexp.MustCompile(`<rss|<rdf`)
	jsonFeedRe = regexp.MustCompile(`https://jsonfeed.org/version/`)
	itunesRSS  = regexp.MustCompile(`(?i)xmlns:itunes\s?=\s?["']http://www\.itunes\.com/dtds/podcast-1\.0\.dtd["']`)
)

// detect follows Feedjira.parser_for_xml: the first parser (in Feedjira's
// order) whose able_to_parse? matches the first 2000 bytes.
func detect(body []byte) (kind, error) {
	head := string(body[:min(len(body), 2000)])
	switch {
	case rssOrRDF.MatchString(head) && strings.Contains(head, "feedburner"):
		return feedBurner, nil // RSSFeedBurner
	case docsAtom.MatchString(head):
		return 0, fmt.Errorf("%w (GoogleDocsAtom)", ErrUnsupported)
	case strings.Contains(head, `xmlns:yt="http://www.youtube.com/xml/schemas/2015"`):
		return 0, fmt.Errorf("%w (AtomYoutube)", ErrUnsupported)
	case strings.Contains(head, "<feed") && strings.Contains(head, "Atom") && strings.Contains(head, "feedburner") && !rssOrRDF.MatchString(head):
		return 0, fmt.Errorf("%w (AtomFeedBurner)", ErrUnsupported)
	case atomFeed.MatchString(head):
		return 0, fmt.Errorf("%w (Atom)", ErrUnsupported)
	case itunesRSS.MatchString(head):
		return plainRSS, nil // ITunesRSS: the same item fields as RSS
	case rssOrRDF.MatchString(head):
		return plainRSS, nil // RSS
	case jsonFeedRe.MatchString(head):
		return 0, fmt.Errorf("%w (JSONFeed)", ErrUnsupported)
	}
	return 0, errors.New("No valid parser for XML.")
}

type item struct {
	title, summary, content *string
	link                    *string
	origLink                *string
	guid                    *string
	guidPermaLink           bool
	published               *time.Time
	updated                 *time.Time
	parsed                  map[string]bool // configs already read (first wins)
}

func (it *item) url(feedBurner bool) string {
	if feedBurner && it.origLink != nil {
		return *it.origLink
	}
	// link and a10:link share the setter; the later one in the item wins.
	if it.link != nil {
		return *it.link
	}
	if it.guid != nil && it.guidPermaLink {
		return *it.guid
	}
	return ""
}

// Elements read from an item (RSSEntryUtilities). Names are as written in
// the document, prefix included.
var textElements = map[string]bool{
	"title": true, "content:encoded": true, "a10:content": true, "description": true,
	"link": true, "guid": true, "feedburner:origLink": true,
	"pubDate": true, "pubdate": true, "issued": true, "dc:date": true, "dc:Date": true, "dcterms:created": true,
	// Read but not used: they still capture the text of their children.
	"author": true, "dc:creator": true, "a10:name": true, "dcterms:modified": true, "a10:updated": true,
	"dc:identifier": true, "comments": true,
}

type frame struct {
	name string
	buf  *bytes.Buffer // nil: no text seen (SAXMachine's NO_BUFFER)
	attr []xml.Attr
}

// parseItems reads items the way Nokogiri's SAX parser feeds SAXMachine:
// libxml2 runs without recovery, so the first well-formedness error (an
// undefined entity such as &nbsp; without a DOCTYPE, a bare "&", a mismatched
// end tag, a duplicated attribute, text or a second element outside the root,
// invalid UTF-8) ends the document. Items finished before it are kept; the
// open item and the rest are lost, without an error. A body that cannot be
// converted from its declared encoding gives no items (libxml2 converts it in
// large chunks and loses what it had read). testdata/feeds.json holds Ruby's
// output for each of these.
func parseItems(body []byte) []*item {
	// Feedjira lstrips the body before parsing.
	body = bytes.TrimLeft(body, " \t\r\n\f\v")
	// libxml2 skips a UTF-8 byte order mark; encoding/xml would read it as text.
	body = bytes.TrimPrefix(body, []byte("\xEF\xBB\xBF"))
	// libxml2 reads an XML 1.1 declaration as 1.0 (with a warning); encoding/xml rejects it.
	body = xml11.ReplaceAll(body, []byte("${1}1.0${2}"))
	d := xml.NewDecoder(bytes.NewReader(body))
	d.Strict = true
	// Only the XML declaration picks the encoding, as in libxml2.
	d.CharsetReader = charsetReader

	var items []*item
	var cur *item
	var open []string  // every open element, to check end tags
	var stack []*frame // configured elements open inside the current item
	rootDone := false
	for first := true; ; first = false {
		tok, err := d.RawToken()
		if err != nil {
			if errors.Is(err, errDecode) {
				return nil
			}
			return items // io.EOF or the first error
		}
		switch t := tok.(type) {
		case xml.ProcInst:
			if t.Target == "xml" && !first {
				return items // an XML declaration not at the start
			}
		case xml.Directive:
			// With an external DTD (DOCTYPE ... PUBLIC/SYSTEM), libxml2 only
			// warns about an undeclared entity and drops it.
			if bytes.HasPrefix(t, []byte("DOCTYPE")) && externalID.Match(t) {
				d.Entity = droppedEntities(body)
			}
		case xml.StartElement:
			if len(open) == 0 && rootDone {
				return items // a second root element
			}
			if hasDuplicateAttr(t.Attr) {
				return items
			}
			name := qname(t.Name)
			open = append(open, name)
			switch {
			case cur == nil && name == "item" && len(stack) == 0:
				cur = &item{parsed: map[string]bool{}}
			case cur != nil:
				if name == "a10:link" && !cur.parsed[name] {
					// The url setter is called with the href, nil when there is none
					// (which drops an earlier <link>).
					cur.parsed[name] = true
					cur.link = nil
					for _, a := range t.Attr {
						if qname(a.Name) == "href" {
							v := a.Value
							cur.link = &v
						}
					}
				}
				if textElements[name] {
					stack = append(stack, &frame{name: name, attr: t.Attr})
				}
			}
		case xml.CharData:
			if len(open) == 0 {
				if len(bytes.TrimLeft(t, " \t\r\n")) > 0 {
					return items // text outside the root element
				}
				continue
			}
			if len(stack) > 0 {
				f := stack[len(stack)-1]
				if f.buf == nil {
					f.buf = &bytes.Buffer{}
				}
				f.buf.Write(t)
			}
		case xml.EndElement:
			name := qname(t.Name)
			if len(open) == 0 || open[len(open)-1] != name {
				return items // mismatched end tag
			}
			open = open[:len(open)-1]
			if len(open) == 0 {
				rootDone = true
			}
			if cur == nil {
				continue
			}
			if len(stack) > 0 && stack[len(stack)-1].name == name {
				f := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				cur.set(f)
				continue
			}
			if len(stack) == 0 && name == "item" {
				items = append(items, cur)
				cur = nil
			}
		}
	}
}

var (
	xml11      = regexp.MustCompile(`\A(<\?xml\s+version\s*=\s*["'])1\.1(["'])`)
	externalID = regexp.MustCompile(`\s(PUBLIC|SYSTEM)\s`)
)

// droppedEntities maps every entity name referenced in body (other than the
// five XML ones) to "", as libxml2 drops an undeclared entity when the
// document has an external DTD.
func droppedEntities(body []byte) map[string]string {
	m := map[string]string{}
	for _, ref := range entityRef.FindAllSubmatch(body, -1) {
		switch name := string(ref[1]); name {
		case "amp", "lt", "gt", "quot", "apos":
		default:
			m[name] = ""
		}
	}
	return m
}

var entityRef = regexp.MustCompile(`&([A-Za-z_:][A-Za-z0-9_:.-]*);`)

func hasDuplicateAttr(attrs []xml.Attr) bool {
	seen := map[string]bool{}
	for _, a := range attrs {
		k := qname(a.Name)
		if seen[k] {
			return true
		}
		seen[k] = true
	}
	return false
}

var errDecode = errors.New("invalid byte for the declared encoding")

// Encoding names libxml2 (iconv) knows and the WHATWG index does not.
var charsetAliases = map[string]string{"cp932": "shift_jis", "ms932": "shift_jis", "windows-31j": "shift_jis", "sjis": "shift_jis", "x-sjis": "shift_jis", "eucjp": "euc-jp", "x-euc-jp": "euc-jp"}

func charsetReader(label string, input io.Reader) (io.Reader, error) {
	if alias, ok := charsetAliases[strings.ToLower(label)]; ok {
		label = alias
	}
	var r io.Reader
	switch strings.ToLower(label) {
	case "iso-8859-1", "iso_8859-1", "latin1", "l1":
		// libxml2's own Latin-1; WHATWG maps this label to windows-1252.
		r = charmap.ISO8859_1.NewDecoder().Reader(input)
	case "us-ascii", "ascii":
		r = &asciiOnly{r: input}
	default:
		var err error
		if r, err = charset.NewReaderLabel(label, input); err != nil {
			return nil, err
		}
	}
	return &strictDecode{r: r}, nil
}

// asciiOnly fails on a byte >= 0x80, as libxml2's ASCII decoder does.
type asciiOnly struct{ r io.Reader }

func (a *asciiOnly) Read(p []byte) (int, error) {
	n, err := a.r.Read(p)
	for _, c := range p[:n] {
		if c >= 0x80 {
			return 0, errDecode
		}
	}
	return n, err
}

// strictDecode fails on U+FFFD, which the x/text decoders write for a byte
// they cannot convert; iconv fails there instead. tail keeps the last bytes
// of the previous read so a U+FFFD split across two reads is seen.
type strictDecode struct {
	r    io.Reader
	tail []byte
}

func (s *strictDecode) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	joined := append(append([]byte(nil), s.tail...), p[:n]...)
	if bytes.Contains(joined, []byte("\uFFFD")) {
		return 0, errDecode
	}
	s.tail = joined[max(0, len(joined)-2):]
	return n, err
}

func qname(n xml.Name) string {
	if n.Space == "" {
		return n.Local
	}
	return n.Space + ":" + n.Local
}

func (it *item) set(f *frame) {
	if it.parsed[f.name] {
		return
	}
	it.parsed[f.name] = true
	if f.name == "guid" {
		// GloballyUniqueIdentifier: url unless isPermaLink="false".
		it.guidPermaLink = true
		for _, a := range f.attr {
			if qname(a.Name) == "isPermaLink" && a.Value == "false" {
				it.guidPermaLink = false
			}
		}
		if f.buf != nil {
			v := f.buf.String()
			it.guid = &v
		}
		return
	}
	if f.buf == nil {
		return // an empty element sets nothing
	}
	v := f.buf.String()
	switch f.name {
	case "title":
		it.title = &v
	case "content:encoded", "a10:content":
		it.content = &v
	case "description":
		it.summary = &v
	case "link":
		it.link = &v
	case "feedburner:origLink":
		it.origLink = &v
	case "pubDate", "pubdate", "issued", "dc:date", "dc:Date", "dcterms:created":
		// FeedEntryUtilities#published=: keep the oldest time found.
		if t, ok := ParseDate(v); ok && (it.published == nil || t.Before(*it.published)) {
			it.published = &t
		}
	case "dcterms:modified", "a10:updated":
		// updated=: keep the newest. published falls back to it.
		if t, ok := ParseDate(v); ok && (it.updated == nil || t.After(*it.updated)) {
			it.updated = &t
		}
	}
}
