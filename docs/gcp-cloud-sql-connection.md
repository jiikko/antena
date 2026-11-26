# Cloud Run Jobの接続情報を使ってCloud SQLへ接続する

Cloud Run Jobに設定されている `DATABASE_URL` とCloud SQL Auth Proxyを使い、手元の端末からCloud SQLへ接続する手順。コマンド中のプロジェクト、リージョン、Job名、インスタンス接続名は自分の環境に置き換える。

## 必要なもの

- Google Cloud SDK（`gcloud`）
- Cloud SQL Auth Proxy
- 対象プロジェクトとCloud SQLインスタンスへ接続するGoogle Cloud権限（通常は Cloud SQL Client ロール）
- `DATABASE_URL` に含まれるデータベースのユーザー名とパスワード。IAMデータベース認証を使う場合はその設定に従う

Cloud SQL ProxyのADC認証はGoogle Cloud上のトンネルを開くためのもの。MySQL/PostgreSQLのユーザー認証は別で、通常は `DATABASE_URL` に含まれるユーザー名とパスワードを使う。

## Cloud Run Jobの接続URLを確認する

Google Cloud Consoleで **Cloud Run → Jobs → 対象Job → コンテナ → Variables & secrets** を開き、`DATABASE_URL` を確認する。

CLIからJob設定を確認する場合は次を使う。

```sh
gcloud run jobs describe JOB_NAME --project PROJECT_ID --region REGION
```

出力にユーザー名やパスワードが含まれる場合がある。自分の端末内だけで扱い、チャット、Issue、ログ、Gitへ貼らない。

## UnixソケットでProxyを起動する

必要ならADC認証を行い、Proxyを起動する。Proxyを起動した端末は、接続確認が終わるまで開いたままにする。

```sh
gcloud auth application-default login
mkdir -p /tmp/cloudsql
cloud-sql-proxy --unix-socket /tmp/cloudsql PROJECT_ID:REGION:INSTANCE_ID
```

このときのローカルソケットパスは次の形式になる。

```text
/tmp/cloudsql/PROJECT_ID:REGION:INSTANCE_ID
```

## ローカルの `DATABASE_URL` を設定する

Cloud Run Jobの `DATABASE_URL` 全体を使い、`socket` クエリパラメータを上記のローカルソケットパスに置き換える。パラメータがなければ追加する。ユーザー名、パスワード、DB名、その他のクエリパラメータは保持する。

URLをコマンド履歴へ直接書かず、非表示入力して環境変数に設定する。

```sh
printf 'DATABASE_URL（入力は非表示）: '
read -r -s DATABASE_URL
printf '\n'
export DATABASE_URL
```

入力するURLの形式例（値はプレースホルダー）。

```text
trilogy://DB_USER:DB_PASSWORD@/DATABASE_NAME?socket=/tmp/cloudsql/PROJECT_ID:REGION:INSTANCE_ID
```

実際のURLやパスワードをこのファイルへ書いたり、コミットしたりしない。

## 接続を確認する

Trilogyを使うRailsアプリなら、アプリのリポジトリで読み取り専用クエリを実行する。

```sh
SECRET_KEY_BASE=1 RAILS_ENV=production bundle exec rails runner 'puts ActiveRecord::Base.connection.select_value("SELECT 1")'
```

接続できれば `1` が表示される。この環境ではTrilogyをTCP Proxyへ接続すると `Got packets out of order` が起きたため、Unixソケットを使う。

## 作業を終える

接続に使ったURLを現在のシェルから削除し、Proxyを `Ctrl-C` で停止する。

```sh
unset DATABASE_URL
```

サイト定義のエクスポートや初期データ移行など、一度だけDBを読む作業にこの接続を使える。DBなしで動かす定期バッチには `DATABASE_URL` を設定しない。
