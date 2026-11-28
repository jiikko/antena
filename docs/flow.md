# バッチの実行フローと運用

この文書は、https://prpr-antena.com/ を書くバッチ（Go の実装 `src/`。2026-11-28 までは Ruby の実装）の実行フロー、記事状態の置き場、運用の手順、Cloud SQL からの移行の経緯を説明する。

## 実行フロー

```text
Cloud Scheduler antena-fetch-posts-next-scheduler-trigger（30分おき・8〜23時 JST）
  → Cloud Run Job antena-fetch-posts-next（asia-northeast1）
      → /app/antena-batch（Go の実装 src/。DBなし）
          ├─ config/sites.yml を読む
          ├─ S3 から state/posts.json を読む
          ├─ enabledなサイトのRSSを取得（並列）・重複確認（サイト id の順）
          ├─ 古い記事を間引く（記事が50件を超えるサイトの、5日より古い記事）
          ├─ state/posts.json を S3 へ保存
          └─ HTMLを生成し、ページ用S3の公開キーへアップロード（並列）
```

リリースワークフロー（`.github/workflows/release.yml` の `deploy_batch`）が、master への push のたびにジョブのイメージ（`batch-go:master-<sha>`）と env を更新する。ジョブの prefix は空（公開キー）で、バッチは `ANTENA_ALLOW_PUBLIC=true` が無いと空の prefix を拒否する。ワークフローはデプロイ後にジョブの設定を読み返し、command が `/app/antena-batch`、args が空、prefix が空であることを確かめる。

ワークフローは Scheduler の `update` だけを行い、resume しない。記事状態の作り直しや緊急時に pause したものを、push が勝手に戻さないため。pause 中なら warning を出す。

## Go の実装への切り替え（2026-11-28）

本番のジョブ `antena-fetch-posts-next` のイメージを、Ruby の実装（`web:master-<sha>`）から Go の実装（`batch-go:master-<sha>`）に替えた（ユーザー指示）。ジョブの名前・Scheduler・アラート・記事状態・ロックは同じものを使う。

- 切り替える前に、Go の実装を shadow のジョブ `antena-fetch-posts-go`（prefix `migration-preview`、本番の記事状態のコピーから開始）で 11 回、本番と同じ時刻に動かした。本番と取り込んだ記事は、取得した時刻の差で1回分ずれた回を除いて一致した。shadow のジョブとその Scheduler、`state/migration-preview/` は切り替えの後に消した
- プログラムの部分（ログの最初の行から最後の行まで）は、Ruby の約 28 秒に対して Go は 17〜19 秒。実行全体の大半はコンテナの起動待ち（60〜90 秒）なので、ページが更新されるまでの時間はほぼ変わらない
- 切り替えの後、RSS の取得とページのアップロードを並列にした（同時に 8 本まで）。取り込む順はサイト id の順のままなので、記事の id と重複の判定は取得が返る順に左右されない
- 並列にした後の 11-28 19:00 の回は、起動の行から最後の行まで約 10 秒（全サイトの取得まで 6.6 秒、記事状態とページの書き込みまで 3.2 秒）、実行全体は 56 秒。起動の行は 11-28 19:00 の回から出ている（それより前のログは、最初の行が1サイト目の取得の後）
- 11-28 19:20 にジョブと Scheduler を us-central1 から asia-northeast1 へ移した（ユーザー指示）。us-central1 は 2023-05 の旧 Rails のジョブから引き継いだもので、理由は記録に無い。イメージ・S3・フィードはどれも日本にある。移した後の2回（手動 19:23、定期 19:30）は、実行全体が 9.5 秒と 14.4 秒、起動の行から最後の行までが 4.2 秒と 4.4 秒（us-central1 では全体 56〜123 秒、プログラム約 10 秒）。回数が少ないので、何回か見てから値を直す
- Ruby の実装（`migration-next/`・`spec/`・`Gemfile`）は、切り替えた日のうちに repo から消した（ユーザー指示）。コードは commit `bcbfaff` まで残っている。Go のテストが比べる答えは、Ruby が作ったものを `src/internal/*/testdata/` に移して使い続けている
- 戻すときは、最後の Ruby のイメージ `asia-northeast1-docker.pkg.dev/jiikko/prpr-antena/web:master-897d78f36252cd6d70c2bb143f4dc3e0e26a26a7` で `gcloud run jobs deploy antena-fetch-posts-next --image=<そのイメージ> --command=bundle --args=exec,ruby,migration-next/bin/run.rb` に `--update-env-vars=MIGRATION_NEXT_PREFIX=,MIGRATION_NEXT_ALLOW_PUBLIC=true` を付けて打つ（region は今のまま。Ruby は 11-28 に改名する前の名前の環境変数とロックのキー `locks/migration-next-run.lock` を使う。AWS の鍵はジョブに残っているので `--set-env-vars` は渡さない。Go と同時に動かすとロックで排他されないので、Go のジョブを置き換える形でだけ使う）。次の push で Go に戻るので、戻したままにするなら release.yml も直す（Ruby のイメージにはコードが入っているので、repo から消した後もこの手順で動く）

