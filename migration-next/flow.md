# migration-next の移行先と実行フロー

この文書は、現行バッチと migration-next の違い、記事状態の置き場、移行後の定期実行、切り替えとロールバックの手順を説明する。

## 移行先

| データ | 移行前 | 移行後 |
| --- | --- | --- |
| カテゴリ・サイト定義 | Cloud SQL の `categories` / `sites` | Git 管理する `config/sites.yml` |
| 記事と本文 | Cloud SQL の `posts` | ページ用バケット `prpr-antena` の `state/posts.json`（サイトからは読めない） |
| 公開HTML | S3 バケット `prpr-antena` | 同じページ用バケット。確認中は `migration-preview/`、切り替え後は現在の公開キー |

`admin_users` はサイト定義や記事状態に含めない。Rails のコード（Webサービス・管理画面・旧バッチ）は 2026-11-27 に repo から削除した。GCP 上の旧ジョブ・Webサービス・Cloud SQL は、ロールバックの様子見が終わる 12/4 まで最後のイメージのまま残す（`migration.md` のタスク3）。

## 記事状態の置き場

記事データと実行ロックは、HTML と同じ `prpr-antena` に置く（バケットを増やさない、というユーザーの決定。2026-11-26）。記事状態にはフィード本文や要約が含まれ、次回実行時の重複確認、記事ページ生成、古い記事の間引きに使う。

| キー | 内容 |
| --- | --- |
| `state/posts.json` | 全記事の状態。無ければ初回の実行が空から作り（「記事状態の始まり」）、以後はmigration-nextが読み書きする |
| `locks/migration-next-run.lock` | 多重起動を防ぐ一時ロック。バッチ開始時に作成し、終了時に削除する |

同じバケットに置くために、`prpr-antena` に次の設定を入れてある（2026-11-26 に適用。適用前の設定は手元の `tmp/migration-next-cutover-20261126/aws2/before-*.json`）。

| 設定 | 中身 | 無いと起きること |
| --- | --- | --- |
| バケットポリシーの `HideMigrationState` | CloudFront の OAI に `state/*` と `locks/*` の GetObject を拒否する | `https://prpr-antena.com/state/posts.json` で全記事の本文が読める |
| ライフサイクル | 1日で消すのは `sites/`・`migration-preview/`・`migration/` だけ。以前は全キーが対象だった | `state/posts.json` が1日で消える |

トップページとカテゴリのページも自動では消えなくなった。バッチが止まっても、古い内容のまま表示され続ける。バージョニングは無効のまま（ユーザー判断）。`state/posts.json` が壊れたら、新ジョブの Scheduler を pause してファイルを消し、resume する。次の実行が空から作り直す。

リリースワークフローは、`state/deny-probe.txt` を置いてサイトから 403 になること（Deny が効いていること）を確認してからデプロイ処理へ進む。無いキーは Deny が無くても 403 になるので、在るファイルで確かめる。

## GCP上のCloud SQLへの接続

一時的なDB接続手順は、他のリポジトリでも参照できるよう [`docs/gcp-cloud-sql-connection.md`](../docs/gcp-cloud-sql-connection.md) に分けてある。切り替え後の定期fetchジョブには `DATABASE_URL` を設定しない。

## Before: 現行の定期バッチ

```text
Cloud Scheduler（30分おき）
  → Cloud Run Job anten-fetch-posts
      → Rails / ActiveRecord
          → Cloud SQL: sites・categories・posts を読む
          → RSSを取得して posts に保存
          → S3Uploader がHTMLを生成してページ用S3へ書く

Cloud Scheduler（毎晩）
  → Cloud Run Job antena-delete-old-posts
      → Rails / ActiveRecord
          → Cloud SQL の posts を間引く
```

記事状態の正本はCloud SQL。fetchとHTML生成のたびにRailsがDBへ接続する。

## After: migration-next の定期バッチ

