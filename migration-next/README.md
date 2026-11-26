# migration-next (shadow runner)

This directory contains a DB-free fetch and static-page generator for the Cloud SQL migration. The release workflow deploys it as a separate Cloud Run job, `antena-fetch-posts-next`, next to the legacy Rails job `anten-fetch-posts`. Until the cutover the legacy job keeps writing the public pages and the new job writes preview pages under `migration-preview/`. The repository variable `MIGRATION_NEXT_MODE` (`preview` when unset, or `public`) selects where the new job writes. Remove this directory and the `deploy_migration_next` workflow job to remove the migration implementation.

For the Japanese before/after architecture and bucket roles, see [flow.md](flow.md).

The default page prefix is `migration-preview/`; preview HTML is written there while the current public site continues to use its existing keys. The site catalog is read from the checked-in `config/sites.yml`; `MIGRATION_NEXT_SITES_FILE` can override its path. The post JSON (`state/posts.json`) and the run lock (`locks/`) live in the same page bucket. They contain full feed content, so the `prpr-antena` bucket policy denies the CloudFront origin access identity on `state/*` and `locks/*`, and the bucket lifecycle expires only `sites/`, `migration-preview/` and `migration/`. Before deploying, the release workflow writes `state/deny-probe.txt` and checks that `https://prpr-antena.com/state/deny-probe.txt` returns 403.

Posts are not imported from Cloud SQL. When `state/posts.json` does not exist the runner logs a `WARN` line and starts from an empty post set, so the first run fills it from the current RSS entries (all stamped with that run's time). Bucket versioning is off; to reset a broken state, pause the job's scheduler, delete `state/posts.json`, and resume. Any other read failure (for example AccessDenied or invalid JSON) stops the run instead of overwriting the file.

## First shadow run

1. Verify `config/sites.yml` in the release image. Site and category changes are made in this file and take effect with the next release.
2. Run one preview batch (the first run creates `state/posts.json`):

   ```sh
   bundle exec ruby migration-next/bin/run.rb
   ```

   Configure the job with AWS credentials that can read/write `state/`, create/delete the lock object under `locks/`, and write pages in the page bucket. This runner is plain Ruby and does not boot Rails or ActiveRecord, so it needs no database URL or Rails secret.
3. Inspect pages below `/migration-preview/` and compare them with the live pages.

The runner acquires `locks/migration-next-run.lock` in the page bucket with an S3 conditional PUT; a concurrent invocation logs a `WARN migration-next skipped` line to stderr and exits without changing state. A lock older than one hour (`S3RunLock::DEFAULT_STALE_AFTER_SECONDS`) is treated as left behind by a killed execution and taken over with an `If-Match` PUT, so only one waiting execution wins; this logs `WARN migration-next took over a stale S3 lock`. The job's task timeout (30 minutes in `release.yml`) must stay below that age. The runner writes state before HTML pages so a failed page upload can be repaired by the next run.

## Configuration

- `MIGRATION_NEXT_BUCKET`: defaults to `prpr-antena`.
- `AWS_REGION`: defaults to `ap-northeast-1`.
- `MIGRATION_NEXT_PREFIX`: defaults to `migration-preview`.
- `MIGRATION_NEXT_SITES_FILE`: optional path override; defaults to `config/sites.yml` in the application image.

An empty page prefix writes the live site keys and is rejected unless `MIGRATION_NEXT_ALLOW_PUBLIC=true` is also set. That override is reserved for an intentional cutover. The post state remains at `state/posts.json` when the page prefix changes.

The state schema is version 1 and retains the existing post `id` as the stable page-order key. `title` maps to the current DB `name`; `created_at` remains the fetch/insert time used by current pages. `published_at` is recorded when the feed item has one.
