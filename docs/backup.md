# Backup and restore

`tgp-server backup` writes one archive with everything needed to rebuild
an installation. `tgp-server restore` loads it into a new, empty one,
including onto another database engine.

## What a backup holds

| Part | Included | Notes |
| --- | --- | --- |
| Database | always | every table, as a database-neutral dump |
| Repositories | always | every repository known to the database |
| Master key | by default | `--no-master-key` leaves it out |
| CI logs, artifacts, release assets | with local storage | `--no-storage` leaves them out; never included from S3 |
| CI caches | with local storage | `--no-caches` leaves out the caches and their records; jobs rebuild them |

Not included:
- the config file, which holds database passwords and S3 keys and which
  you manage yourself;
- blobs in **S3** storage: back up the bucket with your object store's
  versioning or replication;
- files the server recreates at start (git hook scripts, the git home
  directory).

The archive is a `.tar.gz` created with mode `0600`. It holds the master
key next to the encrypted CI secrets and 2FA seeds, plus password hashes:
**store backups as carefully as the server itself**.

## Taking a backup

```sh
tgp-server backup -c /etc/tgp/config.toml                     # ./tgp-backup-<time>.tar.gz
tgp-server backup -c /etc/tgp/config.toml -o /backups/tgp.tar.gz --no-caches
tgp-server backup -c /etc/tgp/config.toml -o - | ssh backup 'cat > tgp.tar.gz'
tgp-server backup -c /etc/tgp/config.toml --json             # result as JSON
```

Run it as the server's user (`sudo -u tgp`), on the server host, **while
the server keeps running**:
- The database is read from a consistent snapshot. On SQLite this is a
  copy made with `VACUUM INTO`, in the database's directory; on PostgreSQL
  a repeatable-read transaction. The server doesn't stall while it runs.
- Repositories are copied refs first and objects last. A push that lands
  during the copy is either fully in the backup or not at all.
- The archive is written to a temporary file and renamed when complete,
  so a failed backup never leaves a truncated archive behind.

The only gap: something created after the database snapshot, such as a
repository or an uploaded asset, may be missing from the backup. Something
deleted during the backup may leave a database row without its file. For a
backup that is exact to the second, stop the server first.

### Scheduled backups

[`deploy/systemd/tgp-backup.timer`](../deploy/systemd/tgp-backup.timer)
runs [`tgp-backup.service`](../deploy/systemd/tgp-backup.service) every
night at about 03:15. The service:
- writes `/var/backups/tgp/tgp-<time>.tar.gz` without caches;
- deletes archives older than 14 days.

```sh
cp deploy/systemd/tgp-backup.service deploy/systemd/tgp-backup.timer /etc/systemd/system/
systemctl daemon-reload
systemctl enable --now tgp-backup.timer
systemctl start tgp-backup          # take one now
systemctl list-timers tgp-backup.timer
```

Copy the archives off the machine as well (rsync, restic, an S3 bucket).

With Docker Compose:

```sh
docker compose exec -T tgp tgp-server backup -o - > tgp-$(date +%F).tar.gz
```

## Restoring

A restore needs an **empty** installation:
- a new database: delete the SQLite file, or create a new PostgreSQL
  database;
- a repository directory without repositories.

It refuses to overwrite anything.

1. Install the same tgp version as the backup, or a newer one, and write
   the config file. `tgp-server version` prints the installed version; the
   backup's version is in its `manifest.json`
   (`tar -xOzf backup.tar.gz manifest.json`).
2. Stop the server: `systemctl stop tgp-server`.
3. Restore:

   ```sh
   sudo -u tgp tgp-server restore -c /etc/tgp/config.toml /backups/tgp-20261114-031500.tar.gz
   ```

4. Start the server: `systemctl start tgp-server`. Everyone signs in with
   their old passwords and tokens.

The restore:
- brings the database to the backup's schema, loads every table, checks
  references, then applies migrations newer than the backup. Restoring an
  older backup into a newer tgp therefore works; restoring a newer backup
  into an older tgp is refused.
- writes repositories into `[repos] root`, and blobs into the configured
  storage. That can be S3 even if the backup came from local storage.
- writes the backup's master key to `master_key_file`. A different key
  already there is kept as `master.key.replaced-<time>`.

If a restore fails partway, empty the database and repository directory
and run it again.

Test your backups: restore one onto a scratch machine or into a scratch
directory now and then:

```sh
mkdir /tmp/tgp-check && TGP_DATA_DIR=/tmp/tgp-check TGP_DB_DSN=/tmp/tgp-check/tgp.db \
  TGP_REPOS_ROOT=/tmp/tgp-check/repos TGP_STORAGE_PATH=/tmp/tgp-check/storage \
  TGP_MASTER_KEY_FILE=/tmp/tgp-check/master.key tgp-server restore /backups/latest.tar.gz
```

## Moving to PostgreSQL

The dump is database-neutral, so a backup moves an installation between
SQLite and PostgreSQL in either direction:

1. `tgp-server backup -o tgp.tar.gz` on the old setup.
2. Create an empty PostgreSQL database, and point `[database]` at it
   (`driver = "postgres"`, `dsn = "postgres://…"`).
3. `tgp-server restore tgp.tar.gz` with the new config, then start the
   server.

The same steps move an installation to a new machine, or from local blob
storage to S3.

## Archive format

A gzip-compressed tar archive:

```
manifest.json            format, tgp version, source database, schema migrations, contents
db/NNN-<table>.jsonl     per table, in load order: a header line {"table","columns"},
                         then one JSON array per row; binary values are {"$b": base64},
                         timestamps {"$t": RFC 3339}
master.key
repos/<name>.git/...     bare repositories
storage/<key>            blobs, under their storage keys
```

`tgp-server restore` reads the format version in `manifest.json` and
refuses archives newer than it understands.
