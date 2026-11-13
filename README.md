<img src="assets/icon.svg" width="64" height="64" alt="">

# tgp

A self-hosted git platform for a single team: repositories, pull requests,
issues, CI with strict resource limits, and an agent-friendly CLI.

> **Status:** early development: setup, users, authentication, repositories,
> git over HTTPS, code browsing, the `tgp` CLI, pull requests with reviews and
> branch protection work; issues come next.
> See [docs/roadmap.md](docs/roadmap.md).

## Components

| Binary       | What it does                                                                    |
| ------------ | ------------------------------------------------------------------------------- |
| `tgp-server` | API, web UI, git over HTTPS, CI scheduler. Stateless; scale out with Postgres.  |
| `tgp`        | Command line client. `--json` on every command for scripts and agents.          |
| `tgp-runner` | CI agent. Pulls jobs from the server and runs each one in a Docker container.   |

## Building

Requirements: Go 1.27+, Node 24+ with pnpm, and `git` 2.43+ on the server host (tested with 2.47).

```sh
make            # build the web UI, then bin/tgp-server, bin/tgp, bin/tgp-runner
make test       # Go + web tests
make lint
make generate   # regenerate code from api/openapi.yaml after editing it
make e2e        # browser tests (set CHROMIUM_PATH to use a system Chromium)
```

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

The server also creates `master.key` in the data dir. It encrypts 2FA seeds
(and later CI secrets): back it up with the database.

### Configuration

Configuration is a TOML file (see [deploy/config.example.toml](deploy/config.example.toml))
with `TGP_*` environment overrides.

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
  servercmd     tgp-server CLI
  cli           tgp CLI (commands, JSON/jq output, errors, credentials)
  testserver    real in-process server for tests of other packages
  runnercmd     tgp-runner CLI
web/            React + TypeScript UI, embedded into tgp-server at build time
deploy/         example configuration and deployment files
docs/           documentation
```
