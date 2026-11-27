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
