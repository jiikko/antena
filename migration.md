# Cloud SQL 切り離し移行メモ（prpr-antena）

## 目的

Cloud SQL インスタンス `jiikko:asia-northeast1:hoby-db` を最終的に停止したい。
→ 2026-11-27 に、中の全DB（他のサービスの分も。ユーザーが使っていないと判断）とバックアップごとインスタンスを削除した。
このインスタンスには blog.jiikko.com（`lokka_production`）・pbm-cloud（`pbm_cloud_production`）・filetree-meta-manager（`filetree_meta_manager_production`）のDBもある（2026-11-27 の `gcloud sql databases list`）。antena は 2026-11-27 に切り離した。
このドキュメントは antena 側の移行仕様・決定事項・進捗をまとめたもの。

移行前後の環境と記事状態の置き場は [`migration-next/flow.md`](migration-next/flow.md) にまとめている。

## 移行前の構成（確認済みの事実。Rails のコードと GCP の旧資源は 2026-11-27 に撤去済み）

- Rails 7.1 + trilogy (MySQL)、本番 DB は `antena_production`。テーブルは `sites` / `posts` / `categories` / `admin_users` の4つ
- 配信は既に静的化済み: ブラウザは S3 バケット `prpr-antena`（ap-northeast-1）+ CloudFront を見る。Web サービスは管理画面用
- バッチの実体は `Cron::FetchAndUpload` = `Site.fetch` + `S3Uploader.new.execute`
  - `Site.fetch`: 各サイトの RSS を取得し、本文を掃除して posts に保存（重複チェックは URL / タイトル）
  - `S3Uploader`: トップ / カテゴリ別 / サイト別ページを HTML 化して S3 へアップロード
- ジョブ: Cloud Run Jobs `anten-fetch-posts`（us-central1）を Scheduler が30分おき・8〜23時 JST に実行。削除ジョブ `antena-delete-old-posts` が毎晩0時に `Site.destroy_old_posts`（サイトあたり50件超かつ5日より古い記事を削除）
- リリース: master への push で `.github/workflows/release.yml` がイメージをビルドし、ジョブとサービスを更新する。ジョブの環境変数は `DATABASE_URL`（平文）と `SECRET_KEY_BASE=1`
- 表示要件: カテゴリ画面は直近2日・最大150件、サイト別は50件ページング、トップはサイトごとに最新4件。レンダリングは `Category.find_by!(slug:)`、`Site.enabled`、`position` 昇順に依存
- ページ URL は `sites/:id` 形式

## 決定事項

1. **管理画面は撤去する。** admin / admin_users / sorcery / Web サービスは削除対象
2. **sites / categories の正本はリポジトリ内 `config/sites.yml`。** 変更は「YAML を編集してマージ → 次回バッチから反映」でよい
   - sites の **id は固定**（既存ページ URL と対応）。変更・再利用禁止。新規サイトは「現在の最大 id + 1」を採番
   - サイトをやめるときは行を消さず `enable: false` にする
   - category は slug で参照し、**slug の変更も禁止**（既存 URL が壊れるため）
   - name / rss_url は YAML に明示する。現行の Site 作成コールバック（RSS URL と名前を HTTP で自動取得する処理）は使わない
3. **posts の状態は S3 上の JSON ファイルで保持する**（SQL は使わない）
   - バッチ開始時に S3 からダウンロードし、終了時に S3 へアップロードし直す。キーは `s3://prpr-antena/state/posts.json`（ページと同じバケット）。本文を含むので、バケットポリシーで CloudFront からの読み取りを拒否し、ライフサイクルの削除対象から外してある（`migration-next/flow.md`「記事状態の置き場」）
   - 1件のフィールド: `site_id` / `title` / `url` / `summary` / `content` / `published_at` / `created_at`
   - posts に対して必要な処理は、重複チェック（URL / タイトル）、間引き（5日 / サイトあたり50件）、表示用の絞り込みと並び替えのみ。全件メモリに読み込んで処理してよい
   - バージョニングは使わない（2026-11-26 ユーザー判断）。破損時はファイルを消せば、次のバッチが空から作り直す
   - 初期ファイルは作らない。DB の posts は引き継がず、ファイルが無ければバッチが空から始める（2026-11-26 ユーザー判断）
4. **進め方: まず現行 Rails のまま RDB を切り離して運用を安定させる。その後に Go 書き換えを検討する**（同時にはやらない）。YAML / JSON の形式は Go から読みやすいことを意識する

## タスク

### タスク1: 本番 DB から YAML 初版を作る（完了）

- 完了内容:
  - 本番 DB から `config/sites.yml` を生成し、categories 4件・sites 15件を確認した。全件の name / rss_url に欠けはない
  - 最終更新日時は sites YAML に固定値として持たせず、posts の状態からバッチ実行時に集計する
  - `bin/export_sites_yaml.rb`（DB から YAML を再生成する手動スクリプト）は、Rails と一緒に 2026-11-27 に削除した。以後の正本は `config/sites.yml` で、DB から作り直すことは無い
- ローカル実行ルートのハマりどころ（判明分）:
  - ADC が必要: `gcloud auth application-default login`
  - 起動に `SECRET_KEY_BASE=1` が必要（本番ジョブと同じダミーでよい）
  - `DATABASE_URL` は `gcloud run jobs describe anten-fetch-posts --region us-central1` の env から取得し、値をチャットやコミットに残さないこと
  - TCP 接続（127.0.0.1:3306）では Trilogy が `Got packets out of order` になった。Unix ソケット経由なら接続できた

