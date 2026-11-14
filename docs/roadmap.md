# Roadmap

Each milestone ships as its own pull request.

1. **Skeleton**: Go module, config, DB + migrations (SQLite/Postgres), three binaries, OpenAPI codegen, embedded web UI, build + CI. ✅
2. **Auth & setup**: first-start wizard (setup token), users/roles, temp passwords + forced change, TOTP 2FA + recovery codes, sessions, personal access tokens, audit log, admin panel (users, org, audit). ✅ Per-repository grants land with repositories in milestone 3.
3. **Repos & git**: create/list/rename/archive/delete repos, per-user repository grants, git smart HTTP push/pull with authorization, code browsing API + UI (tree, file, blame, history, commit diffs, README), pre/post-receive hooks. ✅
4. **CLI foundation**: auth + git credential helper, repo commands, `--json`/`--jq`/error/exit-code framework, `tgp api`, `tgp schema`, admin user commands. ✅ See [cli.md](cli.md).
5. **Pull requests & reviews**: PR lifecycle, diffs, inline comments, reviews, merge/squash/rebase, branch protection, `tgp pr`. ✅ See [pull-requests.md](pull-requests.md).
6. **Issues, labels, milestones**: plus assignees, closing keywords, `#N` links, `tgp issue/label/milestone`. ✅ See [issues.md](issues.md).
7. **CI core**: workflow YAML, triggers, resource-limited scheduler, runner protocol, Docker executor, live logs, secrets, required checks, `tgp run/workflow/secret`. ✅ See [ci.md](ci.md).
8. **Artifacts, cache, releases**: `upload-artifact`/`download-artifact`/`cache` built-ins, `hashFiles`, releases with assets and source archives, `on: release`, local/S3 storage, retention and quotas, `tgp release/artifact/cache`. ✅ See [releases.md](releases.md) and [ci.md](ci.md#artifacts).
9. **Webhooks**: repository and organisation webhooks, push/create/delete, pull request, review, issue, comment, release and CI run events, HMAC signatures, ordered delivery with retries, delivery log and redelivery, `tgp webhook`. ✅ See [webhooks.md](webhooks.md).
10. **Admin & ops**: server health, CI limits UI, deployment docs, backup/restore.

## Roles

| Role       | Access                                                                                                             |
| ---------- | ------------------------------------------------------------------------------------------------------------------ |
| Admin      | Everything, including creating repositories and the admin panel.                                                   |
| Maintainer | Read/write on granted repos (all or a selected list): push, merge, manage branches, CI and labels.                 |
| Tester     | On granted repos: clone, issues, PR comments, approve / request changes, re-run CI. No pushes to protected branches, no merges. |