## 失敗の通知

ページはバッチが止まっても古いまま表示され続けるので、止まったことは Cloud Monitoring のアラートで知る（2026-11-28 に作成）。通知はメールの通知チャンネル「antena 運用メール」へ届く。

| アラート | 鳴る条件 |
| --- | --- |
| antena: バッチの実行が失敗した | `antena-fetch-posts-next` の実行が 1 回でも失敗した |
| antena: 2時間ページを書いていない | JST 10:00〜翌 0:59 の間に、直近 2 時間の成功した実行が 0 回 |

- どちらも指標 `run.googleapis.com/job/completed_execution_count` を PromQL で見る。2 本目は、実行が 1 回も無い（Scheduler の pause・ジョブの起動失敗）ときにも鳴る
- 2 本目を見る時間を JST 10:00〜翌 0:59 に限るのは、夜の停止（23:30〜翌 8:00）の間と明けに鳴らさないため。Scheduler の時間を変えたら、この時間（PromQL の `hour()`。UTC）も直す
- 作り直しや緊急の対応で Scheduler を 2 時間以上 pause するときは、2 本目が鳴る
- メールが届くことは、2026-11-28 に必ず鳴る試しのアラートで確かめた（試しのアラートは消した）
- ロックを取れずに飛ばした実行も「成功」に数える。飛ばしが続いても 2 本目は鳴らないが、1 時間たったロックは次の実行が奪う

## 記事状態の置き場

記事データと実行ロックは、HTML と同じ `prpr-antena` に置く（バケットを増やさない、というユーザーの決定。2026-11-26）。記事状態にはフィード本文や要約が含まれ、次回実行時の重複確認、記事ページ生成、古い記事の間引きに使う。

| キー | 内容 |
| --- | --- |
| `state/posts.json` | 全記事の状態。無ければ実行が空から作り（「記事状態の始まりと作り直し」）、以後は読み書きする |
| `locks/antena-run.lock` | 多重起動を防ぐ一時ロック。バッチ開始時に作成し、終了時に削除する |

これは本番（prefix が空）のキー。prefix を付けた実行（試しに動かすときの `preview` や、別の実装の shadow）は `state/<prefix>/posts.json` と `locks/<prefix>/antena-run.lock` を使い、本番の記事状態に触れない。中身の形式とロックの取り方、別の実装を並べて動かす手順は `state-format.md` にある。

同じバケットに置くために、`prpr-antena` に次の設定を入れてある（2026-11-26 に適用）。