```text
Cloud Scheduler antena-fetch-posts-next-scheduler-trigger（30分おき）
  → Cloud Run Job antena-fetch-posts-next
      → bundle exec ruby migration-next/bin/run.rb（Rails / DBなし）
          ├─ config/sites.yml を読む
          ├─ S3 から state/posts.json を読む
          ├─ enabledなサイトのRSSを取得・重複確認
          ├─ 古い記事を間引く
          ├─ state/posts.json を S3 へ保存
          └─ HTMLを生成し、ページ用S3へアップロード
```

旧ジョブとは別の Cloud Run Job で、切り替えまでは旧ジョブと並走する。古い記事の間引きがfetchバッチに入るため、切り替え後は旧削除ジョブのSchedulerも止める。migration-nextの実行環境には `DATABASE_URL` とCloud SQL接続設定を渡さない。

## 記事状態の始まり

Cloud SQL の記事は引き継がない（2026-11-26 ユーザー判断）。`state/posts.json` が無いとき、runner は WARN を出して空の状態から始める。初回の実行で、各サイトの RSS に載っている記事（サイトあたり 10〜20 件程度）が、その実行時刻の記事として入る。トップページの「直近2日」は2日ほどで普段の見た目に戻る。

ファイルが在るのに読めない（権限エラー・JSON の破損）ときは空から始めず、実行を失敗させる。既にある記事を空で上書きしないため。

## 段階と `MIGRATION_NEXT_MODE`

リリースワークフローの `deploy_migration_next` job が新ジョブを配置する。書き込み先は GitHub の repository variable `MIGRATION_NEXT_MODE` で決まり、push のたびにこの値で設定し直される。

| 段階 | `MIGRATION_NEXT_MODE` | 公開ページを書くもの | 新ジョブの書き込み先 | 旧Scheduler 2本 |
| --- | --- | --- | --- | --- |
| shadow | 未設定 / `preview` | 旧Railsジョブ | `migration-preview/` | 動かす |
| 切り替え後 | `public` | 新ジョブ | 公開キー（prefixなし・`MIGRATION_NEXT_ALLOW_PUBLIC=true`） | ワークフローがpauseする |

旧Railsジョブ（`anten-fetch-posts` / `antena-delete-old-posts`）は、Rails のコードを消した 2026-11-27 からイメージを更新していない。最後にデプロイしたイメージ（`web:master-45f78e3…`）のまま残しているので、ロールバックではSchedulerを戻すだけで済む。Artifact Registry は「最新5個に入らず、かつ30日より古い」イメージを消すので、このイメージは push の数に関係なく 12/27 まで残り、その後に消える。それより後はロールバックできない。

### 1. 事前準備（手作業・初回のみ）

1. `prpr-antena` のバケットポリシーとライフサイクルが上の「記事状態の置き場」の状態であることを確かめる（適用済み）
2. ジョブに渡す `AWS_ACCESS_KEY_ID`（IAM ユーザー `prpr_antena_upload_user`、ポリシー `UploadToPrprAntenaBucketPolicy`）の権限（適用済み。2026-11-26 にポリシーの v2 を作った。v1 は PutObject / PutObjectAcl だけ）
   - `prpr-antena/*`: PutObject / PutObjectAcl
   - `state/*`・`locks/*`: GetObject / DeleteObject
   - `prpr-antena`: ListBucket（無いと、無いキーの GET が 404 でなく 403 になり、`posts.json` が無い初回と lock の処理が失敗する）
3. 新ジョブを一度作る。`gcloud run jobs deploy antena-fetch-posts-next` を、`release.yml` の `Deploy DB-free fetch job` と同じ引数で手元から打つ。ワークフローのサービスアカウントにジョブの作成権限があれば、この手順は省いてよい
4. 新ジョブのSchedulerを作る（作成済み。2026-11-26 に旧トリガーと同じ形・PAUSED で作った）。ワークフローはこのトリガーの `update` しか行わず、resume もしない。新ジョブを手で1回実行して `migration-preview/` を確かめてから、`gcloud scheduler jobs resume antena-fetch-posts-next-scheduler-trigger --location us-central1` で動かす

