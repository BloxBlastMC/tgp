# CI

tgp runs workflows from `.tgp/workflows/*.yml` (or `.yaml`) in each
repository. The format is close to GitHub Actions, so most of what you know
carries over. **Every job runs in a Docker container** on a separate machine
running `tgp-runner`, with CPU, memory and process limits the admins set.

```yaml
# .tgp/workflows/ci.yml
name: CI
on:
  push:
    branches: [main]
  pull_request:
jobs:
  test:
    container: gradle:8-jdk21
    resources: {cpus: 2, memory: 4g}
    steps:
      - uses: checkout
      - run: ./gradlew test
```

Push it, and the run shows up under the repository's **CI** tab, on the
pull request, and in `tgp run list`.

## Setting up runners (admins)

1. **Admin → Runners → Add runner** (or `tgp admin runner token`) gives a
   one-time registration token and a ready-to-paste command.
2. On a machine with Docker, as a user that can run `docker`:

   ```sh
   tgp-runner register --url https://git.example.com --token tgp_reg_… \
     --name build-01 --labels linux,docker,amd64 --capacity 2
   tgp-runner run
   ```

   `register` writes the runner's config (with its secret token, mode 0600)
   to `~/.config/tgp-runner/runner.toml`, or `/etc/tgp-runner/runner.toml`
   for root (`--config` / `TGP_RUNNER_CONFIG` override). `--capacity` is how
   many jobs it runs at once.
3. `tgp-runner run` takes jobs until interrupted. The first Ctrl-C (or
   SIGTERM) stops taking jobs and waits for running ones. A second cancels
   them.

The runner host needs Docker and `git`. Checkouts happen on the host and are
copied into the job's container, so job images don't need git. Job images
need `sh` and `tail` (every common base image has them).

A runner reports its CPU and memory load every 10 seconds. One that stays
silent past `runner_offline_after_seconds` (default 60) is offline, and its
running jobs fail with "The runner stopped responding". Runners can be
renamed, disabled (they finish their current jobs, then get no new ones) and
removed under Admin → Runners.

## Resource management

The scheduler starts a queued job only when **every** limit allows it. All
limits are under **Admin → CI settings** (`tgp admin ci view|set`); 0 means
no limit.

| Limit | Default | What it does |
| --- | --- | --- |
| `max_concurrent_jobs` | 0 | Jobs running at once across the whole instance. |
| runner capacity | 1 | Per runner (`--capacity`). |
| `runner_max_cpu_percent` | 90 | Runners reporting more CPU load (load average ÷ cores) get no new jobs. |
| `max_jobs_per_repo` | 4 | Jobs one repository may run at once. |
| `strategy.max-parallel` | — | Per matrix, in the workflow. |
| `max_queued_runs_per_repo` | 50 | Unstarted runs per repository. When full, the oldest unstarted run for the same workflow and branch is superseded; if there is none, the new run is rejected with that reason. |
| `cancel_superseded` | on | A new push to a branch or pull request cancels that workflow's older runs for it. Not on the default branch or tags, where every commit gets its run. |
| `default_cpus` / `max_cpus` | 2 / 4 | `docker run --cpus`. Jobs ask with `resources.cpus`. |
| `default_memory_mb` / `max_memory_mb` | 4096 / 8192 | `--memory` (and `--memory-swap`, so no swap). |
| `default_pids` / `max_pids` | 4096 / 16384 | `--pids-limit`. |
| `default_timeout_minutes` / `max_timeout_minutes` | 60 / 360 | Job timeout; the runner stops the job, and the server enforces it too. |
| `max_log_mb` | 32 | Log size per job; later output is dropped with a notice. |
| `log_retention_days` | 90 | Logs of jobs older than this are deleted. |
| `artifact_max_mb` | 2048 | Largest artifact (zipped) a job may upload. A single cache can't be bigger either. |
| `artifact_retention_days` | 90 | Artifacts are deleted this long after upload. Workflows may ask for less with `retention-days`. |
| `cache_quota_mb` | 10240 | Cache space per repository. Above it, the least recently used caches are evicted. 0 = no limit. |
| `cache_retention_days` | 7 | Caches nobody restored for this long are deleted. |
| `release_asset_max_mb` | 2048 | Largest file that can be attached to a release. |

