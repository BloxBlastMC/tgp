# Pull request labeler

tgp labels pull requests from the files they change and their branch
names, like the `actions/labeler` GitHub Action, without a workflow: the
server applies the rules when a pull request is opened, when its branch is
pushed to and when it's reopened.

## Configuration

Put the rules in `.tgp/labeler.yml` on the default branch, or keep an
existing `.github/labeler.yml` (actions/labeler v5 format; the v4 list
shorthand works too). `.tgp/labeler.yml` wins if both exist.

```yaml
issues:
  - changed-files:
      - any-glob-to-any-file: ['services/issues/**']
finance:
  - changed-files:
      - any-glob-to-any-file: services/finance/**
docs:
  - all:
      - changed-files:
          - any-glob-to-any-file: '**/*.md'
          - all-globs-to-all-files: '!src/**'
feature:
  - head-branch: ['^feature/', 'feature']
release:
  - base-branch: '^release/'
ci: '.tgp/**'                   # v4 shorthand: any glob matches any file
sync-labels: false              # true: also remove configured labels that stop matching
```

### Rules

| Rule | Matches when |
| --- | --- |
| `changed-files` → `any-glob-to-any-file` | any glob matches any changed file |
| `any-glob-to-all-files` | one glob matches every changed file |
| `all-globs-to-any-file` | some changed file matches every glob |
| `all-globs-to-all-files` | every glob matches every changed file |
| `head-branch` | the pull request's branch matches a regular expression |
| `base-branch` | the target branch matches a regular expression |

They combine as in actions/labeler v5:

- Rules written directly under a label form one `any` group: one of them is
  enough, and within each, one option or pattern is enough.
- Each `any:` entry needs one of its rules to match.
- Each `all:` entry needs all its rules to match, with every option and
  pattern inside them.
- A label applies when all of its entries match.

Globs follow minimatch, with dot files matched: `*` and `?` stay within a
path segment, `**` spans segments, plus `[abc]`, `[!abc]` and `{a,b}`. A
leading `!` negates a glob (useful with `all-globs-to-all-files`). A rename
counts as deleting the old path and adding the new one.

## Behaviour

- **Rules come from the base branch.** A pull request that edits the
  labeler configuration doesn't change how it is labeled itself.
- **Labels are only added** (as with actions/labeler's default). Removing a
  label by hand sticks until the pull request's changes match it again on a
  later push. With `sync-labels: true`, configured labels that no longer
  match are removed.
- **Labels must exist.** Labels the repository doesn't have are skipped
  (and logged by the server), never created; create them under **Labels**.
- **Who did it.** The timeline shows the changes without a person, and
  webhooks send `pull_request` `labeled` / `unlabeled` events with the
  `tgp` bot as sender.
- **Mistakes don't block anything.** An invalid configuration is ignored
  (and logged); check it locally with `tgp workflow lint`, which validates
  the labeler config next to the workflows.
