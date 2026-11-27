# https://prpr-antena.com/

- 運用コストが安い自分専用のアンテナサイトです

## 使っている技術

- GCP Cloud Run Jobs（Ruby のバッチ。Rails と DB は使わない）
- AWS S3 + CloudFront

## アーキテクチャ

- Cloud Run Job => S3 <= CloudFront <= ブラウザ

## バッチ処理

- Cloud Run Job `antena-fetch-posts-next` を Cloud Scheduler が 30 分おき（8〜23 時 JST）に動かす
  - `config/sites.yml` のサイトの RSS を取得し、記事の状態 `s3://prpr-antena/state/posts.json` に足す
  - 古い記事を間引き（5 日より古く、サイトあたり 50 件を超える分）、トップ・カテゴリ・サイト別の HTML を生成して S3 にアップロードする
- 実装は `migration-next/`（詳細は `migration-next/README.md` と `migration-next/flow.md`）

## サイトの追加・停止

- `config/sites.yml` を編集して master に push する。次のバッチから反映される（規則はファイルの冒頭）

## 本番環境へのデプロイ

- master ブランチへ push すると、`.github/workflows/release.yml` がイメージをビルドし、バッチのジョブを更新する

## テスト

- `bundle exec rspec`
