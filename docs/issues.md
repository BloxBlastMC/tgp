# Issues, labels and milestones

The same features are in the API (`/api/v1/repos/{repo}/issues…`, `/labels`, `/milestones`, `/issues`) and the CLI (`tgp issue`, `tgp label`, `tgp milestone`, see [cli.md](cli.md)).

## Lists
- **Repository → Issues** lists the repository's issues.
  - **Open / Closed** toggle, each with a count.
  - Filters:
    - title search;
    - **Labels** (an issue must have every label you pick; clicking a label chip in a row also filters by it);
    - milestone (any, has one, none, or a specific milestone);
    - assignee (anyone, unassigned, or a person);
    - author;
    - sort (updated, newest, comments).
  - Filters live in the URL, so filtered lists can be bookmarked and shared.
- **Issues** in the sidebar lists issues across repositories: assigned to you, created by you, or all.

## Opening an issue
- **New issue** (on the Issues tab, or ⌘K → "New issue") needs triage access, which testers have.
- Fill in a title and a Markdown description (with a preview tab).
- Optionally set labels, assignees and a milestone in the sidebar.

## The issue page
- **Title**: the author and maintainers can edit it, and the description too.
- **Timeline**: comments (authors can edit or delete them) and one-line events: labels added or removed (shown as chips), assignments, milestone changes, title changes, closing and reopening.
- **Closing**: "Close issue" closes as completed; the arrow next to it offers **Close as not planned** (won't fix, duplicate, can't reproduce). Text in the comment box is posted first ("Close with comment"). **Reopen issue** reopens.
- **Closed by a pull request**: when a merged pull request or a commit pushed to the default branch says `Fixes #12`, the issue is closed and the timeline links the pull request or commit.
- **Sidebar**: anyone with triage access can change Assignees, Labels and Milestone with the ⚙ buttons ("assign yourself" is a shortcut).
- **References**: `#12` in any Markdown links to issue or pull request 12. Issue links that point at a pull request redirect to it, and the other way round.

## Pull requests
Pull requests have the same Assignees, Labels and Milestone sidebar. You can also set them when opening one on the compare page. Labels show in pull request lists.

## Labels and milestones
- **Labels** (link at the top of the Issues tab, or ⌘K):
  - Lists every label with its description and how many open issues and pull requests have it.
  - Maintainers and admins can add, rename, recolour (colour picker, hex value or 🎲 random) and delete labels.
  - Deleting a label removes it from everything.
  - New repositories start with bug, enhancement, documentation, question, needs testing, duplicate and wontfix.
- **Milestones** (open and closed tabs):
  - Each milestone has a progress bar (closed ÷ all issues and pull requests in it).
  - The due date shows in red once past due.
  - Clicking a milestone opens the issue list filtered by it.
  - Maintainers and admins can create, edit, close, reopen and delete milestones.
  - Deleting a milestone keeps its issues, without the milestone.

## Rules

**Who can do what**
- Opening, commenting, closing, reopening, labelling, assigning and setting milestones needs triage access, so testers can file and triage bugs.
- Editing an issue's title and description needs to be the author, or write access.
- Creating, renaming and deleting labels and milestones needs write access.
- Deleting a comment needs to be its author, or an admin.

**Numbers** are shared with pull requests: `#12` is either an issue or a pull request, never both. The API answers an issue request for a pull request's number (and the reverse) with `not_found` and `details.is_pull` / `details.is_issue`.

**Closing keywords**
- `close`, `closes`, `closed`, `fix`, `fixes`, `fixed`, `resolve`, `resolves` or `resolved`, followed by `#N` (optionally with a colon: `Fixes: #12`), closes issue N as completed.
- They count in two places: a pull request merged into the default branch (its title, description and commit messages), and commits pushed directly to the default branch.
- The close event records the pull request and/or the commit.
- Only issues in the same repository are closed, never pull requests.

**Labels, assignees and milestones on the API**
- `labels` and `assignees` on `PATCH` replace the whole list.
- `add_labels`, `remove_labels`, `add_assignees` and `remove_assignees` change it without a race.
- `milestone: 0` removes the milestone.
- Every change is a timeline event, except values set when the issue or pull request is created.
- Assignees must have access to the repository; there are at most 10 per issue.

**Milestone progress** counts issues and pull requests together. Merged and closed both count as done.
