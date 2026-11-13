# Repositories and git

## Creating repositories

Admins create repositories (Repositories → New repository, or
`POST /api/v1/repos`). Options: name, description, default branch (default
`main`) and whether to start with a README commit. Names are 1–100 letters,
digits, `.`, `-` or `_`, case-insensitively unique, and can't clash with app
routes (`admin`, `settings`, `pulls`…).

Storage: `<repos.root>/<lowercased name>.git` (bare repositories). Renaming a
repository moves its directory; existing clones must update their remote URL.

## Who can do what

| Permission | Who                                              | Allows                                                       |
| ---------- | ------------------------------------------------ | ------------------------------------------------------------ |
| read       | (tokens with only `read` scope)                  | Clone, fetch, browse code and history                        |
| triage     | Testers with access                              | read + issues, comments, reviews, CI re-runs (later milestones). **No pushes.** |
| write      | Maintainers with access                          | triage + push, change description and default branch         |
| admin      | Admins                                           | Everything: rename, archive, delete, manage access           |

A maintainer or tester has access to a repository when their account has
**All repositories** turned on, or the repository is in their list of
granted repositories (Admin → Users → edit). Repositories you can't access
behave as if they don't exist (404).

## Clone and push

```sh
git clone https://git.example.com/minigames.git
# Username: your username
# Password: a personal access token (Settings → Personal access tokens)
```

Only smart HTTP over HTTPS is supported. The token's scopes apply: `read`
can clone, `write` is needed to push. The `tgp` CLI (next milestone) sets up
a git credential helper so you don't have to paste tokens.

### Push rules

Enforced by the server's `pre-receive` hook:

- Only `refs/heads/*` (branches) and `refs/tags/*` can be pushed. Other refs
  such as `refs/pull/*` are reserved for tgp.
- The default branch can't be deleted. Change the default branch first.
- Archived repositories reject all pushes.
- Branch protection (required reviews, required CI, no force-push) arrives
  with pull requests in milestone 5.

If the first push to an empty repository doesn't include the default branch
(for example you push `master` to a repository created with `main`), the
pushed branch becomes the default.

### Limits

- `repos.max_concurrent_git` caps simultaneous clone/fetch/push operations
  (default 2 × CPU cores). Extra requests wait up to 30 seconds and then get
  `503 server busy`, so a burst of CI clones can't overload the server.
- `repos.max_push_bytes` caps a single push (default 2 GiB).

## How hooks work

`tgp-server` writes `pre-receive` and `post-receive` scripts to
`<data_dir>/hooks` on start and runs `git receive-pack` with
`core.hooksPath` pointing there, so repositories need no per-repo hook
installation and upgrades take effect immediately. The scripts exec
`tgp-server hook <kind>`, which reports the ref updates (and whether each is
a force-push) to the server process over a loopback endpoint authenticated
with a random per-process secret.

## Browsing

The web UI shows the file tree with the latest commit, rendered READMEs
(relative links and images resolve inside the repository), files with syntax
highlighting and linkable line numbers (`#L10`, `#L10-L20`), blame, history
(optionally per path) and commit diffs in unified or split view.

The raw file endpoint never serves HTML as HTML: text is `text/plain`,
images are served inline, everything else downloads, and responses carry a
sandboxing Content-Security-Policy.

Diffs are capped (500 files, 20,000 lines) to keep pages and API responses
fast; truncated diffs say so.

## API summary

| Endpoint                                   | Purpose                                   |
| ------------------------------------------ | ----------------------------------------- |
| `GET/POST /repos`                          | List accessible / create (admin)          |
| `GET/PATCH/DELETE /repos/{repo}`           | Get / update / delete                     |
| `GET /repos/{repo}/access`                 | Who has access and why                    |
| `GET /repos/{repo}/branches`, `/tags`      | Refs                                      |
| `GET /repos/{repo}/tree?ref&path`          | Directory listing                         |
| `GET /repos/{repo}/blob?ref&path`          | File content (JSON, ≤ 1 MiB)              |
| `GET /repos/{repo}/raw?ref&path`           | File bytes                                |
| `GET /repos/{repo}/readme?ref&path`        | Rendered README                           |
| `GET /repos/{repo}/commits?ref&path&page`  | History                                   |
| `GET /repos/{repo}/commits/{sha}`          | Commit with diff                          |
| `GET /repos/{repo}/blame?ref&path`         | Blame                                     |
| `POST /markdown`                           | Render markdown                           |
| `GET/PUT /admin/users/{username}/repos`    | A user's repository access (admin)        |
