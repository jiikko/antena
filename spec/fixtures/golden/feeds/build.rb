# frozen_string_literal: true

# Writes cases.json: synthetic feeds and the post candidates the Ruby
# implementation (MigrationNext::FeedEntries.parse, Feedjira 3.2) makes from
# them. The Go port (batch/internal/feed) must produce the same title, url,
# summary and published_at, and the same image tags from content.
# Run: bundle exec ruby spec/fixtures/golden/feeds/build.rb
require "json"
require_relative "../../../../migration-next/lib/migration_next"

NOW = Time.iso8601("2026-11-27T12:00:00Z")

def rss(items, ns: "", channel: "")
  %(<?xml version="1.0" encoding="UTF-8"?>\n<rss version="2.0" xmlns:content="http://purl.org/rss/1.0/modules/content/" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:dcterms="http://purl.org/dc/terms/"#{ns}><channel><title>Feed</title><link>https://feed.example/</link><description>d</description>#{channel}#{items}</channel></rss>\n)
end

def rdf(items)
  %(<?xml version="1.0" encoding="UTF-8"?>\n<rdf:RDF xmlns="http://purl.org/rss/1.0/" xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:content="http://purl.org/rss/1.0/modules/content/"><channel rdf:about="https://feed.example/"><title>Feed</title><link>https://feed.example/</link><description>d</description></channel>#{items}</rdf:RDF>\n)
end

cases = {
  "rss basic" => rss(%(<item><title>T1</title><link>https://a.example/1</link><description>S1</description><content:encoded><![CDATA[<p>body <img src="https://a.example/1.jpg"></p>]]></content:encoded><pubDate>Fri, 27 Nov 2026 10:00:00 +0900</pubDate></item>)),
  "rdf basic" => rdf(%(<item rdf:about="https://a.example/2"><title>T2</title><link>https://a.example/2</link><description>S2</description><dc:date>2026-11-27T19:00:44+09:00</dc:date></item>)),
  "first element wins" => rss(%(<item><title>first</title><title>second</title><link>https://a.example/first</link><link>https://a.example/second</link></item>)),
  "whitespace kept" => rss(%(<item><title>\n  spaced title  \n</title><link>\n https://a.example/ws \n</link></item>)),
  "nested text appended" => rss(%(<item><title>a<b>bold</b>c</title><link>https://a.example/n</link><description>x<span>y<i>z</i></span>w</description></item>)),
  "entities" => rss(%(<item><title>A &amp; B &lt;c&gt; &#x3042;&#12354; &quot;q&quot;</title><link>https://a.example/e?a=1&amp;b=2</link></item>)),
  "html entity undefined in xml" => rss(%(<item><title>x&nbsp;y&hellip;</title><link>https://a.example/nbsp</link></item>)),
  "escaped html description" => rss(%(<item><title>t</title><link>https://a.example/esc</link><description>&lt;p&gt;para &lt;img src="https://a.example/e.jpg"&gt;&lt;/p&gt;</description></item>)),
  "empty title" => rss(%(<item><title/><link>https://a.example/empty</link></item><item><title></title><link>https://a.example/empty2</link></item>)),
  "no title" => rss(%(<item><link>https://a.example/notitle</link></item>)),
  "guid permalink" => rss(%(<item><title>g</title><guid>https://a.example/guid</guid></item><item><title>g2</title><guid isPermaLink="false">tag:x</guid></item><item><title>g3</title><guid isPermaLink="true">https://a.example/guid3</guid><link>https://a.example/link3</link></item>)),
  "no url dropped" => rss(%(<item><title>nolink</title><description>d</description></item>)),
  "oldest published" => rss(%(<item><title>p</title><link>https://a.example/p</link><pubDate>Fri, 27 Nov 2026 10:00:00 +0000</pubDate><dc:date>2026-11-27T08:00:00Z</dc:date></item>)),
  "updated fallback" => rss(%(<item><title>u</title><link>https://a.example/u</link><dcterms:modified>2026-11-26T08:00:00Z</dcterms:modified><dcterms:modified>2026-11-26T09:00:00Z</dcterms:modified></item>)),
  "future dropped" => rss(%(<item><title>future</title><link>https://a.example/future</link><pubDate>Fri, 27 Nov 2026 13:00:00 +0000</pubDate></item><item><title>past</title><link>https://a.example/past</link><pubDate>Fri, 27 Nov 2026 11:59:59 +0000</pubDate></item>)),
  "date formats" => rss([
    "Fri, 27 Nov 2026 10:00:00 GMT", "Fri, 27 Nov 2026 10:00:00 JST", "Fri, 27 Nov 2026 10:00:00 EST", "27 Nov 2026 10:00:00 +0900",
    "Fri, 7 Nov 2026 10:00:00 +0900", "2026-11-27T10:00:00+0900", "2026-11-27T10:00+09:00", "2026-11-27T10:00:00.123456789Z",
    "2026-11-27", "2026-11-27 10:00:00", "not a date", "Fri, 27 Nov 2026 10:00 +0900",
  ].each_with_index.map { |d, i| %(<item><title>d#{i}</title><link>https://a.example/d#{i}</link><pubDate>#{d}</pubDate></item>) }.join),
  "content cleaning" => rss(%(<item><title>c</title><link>https://a.example/c</link><content:encoded><![CDATA[<script>x()</script><img src="https://a.example/1px.gif" width="1" height="1"><!-- c --><br><br /><a href="https://a.example/more">続きを読む</a><div id="related-title"><img src="https://a.example/rel.jpg"></div><div class="x automatic-related"><img src="https://a.example/auto.jpg"></div><div id="article_mid"><img src="https://a.example/mid.jpg"></div><p class="kijinai"><img src="https://a.example/k.jpg"></p><img src="https://a.example/keep.jpg" alt="&gt;"> &nbsp;]]></content:encoded></item>)),
  "summary used when no content" => rss(%(<item><title>s</title><link>https://a.example/s</link><description><![CDATA[<img src="https://a.example/s.jpg"><script>y()</script>]]></description></item>)),
  "feedburner origLink" => rss(%(<item><title>fb</title><link>https://feedproxy.example/x</link><feedburner:origLink>https://a.example/orig</feedburner:origLink></item>), ns: %( xmlns:feedburner="http://rssnamespace.org/feedburner/ext/1.0")),
  "itunes" => rss(%(<item><title>it</title><link>https://a.example/it</link><itunes:summary>isum</itunes:summary><description>desc</description></item>), ns: %( xmlns:itunes="http://www.itunes.com/dtds/podcast-1.0.dtd")),
  "a10 link" => rss(%(<item><title>a10</title><a10:link href="https://a.example/a10" /></item>), ns: %( xmlns:a10="http://www.w3.org/2005/Atom")),
  "item outside channel description" => rss(%(<item><title>i1</title><link>https://a.example/i1</link></item>), channel: %(<description>second</description><image><url>https://feed.example/i.png</url><title>img</title><link>https://feed.example/</link></image>)),
  "empty cdata title dedups" => rss(%(<item><title><![CDATA[]]></title><link>https://a.example/c1</link></item>)),
  "noscript and table images" => rss(%(<item><title>n</title><link>https://a.example/ns</link><content:encoded><![CDATA[<noscript><img src="https://a.example/ns.jpg"></noscript><table><img src="https://a.example/t1.jpg"><tr><td><img src="https://a.example/t2.jpg"></td></tr></table><table class="kijinai"><img src="https://a.example/t3.jpg"><tr><td>x</td></tr></table><image src="https://a.example/image.jpg">]]></content:encoded></item>)),
  "bare ampersand stops the feed" => rss(%(<item><title>ok</title><link>https://a.example/ok</link></item><item><title>A & B</title><link>https://a.example/amp</link></item><item><title>after</title><link>https://a.example/after</link></item>)),
  "mismatched tag stops the feed" => rss(%(<item><title>ok</title><link>https://a.example/ok2</link></item><item><title>bad</b></title><link>https://a.example/bad</link></item>)),
  "invalid utf-8 stops the feed" => rss(%(<item><title>ok</title><link>https://a.example/ok3</link></item><item><title>x\xFFy</title><link>https://a.example/bin</link></item>)).b,
  "leading whitespace" => "\n\n  " + rss(%(<item><title>lw</title><link>https://a.example/lw</link></item>)),
  "a10 content and link order" => rss(%(<item><title>o</title><a10:link href="https://a.example/a10first" /><link>https://a.example/linklater</link><a10:content>&lt;img src="https://a.example/a10.jpg"&gt;</a10:content></item>), ns: %( xmlns:a10="http://www.w3.org/2005/Atom")),
  "shift_jis" => %(<?xml version="1.0" encoding="Shift_JIS"?>\n<rss version="2.0"><channel><title>t</title><item><title>日本語のタイトル</title><link>https://a.example/sjis</link><description>説明</description></item></channel></rss>\n).encode("Shift_JIS").b,
  "junk before the declaration" => "Notice: x<br />\n" + rss(%(<item><title>j</title><link>https://a.example/junk</link></item>)),
  "text before the root without a declaration" => %(junk<rss version="2.0"><channel><item><title>tb</title><link>https://a.example/tb</link></item></channel></rss>\n),
  "two root elements" => rss(%(<item><title>r1</title><link>https://a.example/r1</link></item>)) + %(<rss><channel><item><title>r2</title><link>https://a.example/r2</link></item></channel></rss>),
  "cp932 label" => %(<?xml version="1.0" encoding="CP932"?>\n<rss version="2.0"><channel><title>t</title><item><title>日本語</title><link>https://a.example/cp932</link></item></channel></rss>\n).encode("Windows-31J").b,
  "invalid shift_jis byte" => (%(<?xml version="1.0" encoding="Shift_JIS"?>\n<rss version="2.0"><channel><title>t</title><item><title>前</title><link>https://a.example/sj1</link></item><item><title>).encode("Shift_JIS").b + "\x81\x20".b + %(</title><link>https://a.example/sj2</link></item><item><title>後</title><link>https://a.example/sj3</link></item></channel></rss>\n).encode("Shift_JIS").b),
  "doctype with external id and html entity" => %(<?xml version="1.0" encoding="UTF-8"?>\n<!DOCTYPE rss PUBLIC "-//Netscape Communications//DTD RSS 0.91//EN" "http://my.netscape.com/publish/formats/rss-0.91.dtd">\n<rss version="0.91"><channel><title>t</title><item><title>A&nbsp;B&hellip;</title><link>https://a.example/dt</link></item><item><title>C &amp; D</title><link>https://a.example/dt2</link></item></channel></rss>\n),
  "a10 link without href" => rss(%(<item><title>a</title><link>https://a.example/a10none</link><a10:link rel="x"/></item>), ns: %( xmlns:a10="http://www.w3.org/2005/Atom")),
  "xml version 1.1" => rss(%(<item><title>v</title><link>https://a.example/v11</link></item>)).sub('version="1.0"', 'version="1.1"'),
  "duplicate attribute stops the feed" => rss(%(<item><title>ok</title><link>https://a.example/da1</link></item><item><title a="1" a="2">x</title><link>https://a.example/da2</link></item>)),
  "uppercase RSS with itunes" => %(<?xml version="1.0" encoding="UTF-8"?>\n<RSS xmlns:itunes="http://www.itunes.com/dtds/podcast-1.0.dtd"><channel><item><title>up</title><link>https://a.example/up</link></item></channel></RSS>\n),
  "content in raw text tags" => rss(%(<item><title>rt</title><link>https://a.example/rt</link><content:encoded><![CDATA[<noscript><div class="kijinai"><img src="https://a.example/ad.jpg"></div><img src="https://a.example/nsok.jpg"></noscript><textarea>&lt;img src="https://a.example/esc.jpg"&gt;<img src="https://a.example/ta.jpg"></textarea><title><img src="https://a.example/ti.jpg"></title><div class="kijinai\u3000pr"><img src="https://a.example/ideo.jpg"></div><div class="pr	kijinai"><img src="https://a.example/tab.jpg"></div>]]></content:encoded></item>)),
  "utf-8 bom" => "\xEF\xBB\xBF".b + rss(%(<item><title>b</title><link>https://a.example/bom</link></item>)).b,
  "self-closing raw text tags" => rss(%(<item><title>sc</title><link>https://a.example/sc</link><content:encoded><![CDATA[<iframe src="https://y.example/e"/><div class="kijinai"><img src="https://ad.example/a.jpg"></div><p><img src="https://a.example/b.jpg"></p>]]></content:encoded></item><item><title>sc2</title><link>https://a.example/sc2</link><description><![CDATA[<script src="x"/><p><img src="https://a.example/p.jpg"></p>]]></description></item>)),
  "internal dtd entity" => %(<?xml version="1.0" encoding="UTF-8"?>\n<!DOCTYPE rss [<!ENTITY nbsp "&#160;"><!ENTITY site "Site &amp; Co">]>\n<rss version="2.0"><channel><title>t</title><item><title>a&nbsp;b &site;</title><link>https://a.example/idtd</link></item></channel></rss>\n),
  "external dtd with a non-html entity" => %(<?xml version="1.0" encoding="UTF-8"?>\n<!DOCTYPE rss SYSTEM "http://example.com/x.dtd">\n<rss version="2.0"><channel><title>t</title><item><title>a&custom;b</title><link>https://a.example/edtd</link></item></channel></rss>\n),
  "iso-8859-1 high byte" => (%(<?xml version="1.0" encoding="ISO-8859-1"?>\n<rss version="2.0"><channel><title>t</title><item><title>q).b + "\x93\xE9".b + %(</title><link>https://a.example/latin1</link></item></channel></rss>\n).b),
  "us-ascii high byte" => (%(<?xml version="1.0" encoding="US-ASCII"?>\n<rss version="2.0"><channel><title>t</title><item><title>ok</title><link>https://a.example/ascii1</link></item><item><title>x).b + "\xE9".b + %(</title><link>https://a.example/ascii2</link></item></channel></rss>\n).b),
  "multiple items order" => rss((1..5).map { |i| %(<item><title>m#{i}</title><link>https://a.example/m#{i}</link></item>) }.join),
}
out = cases.map do |name, xml|
  entries = MigrationNext::FeedEntries.parse(xml, now: NOW)
  rows = entries&.map do |e|
    post = MigrationNext::PostRecord.new("id" => 1, "site_id" => 1, "content" => e["content"], "created_at" => NOW.iso8601)
    { "title" => e["title"], "url" => e["url"], "summary" => e["summary"],
      "published_at" => e["published_at"]&.utc&.iso8601(6), "images" => post.only_image_tags_by_content }
  end
  { "name" => name, "now" => NOW.iso8601, "xml_base64" => [xml].pack("m0"), "entries" => rows }
end
File.write(File.join(__dir__, "cases.json"), JSON.pretty_generate(out) + "\n")
puts "#{out.size} cases"