### タスク2: DB なしバッチの実装（完了。2026-11-26〜27 に shadow で確認）

- 旧ジョブとは別の Cloud Run Job `antena-fetch-posts-next` として並走させ、`MIGRATION_NEXT_MODE`（repository variable）で preview と public を切り替える。手順は `migration-next/flow.md`

- `migration-next/` にDBなしrunner、記事状態のJSONストア、フィード取得、静的HTML生成を実装した
- バッチ開始時に `config/sites.yml` を読み込み、Site / Category を DB ではなく YAML 由来のデータで扱う
- posts は S3 の JSON 状態ファイルから読み込み、fetch・重複チェック・間引き・HTML 生成を行い、終了時に S3 へ保存し直す
- `Site.fetch` のフィード取得と本文掃除（正規表現群）の挙動は現行と変えない
- `Post#clean_content!` から参照される `ContentClearnupService` はリポジトリ内に定義がなく、死んだコードの可能性が高い。移行時に確認し、実質の移植対象は「フィード取得・本文掃除・HTML 生成・S3 アップロード」の4点とみる
- 削除ジョブ（`destroy_old_posts` 相当）は間引きとして fetch バッチ側に統合してよい

### タスク3: 撤去（2026-11-27 完了）

予定では切り替え後 1 週間はロールバックのために GCP の旧資源を残すことにしていたが、ユーザー判断で同じ日に撤去した（2026-11-27）。

- repo: Rails 一式（管理画面・sorcery・モデル・旧バッチ `Cron::FetchAndUpload` / `S3Uploader`・旧 spec・設定・`bin/export_sites_yaml.rb`）と Rails の gem を削除。`release.yml` は新ジョブだけをデプロイし、常に公開キーへ書く（`MIGRATION_NEXT_MODE` による切り替えと旧 Scheduler の pause を外した）
- GCP: Cloud Run Job `anten-fetch-posts` / `antena-delete-old-posts` と Scheduler 2本、Cloud Run サービス `prpr-antena`（asia-northeast1）、`hoby-db` の DB `antena_production` を削除。続けてインスタンス `hoby-db` を、他のサービスの DB とバックアップごと削除した（ユーザー判断）
- GitHub: secrets `DATABASE_URL` / `SECRET_KEY_BASE` / `RAILS_MASTER_KEY` と repository variable `MIGRATION_NEXT_MODE` を削除
- `antena_production` を消す直前にオンデマンドバックアップ（ID `1795776107987`）を取ったが、インスタンスと一緒に削除したので、移行前の記事データは残っていない。手元からのダンプは、trilogy が cloud-sql-proxy 越しの認証で `Got packets out of order` になり取れなかった

### タスク4: 切り替え（2026-11-27 19:02〜19:10 に実施）

当時の手順の要点は次のとおり（`MIGRATION_NEXT_MODE` による切り替えは撤去で外した）。

1. `posts.json` は shadow の初回実行で自動で作られる。切り替えの時点では作り直さない
2. `MIGRATION_NEXT_MODE=public` にしてワークフローを実行する。旧 Scheduler 2本が pause され、新ジョブが公開キーを書く
3. 新ジョブを手動で実行し、公開ページが更新されることを確認する
4. 予定では1週間様子見してから撤去する予定だったが、同じ日に撤去した（タスク3）。Rails のバッチへ戻す手段はもう無い

### タスク5: その後

- 運用が安定したら Go 書き換えを検討（今の `migration-next` の S3 出力が正解データになる）
- 今の Ruby の実装の基準値。2026-11-26 21:49〜11-27 23:00 JST の 38 回の実行（失敗 0）を、Cloud Run の実行履歴・ジョブのログ・Monitoring から集計した。Go と比べるときは同じ取り方で測る
  | 項目 | 本番 10 回（11-27 19:06 以降） | shadow 26 回（初回の2回を除く） |
  | --- | --- | --- |
  | 実行の長さ（startTime〜completionTime。最小 / 中央 / 最大） | 43 / 81 / 134 秒 | 53 / 78 / 195 秒 |
  | プログラムのログの最初の行から最後の行まで（最初のサイトの取得前は入らない） | 26 / 28 / 76 秒 | 30 / 33 / 153 秒 |
  | 1回の新しい記事（最小 / 中央 / 最大） | 3 / 7 / 11 件 | 2 / 6.5 / 51 件（夜の停止明け） |
  | サイトの取得の WARN | 0 | 1 |
  - 有効なサイトは 11、ページは 19〜20 件
  - 記事状態: 539 件、`state/posts.json` は 2.5 MB（11-27 23:02）。記事が50件を超えて間引きが始まるまでは増え続ける
  - メモリ: 上限 512 MiB のうち、Monitoring の `container/memory/utilizations` の平均の最大で 17%（約 87 MiB）。1分ごとの値なので、短い山は入らない
  - 実行の長さとプログラムのログの長さの差（約 20〜100 秒）は、主にコンテナの起動。本番の 76 秒（23:00）は原因を確かめていない
- 別の実装（Go など）は、本番と別の prefix で並べて動かしてから切り替える。記事状態とロックの形式と手順は `migration-next/state-format.md`

## 制約

- 認証情報（`DATABASE_URL` など）をコード・ドキュメント・コミットに残さない
- 移行中も30分おきのバッチと S3 の公開ページは止めない（切り替え手順の Scheduler 停止はタスク4の手順内でのみ行う）
