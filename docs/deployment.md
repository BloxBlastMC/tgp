# Deploying tgp

tgp is three binaries: `tgp-server` (API, web UI, git over HTTPS, CI
scheduler), `tgp-runner` (runs CI jobs in Docker, on separate machines) and
`tgp` (the command line client). This page covers running them in
production. Example files live in [`deploy/`](../deploy).

- [Choosing a setup](#choosing-a-setup)
- [systemd (binaries)](#systemd)
- [Docker Compose](#docker-compose)
- [Reverse proxy and TLS](#reverse-proxy-and-tls)
- [Runners](#runners)
- [Monitoring](#monitoring)
- [Upgrades](#upgrades)
- [Scaling out](#scaling-out)
- [Security checklist](#security-checklist)

Backups and restores have their own page: [backup.md](backup.md).

## Choosing a setup

| | Small team, one machine | Several server nodes |
| --- | --- | --- |
| Database | SQLite (default) | PostgreSQL |
| Repositories | local disk | shared storage (NFS) mounted on every node |
| Blobs (CI logs, artifacts, caches, release assets) | local disk | S3-compatible bucket |
| Runners | separate machines with Docker | same |

Starting with SQLite is fine: a backup restores into PostgreSQL later (see
[backup.md](backup.md#moving-to-postgresql)).

**Server host requirements:** Linux (or macOS), `git` 2.43 or newer, and
disk for repositories and blobs. The server binary is static and needs
nothing else. Git transfers are the main load; `[repos] max_concurrent_git`
caps how many run at once.

## systemd

1. **Binaries.** Build with `make` and install:

   ```sh
   install -m 0755 bin/tgp-server bin/tgp /usr/local/bin/
   ```

2. **User and directories.**

   ```sh
   useradd --system --home-dir /var/lib/tgp --shell /usr/sbin/nologin tgp
   install -d -m 0750 -o root -g tgp /etc/tgp
   ```

   systemd creates `/var/lib/tgp` (owned by `tgp`) from the unit's
   `StateDirectory`.

3. **Configuration.** Copy [`deploy/config.example.toml`](../deploy/config.example.toml)
   to `/etc/tgp/config.toml` (mode `0640`, group `tgp`) and set at least:

   ```toml
   data_dir = "/var/lib/tgp"

   [server]
   listen = "127.0.0.1:3000"            # only the reverse proxy reaches it
   base_url = "https://git.example.com"
   trust_proxy = true

   [database]
   driver = "sqlite"                    # or "postgres" with a postgres:// dsn
   dsn = "/var/lib/tgp/tgp.db"
   ```

   Every key also has a `TGP_*` environment variable, shown next to it in
   the example file.

4. **Service.** Install [`deploy/systemd/tgp-server.service`](../deploy/systemd/tgp-server.service):

   ```sh
   cp deploy/systemd/tgp-server.service /etc/systemd/system/
   systemctl daemon-reload
   systemctl enable --now tgp-server
   journalctl -u tgp-server -n 50       # shows the one-time setup token
   ```

   Open `https://git.example.com/setup`, enter the token, and create the
   first admin.

5. **Backups.** Install the nightly backup timer: see
   [backup.md](backup.md#scheduled-backups).

The unit runs the server with a read-only file system apart from
`/var/lib/tgp`. If repositories or blobs live elsewhere, add their paths to
`ReadWritePaths=`.

## Docker Compose

[`deploy/docker/compose.yml`](../deploy/docker/compose.yml) runs tgp with
PostgreSQL, behind Caddy, which gets a certificate automatically:

```sh
cd deploy/docker
cp .env.example .env          # set TGP_DOMAIN and POSTGRES_PASSWORD
docker compose up -d --build
docker compose logs tgp       # the setup token
```

Point the domain's DNS at the machine and open ports 80 and 443 first, so
Caddy can get the certificate.

- Repositories, blobs and the master key live in the `tgp-data` volume;
  the database lives in `pg-data`.
- The image is built from this repository
  ([`deploy/docker/Dockerfile`](../deploy/docker/Dockerfile)). The server
  runs as uid 1000, and Docker checks it with `/readyz`.
- Backups: `docker compose exec tgp tgp-server backup -o - > tgp-$(date +%F).tar.gz`.

## Reverse proxy and TLS

tgp speaks plain HTTP; put it behind a proxy that terminates TLS.
- Set `base_url` to the public `https://` URL. Clone URLs, webhooks and
  secure cookies use it. Cookies are also marked `Secure` on requests that
  arrive over HTTPS (directly, or with `X-Forwarded-Proto: https` from a
  trusted proxy); `[security] secure_cookies` overrides both.
- Set `trust_proxy = true` so the audit log and login rate limits see real
  client addresses. Only enable it when the proxy is the only way in (bind
  `listen` to `127.0.0.1` or firewall it); otherwise clients can fake their
  address.
- tgp expects **one** proxy hop: it takes the client address from the
  rightmost `X-Forwarded-For` entry (falling back to `X-Real-IP`) and ignores
  `True-Client-IP`. HTTPS is detected from `X-Forwarded-Proto: https`, which
  also turns on `Strict-Transport-Security`.

The proxy must:

- **not limit request size**, or allow at least your largest push and
  release asset. tgp enforces its own limits: `max_push_bytes`,
  `release_asset_max_mb` and `artifact_max_mb`.
- **stream** requests and responses instead of buffering them. Git
  transfers and live CI logs (server-sent events) depend on this.
- allow **long requests**: big clones take minutes, and log streams stay
  open while a job runs.

Ready-made configurations:

- **Caddy**: [`deploy/proxy/Caddyfile`](../deploy/proxy/Caddyfile). Caddy
  handles certificates itself and only needs `flush_interval -1` on top.
- **nginx**: [`deploy/proxy/nginx.conf`](../deploy/proxy/nginx.conf), with
  `client_max_body_size 0`, `proxy_request_buffering off`,
  `proxy_buffering off` and one-hour timeouts.

Serve tgp on its own host name (`git.example.com`), not under a path.

### Tailscale

tgp can't terminate TLS itself, so pick one of:

- **Recommended: `tailscale serve`.** It terminates TLS with the tailnet's
  certificate and proxies to tgp:

  ```sh
  tailscale serve --bg 3000          # https://<host>.<tailnet>.ts.net → 127.0.0.1:3000
  ```

  ```toml
  [server]
  listen = "127.0.0.1:3000"
  base_url = "https://<host>.<tailnet>.ts.net"
  trust_proxy = true
  ```

  `tailscale serve` replaces any client-sent `X-Forwarded-For` with the
  caller's tailnet address and sets `X-Forwarded-Proto: https`, so login
  rate limits and the audit log see real addresses. Caddy or nginx on the
  same host (above) work the same way.

- **Plain HTTP over the tailnet.** WireGuard already encrypts the traffic,
  but cookies can't be `Secure` and browsers get no HSTS:

  ```toml
  [server]
  listen = "100.x.y.z:3000"          # the host's Tailscale address
  base_url = "http://<host>:3000"
  trust_proxy = false                 # no proxy: never trust forwarded headers
  ```

  Don't use an `https://` `base_url` here: it makes cookies `Secure` and
  logging in over plain HTTP fails.

## Runners

Runners pull jobs from the server, so they need outbound HTTPS to it and
nothing inbound. Run them on separate machines: each job is a Docker
container, and membership of the `docker` group is root-equivalent on that
host.

**systemd:** install `tgp-runner`, `git` and Docker, then follow the
comments in [`deploy/systemd/tgp-runner.service`](../deploy/systemd/tgp-runner.service):

```sh
useradd --system --home-dir /var/lib/tgp-runner --shell /usr/sbin/nologin -G docker tgp-runner
install -d -m 0750 -o tgp-runner -g tgp-runner /etc/tgp-runner
sudo -u tgp-runner tgp-runner register -c /etc/tgp-runner/runner.toml \
  --url https://git.example.com --token tgp_reg_… --name build-01 --capacity 2
systemctl enable --now tgp-runner
```

The registration token comes from **Admin → Runners → Add runner** or
`tgp admin runner token`.

On `systemctl stop`, the runner stops taking jobs and waits for running
ones. `TimeoutStopSec` (30 minutes in the example) bounds the wait.

**Docker:** [`deploy/docker/Dockerfile.runner`](../deploy/docker/Dockerfile.runner)
builds a runner image that uses the host's Docker through its socket. The
compose file has it under the `runner` profile.

Capacity is set per runner (`--capacity`); CPU, memory and queue limits are
under **Admin → CI settings**. See [ci.md](ci.md#resource-management).

**Host jobs.** To build and push container images, set
`allow_host_jobs = "trusted"` in a runner's `runner.toml` (or register with
`--allow-host-jobs trusted`): jobs with `container: none` from pushes to the
default branch, dispatches and releases then run directly on that machine,
one at a time, with its Docker daemon. Use `"all"` only on a disposable
machine, since it runs pull request code on the host. For CPU and memory
limits on host jobs, run the runner where `systemd-run` can create scopes:
as root, or as a user with lingering enabled (`loginctl enable-linger
tgp-runner`) and cgroup delegation, and with `XDG_RUNTIME_DIR` set. The
runner logs at startup whether limits are enforced. See
[ci.md](ci.md#runners-for-host-jobs).

## Container registry

The registry is part of tgp-server at `/v2/` and needs no setup beyond a
`base_url` (image names use its host). The reverse proxy must pass large
request bodies unbuffered; the nginx and Caddy examples already do. Images
are stored in the configured blob storage (local or S3) and count against
**Admin → Registry** quotas. See [registry.md](registry.md).

## Monitoring

| What | Where |
| --- | --- |
| Liveness | `GET /healthz`: `200 ok` while the process serves requests. |
| Readiness | `GET /readyz`: `200 ready` when the database answers, `503` otherwise. Use it for load balancers. |
| Full health | **Admin → Health**, `tgp admin health`, or `GET /api/v1/admin/health` (admin token). |
| Logs | stderr; set `[log] format = "json"` for log shippers. |
| Audit | **Admin → Audit log**, `tgp admin audit`. |

**Admin → Health** runs these checks and shows the worst result at the top:

| Check | Warns when | Fails when |
| --- | --- | --- |
| `database` | | the database doesn't answer within 3 seconds |
| `migrations` | migrations are pending | |
| `storage` | | a test blob can't be written, read back and deleted |
| `disk` (each file system holding data, repositories, blobs or the database) | 90% full or more | 95% full or more |
| `master_key` | the key file is missing or readable by other users | |
| `runners` | an enabled runner is offline | jobs are queued and no runner is online |
| `ci_queue` | the oldest queued job has waited over 30 minutes | |
| `webhooks` | the oldest pending delivery is over an hour old | |

The page also shows:
- database size and latency;
- the CI and webhook queues;
- counts of users, sessions, pull requests and issues;
- **storage per repository**: the repository on disk plus its CI logs,
  artifacts, caches and release assets, largest first.

Repository sizes are measured at most every 5 minutes; **Refresh sizes**
measures them now.

`tgp admin health` exits with status 1 when a check fails, so a cron job or
monitoring agent can alert on it:

```sh
tgp admin health >/dev/null 2>&1 || notify "tgp is unhealthy"
tgp admin health --json --jq '.checks[] | select(.status != "ok")'
```

## Upgrades

1. Read the release notes.
2. Take a backup: `tgp-server backup -c /etc/tgp/config.toml`.
3. Replace the binary (or `docker compose up -d --build`).
4. Restart: `systemctl restart tgp-server`. Migrations run automatically at
   start; `tgp-server migrate status` lists them.

Downgrading isn't supported once newer migrations have run. To go back,
restore the backup taken before the upgrade, using the old version.

## Scaling out

Several `tgp-server` nodes can run behind one load balancer when:

- they share a **PostgreSQL** database (SQLite is single-node);
- `[repos] root` is the **same shared directory** on every node (NFS or
  similar, with working file locking);
- blobs are in **S3** (`[storage] type = "s3"`) or a shared directory;
- every node has the **same master key** file;
- `base_url` is the load balancer's URL.

Nodes need no session affinity: sessions, the CI queue and webhook
deliveries all live in the database.

## Security checklist

- Serve only over HTTPS, with `base_url` starting `https://`; that turns on
  secure cookies.
- Keep the server's own port private: bind `listen` to `127.0.0.1` behind a
  local proxy, or firewall it.
- Back up `master.key` and keep it `chmod 600`. Every backup includes it
  unless you pass `--no-master-key`. Without the key, CI secrets and 2FA
  seeds are lost. **Admin → Health** warns if other users can read the key.
- Backups contain password hashes and encrypted secrets alongside the key:
  store them like the server's data.
- Run runners on machines dedicated to CI.
- Keep `trust_proxy = false` unless a proxy is the only way to reach tgp.
- Leave `[webhooks] block_private_networks` unset (blocked) unless webhooks
  must reach internal services; check upgraded configs for an old explicit
  `false` (see [webhooks.md](webhooks.md)).
- To revoke a runner, remove it; disabling only stops new jobs.
