<img src="assets/icon.svg" width="64" height="64" alt="">

# tgp

A self-hosted git platform for a single team: repositories, pull requests,
issues, CI with strict resource limits, and an agent-friendly CLI.

> **Status:** early development. This is the project skeleton (milestone 1).
> See [docs/roadmap.md](docs/roadmap.md) for what's coming.

## Components

| Binary       | What it does                                                                    |
| ------------ | ------------------------------------------------------------------------------- |
| `tgp-server` | API, web UI, git over HTTPS, CI scheduler. Stateless; scale out with Postgres.  |
| `tgp`        | Command line client. `--json` on every command for scripts and agents.          |
| `tgp-runner` | CI agent. Pulls jobs from the server and runs each one in a Docker container.   |

## Building

Requirements: Go 1.27+, Node 24+ with pnpm, and `git` on the server host.

```sh
make            # build the web UI, then bin/tgp-server, bin/tgp, bin/tgp-runner
make test       # Go + web tests
make lint
make generate   # regenerate code from api/openapi.yaml after editing it
```

## Running

```sh
./bin/tgp-server                      # SQLite + data in ./data, listens on :3000
./bin/tgp-server -c deploy/config.example.toml
./bin/tgp-server migrate status
```

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
  config        TOML + env configuration
  db            bun (SQLite/Postgres) + embedded migrations
  server        HTTP server, routing, SPA serving
  servercmd     tgp-server CLI
  cli           tgp CLI
  runnercmd     tgp-runner CLI
web/            React + TypeScript UI, embedded into tgp-server at build time
deploy/         example configuration and deployment files
docs/           documentation
```
