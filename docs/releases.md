# Releases

A release is a tagged version with notes and files (assets): game builds,
plugin jars, installers. Find them under a repository's **Releases** tab or
with `tgp release`.

## Creating a release

Maintainers and admins create releases; testers can see and download
published ones.

```sh
tgp release create v1.2.0 build/libs/game.jar --notes-file CHANGELOG.md
tgp release create v2.0.0-rc1 --prerelease --target release/2.0
tgp release create v1.3.0 --draft -t "Spring update"     # publish later
tgp release edit v1.3.0 --draft=false
```

- **Tags.** If the tag exists, the release points at it. If not, publishing
  the release creates it at `--target` (a branch or commit; default: the
  default branch). That also starts `on: push: tags` workflows.
- **Drafts** create no tag. Only users with write access see them, so you can
  upload files and polish the notes before publishing.
- **Pre-releases** are marked as such and never become **Latest**. Latest is
  the newest published release that isn't a pre-release
  (`GET /api/v1/repos/{repo}/releases/latest`, `tgp release view`).
- **Notes** are Markdown, with the same rendering as issues.

## Files

- **Uploading.** `tgp release upload TAG FILE...` (`--clobber` replaces files
  with the same name), or drop files on the release form in the web UI.
  `FILE#label` sets a display label.
- **Size limit.** The admin's `release_asset_max_mb` setting (2 GiB by
  default), under **Admin → CI settings → Storage & retention**.
- **Downloading.** `tgp release download [TAG] -p '*.jar' -D out`, or the
  links on the release page. Download counts are shown.
- **Source code.** Every published release links the repository's source at
  its tag as `.zip` and `.tar.gz`
  (`GET /api/v1/repos/{repo}/archive?ref=TAG&format=zip`; works for any
  branch, tag or commit). `tgp release download TAG --archive zip`.

## Publishing from CI

Workflows can run when a release is published:

```yaml
on:
  release:
    types: [released]        # published (default), released or prereleased
jobs:
  build:
    steps:
      - uses: checkout       # checks out the release's tag
      - run: ./gradlew build
      - uses: upload-artifact
        with: {name: jars, path: build/libs/*.jar}
```

Job tokens can't change the repository, so attaching CI output to a release
needs a token: store a personal access token with write scope as a secret and
run `tgp release upload` with it, or download the artifact and upload it
yourself.

## Deleting

`tgp release delete TAG` deletes the release and its files; the tag stays
unless you add `--cleanup-tag`. Deletions are in the audit log.

## Storage

Release files, CI artifacts, caches and logs live in the configured blob
storage: a local directory (`[storage.local]`) or an S3-compatible bucket
(`[storage.s3]`, see `deploy/config.example.toml`). Deleting a repository
deletes its files.