### 2. shadow（数日）

masterへのpushで新ジョブが `migration-preview/` を書き始める。旧ジョブは今までどおり公開ページを書く。両者は別々にRSSを取るので、記事の `id` と `created_at` はDBと一致しない。新ジョブの状態の中で整合していればよい。

見るもの:
- `/migration-preview/` 以下のトップ・カテゴリ・サイト別ページを、公開ページと比べる（件数・並び・日付の区切り・ページ送り・画像）
- 新ジョブのログに出る `WARN`（feed error・lockのskipや引き継ぎ）

### 3. 切り替え

ワークフローは新 Scheduler を resume しない（pause 中なら warning を出すだけ）。resume は下の手順の中で手で行う。

1. 新 Scheduler を止める: `gcloud scheduler jobs pause antena-fetch-posts-next-scheduler-trigger --location us-central1`
2. `MIGRATION_NEXT_MODE` を `public` にし、ワークフローを `workflow_dispatch` で実行して完了を待つ。`deploy_migration_next` が旧 Scheduler 2本を pause してから、新ジョブの prefix を空にし、デプロイ後に env を読み返して空であることを確かめる。同じグループで待っている実行は新しい push に置き換えられるので、「cancelled」になったら実行し直す
3. 旧ジョブの実行中のものが終わるのを待つ（`gcloud run jobs executions list --job anten-fetch-posts --region us-central1` に完了していないものが無い）。Scheduler の pause は実行中のジョブを止めない
4. 新ジョブを手動で1回実行する（`gcloud run jobs execute antena-fetch-posts-next --region us-central1`）。公開ページのリンクに `/migration-preview` が含まれず、1ページ目に `noindex` の meta が無いことを確かめる
5. 新 Scheduler を resume する: `gcloud scheduler jobs resume antena-fetch-posts-next-scheduler-trigger --location us-central1`
6. 記事状態は shadow の間に育った `state/posts.json` をそのまま使う

旧Schedulerをpauseした後に新ジョブのデプロイが失敗すると、公開ページの更新が止まる。そのときは下のロールバックを行う。

### 4. ロールバック（様子見の1週間はCloud SQLを残す）

順序を守る。旧 Scheduler を先に戻すと、`MIGRATION_NEXT_MODE=public` のまま走るワークフローが再び pause する。

1. 新ジョブを止める: `gcloud scheduler jobs pause antena-fetch-posts-next-scheduler-trigger --location us-central1`
2. 新ジョブの実行中のものが終わるのを待つ（`gcloud run jobs executions list --job antena-fetch-posts-next --region us-central1`）。終わるまでは公開キーを書き続ける
3. `MIGRATION_NEXT_MODE` を `preview` に戻し、ワークフローを `workflow_dispatch` で実行して完了を待つ。新ジョブの書き込み先が `migration-preview/` に戻る。これを済ませるまで新ジョブを手で実行しない（公開キーを書く）
4. 旧 Scheduler を resume する: `anten-fetch-posts-scheduler-trigger` と `antena-delete-old-posts-scheduler-trigger`（`gcloud scheduler jobs resume <名前> --location us-central1`）
5. shadow を続けるなら新 Scheduler を resume する（書き込み先は preview）
6. 新ジョブで動いていた間の記事はDBに無い。RSSにまだ残っている分は、旧ジョブの次の実行で取り直される

どちらのバッチも、使わなくなったキー（ページ数が減ったサイトの末尾ページ）を消さない。切り替えやロールバックの後、末尾のページには前のバッチの出力がしばらく残る。

## 記事ごとの最終日時

`config/sites.yml` はカテゴリとサイトの固定定義だけを持つ。サイトごとの最終記事日時は、記事状態から `site_id` ごとの `MAX(created_at)` でバッチ中に集計できる。これは最後に新しい記事を保存した日時で、RSSを巡回しただけで新着がなかった時刻までは表さない。
