# 記事状態とロックの形式

この文書は、バッチが S3 に置く記事状態（`posts.json`）と実行ロックの形式、それを読み書きするときの決まりを定める。目的は、別の実装（Go への置き換えなど）と今の Ruby の実装（`migration-next/lib/migration_next.rb`）が、同じファイルを交互に読み書きできること。どちらの実装も、ここに書いていない挙動を当てにしない。運用の手順は `flow.md` にある。

## キー

バケットは `prpr-antena`。キーは `S3Store#state_key` と `S3Store#lock_key` が `MIGRATION_NEXT_PREFIX`（ページの prefix）から決める。

| prefix | 記事状態 | ロック | ページ |
| --- | --- | --- | --- |
| 空（本番） | `state/posts.json` | `locks/migration-next-run.lock` | `index.html` など |
| `<p>`（shadow） | `state/<p>/posts.json` | `locks/<p>/migration-next-run.lock` | `<p>/index.html` など |

- prefix の前後の `/` は落とす。指定しないときの prefix は `migration-preview`
- prefix が違えば、記事状態とロックは別のものになる。shadow の実行は本番の記事状態を読まず、書かず、本番とロックを取り合わない
- `state/` と `locks/` は必ずキーの先頭に置く。バケットポリシーの `HideMigrationState` が `state/*` と `locks/*` をサイトから読めなくしているのは、この位置に限られる（`flow.md`「記事状態の置き場」）

## posts.json

UTF-8 の JSON。書くときは次の形にする。

```json
{
  "schema_version": 1,
  "exported_at": "2026-11-27T12:30:41.123456Z",
  "posts": [
    {
      "id": 512,
      "site_id": 7,
      "title": "記事の題",
      "url": "https://example.com/archives/1.html",
      "summary": "フィードの要約（そのまま）",
      "content": "<!DOCTYPE html ...>...</html>",
      "published_at": "2026-11-27T12:10:00.000000Z",
      "created_at": "2026-11-27T12:30:40.654321Z",
      "updated_at": "2026-11-27T12:30:40.654321Z"
    }
  ]
}
```

読むとき:

- 最上位が配列なら、それを `posts` として読む
- `schema_version` は読まない。形を変えるときは、読む側を先に両方の形へ対応させてから書く側を変える
- `id`・`site_id`・`created_at` は必須。どれかが無いか空、`id`・`site_id` が整数として読めない、日時が読めないときは、ファイル全体を読めないものとして扱う
- `title` が無いときは `name` を題として読む（以前の形）
- 日時は ISO 8601 で書く。Ruby の実装は、ISO 8601 で読めない文字列も `Time.parse` で読もうとするが、書く側はこれに頼らない。空文字列と `null` は値なしにする

各項目:

| 項目 | 内容 |
| --- | --- |
| `id` | 整数。ファイルの中で記事ごとに一意。新しい記事に、今ある最大の `id` + 1 から振る（間引いた記事の `id` は、また振られることがある）。ページはサイトごとの一覧と「直近2日」を `id` の降順で並べるので、取り込んだ順に大きくする |
| `site_id` | `config/sites.yml` の `sites[].id`。無効にしたサイト（`enable: false`）も含め、`sites.yml` に在る id でなければならない。無い id の記事が1件でもあると実行は失敗する（サイトを `sites.yml` から消すときは、先にそのサイトの記事を消す） |
| `title` / `url` / `summary` | フィードの項目のまま。`summary` は `null` でよい |
| `content` | フィードの本文（無ければ要約）から広告・関連記事などを取り除き、HTML 文書として書き出したもの。取り除く規則は `FeedEntries.cleaned_content` で、`spec/migration_next/feed_entries_golden_spec.rb` が記録した RSS で期待値を固めている |
| `published_at` | フィードの公開日時（UTC。Feedjira が UTC に直す）。無ければ `null` |
| `created_at` / `updated_at` | 取り込んだ日時（UTC）。ページの時刻と古い記事の間引きは `created_at` で決める |

`posts` の並びに意味は無い。ページはどれも `id` か `created_at` で並べ直す。

## 1回の実行で記事状態にすること

1. ロックを取る。取れなければ何も書かずに終える
2. 記事状態を読む。キーが無いときだけ WARN を出して空から始める。権限エラー・壊れた JSON など、それ以外の失敗では実行を止める（今ある記事を空で上書きしないため）
3. 有効なサイトを `id` の昇順に回り、フィードの順に新しい記事を足す。今ある記事（全サイト）と URL か題が同じものは足さない。公開日時が未来のものと URL の無いものも足さない。1つのサイトの取得に失敗しても、そのサイトを WARN で飛ばして続ける
4. 古い記事を間引く。記事が50件を超えるサイト（無効のサイトも含む）では、`created_at` が5日より前の記事を全部消す。50件以下のサイトでは消さない
5. 記事状態を書く。続けてページを書く（ページの書き込みが失敗しても、次の実行が同じ記事状態から書き直せる順）
6. ロックを消す

## ロック

中身は次の JSON。`token` は実行ごとの UUID、`started_at` は取った日時（UTC）。

```json
{"token": "3f0c…", "started_at": "2026-11-27T12:30:00.000000Z"}
```

取り方:

1. `If-None-Match: *` を付けて PUT する。通れば取れた
2. 409・412 なら今のロックを読む。それ以外の失敗（権限エラー・通信の失敗など）では実行を失敗にする。`token` が自分のものなら取れた（応答が失われた PUT を SDK が送り直した場合）
3. 409 だったとき、またはロックが読む前に消えていたときは、2回まで 1 からやり直す
4. `started_at` から1時間（`S3RunLock::DEFAULT_STALE_AFTER_SECONDS`）を過ぎていれば、止められた実行の残したロックとみなして奪う。奪うときは、読んだときの ETag を `If-Match` に付けて PUT し直す。ETag が読めなかったときは奪わない。`started_at` が読めないもの、中身が JSON のオブジェクトでないものも、過ぎたものとして扱う。奪う PUT が 404・409・412 なら別の実行が先に奪ったので、読み直す
5. 奪えなかったとき、まだ1時間たっていないときは、WARN（`migration-next skipped`）を出して何も書かずに終える

消し方: 読んだロックの `token` が自分のものなら消す。自分のものでなければ（奪われていれば）消さない。

ジョブの task-timeout（`release.yml` の 1800 秒）は、ロックを奪えるまでの1時間より短くしておく。実行中のタスクが Cloud Run に止められてからでないと、別の実行がそのロックを奪えないようにするため。

## 別の実装を本番へ入れるとき

1. 別の実装を、prefix `migration-preview` で動かす。記事状態は `state/migration-preview/posts.json` になり、本番の記事状態に触れない。ページはライフサイクルで1日後に消えるので、片付けが要らない（別の prefix を使うなら、終わった後にページと `state/<prefix>/` を消す）。Ruby の実装を手で動かすときもこの prefix になるので、shadow の間は手で動かさない
2. 本番と同じ記事から始めて比べたいなら、始める前に `state/posts.json` を `state/migration-preview/posts.json` へコピーする。コピーしないと空から始まり、「直近2日」が揃うまで2日ほどかかる
3. 切り替えるときは、別の実装の prefix を空にする。本番と同じ `state/posts.json` とロックを使うので、旧い実装と同時に動いても、ロックで片方が飛ばされる。旧い実装の Scheduler を止めてから、新しい方の Scheduler を動かす
