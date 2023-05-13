# https://prpr-antena.com/

* How to run the test suite

## Deployment instructions
* masterブランチへのコミットでイメージをビルドしてECRにpushする
* AppRunnerへデプロイする
* SchedulerからAppRunnerの出力をS3へアップロードする
* https://prpr-antena.com にアクセスると CloudFront/S3がHTMLを返す
