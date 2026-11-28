# golden pages

固定の入力から生成した全ページの、期待する出力。今の Ruby 実装（`migration-next/lib/migration_next.rb` の `HtmlRenderer`）の出力をそのまま正解として固めたもので、書き換え（Go など）の実装はこの入力から同じ出力を出せば合格とする。

## 入力

| ファイル | 中身 |
| --- | --- |
| `input/sites.yml` | サイト定義（`config/sites.yml` と同じ形。本物とは別に固定してあり、本物を編集しても golden は変わらない） |
| `input/posts.json` | 記事状態（`state/posts.json` と同じ形、`schema_version` 1）。`build_input.rb` が作る架空の記事で、本物のフィードの中身は入っていない |

- 生成の時刻 `now` は `2026-11-27T12:00:00+09:00`（「直近2日」の判定に使う）
- prefix は空（公開キーのリンク。`noindex` は付かない）

`input/posts.json` は出力が分かれる条件を一通り含む: 直近2日が150件を超える / ちょうど2日前と2日と1秒前 / 未来の記事 / JST の日付の変わり目 / 50件を超えるサイト（ページ送り） / エスケープが要る題と URL / `ｗｗｗｗ` / 40字を超える題 / 題が無い記事 / 1px の画像を含む本文 / 無効のサイトの記事 / 記事の無いサイト / 名前の無いサイト / id の順と時刻の順が食い違う記事。

## 出力

`pages/manifest.json` がページのキー（S3 のキー。`index.html`・`vip`・`sites/1`・`sites/1/2` など）と `pages/` の下のファイル名の対応を持つ。キー `sites/1` と `sites/1/2` はファイルとディレクトリで重なるので、ファイル名では `/` を `_` に置き換えている。

比べ方はバイト単位の一致（`spec/migration_next/golden_pages_spec.rb`）。キーの集合も manifest と一致すること。

今の実装の振る舞いで、書き換えのときに見落としやすいもの:

- 直近2日の一覧は時刻ではなく記事の id の降順に並び、上位150件で切る
- サイドバーの新着15件は全記事を時刻の降順に並べたもので、未来の記事と無効のサイトの記事も入る
- 記事の無いサイトはカードは出るが、サイトのページ（`sites/<id>`）は作られない
- エスケープは Ruby の `CGI.escapeHTML`（`'` は `&#39;`）

## 更新のしかた

出力を意図して変えたときだけ更新する。

```sh
ruby spec/fixtures/golden/build_input.rb      # 入力を作り直すとき
UPDATE_GOLDEN=1 bundle exec rspec spec/migration_next/golden_pages_spec.rb
git diff spec/fixtures/golden/pages            # 変わったページが意図どおりか読む
```

## 記録した RSS（フィード → 記事）

HTML の golden とは別に、本物の RSS から作る記事の候補（`MigrationNext::FeedEntries.parse` の出力）も固めてある。第三者の記事本文を含むので public の repo には入れず、記録した Mac の `tmp/feed-record-20261127/` だけに置いている（ユーザー判断 2026-11-27）。

- サイトごとに `<id>.xml`（runner と同じ `Net::HTTP.get_response` で受けた本文そのもの）、`<id>.meta.json`（URL・status・記録した時刻）、`<id>.expected.json`（期待する記事の候補）
- `spec/migration_next/feed_entries_golden_spec.rb` が比べる。未来の記事を外す判定の `now` は記録した時刻。ファイルが無いと pending になる（緑にはならない）。置き場は `ANTENA_FEED_FIXTURES` で変えられる
- 記録し直すスクリプトは同じ場所の `record.rb`（`bundle exec ruby tmp/feed-record-20261127/record.rb <出力先>`）。記録し直したら `UPDATE_GOLDEN=1` で期待値を作り直す

## 画像タグとフィード（Go の実装と比べる答え）

`images/cases.json` と `feeds/cases.json` は、作った入力を Ruby の実装に通した出力。Go の実装（`batch/`）のテストが、同じ入力から同じ出力が出ることを確かめる（`batch/README.md`）。入力はどれも架空のもので、本物の本文は入っていない。

| ファイル | 入力 | 出力 |
| --- | --- | --- |
| `images/cases.json` | 記事の本文（HTML） | `PostRecord#only_image_tags_by_content` が取り出した `<img>`（libxml2 の書き出し方） |
| `feeds/cases.json` | RSS / RDF の本文（base64） | `FeedEntries.parse` の記事の題・URL・要約・公開日時と、本文から取り出した `<img>` |

作り直すときは `bundle exec ruby spec/fixtures/golden/images/build.rb` と `bundle exec ruby spec/fixtures/golden/feeds/build.rb`。入力を足したら Go のテストも回す。CI（release.yml の `test_go`）も毎回この2つを Linux で作り直し、commit したものと違えば落ちる（gem の更新や、Mac と Linux の libxml2・iconv の違いで出力が変わったとき）。
