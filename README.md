<img src="assets/icon.svg" width="64" height="64" alt="">

# tgp

A self-hosted git platform for a single team: repositories, pull requests,
issues, CI with strict resource limits, and an agent-friendly CLI.

> **Status:** all ten milestones of the first version are in: setup, users,
> authentication, repositories, git over HTTPS, code browsing, the `tgp` CLI,
> pull requests with reviews and branch protection, issues with labels and
> milestones, CI (Docker runners with resource limits, artifacts and caches),
> releases, webhooks, server health, and backup/restore.
> See [docs/roadmap.md](docs/roadmap.md).

## Components

| Binary       | What it does                                                                    |
| ------------ | ------------------------------------------------------------------------------- |
| `tgp-server` | API, web UI, git over HTTPS, CI scheduler. Stateless; scale out with Postgres.  |
| `tgp`        | Command line client. `--json` on every command for scripts and agents.          |
| `tgp-runner` | CI agent. Pulls jobs from the server and runs each one in a Docker container (or on the host, if allowed). |

## Building

Requirements: Go 1.27+, Node 24+ with pnpm, and `git` 2.43+ on the server host (tested with 2.47).

```sh
make            # build the web UI, then bin/tgp-server, bin/tgp, bin/tgp-runner
make test       # Go + web tests
make lint
make generate   # regenerate code from api/openapi.yaml after editing it
make e2e        # browser tests (set CHROMIUM_PATH to use a system Chromium)
```

To review the web UI, `cd web && TGP_SHOTS=/tmp/shots pnpm e2e tour` seeds a
server with sample data and screenshots every page in the dark and light
themes (run `make web build` first). In the UI, press <kbd>?</kbd> for
keyboard shortcuts and <kbd>Ctrl</kbd>/<kbd>⌘</kbd>+<kbd>K</kbd> for the
command palette.

## Running

```sh
./bin/tgp-server                      # SQLite + data in ./data, listens on :3000
./bin/tgp-server -c deploy/config.example.toml
./bin/tgp-server migrate status
```

### First start

On first start `tgp-server` prints a one-time **setup token** to its log.
Open the web UI, enter the token, then create the first admin, name the org
and optionally add your team. Each new person gets a temporary password
(valid 72 hours) and picks their own password at first login, with optional
two-factor authentication. See [docs/authentication.md](docs/authentication.md).

Admins create repositories; clone and push over HTTPS with your username
and a personal access token as the password. See
[docs/repositories.md](docs/repositories.md).

### CLI

```sh
tgp auth login --host git.example.com   # stores a token; offers to set up git
tgp repo clone game-server              # git push/pull then use your tgp login
tgp repo list --json=name,clone_url     # every command: --json, --jq, stable exit codes
```

See [docs/cli.md](docs/cli.md).

### CI

Add workflows under `.tgp/workflows/` (GitHub Actions-like YAML) and connect
one or more `tgp-runner` machines with Docker:

```sh
tgp admin runner token                  # or Admin → Runners → Add runner
tgp-runner register --url https://git.example.com --token tgp_reg_…
tgp-runner run
```

See [docs/ci.md](docs/ci.md) for the workflow syntax and resource limits,
including host jobs that build and push images. Check workflow files
locally with `tgp workflow lint`.

### Container registry and labeler

Images live at `<host>/<org>/<repo>[/<name>]:<tag>` in the built-in
registry (`docker login` with a personal access token; CI jobs use their job
token): see [docs/registry.md](docs/registry.md). Pull requests are labeled
from `.tgp/labeler.yml` (or `.github/labeler.yml`): see
[docs/labeler.md](docs/labeler.md).

The server also creates `master.key` in the data dir. It encrypts 2FA seeds
and CI secrets: back it up with the database.

### Configuration

Configuration is a TOML file (see [deploy/config.example.toml](deploy/config.example.toml))
with `TGP_*` environment overrides.

### Production

[docs/deployment.md](docs/deployment.md) covers systemd units, Docker
Compose, Caddy and nginx, runners, monitoring (**Admin → Health**,
`tgp admin health`), upgrades and scaling out. Back up with
`tgp-server backup` and restore with `tgp-server restore`, which also moves
an installation from SQLite to PostgreSQL: see [docs/backup.md](docs/backup.md).

For UI work, run `./bin/tgp-server` and `cd web && pnpm dev`; Vite proxies
`/api` to the server on :3000.

## Repository layout

```
cmd/            entry points for the three binaries
api/            openapi.yaml: source of truth for the REST API
internal/
  api/apigen    Go server interface + client generated from the spec
  apierr        structured API errors ({"error":{"code","message"}})
  audit         security audit log
  auth          password hashing, tokens, TOTP, login rate limiting
  authz         per-repository permission levels
  config        TOML + env configuration
  db            bun (SQLite/Postgres) + embedded migrations
  git           git CLI wrapper: storage, reads, diffs, blame, smart HTTP
  githook       server-side pre/post-receive hooks
  markdown      GitHub-flavoured markdown rendering + sanitising
  model         database models
  repos         repository records and on-disk storage
  secretbox     AES-GCM encryption of secrets under the master key
  server        HTTP server, routing, authn/authz policy, API handlers
  users         user creation and management shared by setup and admin
  servercmd     tgp-server CLI (serve, migrate, backup, restore)
  backup        backup archives and restores
  cli           tgp CLI (commands, JSON/jq output, errors, credentials)
  testserver    real in-process server for tests of other packages
  runnercmd     tgp-runner CLI
web/            React + TypeScript UI, embedded into tgp-server at build time
deploy/         example config, systemd units, Docker images + Compose, proxy configs
docs/           documentation
```
