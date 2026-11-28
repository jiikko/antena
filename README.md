# https://prpr-antena.com/

- 運用コストが安い自分専用のアンテナサイトです

## 使っている技術

- GCP Cloud Run Jobs（Go のバッチ。DB は使わない）
- AWS S3 + CloudFront

## アーキテクチャ

- Cloud Run Job => S3 <= CloudFront <= ブラウザ

## バッチ処理

- Cloud Run Job `antena-fetch-posts-next` を Cloud Scheduler が 30 分おき（8〜23 時 JST）に動かす
  - `config/sites.yml` のサイトの RSS を取得し、記事の状態 `s3://prpr-antena/state/posts.json` に足す
  - 古い記事を間引き（5 日より古く、サイトあたり 50 件を超える分）、トップ・カテゴリ・サイト別の HTML を生成して S3 にアップロードする
- 実装は `batch/`（Go。詳細は `batch/README.md`）、運用は `docs/flow.md`、記事状態とロックの形式は `docs/state-format.md`
- 2026-11-28 までは Ruby の実装だった（移行の経緯は `migration.md`）

## サイトの追加・停止

- `config/sites.yml` を編集して master に push する。次のバッチから反映される（規則はファイルの冒頭）

## 本番環境へのデプロイ

- master ブランチへ push すると、`.github/workflows/release.yml` が Go のイメージ（Dockerfile の `go` target）をビルドし、バッチのジョブを更新する

## テスト

- `cd batch && go test ./...`
