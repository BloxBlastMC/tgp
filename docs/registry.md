# Container registry

tgp-server includes a container registry, like GitHub Packages: it speaks
the OCI Distribution API under `/v2/`, so `docker`, `buildx`, `podman`,
`crane`, `skopeo` and Kubernetes pull from and push to it.

## Image names

```
<host>/<namespace>/<repo>[/<name>]:<tag>
git.example.com/bloxblast/bloxblast/issues:g1a2b3c4d5e6f
```

- `<host>` is the host of the server's base URL (`server.base_url`, or the
  one set during setup).
- `<namespace>` is the organisation's name in lowercase, with anything
  other than letters and digits turned into `-` ("BloxBlast" →
  `bloxblast`). The Packages tab shows the exact paths.
- `<repo>` is a tgp repository, lowercased. Access to images follows access
  to the repository.
- `<name>` is optional and may contain `/`: one repository can hold
  several images (`…/bloxblast/issues`, `…/bloxblast/finance`). Without it
  the image is the repository's own (`…/bloxblast/bloxblast:latest`).

Names must follow the OCI rules: lowercase letters, digits and single
separators (`.`, `_`, `__`, `-`). A repository whose lowercased name breaks
them (for example `a._b`) can't hold images.

## Logging in

Use your username and a personal access token (**Settings → Access
tokens**): `read` to pull, `write` to push.

```sh
docker login git.example.com -u mia --password-stdin < token.txt
docker push git.example.com/bloxblast/game/server:1.4.0
docker pull git.example.com/bloxblast/game/server:1.4.0
```

| You want to | You need |
| --- | --- |
| Pull | Read access to the repository (testers have it) and a token with `read` |
| Push, delete | Write access (maintainers, admins), a `write` token and an unarchived repository |

Failed logins count against the same per-IP limit as web logins.

**In CI**, use the job token instead of a personal one: steps get
`TGP_TOKEN`, `TGP_REGISTRY` and `TGP_IMAGE`, and `uses: registry-login`
logs Docker in. A job token pulls from its own repository on any run, and
pushes only on trusted runs (pushes to the default branch, manual
dispatches, releases). See [ci.md](ci.md#job-tokens).

## Browsing and deleting

- **Web:** a repository's **Packages** tab lists its images with size,
  tags and storage use against the quota. An image's page lists versions
  (manifests), newest first, with tags, platforms, size, who pushed them
  (and whether from CI), a `docker pull` line, and delete buttons for tags
  and versions (write access).
- **CLI:** `tgp package list`, `tgp package versions issues`,
  `tgp package delete issues:g1a2b3c4d5e6f` (a tag) or
  `tgp package delete issues@sha256:…` (a version and its tags). See
  [cli.md](cli.md).
- **Docker API:** `DELETE /v2/<name>/manifests/<tag or digest>` works too
  (for example `crane delete`).

Deleting a tag leaves its version; untagged versions are removed after the
retention period. Deletes are recorded in the audit log and send `package`
webhooks.

## Quota and retention

Admins set these under **Admin → Registry** or with
`tgp admin registry set`:

| Setting | Default | What it does |
| --- | --- | --- |
| `quota_mb` | 10240 | Storage per repository. A blob is counted once per repository however many images use it. A push that would go over is refused with `DENIED` and a message saying how much is used. 0 = no quota. |
| `untagged_retention_days` | 7 | Untagged versions older than this are deleted. Platform images inside a tagged multi-platform image are kept with it. |
| `keep_last_tags` | 0 | Keep only the newest N tags matching `keep_last_tags_patterns` per image; older ones are deleted (and their versions then age out as untagged). 0 = keep every tag. |
| `keep_last_tags_patterns` | `g*` | Globs of the tags `keep_last_tags` applies to, so moving tags like `master` or `latest` are never pruned. |

Garbage collection runs with the CI janitor, at most once a minute:

1. uploads nobody finished within a day are discarded;
2. surplus tags (`keep_last_tags`) are deleted;
3. untagged versions past retention are deleted;
4. blobs an image holds but none of its versions uses (an abandoned push, a
   deleted version) are released after a day;
5. blobs no image holds any more are deleted from storage after an hour;
6. images left with nothing in them disappear.

The grace periods keep a push in progress (layers first, manifest last)
from losing its layers.

## Storage

Blobs are content-addressed (`registry/blobs/sha256/<digest>`) in the same
blob storage as CI artifacts (local disk or S3, see
[deployment.md](deployment.md)), stored once however many repositories use
them. Manifests and tags live in the database. Uploads in progress are kept
as parts in blob storage with their hash state in the database, so chunked
uploads work across several server nodes; on S3 each finished blob is
assembled from its parts with one more copy.

`tgp-server backup` includes the registry's database tables and, with
local storage, its blobs; see [backup.md](backup.md).

## Behind a proxy

Pushes send large request bodies: the proxy in front of tgp-server must not
limit or buffer them (`client_max_body_size 0; proxy_request_buffering off;`
in nginx; Caddy needs nothing). The examples in `deploy/proxy` already do
this for every path, which covers `/v2/`.

## Not supported

- The referrers API (`/v2/<name>/referrers/…`); clients fall back to tags.
- Docker schema 1 manifests.
- Anonymous pulls: every request needs a token.
- Range requests on blobs (clients don't need them for normal pulls).
