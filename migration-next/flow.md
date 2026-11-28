# バッチの実行フローと運用

この文書は、https://prpr-antena.com/ を書くバッチ（`migration-next`）の実行フロー、記事状態の置き場、運用の手順、Cloud SQL からの移行の経緯を説明する。

## 実行フロー

```text
Cloud Scheduler antena-fetch-posts-next-scheduler-trigger（30分おき・8〜23時 JST）
  → Cloud Run Job antena-fetch-posts-next（us-central1）
      → bundle exec ruby migration-next/bin/run.rb（Rails / DBなし）
          ├─ config/sites.yml を読む
          ├─ S3 から state/posts.json を読む
          ├─ enabledなサイトのRSSを取得・重複確認
          ├─ 古い記事を間引く（記事が50件を超えるサイトの、5日より古い記事）
          ├─ state/posts.json を S3 へ保存
          └─ HTMLを生成し、ページ用S3の公開キーへアップロード
```

リリースワークフロー（`.github/workflows/release.yml` の `deploy_migration_next`）が、master への push のたびにジョブのイメージと env を更新する。ジョブの prefix は空（公開キー）で、runner は `MIGRATION_NEXT_ALLOW_PUBLIC=true` が無いと空の prefix を拒否する。ワークフローはデプロイ後に env を読み返し、prefix が空であることを確かめる。

ワークフローは Scheduler の `update` だけを行い、resume しない。記事状態の作り直しや緊急時に pause したものを、push が勝手に戻さないため。pause 中なら warning を出す。

## 失敗の通知

ページはバッチが止まっても古いまま表示され続けるので、止まったことは Cloud Monitoring のアラートで知る（2026-11-28 に作成）。通知はメールの通知チャンネル「antena 運用メール」へ届く。

| アラート | 鳴る条件 |
| --- | --- |
| antena: バッチの実行が失敗した | `antena-fetch-posts-next` の実行が 1 回でも失敗した |
| antena: 2時間ページを書いていない | JST 10:00〜翌 0:59 の間に、直近 2 時間の成功した実行が 0 回 |

- どちらも指標 `run.googleapis.com/job/completed_execution_count` を PromQL で見る。2 本目は、実行が 1 回も無い（Scheduler の pause・ジョブの起動失敗）ときにも鳴る
- 2 本目を見る時間を JST 10:00〜翌 0:59 に限るのは、夜の停止（23:30〜翌 8:00）の間と明けに鳴らさないため。Scheduler の時間を変えたら、この時間（PromQL の `hour()`。UTC）も直す
- 作り直しや緊急の対応で Scheduler を 2 時間以上 pause するときは、2 本目が鳴る
- メールが届くことは、2026-11-28 に必ず鳴る試しのアラートで確かめた（届くまで1時間ほどかかった。試しのアラートは消した）
- ロックを取れずに飛ばした実行も「成功」に数える。飛ばしが続いても 2 本目は鳴らないが、1 時間たったロックは次の実行が奪う

## 記事状態の置き場

記事データと実行ロックは、HTML と同じ `prpr-antena` に置く（バケットを増やさない、というユーザーの決定。2026-11-26）。記事状態にはフィード本文や要約が含まれ、次回実行時の重複確認、記事ページ生成、古い記事の間引きに使う。

| キー | 内容 |
| --- | --- |
| `state/posts.json` | 全記事の状態。無ければ実行が空から作り（「記事状態の始まりと作り直し」）、以後は読み書きする |
| `locks/migration-next-run.lock` | 多重起動を防ぐ一時ロック。バッチ開始時に作成し、終了時に削除する |

これは本番（prefix が空）のキー。prefix を付けた実行（`migration-preview` や、別の実装の shadow）は `state/<prefix>/posts.json` と `locks/<prefix>/migration-next-run.lock` を使い、本番の記事状態に触れない。中身の形式とロックの取り方、別の実装を並べて動かす手順は `state-format.md` にある。

同じバケットに置くために、`prpr-antena` に次の設定を入れてある（2026-11-26 に適用）。

