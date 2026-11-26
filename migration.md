# Cloud SQL 切り離し移行メモ（prpr-antena）

## 目的

Cloud SQL インスタンス `jiikko:asia-northeast1:hoby-db` を最終的に停止したい。
このインスタンスを使っているのは blog.jiikko.com と prpr-antena の2つで、先に antena を切り離す。
このドキュメントは antena 側の移行仕様・決定事項・進捗をまとめたもの。

移行前後の環境と記事状態の置き場は [`migration-next/flow.md`](migration-next/flow.md)、GCP上のCloud SQL接続手順は [`docs/gcp-cloud-sql-connection.md`](docs/gcp-cloud-sql-connection.md) にまとめている。

## 現在の構成（確認済みの事実）

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
  - `bin/export_sites_yaml.rb` は YAML を再生成するための手動スクリプトとして残す。admin_users はパスワードハッシュを含むため含めない
- ローカル実行ルートのハマりどころ（判明分）:
  - ADC が必要: `gcloud auth application-default login`
  - 起動に `SECRET_KEY_BASE=1` が必要（本番ジョブと同じダミーでよい）
  - `DATABASE_URL` は `gcloud run jobs describe anten-fetch-posts --region us-central1` の env から取得し、値をチャットやコミットに残さないこと
  - TCP 接続（127.0.0.1:3306）では Trilogy が `Got packets out of order` になった。Unix ソケット経由なら接続できた

### タスク2: DB なしバッチの実装（コード実装済み・shadow 未確認）

- 旧ジョブとは別の Cloud Run Job `antena-fetch-posts-next` として並走させ、`MIGRATION_NEXT_MODE`（repository variable）で preview と public を切り替える。手順は `migration-next/flow.md`

- `migration-next/` にDBなしrunner、記事状態のJSONストア、フィード取得、静的HTML生成を実装した
- バッチ開始時に `config/sites.yml` を読み込み、Site / Category を DB ではなく YAML 由来のデータで扱う
- posts は S3 の JSON 状態ファイルから読み込み、fetch・重複チェック・間引き・HTML 生成を行い、終了時に S3 へ保存し直す
- `Site.fetch` のフィード取得と本文掃除（正規表現群）の挙動は現行と変えない
- `Post#clean_content!` から参照される `ContentClearnupService` はリポジトリ内に定義がなく、死んだコードの可能性が高い。移行時に確認し、実質の移植対象は「フィード取得・本文掃除・HTML 生成・S3 アップロード」の4点とみる
- 削除ジョブ（`destroy_old_posts` 相当）は間引きとして fetch バッチ側に統合してよい

### タスク3: 撤去

- 管理画面・admin_users・sorcery・Web サービスを削除
- `release.yml` から `DATABASE_URL` を除去し、ジョブが DB 接続なしで動く構成にする

### タスク4: 切り替え

手順とロールバックは `migration-next/flow.md` の「段階と `MIGRATION_NEXT_MODE`」。要点は次のとおり。

1. `posts.json` は shadow の初回実行で自動で作られる。切り替えの時点では作り直さない
2. `MIGRATION_NEXT_MODE=public` にしてワークフローを実行する。旧 Scheduler 2本が pause され、新ジョブが公開キーを書く
3. 新ジョブを手動で実行し、公開ページが更新されることを確認する
4. 1週間様子見。この間 Cloud SQL と旧ジョブは残し、Scheduler を戻すだけでロールバックできるようにしておく

### タスク5: その後

- 運用が安定したら Go 書き換えを検討（Rails 版の S3 出力が正解データになる）
- blog.jiikko.com の静的化は別リポジトリ（`~/src/blog`）の話。両方が外れたら、他に `hoby-db` の利用者がいないか GCP 側で確認してからインスタンスを停止する

## 制約

- 認証情報（`DATABASE_URL` など）をコード・ドキュメント・コミットに残さない
- 移行中も30分おきのバッチと S3 の公開ページは止めない（切り替え手順の Scheduler 停止はタスク4の手順内でのみ行う）
