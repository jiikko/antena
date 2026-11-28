# migration-next (the former Ruby implementation)

This directory contains the Ruby implementation of the DB-free fetch and static-page generator that writes https://prpr-antena.com/. Since 2026-11-28 production runs the Go implementation in `batch/` instead (the Cloud Run job `antena-fetch-posts-next` runs the `batch-go` image; see [flow.md](flow.md)). This code is no longer deployed; it is kept because it produces the golden files (`spec/fixtures/golden/`) that the Go tests compare with. Until then it was deployed as that job, writing the public page keys (empty `MIGRATION_NEXT_PREFIX` with `MIGRATION_NEXT_ALLOW_PUBLIC=true`). The legacy Rails batch and its Cloud SQL database were removed on 2026-11-27 (see [flow.md](flow.md) and `migration.md`).

For the Japanese before/after architecture and bucket roles, see [flow.md](flow.md).

When `MIGRATION_NEXT_PREFIX` is unset the page prefix is `migration-preview/` (used for the shadow runs before the cutovers, and safe for a manual local run); the production job sets it to empty. The site catalog is read from the checked-in `config/sites.yml`; `MIGRATION_NEXT_SITES_FILE` can override its path. The post JSON and the run lock live in the same page bucket: `state/posts.json` and `locks/migration-next-run.lock` for the live (empty) prefix, `state/<prefix>/posts.json` and `locks/<prefix>/migration-next-run.lock` for any other prefix, so a preview run never touches the live state. Their format is specified in [state-format.md](state-format.md). They contain full feed content, so the `prpr-antena` bucket policy denies the CloudFront origin access identity on `state/*` and `locks/*`, and the bucket lifecycle expires only `sites/`, `migration-preview/` and `migration/`. Before deploying, the release workflow writes `state/deny-probe.txt` and checks that `https://prpr-antena.com/state/deny-probe.txt` returns 403.

Posts are not imported from Cloud SQL. When `state/posts.json` does not exist the runner logs a `WARN` line and starts from an empty post set, so the first run fills it from the current RSS entries (all stamped with that run's time). Bucket versioning is off; to reset a broken state, pause the job's scheduler, delete `state/posts.json`, and resume. Any other read failure (for example AccessDenied or invalid JSON) stops the run instead of overwriting the file.

## Preview run

1. Site and category changes are made in `config/sites.yml` and take effect with the next release.
2. Run one preview batch (the first run creates `state/migration-preview/posts.json`; copy `state/posts.json` there first to start from the live posts):

   ```sh
   bundle exec ruby migration-next/bin/run.rb
   ```

   Configure the job with AWS credentials that can read/write `state/`, create/delete the lock object under `locks/`, and write pages in the page bucket. This runner is plain Ruby and does not boot Rails or ActiveRecord, so it needs no database URL or Rails secret.
3. Inspect pages below `/migration-preview/` and compare them with the live pages.

The runner acquires the lock object for its prefix with an S3 conditional PUT; a concurrent invocation logs a `WARN migration-next skipped` line to stderr and exits without changing state. A lock older than one hour (`S3RunLock::DEFAULT_STALE_AFTER_SECONDS`) is treated as left behind by a killed execution and taken over with an `If-Match` PUT, so only one waiting execution wins; this logs `WARN migration-next took over a stale S3 lock`. The job's task timeout (30 minutes in `release.yml`) must stay below that age. The runner writes state before HTML pages so a failed page upload can be repaired by the next run.

## Configuration

- `MIGRATION_NEXT_BUCKET`: defaults to `prpr-antena`.
- `AWS_REGION`: defaults to `ap-northeast-1`.
- `MIGRATION_NEXT_PREFIX`: defaults to `migration-preview`.
- `MIGRATION_NEXT_SITES_FILE`: optional path override; defaults to `config/sites.yml` in the application image.

An empty page prefix writes the live site keys and is rejected unless `MIGRATION_NEXT_ALLOW_PUBLIC=true` is also set. The release workflow sets both; a local run without them stays in `migration-preview/`.

The state schema, the run steps that change it and the lock protocol are in [state-format.md](state-format.md) (in Japanese). Another implementation that reads or writes the same files must follow it.
