# frozen_string_literal: true

# Regenerates spec/fixtures/golden/input/posts.json (synthetic posts; no real
# feed content). Run: ruby spec/fixtures/golden/build_input.rb
# The golden pages are rendered at NOW (spec/migration_next/golden_pages_spec.rb).
require "json"
require "time"

NOW = Time.iso8601("2026-11-27T12:00:00+09:00")
posts = []
# ids are assigned at the end in created_at order, as the runner assigns them
# in fetch order; only the post marked id_out_of_order breaks that.
add = lambda do |site_id, created_at, title:, url: nil, content: nil, published_at: nil, id_out_of_order: false|
  posts << {
    "id" => nil, "site_id" => site_id, "title" => title, "id_out_of_order" => id_out_of_order,
    "url" => url || "https://site#{site_id}.example/articles/#{posts.size + 1}",
    "summary" => nil, "content" => content,
    "published_at" => published_at&.utc&.iso8601(6),
    "created_at" => created_at.utc.iso8601(6), "updated_at" => created_at.utc.iso8601(6),
  }
end

# Site 1: 120 posts, one every 30 minutes back from NOW - 10 min. More than 50
# (3 site pages) and, with site 2, more than 150 posts in the last 2 days.
120.times { |i| add.call(1, NOW - 600 - i * 1800, title: "サイトAの記事#{i + 1}") }
# Site 2: 60 posts every 40 minutes.
60.times { |i| add.call(2, NOW - 300 - i * 2400, title: "サイトBの記事#{i + 1}") }

# Edge cases, all on site 2 unless noted.
add.call(2, NOW - 2 * 86_400, title: "ちょうど2日前（一覧に入る）")
add.call(2, NOW - 2 * 86_400 - 1, title: "2日と1秒前（一覧に入らない）")
add.call(2, NOW + 3600, title: "未来の記事（一覧に入らない）")
add.call(2, NOW - 120, title: %(エスケープ <b>"&'</b> &amp; の題), url: "https://b.example/a?x=1&y=<2>")
add.call(2, NOW - 110, title: "草ｗｗｗｗ生えるｗｗｗｗｗ")
add.call(2, NOW - 100, title: "四十字を超える長い題" * 6)
add.call(2, NOW - 90, title: nil)
add.call(2, NOW - 80, title: "画像つき",
         content: %(<p>本文</p><img src="https://img.example/1.jpg"><img src="https://img.example/px.gif" width="1" height="1"><img src="https://img.example/2.png" alt="&quot;x&quot;">))
# id order and time order disagree: this post has a higher id but an older time.
add.call(2, NOW - 3 * 3600, title: "idは新しいが時刻は古い記事", id_out_of_order: true)
# JST midnight boundary: 23:59:59 and 00:00:00 JST on 11/26-27.
add.call(1, Time.iso8601("2026-11-26T23:59:59+09:00"), title: "11/26 23:59:59 JST")
add.call(1, Time.iso8601("2026-11-27T00:00:00+09:00"), title: "11/27 00:00:00 JST")
# Disabled site 3 still has posts in the state.
add.call(3, NOW - 50, title: "無効のサイトの記事")
# Site with an empty name, and the mom category.
add.call(10, NOW - 7200, title: "名前の無いサイトの記事", published_at: NOW - 7300)
add.call(20, NOW - 30 * 3600, title: "奥様の記事")

ordered = posts.reject { _1["id_out_of_order"] }.sort_by { _1["created_at"] } + posts.select { _1["id_out_of_order"] }
ordered.each.with_index(1) { |post, id| post["id"] = id; post.delete("id_out_of_order") }
posts = ordered
path = File.join(__dir__, "input", "posts.json")
File.write(path, JSON.pretty_generate("schema_version" => 1, "posts" => posts) + "\n")
puts "wrote #{posts.size} posts to #{path}"
