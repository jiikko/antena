# src（Go の実装）

https://prpr-antena.com/ を書くバッチの Go の実装（2026-11-28 から本番。Cloud Run Job `antena-fetch-posts-next`）。ファイルの形式とロックの取り方は `docs/state-format.md`、運用の手順は `docs/flow.md`。

```text
cmd/antena-batch/   起動の入口（1回の実行）
internal/catalog    config/sites.yml
internal/state      posts.json
internal/feed       RSS / RDF → 記事の候補（Feedjira・SAXMachine と同じ読み方）
internal/content    本文の掃除と、ページに出す <img> の取り出し（libxml2 と同じ書き出し）
internal/render     ページ（前の Ruby の実装の HtmlRenderer をバイト単位で移したもの）
internal/store      S3 のキーとロック
internal/runner     実行の流れ
internal/s3fake     テスト用の S3（本物の SDK のクライアントからつなぐ）
```

## テスト

```sh
cd batch && go test ./...
```

Go の実装は、前の Ruby の実装（2026-11-28 まで本番。2026-11-28 に repo から削除）と同じ出力を出すように作った。次の答えはどれも Ruby の実装が作ったもので、今は Go の出力の基準として使う。

| テスト | 答え | 出力を意図して変えたとき |
| --- | --- | --- |
| `render` の `TestGoldenPages` | `internal/render/testdata/`（固定の入力 `input/` と、そこから作るページ `pages/`・`pages-preview/`） | `UPDATE_GOLDEN=1 go test ./internal/render/` で Go の出力に書き直し、`git diff` で変わったページを読む |
| `content` の `TestImageTagsMatchRuby` | `internal/content/testdata/images.json`（作った本文と、取り出す `<img>`） | 手で直す |
| `feed` の `TestSyntheticFeedsMatchRuby` | `internal/feed/testdata/feeds.json`（作ったフィード（base64）と、読み取る記事） | 手で直す |
| `feed` の `TestRecordedFeedsMatchRuby` | 手元の `tmp/feed-record-20261127/`（本物の RSS と、Ruby が読み取った記事。第三者の本文なので repo に入れない）。無ければ skip | 記録し直す手段は無い（記録のスクリプトは Ruby だった） |

固定の入力（`testdata/input/posts.json`）は、ページの出力が分かれる条件を一通り含む: 直近2日が150件を超える / ちょうど2日前と2日と1秒前 / 未来の記事 / JST の日付の変わり目 / 50件を超えるサイト（ページ送り） / エスケープが要る題と URL / `ｗｗｗｗ` / 40字を超える題 / 題が無い記事 / 1px の画像を含む本文 / 無効のサイトの記事 / 記事の無いサイト / 名前の無いサイト / id の順と時刻の順が食い違う記事。生成の時刻は `2026-11-27T12:00:00+09:00`。`pages/manifest.json` がページのキーとファイル名の対応を持つ（キーの `/` はファイル名では `_`）。

見落としやすいページの決まり:

- 直近2日の一覧は時刻ではなく記事の id の降順に並び、上位150件で切る
- サイドバーの新着15件は全記事を時刻の降順に並べたもので、未来の記事と無効のサイトの記事も入る
- 記事の無いサイトはカードは出るが、サイトのページ（`sites/<id>`）は作られない
- エスケープは Ruby の `CGI.escapeHTML` と同じ（`'` は `&#39;`）

## Ruby の実装と結果が違った場合

切り替えの前に分かっていた違い（Atom を読まない、UTF-8 でないフィードの一部の文字など）は `docs/state-format.md` の「前の Ruby の実装との違い」。
