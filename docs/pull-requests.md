# Pull requests

The same features are in the API (`/api/v1/repos/{repo}/pulls…`) and the CLI (`tgp pr`, see [cli.md](cli.md)).

## Opening a pull request
- After pushing a branch, git prints a link to `/<repo>/compare/<base>...<branch>`. You can also use
  **Pull requests → New pull request**, the **New PR** link on the Branches page, or ⌘K → "New pull request".
- The compare page shows commits, the diff, ahead/behind counts and whether the branch merges cleanly.
- Fill in the title and description (Markdown, with a preview tab), pick reviewers and optionally create the pull request as a draft.
- Opening pull requests needs write access (maintainers and admins).

## Pages
- **Repository → Pull requests** lists pull requests, filterable by state (open / merged / closed / all) and title.
- **Pull requests** in the sidebar lists them across repositories:
  - pull requests waiting for your review (the sidebar badge counts these);
  - pull requests you created;
  - all pull requests.

## Reviewing
- **Conversation**:
  - The description, comments, reviews with their inline threads, and events (pushes, base changes, merges, review requests…).
  - Threads can be replied to and resolved. Threads on code that changed since show **outdated**, together with the original diff lines.
- **Files changed**:
  - Hover a line and click **+** to comment on it.
  - **Add single comment** posts immediately.
  - **Start a review** collects comments until you submit the review.
- **Review** opens the submit dialog: Comment, Approve or Request changes. Authors can only comment. Testers can approve and request changes.
- Maintainers can dismiss an approval or change request, with a reason.

## Merging
The merge box at the bottom of the conversation shows:
- conflicts;
- the review decision;
- how far behind the base the branch is (**Update branch** merges the base into it);
- each requirement that still blocks merging.

To merge:
1. Pick a method: merge commit, squash, or rebase. The last choice is remembered.
2. Optionally edit the commit title and message.
3. Choose whether to delete the branch after merging.
4. Merge.

If a protection rule allows admins to bypass it, admins also see **Merge without requirements**. Using it is recorded in the audit log.

## Branch protection
**Repository → Settings → Branch protection** (only admins can edit):
- Rules match a branch name or a glob (`release/*`); the most specific rule applies.
- Options:
  - pull requests only;
  - required approvals;
  - dismiss stale approvals;
  - require up to date;
  - block force pushes;
  - allow deletion;
  - admins may bypass.

Protected branches are marked on the Branches page.

## How the rules work

**Who can do what**
- Opening, merging and updating branches needs write access (maintainers, admins).
- Commenting, reviewing, resolving threads and requesting reviewers needs triage access (testers and up).
- The title, description, base, draft state and closing can be changed by the author or anyone with write access.
- Changing branch protection needs an admin.

**Reviews**
- Each reviewer's latest verdict counts: approve or request changes. "Comment" reviews don't replace a verdict.
- Dismissed verdicts and the author's own reviews never count.
- With *dismiss stale approvals*, an approval only counts if it was made on the current head commit.
- A change request blocks merging under a protection rule until the reviewer approves or the review is dismissed.
- Without a rule, reviews are informational.
- Submitting a review clears your review request.
- `commit_sha` on a review is checked against the head, so you can't approve commits you haven't seen.

**Merging** happens on the server, without a work tree:
- **merge**: a merge commit, committed by you.
- **squash**: one commit. The author is the pull request's author and you are the committer. The default message lists the commits.
- **rebase**: each commit is replayed onto the base, keeping its author. If the head already contains the base, the base is fast-forwarded to the head instead. Fails if the branch contains merge commits.

The base branch is updated compare-and-swap. If someone pushes to it during a merge, the merge fails with "try again" instead of losing their push. Passing `sha` (the UI always does) merges only if the head is still at that commit.

**Pushes to protected branches** are checked in the pre-receive hook. Depending on the rule, direct pushes, force pushes and deletion are rejected, with the reason shown in `git push` output. Creating a new branch that matches a pattern is allowed. With *admins may bypass*, admins can push directly and force-push.

**Branches follow pushes**
- Pushing to a pull request's head updates it and records the push in the timeline, including force pushes.
- The server keeps `refs/pull/<n>/head` pointing at the latest head, so a pull request's commits stay fetchable after its branch is deleted: `git fetch origin refs/pull/12/head`.
- If an open pull request's head gets pushed into its base outside tgp (for example a local merge and `git push`), the pull request is marked merged, with method `manual`.
- Deleting the head branch blocks merging. Pushing the branch again restores the pull request.

**Numbers** are per repository. Milestone 6's issues will share the same sequence.
