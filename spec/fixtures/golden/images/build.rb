# frozen_string_literal: true

# Writes cases.json: synthetic post content and the image tags the Ruby
# implementation (PostRecord#only_image_tags_by_content, Nokogiri/libxml2)
# extracts from it. The Go port must produce the same bytes
# (batch/internal/content). Run: bundle exec ruby spec/fixtures/golden/images/build.rb
require "json"
require_relative "../../../../migration-next/lib/migration_next"

inputs = [
  %(<p>text</p>),
  %(<img src="https://example.com/a.jpg">),
  %(<IMG SRC="https://example.com/A.JPG" ALT="Up">),
  %(<img src="https://example.com/a.jpg" width="1" height="1">),
  %(<img src="https://example.com/a.jpg" width="100" height="1">),
  %(<img src="https://example.com/a.jpg" width='1'>),
  %(<img src="https://example.com/a.jpg" width=1>),
  %(<img src="https://example.com/a.jpg" border="0"><img src="https://example.com/b.jpg" alt="">),
  %(<div><p><a href="x"><img src="https://example.com/nested.jpg" class="pict" /></a></p></div>),
  %(<img src="https://example.com/q.jpg?a=1&b=2">),
  %(<img src="https://example.com/q.jpg?a=1&amp;b=2">),
  %(<img src="https://example.com/日本語.jpg" alt="日本語 &amp; <>">),
  %(<img src="https://example.com/a b.jpg">),
  %(<img src="https://example.com/%E3%81%82.jpg">),
  %(<img src="https://example.com/{x}|^`[].jpg">),
  %(<img src='https://example.com/single.jpg' alt='it"s'>),
  %(<img src=https://example.com/unquoted.jpg alt=bare>),
  %(<img alt="a&quot;b" title="x'y" src="https://example.com/a.jpg">),
  %(<img src="https://example.com/a.jpg" data-src="https://example.com/日本.jpg" srcset="https://example.com/a.jpg 1x, https://example.com/b b.jpg 2x">),
  %(<img src="https://example.com/a.jpg" ismap>),
  %(<img src="https://example.com/a.jpg" alt="line\nbreak\ttab">),
  %(<img src="  https://example.com/spaces.jpg  ">),
  %(<img src="https://example.com/a.jpg" alt="&#x3042;&#12354;&hearts;&nbsp;">),
  %(<img src="javascript:alert(1)" onerror="alert('x')">),
  %(<img src="https://example.com/a.jpg" src="https://example.com/dup.jpg">),
  %(<img>),
  %(<img src="">),
  %(<!-- <img src="https://example.com/comment.jpg"> --><img src="https://example.com/after.jpg">),
  %(<script>var s = '<img src="https://example.com/script.jpg">';</script><img src="https://example.com/real.jpg">),
  %(<noscript><img src="https://example.com/noscript.jpg"></noscript>),
  %(<table><tr><td><img src="https://example.com/table.jpg"></td></tr></table>),
  %(<p>unclosed <b><img src="https://example.com/unclosed.jpg"),
  %(<img src="https://example.com/a.jpg" alt="<b>bold</b>">),
  %(<img src="https://example.com/a.jpg" alt="a > b">),
  %(<img src="https://example.com/a.jpg" href="https://example.com/ü ü">),
  %(<img src="https://example.com/a.jpg" style="width:1px" alt="&lt;">),
  %(<img src="data:image/gif;base64,R0lGODlhAQABAAAAACw=" width="1">),
  %(<img src="https://example.com/emoji😀.jpg" alt="😀">),
  %(<img src="https://example.com/a.jpg" alt="&amp;amp;">),
  %(<img src="https://example.com/a.jpg"/><br><img src="https://example.com/b.jpg" / >),
]
cases = inputs.map do |html|
  post = MigrationNext::PostRecord.new("id" => 1, "site_id" => 1, "content" => html, "created_at" => "2026-11-27T00:00:00Z")
  { "content" => html, "images" => post.only_image_tags_by_content }
end
File.write(File.join(__dir__, "cases.json"), JSON.pretty_generate(cases) + "\n")
puts "#{cases.size} cases"