A workflow that asks for more than a maximum fails straight away with a clear
reason ("asks for 8 CPUs; this instance allows at most 4"). It isn't silently
capped.

**Fairness.** When several repositories have queued jobs, the repository
with the fewest running jobs goes next. Ties go to the one that started a job
least recently, and within a repository the oldest job goes first. One busy
repository can't starve the others.

**Why is my job waiting?** Queued jobs show the reason in the UI and in
`tgp run view`. Examples: "no runner has labels gpu", "this repository is
running 4 jobs (limit 4)", "Waiting for a free runner (2 matching, all busy
or overloaded)". A job nobody picks up within 24 hours is cancelled.

## Workflow syntax

### Triggers (`on`)

| Event | Filters |
| --- | --- |
| `push` | `branches`, `branches-ignore`, `tags`, `tags-ignore`, `paths`, `paths-ignore` |
| `pull_request` | `branches` / `branches-ignore` (the base branch), `paths`, `paths-ignore`, `types` (`opened`, `synchronize`, `reopened`, `ready_for_review`; default: the first three) |
| `workflow_dispatch` | `inputs` (`string`, `boolean`, `number`, `choice` with `options`; `required`, `default`, `description`) |
| `release` | `types`: `published` (any release is published, the default), `released` (a release that isn't a pre-release), `prereleased`. The run is for the release's tag; `github.event.release` has `tag_name`, `name`, `body`, `prerelease`, `target_commitish` and `html_url`. |

- **Patterns.** `*` matches anything but `/`, and `**` matches anything.
  `?` and `+` repeat the previous character, `[…]` is a character class, and
  a leading `!` excludes. These are GitHub's rules.
- **Branch versus tag filters.** With only branch filters, tag pushes don't
  run, and vice versa.
- **Paths.** Path filters compare against the previous tip. For a new branch
  they compare against where it forked from the default branch; for a pull
  request, against the merge base.
- **Skipping CI.** A commit message containing `[skip ci]`, `[ci skip]`,
  `[no ci]` or `[skip actions]` starts no push or pull request runs.
- **Releases.** Unlike GitHub, `on: release` without `types` runs once per
  published release, not once per activity type. Publishing a release
  whose tag didn't exist also counts as a tag push for `on: push: tags`.
- **Pull request commits.** Pull request runs use the head commit, with
  `github.ref` = `refs/pull/N/head`.

`schedule` and other GitHub events aren't supported (yet).

### Jobs

```yaml
jobs:
  build:
    name: Build ${{ matrix.java }}  # default: the job id, plus matrix values
    runs-on: [linux, gpu]           # runner labels; omit for any runner
    container:                      # default image: the admin's default_image
      image: eclipse-temurin:${{ matrix.java }}
      env: {GRADLE_OPTS: -Xmx2g}
    services:                       # reachable by name on the job's network
      db:
        image: postgres:17
        env: {POSTGRES_PASSWORD: test}
    needs: [lint]
    if: github.ref == 'refs/heads/main'
    strategy:
      fail-fast: true               # a failing matrix job cancels its siblings
      max-parallel: 2
      matrix:
        java: [17, 21]
        include: [{java: 21, experimental: true}]
        exclude: []
    env: {CI_PROFILE: fast}
    timeout-minutes: 30
    continue-on-error: false
    resources: {cpus: 2, memory: 4g, pids: 1024}
    outputs:
      version: ${{ steps.ver.outputs.version }}
    defaults:
      run: {shell: bash, working-directory: app}
    steps: [...]
```

Matrix expansion follows GitHub's rules, including how `include` merges and
adds combinations. A workflow can have at most 256 jobs after expansion.

### Steps

```yaml
steps:
  - uses: checkout                 # also accepted: actions/checkout@v4
    with: {fetch-depth: 0, path: src, ref: other-branch}
  - id: ver
    name: Work out the version
    run: echo "version=1.$TGP_RUN_NUMBER" >> "$TGP_OUTPUT"
  - run: ./deploy.sh
    env: {TOKEN: "${{ secrets.DEPLOY_TOKEN }}"}
    if: github.event_name == 'push'
    shell: bash                    # bash (default if present), sh, python, pwsh, node, or "cmd {0}"
    working-directory: app
    timeout-minutes: 10
    continue-on-error: true
```

- **Built-in steps.** `checkout`, `upload-artifact`, `download-artifact` and
  `cache` (see below). `actions/…@vN` names are accepted. Third-party actions
  aren't supported; use `run:`.
- **Shells.** `bash` runs with `-eo pipefail`, so the first failing command
  fails the step. `sh` runs with `-e`.
- **Step files.** Steps can write to:
  - `$TGP_OUTPUT` (alias `$GITHUB_OUTPUT`): step outputs, as `name=value`
    lines or `name<<EOF … EOF` blocks;
  - `$TGP_ENV` (alias `$GITHUB_ENV`): environment variables for later steps;
  - `$TGP_PATH` (alias `$GITHUB_PATH`): directories to add to `PATH`.
- **Log commands.** `::add-mask::value` hides a value in later output.
  `::group::title` … `::endgroup::` fold lines in the UI. `::error::` and
  `::warning::` lines are highlighted.

### Artifacts

Artifacts are files a job keeps after it ends: build output, test reports.
They're zipped, kept for `artifact_retention_days` (90 by default) and can
be downloaded from the run page, with `tgp artifact download`, or by later
jobs of the same run.

```yaml
jobs:
  build:
    steps:
      - uses: checkout
      - run: make dist
      - uses: upload-artifact            # or actions/upload-artifact@v4
        with:
          name: dist                     # default "artifact"
          path: |                        # one pattern per line; ! excludes
            dist/
            !dist/**/*.map
          retention-days: 7              # at most the admin's limit
          if-no-files-found: error       # warn (default), error or ignore
          # overwrite: true              # replace an artifact of the same name in this run
          # include-hidden-files: true   # files and folders starting with . are left out by default
          # compression-level: 0         # 0 (store) to 9; default 6
  test:
    needs: build                         # artifacts are visible once the uploading job finished
    steps:
      - uses: download-artifact
        with: {name: dist, path: dist}   # without name: every artifact, each into path/<name>/
      - run: ./dist/game --selftest
```

- **Paths.** Relative paths are relative to the workspace; `~` is the home
  directory. `*` matches within a directory, `**` across directories, `?` one
  character. Files are stored relative to the common directory of the
  patterns (`dist/app` above becomes `app` in the zip).
- **Symbolic links** aren't stored in artifacts.
- **Re-runs.** A re-run sees artifacts uploaded by earlier attempts, and its
  own uploads replace them for later jobs.
- `download-artifact` also accepts `pattern` (a glob over artifact names) and
  `merge-multiple: true` (download everything into `path` itself).

### Caches

A cache saves dependency directories between runs:

```yaml
- id: deps
  uses: cache                            # or actions/cache@v4
  with:
    key: gradle-${{ hashFiles('**/*.gradle.kts', 'gradle/wrapper/gradle-wrapper.properties') }}
    restore-keys: |                      # fall back to the newest cache with one of these prefixes
      gradle-
    path: |
      ~/.gradle/caches
      ~/.gradle/wrapper
- run: ./gradlew build
```

- **Lookup.** The step restores the cache with exactly `key`. Failing that,
  it restores the newest cache whose key starts with a `restore-keys` prefix.
  The paths must match too: the same key with different paths is a different
  cache.
- **Saving.** If there was no exact hit, the cache is saved when the job
  succeeds, after its last step (in "Complete job"). A key, once saved, isn't
  overwritten: change the key (that's what `hashFiles` is for).
- **Branches.** A run reads caches saved by its own branch first, then the
  pull request's base branch, then the default branch. It only saves to its
  own branch, so a pull request can't change what `main` restores.
- **Outputs.** `steps.<id>.outputs.cache-hit` is `true` for an exact hit;
  `cache-matched-key` is the restored key.
- **Options.** `lookup-only: true` checks without downloading;
  `fail-on-cache-miss: true` fails the step when nothing matches.
- **Limits.** Each repository has `cache_quota_mb` (10 GiB by default);
  saving above it evicts the least recently used caches. Caches unused for
  `cache_retention_days` (7) are deleted. Maintainers can delete caches
  under **CI → Caches** or with `tgp cache delete`.
- **When the cache service fails**, the step warns and the job continues
  without the cache.

### Expressions

`${{ … }}` works in names, `run`, `env`, `with`, `if`, `runs-on`, images
(matrix values only), `outputs` and `concurrency`. An `if:` can leave out the
`${{ }}`. Without a status function it means `success() && (…)`.

- **Contexts:**
  - `github` (alias `tgp`): `event_name`, `ref`, `ref_name`, `ref_type`,
    `sha`, `repository`, `repository_owner`, `run_id`, `run_number`,
    `run_attempt`, `actor`, `workflow`, `job`, `head_ref`, `base_ref`,
    `server_url`, `api_url`, `event` (`before`/`after`, `pull_request`,
    `inputs`);
  - `env`, `matrix`, `strategy`, `needs.<job>.result` / `.outputs`,
    `steps.<id>.outputs` / `.outcome` / `.conclusion`, `inputs`, `secrets`,
    `job.status` and `runner`.
- **Operators:** `== != < <= > >= && || !`, with `.prop`, `['prop']` and
  `.*.prop`.
- **Functions:** `contains`, `startsWith`, `endsWith`, `format`, `join`,
  `toJSON`, `fromJSON`, `hashFiles`, `success`, `failure`, `cancelled`,
  `always`. `hashFiles('**/go.sum', …)` hashes the matching workspace files
  (`''` if none match) and works in steps.

Steps also get `CI=true` and `TGP_*` / `GITHUB_*` variables (`SHA`, `REF`,
`REF_NAME`, `REPOSITORY`, `RUN_ID`, `RUN_NUMBER`, `RUN_ATTEMPT`, `ACTOR`,
`EVENT_NAME`, `HEAD_REF`, `BASE_REF`, `SERVER_URL`, `WORKFLOW`, `JOB`,
`WORKSPACE`), plus `RUNNER_TEMP` and `RUNNER_OS`.

### Concurrency

```yaml
concurrency:
  group: deploy-${{ github.ref_name }}
  cancel-in-progress: false
```

At most one run per group is active, and one more waits; a newer waiting run
replaces the older one. With `cancel-in-progress: true`, a new run cancels
the active one instead. Groups are per repository.

### Validation

Workflow files are checked strictly: unknown keys, bad expressions, contexts
used where they aren't available, unknown `needs`, dependency cycles and
oversized matrices are all errors, reported with line and column.

- Invalid files are flagged in the CI tab and in `tgp workflow list`.
- Pushing an invalid file creates a failed run that shows the first error.

## Secrets

Workflows reference secrets as `${{ secrets.NAME }}`.

- **Who sets them.**
  - Repository secrets (Settings → Secrets, or `tgp secret set NAME`) need
    write access.
  - Organisation secrets (Admin → CI settings, or `tgp secret set --org`)
    need an admin, and are available to every repository.
  - A repository secret overrides an organisation secret of the same name.
- **Storage.** Values are encrypted with the server's `master.key` and
  can't be read back through the API or UI.
- **What a job receives.** Only the secrets the job references are sent to
  the runner.
- **Masking.** Secret values (and each line of multi-line ones) are replaced
  with `***` in logs.
- **How they reach the container.** The runner passes them to Docker through
  the environment, never on the command line, so they don't appear in the
  host's process list.
- **Names.** Letters, digits and `_`, stored upper-case. `TGP_` and
  `GITHUB_` are reserved.

Runs started by any push can use secrets. Testers can't push, so only
maintainers and admins can change what a workflow does with them.

## Runs, re-runs and cancelling

| Who | Can |
| --- | --- |
| Anyone with read access | See runs, jobs and logs |
| Testers (triage) | Re-run a finished run, its failed jobs, or one job |
| Maintainers (write) | Also cancel runs and start `workflow_dispatch` workflows |

- **Re-runs** start a new attempt with the same commit, workflow file and
  inputs.
- **"Re-run failed jobs"** keeps the results, outputs and logs of the jobs
  that passed. Failed, cancelled and skipped jobs, and the jobs that need
  them, run again.
- **Cancelling** stops queued jobs at once. Running jobs are stopped by their
  runner, which removes the containers; after 2 minutes the server marks them
  cancelled anyway.

## Checks and branch protection

Every job is a **check** named `<workflow> / <job>`, e.g. `CI / test` or
`CI / build (21)`.

- **Where checks show.** Pull requests show the checks of their head commit,
  and `tgp pr checks` prints them.
- **Required checks.** A protection rule can list required checks. The pull
  request can't be merged until each has passed or been skipped. A required
  check that hasn't reported yet counts as pending.
- **Admin bypass.** Admins can bypass if the rule allows it, as with other
  rules.

## Logs

- **Format.** Logs are stored per job in the configured storage (local disk
  by default, `storage.local.path`). Each line keeps its step and timestamp.
- **Live view.** The UI follows running jobs live over server-sent events.
- **CLI.**
  - `tgp run logs` prints logs, prefixed with job and step.
  - `--failed` limits it to failed jobs and their failed steps.
  - `--tail N` keeps the last N lines.
  - `--follow` streams a running job.
- **API.**
  - `GET /api/v1/repos/{repo}/jobs/{id}/logs?after=N` pages through lines.
  - `…/logs/stream` is the SSE stream: `line`, `job` and `end` events.

## CLI

```sh
tgp run list --status failure            # recent runs; --json for scripts
tgp run view 34                          # jobs, steps, wait/failure reasons
tgp run logs 34 --failed --tail 50       # what broke
tgp run watch 34 --exit-status           # wait; exit 1 unless it succeeded
tgp run rerun 34 --failed                # testers can re-run
tgp run cancel 34
tgp workflow list                        # files, triggers, errors, inputs
tgp workflow run deploy.yml -f env=staging --ref main
tgp pr checks --watch                    # exit 0 passed, 1 failed, 8 pending
tgp secret set DEPLOY_KEY < key.pem
tgp admin runner list|view|token|edit|delete
tgp admin ci view
tgp admin ci set max_jobs_per_repo=2 max_concurrent_jobs=8
tgp artifact list --run 34               # a run's artifacts
tgp artifact download 34 -n dist -D out  # download and unpack
tgp cache list                           # usage against the quota
tgp cache delete --key gradle-abc123
```

## Runner protocol

Runners talk to `/api/runner/v1` with their token; the protocol is documented
in `internal/ci/runnerapi`.

- **Fetching.** Runners long-poll `fetch` for jobs and heartbeat every 10
  seconds.
- **Logs and results.** Runners stream logs in numbered batches; retries are
  deduplicated. Steps and results are reported with `update` and `complete`.
- **Lost jobs.** If a runner restarts and no longer reports a job it had,
  that job fails at once, without waiting for the offline timeout.
- **Job tokens.** Each job gets a token that can clone its own repository
  (read-only) only while the job is running.
- **Artifacts and caches** move as raw request bodies under
  `/jobs/{id}/artifacts` and `/jobs/{id}/caches`, only while the job runs.
  The runner builds zips and cache archives itself (with `docker cp`), so job
  images don't need `zip` or `tar`.

## Not yet supported

- Scheduled (cron) workflows.
- Reusable workflows.
- Third-party actions.
- Job-level `concurrency`.
- `if: always()` steps after a cancellation (cancelling stops the job).
