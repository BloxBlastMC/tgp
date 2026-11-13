# Roadmap

Each milestone ships as its own pull request.

1. **Skeleton**: Go module, config, DB + migrations (SQLite/Postgres), three binaries, OpenAPI codegen, embedded web UI, build + CI. ✅
2. **Auth & setup**: first-start wizard (setup token), users/roles, temp passwords + forced change, TOTP 2FA + recovery codes, sessions, personal access tokens, audit log, admin panel (users, org, audit). ✅ Per-repository grants land with repositories in milestone 3.
3. **Repos & git**: create/list/rename/archive/delete repos, per-user repository grants, git smart HTTP push/pull with authorization, code browsing API + UI (tree, file, blame, history, commit diffs, README), pre/post-receive hooks. ✅
4. **CLI foundation**: auth + git credential helper, repo commands, `--json`/`--jq`/error/exit-code framework, `tgp api`, `tgp schema`.
5. **Pull requests & reviews**: PR lifecycle, diffs, inline comments, reviews, merge/squash/rebase, branch protection.
6. **Issues, labels, milestones.**
7. **CI core**: workflow YAML, triggers, resource-limited scheduler, runner protocol, Docker executor, live logs, secrets.
8. **Artifacts, cache, releases**: local/S3 storage, retention.
9. **Webhooks**: signed deliveries, retries, delivery log.
10. **Admin & ops**: server health, CI limits UI, deployment docs, backup/restore.

## Roles

| Role       | Access                                                                                                             |
| ---------- | ------------------------------------------------------------------------------------------------------------------ |
| Admin      | Everything, including creating repositories and the admin panel.                                                   |
| Maintainer | Read/write on granted repos (all or a selected list): push, merge, manage branches, CI and labels.                 |
| Tester     | On granted repos: clone, issues, PR comments, approve / request changes, re-run CI. No pushes to protected branches, no merges. |
