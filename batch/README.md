# batch（Go の実装）

https://prpr-antena.com/ を書くバッチの Go の実装（2026-11-28 から本番。Cloud Run Job `antena-fetch-posts-next`）。前の Ruby の実装（`migration-next/`）と同じ環境変数で動き、同じ S3 のファイル（記事状態・ロック）を読み書きし、同じページを書く。ファイルの形式とロックの取り方は `migration-next/state-format.md`、運用の手順は `migration-next/flow.md`。

```text
cmd/antena-batch/   起動の入口（1回の実行）
internal/catalog    config/sites.yml
internal/state      posts.json
internal/feed       RSS / RDF → 記事の候補（Feedjira・SAXMachine と同じ読み方）
internal/content    本文の掃除と、ページに出す <img> の取り出し（libxml2 と同じ書き出し）
internal/render     ページ（Ruby の HtmlRenderer をバイト単位で移したもの）
internal/store      S3 のキーとロック
internal/runner     実行の流れ
internal/s3fake     テスト用の S3（本物の SDK のクライアントからつなぐ）
```

## テスト

```sh
cd batch && go test ./...
```

Ruby の実装と同じ出力になることを、次の答えと比べて確かめている。答えはどれも Ruby の実装が作ったもの。

| テスト | 答え | 作り直し方 |
| --- | --- | --- |
| `render` の `TestGoldenPages` | `spec/fixtures/golden/pages/`（固定の入力のページ） | `spec/fixtures/golden/README.md` |
| `content` の `TestImageTagsMatchRuby` | `spec/fixtures/golden/images/cases.json`（作った本文と取り出した `<img>`） | `bundle exec ruby spec/fixtures/golden/images/build.rb` |
| `feed` の `TestSyntheticFeedsMatchRuby` | `spec/fixtures/golden/feeds/cases.json`（作ったフィードと記事） | `bundle exec ruby spec/fixtures/golden/feeds/build.rb` |
| `feed` の `TestRecordedFeedsMatchRuby` | 手元の `tmp/feed-record-20261127/`（本物の RSS。第三者の本文なので repo に入れない）。無ければ skip | `spec/fixtures/golden/README.md`「記録した RSS」 |
| `render` の `TestLiveStateMatchesRuby` | `ANTENA_LIVE_DIR` に置いた本物の記事状態と、それを Ruby が描いたページ。指定しなければ skip | 下の「本物の記事状態で比べる」 |

`render` の `TestStyleMatchesRubyTemplate` は、埋め込んだスタイル（`internal/render/style.html`）が Ruby の `migration-next/templates/style.html` と同じことを確かめる。Ruby の実装を消すまでは両方を直す。

### 本物の記事状態で比べる

本番の記事状態は第三者の本文を含むので、手元の `tmp/` にだけ置く。

```sh
D=tmp/go-live-$(date +%Y%m%d); mkdir -p $D
aws s3 cp s3://prpr-antena/state/posts.json $D/posts.json --region ap-northeast-1
cp config/sites.yml $D/sites.yml; echo 2026-11-28T10:30:00+09:00 > $D/now.txt
# Ruby で描く（ruby-pages/ と ruby-pages-preview/）。描くスクリプトは HtmlRenderer を呼ぶだけ
for p in "" migration-preview; do
  bundle exec ruby -e 'require "./migration-next/lib/migration_next"; require "fileutils"
    s, j, now, pre, out = ARGV; c = MigrationNext::Catalog.load(File.read(s)); st = MigrationNext::State.load(File.read(j))
    c.attach_posts(st.posts); FileUtils.mkdir_p(out)
    MigrationNext::HtmlRenderer.new(catalog: c, posts: st.posts, prefix: pre, now: Time.iso8601(now)).pages.each { |k, h| File.write(File.join(out, k.tr("/", "_")), h) }' \
    $D/sites.yml $D/posts.json "$(cat $D/now.txt)" "$p" $D/ruby-pages${p:+-preview}
done
(cd batch && ANTENA_LIVE_DIR=$PWD/../$D go test ./internal/render/ -run Live -count=1 -v)
```

## Ruby の実装と結果が違う場合

`migration-next/state-format.md` の「Go の実装との違い」。