| 設定 | 中身 | 無いと起きること |
| --- | --- | --- |
| バケットポリシーの `HideMigrationState` | CloudFront の OAI に `state/*` と `locks/*` の GetObject を拒否する | `https://prpr-antena.com/state/posts.json` で全記事の本文が読める |
| ライフサイクル | 1日で消すのは `sites/`・`migration-preview/`・`migration/` だけ。以前は全キーが対象だった | `state/posts.json` が1日で消える |

トップページとカテゴリのページは自動では消えない。バッチが止まっても、古い内容のまま表示され続ける。`sites/` は1日で消えるが、バッチが毎日書き直す（夜間の停止は9時間）。バージョニングは無効のまま（ユーザー判断）。

リリースワークフローは、`state/deny-probe.txt` を置いてサイトから 403 になること（Deny で読み取りが拒否されること）を確認してからデプロイ処理へ進む。無いキーは Deny が無くても 403 になるので、在るファイルで確かめる。

ジョブに渡す `AWS_ACCESS_KEY_ID`（IAM ユーザー `prpr_antena_upload_user`、ポリシー `UploadToPrprAntenaBucketPolicy` の v2）に要る権限:

- `prpr-antena/*`: PutObject / PutObjectAcl
- `state/*`・`locks/*`: GetObject / DeleteObject
- `prpr-antena`: ListBucket（無いと、無いキーの GET が 404 でなく 403 になり、`posts.json` が無い初回と lock の処理が失敗する）

## 記事状態の始まりと作り直し

`state/posts.json` が無いとき、runner は WARN を出して空の状態から始める。その実行で、各サイトの RSS に載っている記事（サイトあたり 10〜20 件程度）が、その実行時刻の記事として入る。トップページの「直近2日」は2日ほどで普段の見た目に戻る。

ファイルが在るのに読めない（権限エラー・JSON の破損）ときは空から始めず、実行を失敗させる。既にある記事を空で上書きしないため。

`state/posts.json` が壊れたら、Scheduler を pause してファイルを消し、resume する（`gcloud scheduler jobs pause|resume antena-fetch-posts-next-scheduler-trigger --location us-central1`）。次の実行が空から作り直す。

使わなくなったキー（ページ数が減ったサイトの末尾ページ）は消さない。`sites/` の下はライフサイクルで1日後に消える。

## 記事ごとの最終日時

`config/sites.yml` はカテゴリとサイトの固定定義だけを持つ。サイトごとの最終記事日時は、記事状態から `site_id` ごとの `MAX(created_at)` でバッチ中に集計できる。これは最後に新しい記事を保存した日時で、RSSを巡回しただけで新着がなかった時刻までは表さない。

## 移行の経緯（Cloud SQL から）

移行前は、Rails のジョブ `anten-fetch-posts`（30分おき）と `antena-delete-old-posts`（毎晩）が Cloud SQL の `antena_production` を正本にしてページを書いていた。管理画面は Cloud Run サービス `prpr-antena` だった。

| 日付 | 出来事 |
| --- | --- |
| 2026-11-26 | 新ジョブを shadow で動かし始めた（`migration-preview/` に書く）。Cloud SQL の記事は引き継がず、空から始めた（ユーザー判断） |
| 2026-11-27 19:02〜19:10 | 切り替え。新ジョブが公開キーを書き、旧ジョブの Scheduler を止めた |
| 2026-11-27 | Rails のコードを repo から削除し、続けて GCP の旧資源を撤去した（ユーザー判断で、予定していた1週間の様子見を短縮）: 旧ジョブ2本とその Scheduler、Cloud Run サービス `prpr-antena`、DB `antena_production`、GitHub の secrets `DATABASE_URL` / `SECRET_KEY_BASE` / `RAILS_MASTER_KEY`、repository variable `MIGRATION_NEXT_MODE` |

Rails のバッチへ戻す手段はもう無い。`antena_production` を消した後、`hoby-db` のインスタンスもバックアップごと削除した（2026-11-27、ユーザー判断）ので、移行前の記事データはどこにも残っていない。