| 設定 | 中身 | 無いと起きること |
| --- | --- | --- |
| バケットポリシーの `HideMigrationState` | CloudFront の OAI に `state/*` と `locks/*` の GetObject を拒否する | `https://prpr-antena.com/state/posts.json` で全記事の本文が読める |
| ライフサイクル | 1日で消すのは `sites/` と `preview/` だけ（2026-11-28 に `migration-preview/`・`migration/` から替えた）。以前は全キーが対象だった | `state/posts.json` が1日で消える |

トップページとカテゴリのページは自動では消えない。バッチが止まっても、古い内容のまま表示され続ける。`sites/` は1日で消えるが、バッチが毎日書き直す（夜間の停止は9時間）。バージョニングは無効のまま（ユーザー判断）。

リリースワークフローは、`state/deny-probe.txt` を置いてサイトから 403 になること（Deny で読み取りが拒否されること）を確認してからデプロイ処理へ進む。無いキーは Deny が無くても 403 になるので、在るファイルで確かめる。

ジョブに渡す `AWS_ACCESS_KEY_ID`（IAM ユーザー `prpr_antena_upload_user`、ポリシー `UploadToPrprAntenaBucketPolicy` の v2）に要る権限:

- `prpr-antena/*`: PutObject / PutObjectAcl
- `state/*`・`locks/*`: GetObject / DeleteObject
- `prpr-antena`: ListBucket（無いと、無いキーの GET が 404 でなく 403 になり、`posts.json` が無い初回と lock の処理が失敗する）

## 記事状態の始まりと作り直し

`state/posts.json` が無いとき、runner は WARN を出して空の状態から始める。その実行で、各サイトの RSS に載っている記事（サイトあたり 10〜20 件程度）が、その実行時刻の記事として入る。トップページの「直近2日」は2日ほどで普段の見た目に戻る。

ファイルが在るのに読めない（権限エラー・JSON の破損）ときは空から始めず、実行を失敗させる。既にある記事を空で上書きしないため。

`state/posts.json` が壊れたら、Scheduler を pause してファイルを消し、resume する（`gcloud scheduler jobs pause|resume antena-fetch-posts-next-scheduler-trigger --location asia-northeast1`）。次の実行が空から作り直す。

使わなくなったキー（ページ数が減ったサイトの末尾ページ）は消さない。`sites/` の下はライフサイクルで1日後に消える。

## 記事ごとの最終日時

`config/sites.yml` はカテゴリとサイトの固定定義だけを持つ。サイトごとの最終記事日時は、記事状態から `site_id` ごとの `MAX(created_at)` でバッチ中に集計できる。これは最後に新しい記事を保存した日時で、RSSを巡回しただけで新着がなかった時刻までは表さない。

## 移行の経緯（Cloud SQL から）

移行前は、Rails のジョブ `anten-fetch-posts`（30分おき）と `antena-delete-old-posts`（毎晩）が Cloud SQL の `antena_production` を正本にしてページを書いていた。管理画面は Cloud Run サービス `prpr-antena` だった。

| 日付 | 出来事 |
| --- | --- |
| 2026-11-26 | 新ジョブを shadow で動かし始めた（`migration-preview/` に書く）。Cloud SQL の記事は引き継がず、空から始めた（ユーザー判断） |
| 2026-11-27 19:02〜19:10 | 切り替え。新ジョブが公開キーを書き、旧ジョブの Scheduler を止めた |
| 2026-11-27 | Rails のコードを repo から削除し、続けて GCP の旧資源を撤去した（ユーザー判断で、予定していた1週間の様子見を短縮）: 旧ジョブ2本とその Scheduler、Cloud Run サービス `prpr-antena`、DB `antena_production`、GitHub の secrets `DATABASE_URL` / `SECRET_KEY_BASE` / `RAILS_MASTER_KEY`、repository variable `ANTENA_MODE` |

Rails のバッチへ戻す手段はもう無い。`antena_production` を消した後、`hoby-db` のインスタンスもバックアップごと削除した（2026-11-27、ユーザー判断）ので、移行前の記事データはどこにも残っていない。
