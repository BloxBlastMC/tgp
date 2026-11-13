package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/api/apigen"
)

func newPRCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "pr",
		Aliases: []string{"pull", "pulls"},
		Short:   "Work with pull requests",
		Long: `Create, review and merge pull requests.

A pull request is selected by number ("12" or "#12"), by URL, or by its head
branch name; with no argument, by the current git branch. The repository comes
from -R or the current directory's git remote.`,
	}
	cmd.AddCommand(newPRListCmd(a), newPRStatusCmd(a), newPRViewCmd(a), newPRCreateCmd(a), newPRCheckoutCmd(a),
		newPRDiffCmd(a), newPRCommentCmd(a), newPRCommentsCmd(a), newPRReviewCmd(a), newPRMergeCmd(a),
		newPRStateCmd(a, "close"), newPRStateCmd(a, "reopen"), newPREditCmd(a), newPRReadyCmd(a), newPRUpdateBranchCmd(a))
	return group(cmd)
}

var prURLRe = regexp.MustCompile(`^(https?://.+)/([^/]+)/pulls/(\d+)/?$`)

// resolvePR finds the repository and pull request number for a selector.
func (a *app) resolvePR(ctx context.Context, repoFlag, sel string) (*apiClient, string, int64, error) {
	if m := prURLRe.FindStringSubmatch(sel); m != nil {
		c, repo, err := a.resolveRepo(m[1] + "/" + m[2])
		if err != nil {
			return nil, "", 0, err
		}
		n, _ := strconv.ParseInt(m[3], 10, 64)
		return c, repo, n, nil
	}
	c, repo, err := a.resolveRepo(repoFlag)
	if err != nil {
		return nil, "", 0, err
	}
	s := strings.TrimPrefix(sel, "#")
	if n, err := strconv.ParseInt(s, 10, 64); err == nil && n > 0 {
		return c, repo, n, nil
	}
	branch := sel
	if branch == "" {
		out, err := exec.Command("git", "symbolic-ref", "--quiet", "--short", "HEAD").Output()
		if err != nil || strings.TrimSpace(string(out)) == "" {
			return nil, "", 0, &Error{Code: CodeUsage, Exit: ExitUsage, Message: "no pull request given",
				Hint: "pass a number, URL or branch, or run on a branch that has an open pull request"}
		}
		branch = strings.TrimSpace(string(out))
	}
	r, err := c.ListPullsWithResponse(ctx, repo, &apigen.ListPullsParams{Head: &branch})
	if err != nil {
		return nil, "", 0, err
	}
	page, err := must(r.JSON200, "pull request list")
	if err != nil {
		return nil, "", 0, err
	}
	if len(page.Pulls) == 0 {
		return nil, "", 0, &Error{Code: "not_found", Exit: ExitNotFound,
			Message: fmt.Sprintf("no open pull request for branch %s in %s", branch, repo),
			Hint:    fmt.Sprintf("create one with: tgp pr create --head %s", branch)}
	}
	return c, repo, page.Pulls[0].Number, nil
}

func prSelector(args []string) string {
	if len(args) > 0 {
		return args[0]
	}
	return ""
}

func stateLabel(a *app, state apigen.PullState, draft bool) string {
	switch {
	case state == apigen.PullStateMerged:
		return a.io.paint("35", "merged")
	case state == apigen.PullStateClosed:
		return a.io.Red("closed")
	case draft:
		return a.io.Dim("draft")
	}
	return a.io.Green("open")
}

func decisionLabel(d apigen.ReviewDecision) string {
	switch d {
	case apigen.ReviewDecisionApproved:
		return "approved"
	case apigen.ReviewDecisionChangesRequested:
		return "changes requested"
	case apigen.ReviewDecisionReviewRequired:
		return "review required"
	}
	return "-"
}

func printPullList(w io.Writer, a *app, pulls []apigen.PullSummary, withRepo bool) error {
	if len(pulls) == 0 {
		fmt.Fprintln(a.io.ErrOut, "No pull requests.")
		return nil
	}
	rows := make([][]string, len(pulls))
	for i, p := range pulls {
		num := "#" + strconv.FormatInt(p.Number, 10)
		if withRepo {
			num = p.Repo + num
		}
		rows[i] = []string{num, truncate(p.Title, 60), stateLabel(a, p.State, p.Draft), p.Author.Username,
			p.HeadBranch + " → " + p.BaseBranch, decisionLabel(p.ReviewDecision), ago(&p.UpdatedAt)}
	}
	return printTable(w, a.io, []string{"pr", "title", "state", "author", "branches", "review", "updated"}, rows)
}

func newPRListCmd(a *app) *cobra.Command {
	var (
		repoFlag, state, author, base, head, search, labels string
		limit                                               int
	)
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List pull requests in a repository",
		Example: `  tgp pr list
  tgp pr list --state merged --author mia
  tgp pr list --json=number,title,review_decision`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, repo, err := a.resolveRepo(repoFlag)
			if err != nil {
				return err
			}
			st := apigen.ListPullsParamsState(state)
			switch st {
			case "open", "closed", "merged", "all":
			default:
				return usageErr("invalid --state %q: use open, closed, merged or all", state)
			}
			var all []apigen.PullSummary
			for page := 1; ; page++ {
				per := 100
				p := &apigen.ListPullsParams{State: &st, Page: &page, PerPage: &per}
				if author != "" {
					p.Author = &author
				}
				if base != "" {
					p.Base = &base
				}
				if head != "" {
					p.Head = &head
				}
				if search != "" {
					p.Q = &search
				}
				if labels != "" {
					p.Labels = &labels
				}
				r, err := c.ListPullsWithResponse(cmd.Context(), repo, p)
				if err != nil {
					return err
				}
				pg, err := must(r.JSON200, "pull request list")
				if err != nil {
					return err
				}
				all = append(all, pg.Pulls...)
				if len(pg.Pulls) < per || (limit > 0 && len(all) >= limit) {
					break
				}
			}
			if limit > 0 && len(all) > limit {
				all = all[:limit]
			}
			if all == nil {
				all = []apigen.PullSummary{}
			}
			return a.print(cmd, all, func(w io.Writer) error { return printPullList(w, a, all, false) })
		},
	}
	addRepoFlag(cmd, &repoFlag)
	f := cmd.Flags()
	f.StringVarP(&state, "state", "s", "open", "open, closed, merged or all")
	f.StringVarP(&author, "author", "A", "", "only pull requests by this username")
	f.StringVarP(&base, "base", "B", "", "only pull requests into this branch")
	f.StringVarP(&head, "head", "H", "", "only pull requests from this branch")
	f.StringVarP(&search, "search", "q", "", "only titles containing `text`")
	f.StringVarP(&labels, "label", "l", "", "comma-separated labels (all must match)")
	f.IntVarP(&limit, "limit", "L", 30, "show at most `n` (0 = all)")
	return withOutput(cmd, "[]PullSummary")
}

func newPRStatusCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Open pull requests waiting for your review, and your own, across all repositories",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := a.client()
			if err != nil {
				return err
			}
			get := func(f apigen.ListMyPullsParamsFilter) ([]apigen.PullSummary, error) {
				per := 100
				r, err := c.ListMyPullsWithResponse(cmd.Context(), &apigen.ListMyPullsParams{Filter: &f, PerPage: &per})
				if err != nil {
					return nil, err
				}
				pg, err := must(r.JSON200, "pull requests")
				if err != nil {
					return nil, err
				}
				return pg.Pulls, nil
			}
			review, err := get(apigen.ListMyPullsParamsFilterReviewRequested)
			if err != nil {
				return err
			}
			mine, err := get(apigen.ListMyPullsParamsFilterAuthored)
			if err != nil {
				return err
			}
			out := map[string]any{"review_requested": review, "authored": mine}
			return a.print(cmd, out, func(w io.Writer) error {
				fmt.Fprintln(w, a.io.Bold("Waiting for your review"))
				if len(review) == 0 {
					fmt.Fprintln(w, a.io.Dim("  nothing"))
				} else if err := printPullList(w, a, review, true); err != nil {
					return err
				}
				fmt.Fprintln(w)
				fmt.Fprintln(w, a.io.Bold("Your open pull requests"))
				if len(mine) == 0 {
					fmt.Fprintln(w, a.io.Dim("  nothing"))
					return nil
				}
				return printPullList(w, a, mine, true)
			})
		},
	}
	return withOutput(cmd, "PullStatus")
}

// pullView is what `pr view --json` prints: the pull request plus its
// reviews and every comment, so agents need only one call.
type pullView struct {
	apigen.PullRequest
	Reviews      []apigen.Review  `json:"reviews"`
	CommentsList []apigen.Comment `json:"comment_list"`
}

func fetchPullView(ctx context.Context, c *apiClient, repo string, n int64) (*pullView, error) {
	r, err := c.GetPullWithResponse(ctx, repo, n)
	if err != nil {
		return nil, err
	}
	pr, err := must(r.JSON200, "pull request")
	if err != nil {
		return nil, err
	}
	rr, err := c.ListReviewsWithResponse(ctx, repo, n)
	if err != nil {
		return nil, err
	}
	reviews, err := must(rr.JSON200, "reviews")
	if err != nil {
		return nil, err
	}
	cr, err := c.ListPullCommentsWithResponse(ctx, repo, n)
	if err != nil {
		return nil, err
	}
	comments, err := must(cr.JSON200, "comments")
	if err != nil {
		return nil, err
	}
	return &pullView{PullRequest: *pr, Reviews: *reviews, CommentsList: *comments}, nil
}

func printPull(w io.Writer, a *app, v *pullView, withComments bool) error {
	p := &v.PullRequest
	fmt.Fprintf(w, "%s %s\n", a.io.Bold(p.Title), a.io.Dim("#"+strconv.FormatInt(p.Number, 10)))
	fmt.Fprintf(w, "%s · %s wants to merge %d commit(s) from %s into %s · +%d −%d in %d file(s)\n",
		stateLabel(a, p.State, p.Draft), p.Author.Username, p.Commits, p.Head.Branch, p.Base.Branch, p.Additions, p.Deletions, p.ChangedFiles)
	kv := [][2]string{{"review", decisionLabel(p.ReviewDecision)}}
	if len(p.Reviewers) > 0 {
		var rs []string
		for _, r := range p.Reviewers {
			s := r.User.Username + " (" + strings.ReplaceAll(string(r.State), "_", " ")
			if r.Stale && r.State != apigen.ReviewerStateStateRequested {
				s += ", stale"
			}
			rs = append(rs, s+")")
		}
		kv = append(kv, [2]string{"reviewers", strings.Join(rs, ", ")})
	}
	if p.State == apigen.PullStateOpen {
		m := string(p.Mergeable)
		if p.Mergeable == apigen.PullRequestMergeableConflict {
			m = a.io.Red("conflicts: " + strings.Join(p.ConflictFiles, ", "))
		}
		kv = append(kv, [2]string{"mergeable", m})
		if p.CanMerge {
			kv = append(kv, [2]string{"merge", a.io.Green("ready")})
		} else {
			var bs []string
			for _, b := range p.Blockers {
				bs = append(bs, b.Message)
			}
			kv = append(kv, [2]string{"blocked", a.io.Yellow(strings.Join(bs, "; "))})
		}
	}
	if p.MergedBy != nil {
		kv = append(kv, [2]string{"merged", fmt.Sprintf("by %s %s (%s, %s)", p.MergedBy.Username, ago(p.MergedAt), deref(p.MergeMethod), short(deref(p.MergeCommit)))})
	}
	if err := printFields(w, a.io, kv); err != nil {
		return err
	}
	if body := strings.TrimSpace(p.Body); body != "" {
		fmt.Fprintln(w)
		fmt.Fprintln(w, body)
	}
	if withComments {
		printThreads(w, a, v)
	} else if p.Comments > 0 {
		fmt.Fprintf(w, "\n%s\n", a.io.Dim(fmt.Sprintf("%d comment(s); see them with --comments or tgp pr comments %d", p.Comments, p.Number)))
	}
	return nil
}

func printThreads(w io.Writer, a *app, v *pullView) {
	for _, r := range v.Reviews {
		label := strings.ReplaceAll(string(r.State), "_", " ")
		if r.Dismissed {
			label += ", dismissed"
		}
		fmt.Fprintf(w, "\n%s %s %s\n", a.io.Bold(r.Author.Username), label, a.io.Dim(ago(&r.CreatedAt)))
		if r.Body != "" {
			fmt.Fprintln(w, indent(r.Body, "  "))
		}
	}
	replies := map[int64][]apigen.Comment{}
	for _, c := range v.CommentsList {
		if c.ReplyTo != nil {
			replies[*c.ReplyTo] = append(replies[*c.ReplyTo], c)
		}
	}
	for _, c := range v.CommentsList {
		if c.ReplyTo != nil {
			continue
		}
		where := ""
		if c.Path != nil {
			where = fmt.Sprintf(" on %s:%d", *c.Path, deref(c.Line))
			if deref(c.Outdated) {
				where += " (outdated)"
			}
			if deref(c.Resolved) {
				where += " (resolved)"
			}
		}
		fmt.Fprintf(w, "\n%s%s %s %s\n", a.io.Bold(c.Author.Username), where, a.io.Dim(ago(&c.CreatedAt)), a.io.Dim(fmt.Sprintf("[id %d]", c.Id)))
		fmt.Fprintln(w, indent(c.Body, "  "))
		for _, r := range replies[c.Id] {
			fmt.Fprintf(w, "  ↳ %s %s\n%s\n", a.io.Bold(r.Author.Username), a.io.Dim(ago(&r.CreatedAt)), indent(r.Body, "    "))
		}
	}
}

func indent(s, prefix string) string {
	return prefix + strings.ReplaceAll(strings.TrimRight(s, "\n"), "\n", "\n"+prefix)
}

func short(sha string) string {
	if len(sha) > 10 {
		return sha[:10]
	}
	return sha
}

func newPRViewCmd(a *app) *cobra.Command {
	var (
		repoFlag string
		comments bool
	)
	cmd := &cobra.Command{
		Use:   "view [NUMBER | URL | BRANCH]",
		Short: "Show a pull request with its reviews, comments and merge status",
		Long: `Show a pull request. With --json the output includes every review
(reviews), every comment including inline ones with path, line and diff hunk
(comment_list), and why it can or can't be merged (blockers, can_merge).`,
		Example: `  tgp pr view 12
  tgp pr view --comments
  tgp pr view 12 --json --jq '.comment_list[] | select(.resolved == false) | {path, line, body}'`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, repo, n, err := a.resolvePR(cmd.Context(), repoFlag, prSelector(args))
			if err != nil {
				return err
			}
			v, err := fetchPullView(cmd.Context(), c, repo, n)
			if err != nil {
				return err
			}
			return a.print(cmd, v, func(w io.Writer) error { return printPull(w, a, v, comments) })
		},
	}
	addRepoFlag(cmd, &repoFlag)
	cmd.Flags().BoolVarP(&comments, "comments", "c", false, "also show reviews and comments")
	return withOutput(cmd, "PullView")
}

// readBody returns --body, or the contents of --body-file ("-" = stdin).
func (a *app) readBody(body, file string) (string, error) {
	if file == "" {
		return body, nil
	}
	if body != "" {
		return "", usageErr("give --body or --body-file, not both")
	}
	var b []byte
	var err error
	if file == "-" {
		b, err = io.ReadAll(a.io.In)
	} else {
		b, err = os.ReadFile(file)
	}
	return string(b), err
}

func currentBranch() string {
	out, err := exec.Command("git", "symbolic-ref", "--quiet", "--short", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func newPRCreateCmd(a *app) *cobra.Command {
	var (
		repoFlag, title, body, bodyFile, base, head, reviewers string
		labels, assignees, milestone                           string
		draft, fill                                            bool
	)
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Open a pull request",
		Long: `Open a pull request from --head (default: the current branch) into --base
(default: the repository's default branch). The head branch must already be
pushed. --fill takes the title and body from the commits.`,
		Example: `  git push -u origin my-feature && tgp pr create --fill
  tgp pr create --title "Add shop" --body-file notes.md --reviewer tom,ana
  tgp pr create --head fix/lag --base release --draft --title "Fix lag"`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, repo, err := a.resolveRepo(repoFlag)
			if err != nil {
				return err
			}
			if head == "" {
				if head = currentBranch(); head == "" {
					return usageErr("not on a branch; pass --head")
				}
			}
			b, err := a.readBody(body, bodyFile)
			if err != nil {
				return err
			}
			if fill && (title == "" || b == "") {
				cmpBase := base
				if cmpBase == "" {
					x, err := getRepo(cmd, c, repo)
					if err != nil {
						return err
					}
					cmpBase = x.DefaultBranch
				}
				t, fb, err := fillFromCommits(cmd.Context(), c, repo, cmpBase, head)
				if err != nil {
					return err
				}
				if title == "" {
					title = t
				}
				if b == "" {
					b = fb
				}
			}
			if title == "" {
				if err := a.io.requirePrompt("a title", "--title (or --fill)"); err != nil {
					return err
				}
				if title, err = a.io.Prompt("Title", ""); err != nil {
					return err
				}
			}
			req := apigen.CreatePullRequest{Title: title, Head: head}
			if b != "" {
				req.Body = &b
			}
			if base != "" {
				req.Base = &base
			}
			if draft {
				req.Draft = &draft
			}
			if rs := splitList(reviewers); len(rs) > 0 {
				req.Reviewers = &rs
			}
			req.Labels, req.Assignees = listPtr(labels), listPtr(assignees)
			if req.Milestone, err = milestoneID(cmd.Context(), c, repo, milestone); err != nil {
				return err
			}
			r, err := c.CreatePullWithResponse(cmd.Context(), repo, req)
			if err != nil {
				return err
			}
			pr, err := must(r.JSON201, "pull request")
			if err != nil {
				return err
			}
			return a.print(cmd, pr, func(w io.Writer) error {
				fmt.Fprintf(w, "%s Opened pull request #%d: %s\n", a.io.SuccessIcon(), pr.Number, pr.Title)
				fmt.Fprintf(w, "  %s\n", pullURL(c, repo, pr.Number))
				return nil
			})
		},
	}
	addRepoFlag(cmd, &repoFlag)
	f := cmd.Flags()
	f.StringVarP(&title, "title", "t", "", "title")
	f.StringVarP(&body, "body", "b", "", "description (markdown)")
	f.StringVarP(&bodyFile, "body-file", "F", "", "read the description from a `file` (- for stdin)")
	f.StringVarP(&base, "base", "B", "", "branch to merge into (default: the default branch)")
	f.StringVarP(&head, "head", "H", "", "branch with the changes (default: the current branch)")
	f.StringVarP(&reviewers, "reviewer", "r", "", "comma-separated usernames to request reviews from")
	f.BoolVarP(&draft, "draft", "d", false, "open as a draft")
	f.BoolVarP(&fill, "fill", "f", false, "use the commits for the title and body")
	f.StringVarP(&labels, "label", "l", "", "comma-separated labels")
	f.StringVarP(&assignees, "assignee", "a", "", "comma-separated usernames")
	f.StringVarP(&milestone, "milestone", "m", "", "milestone title or id")
	return withOutput(cmd, "PullRequest")
}

func pullURL(c *apiClient, repo string, n int64) string {
	return fmt.Sprintf("%s/%s/pulls/%d", c.t.Base, repo, n)
}

func fillFromCommits(ctx context.Context, c *apiClient, repo, base, head string) (string, string, error) {
	r, err := c.CompareRefsWithResponse(ctx, repo, &apigen.CompareRefsParams{Base: base, Head: head})
	if err != nil {
		return "", "", err
	}
	cmp, err := must(r.JSON200, "comparison")
	if err != nil {
		return "", "", err
	}
	switch len(cmp.Commits) {
	case 0:
		return "", "", errorf("validation_failed", ExitConflict, "%s has no commits that aren't in %s", head, base)
	case 1:
		return cmp.Commits[0].Subject, cmp.Commits[0].Body, nil
	}
	var b strings.Builder
	for _, cm := range cmp.Commits {
		b.WriteString("* " + cm.Subject + "\n")
	}
	title := strings.ReplaceAll(strings.ReplaceAll(head, "-", " "), "_", " ")
	if i := strings.LastIndex(title, "/"); i >= 0 {
		title = title[i+1:]
	}
	if title != "" {
		title = strings.ToUpper(title[:1]) + title[1:]
	}
	return title, b.String(), nil
}

func newPRCheckoutCmd(a *app) *cobra.Command {
	var repoFlag, remote string
	cmd := &cobra.Command{
		Use:   "checkout NUMBER | URL | BRANCH",
		Short: "Check out a pull request's branch locally",
		Long: `Fetch the pull request's head branch and check it out (fast-forwarding an
existing local branch). If the head branch was deleted, the pull request's
last head is checked out as pr-NUMBER.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, repo, n, err := a.resolvePR(cmd.Context(), repoFlag, args[0])
			if err != nil {
				return err
			}
			r, err := c.GetPullWithResponse(cmd.Context(), repo, n)
			if err != nil {
				return err
			}
			pr, err := must(r.JSON200, "pull request")
			if err != nil {
				return err
			}
			run := func(args ...string) error {
				g := exec.Command("git", args...)
				g.Stdout, g.Stderr = a.io.ErrOut, a.io.ErrOut
				if err := g.Run(); err != nil {
					return errorf(CodeGit, ExitError, "git %s failed: %v", strings.Join(args, " "), err)
				}
				return nil
			}
			branch := pr.Head.Branch
			if pr.Head.Exists {
				if err := run("fetch", remote, "+refs/heads/"+branch+":refs/remotes/"+remote+"/"+branch); err != nil {
					return err
				}
				if exec.Command("git", "rev-parse", "--verify", "--quiet", "refs/heads/"+branch).Run() == nil {
					if err := run("checkout", branch); err != nil {
						return err
					}
					if err := run("merge", "--ff-only", remote+"/"+branch); err != nil {
						return err
					}
				} else if err := run("checkout", "-b", branch, "--track", remote+"/"+branch); err != nil {
					return err
				}
			} else {
				branch = fmt.Sprintf("pr-%d", n)
				if err := run("fetch", remote, fmt.Sprintf("+refs/pull/%d/head:refs/heads/%s", n, branch)); err != nil {
					return err
				}
				if err := run("checkout", branch); err != nil {
					return err
				}
			}
			return a.print(cmd, map[string]any{"number": n, "branch": branch, "head_sha": pr.Head.Sha}, func(w io.Writer) error {
				_, err := fmt.Fprintf(w, "%s Checked out #%d on branch %s\n", a.io.SuccessIcon(), n, branch)
				return err
			})
		},
	}
	addRepoFlag(cmd, &repoFlag)
	cmd.Flags().StringVar(&remote, "remote", "origin", "git remote to fetch from")
	return withOutput(cmd, "PullCheckout")
}

// formatDiff renders an API diff as unified diff text.
func formatDiff(w io.Writer, a *app, d *apigen.Diff, nameOnly bool) {
	for _, f := range d.Files {
		if nameOnly {
			fmt.Fprintln(w, f.NewPath)
			continue
		}
		oldP, newP := "a/"+f.OldPath, "b/"+f.NewPath
		fmt.Fprintln(w, a.io.Bold("diff --git "+oldP+" "+newP))
		switch f.Status {
		case "added":
			oldP = "/dev/null"
		case "deleted":
			newP = "/dev/null"
		case "renamed":
			fmt.Fprintln(w, a.io.Bold("rename from "+f.OldPath))
			fmt.Fprintln(w, a.io.Bold("rename to "+f.NewPath))
		}
		if f.Binary {
			fmt.Fprintf(w, "Binary files %s and %s differ\n", oldP, newP)
			continue
		}
		if len(f.Hunks) == 0 {
			continue
		}
		fmt.Fprintln(w, a.io.Bold("--- "+oldP))
		fmt.Fprintln(w, a.io.Bold("+++ "+newP))
		for _, h := range f.Hunks {
			fmt.Fprintln(w, a.io.paint("36", h.Header))
			for _, l := range h.Lines {
				switch l.Type {
				case "add":
					fmt.Fprintln(w, a.io.Green("+"+l.Content))
				case "del":
					fmt.Fprintln(w, a.io.Red("-"+l.Content))
				default:
					fmt.Fprintln(w, " "+l.Content)
				}
				if deref(l.NoNewline) {
					fmt.Fprintln(w, `\ No newline at end of file`)
				}
			}
		}
		if f.Truncated {
			fmt.Fprintln(w, a.io.Dim("… (diff truncated)"))
		}
	}
	if d.Truncated {
		fmt.Fprintln(w, a.io.Dim("… (some files omitted: diff too large)"))
	}
}

func newPRDiffCmd(a *app) *cobra.Command {
	var (
		repoFlag string
		nameOnly bool
	)
	cmd := &cobra.Command{
		Use:   "diff [NUMBER | URL | BRANCH]",
		Short: "Show a pull request's changes",
		Long:  "Print the pull request's diff (merge base to head) as a unified diff, or as structured hunks with --json.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, repo, n, err := a.resolvePR(cmd.Context(), repoFlag, prSelector(args))
			if err != nil {
				return err
			}
			r, err := c.GetPullDiffWithResponse(cmd.Context(), repo, n)
			if err != nil {
				return err
			}
			d, err := must(r.JSON200, "diff")
			if err != nil {
				return err
			}
			return a.print(cmd, d, func(w io.Writer) error {
				formatDiff(w, a, d, nameOnly)
				return nil
			})
		},
	}
	addRepoFlag(cmd, &repoFlag)
	cmd.Flags().BoolVar(&nameOnly, "name-only", false, "only list changed files")
	return withOutput(cmd, "Diff")
}

var lineRefRe = regexp.MustCompile(`^(.+):(\d+)$`)

func newPRCommentCmd(a *app) *cobra.Command {
	var (
		repoFlag, body, bodyFile, file, side string
		line                                 int
		replyTo                              int64
	)
	cmd := &cobra.Command{
		Use:   "comment [NUMBER | URL | BRANCH]",
		Short: "Comment on a pull request, a line of its diff, or reply in a thread",
		Example: `  tgp pr comment 12 -b "Looks good to me"
  tgp pr comment 12 --file src/Shop.java --line 42 -b "Null check?"
  tgp pr comment 12 --file src/Shop.java:42 -b "Null check?"
  tgp pr comment 12 --reply-to 381 -b "Fixed in the last push"`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			b, err := a.readBody(body, bodyFile)
			if err != nil {
				return err
			}
			if strings.TrimSpace(b) == "" {
				return usageErr("--body (or --body-file) is required")
			}
			req := apigen.CreateComment{Body: b}
			if m := lineRefRe.FindStringSubmatch(file); m != nil && line == 0 {
				file = m[1]
				line, _ = strconv.Atoi(m[2])
			}
			if file != "" {
				if line <= 0 {
					return usageErr("--line is required with --file")
				}
				req.Path, req.Line = &file, &line
				s := apigen.CreateCommentSide(side)
				if s != "new" && s != "old" {
					return usageErr("--side must be new or old")
				}
				req.Side = &s
			}
			if replyTo != 0 {
				req.ReplyTo = &replyTo
			}
			c, repo, n, err := a.resolvePR(cmd.Context(), repoFlag, prSelector(args))
			if err != nil {
				return err
			}
			r, err := c.CreatePullCommentWithResponse(cmd.Context(), repo, n, req)
			if err != nil {
				return err
			}
			cm, err := must(r.JSON201, "comment")
			if err != nil {
				return err
			}
			return a.print(cmd, cm, func(w io.Writer) error {
				_, err := fmt.Fprintf(w, "%s Commented on #%d (comment %d)\n", a.io.SuccessIcon(), n, cm.Id)
				return err
			})
		},
	}
	addRepoFlag(cmd, &repoFlag)
	f := cmd.Flags()
	f.StringVarP(&body, "body", "b", "", "comment text (markdown)")
	f.StringVarP(&bodyFile, "body-file", "F", "", "read the comment from a `file` (- for stdin)")
	f.StringVar(&file, "file", "", "comment on this file (`path` or path:line)")
	f.IntVar(&line, "line", 0, "line number for --file")
	f.StringVar(&side, "side", "new", "new (head version) or old (base version) side of the diff")
	f.Int64Var(&replyTo, "reply-to", 0, "reply in the thread of this comment `id`")
	cmd.MarkFlagsMutuallyExclusive("reply-to", "file")
	return withOutput(cmd, "Comment")
}

func newPRCommentsCmd(a *app) *cobra.Command {
	var (
		repoFlag   string
		unresolved bool
	)
	cmd := &cobra.Command{
		Use:   "comments [NUMBER | URL | BRANCH]",
		Short: "List all comments on a pull request, including inline comments and replies",
		Example: `  tgp pr comments 12 --unresolved
  tgp pr comments 12 --json --jq '.[] | select(.path != null) | "\(.path):\(.line) \(.body)"'`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, repo, n, err := a.resolvePR(cmd.Context(), repoFlag, prSelector(args))
			if err != nil {
				return err
			}
			v, err := fetchPullView(cmd.Context(), c, repo, n)
			if err != nil {
				return err
			}
			if unresolved {
				open := map[int64]bool{}
				var kept []apigen.Comment
				for _, cm := range v.CommentsList {
					if cm.Path != nil && cm.ReplyTo == nil && !deref(cm.Resolved) {
						open[cm.Id] = true
						kept = append(kept, cm)
					} else if cm.ReplyTo != nil && open[*cm.ReplyTo] {
						kept = append(kept, cm)
					}
				}
				v.CommentsList = kept
				v.Reviews = nil
			}
			if v.CommentsList == nil {
				v.CommentsList = []apigen.Comment{}
			}
			return a.print(cmd, v.CommentsList, func(w io.Writer) error {
				if len(v.CommentsList) == 0 && len(v.Reviews) == 0 {
					fmt.Fprintln(a.io.ErrOut, "No comments.")
					return nil
				}
				printThreads(w, a, v)
				return nil
			})
		},
	}
	addRepoFlag(cmd, &repoFlag)
	cmd.Flags().BoolVar(&unresolved, "unresolved", false, "only unresolved inline threads")
	return withOutput(cmd, "[]Comment")
}

func newPRReviewCmd(a *app) *cobra.Command {
	var (
		repoFlag, body, bodyFile, commit string
		approve, requestChanges, comment bool
		inline                           []string
	)
	cmd := &cobra.Command{
		Use:   "review [NUMBER | URL | BRANCH] (--approve | --request-changes | --comment)",
		Short: "Approve, request changes or comment, optionally with inline comments",
		Long: `Submit a review. Inline comments go with --inline PATH:LINE=TEXT (repeatable;
PATH:-LINE=TEXT comments on the old side). The review is tied to the head
commit you name with --commit, or the current head; if the head has moved
since, the review is rejected so you don't approve unseen changes.`,
		Example: `  tgp pr review 12 --approve
  tgp pr review 12 --request-changes -b "See inline" --inline src/Shop.java:42="Null check?"
  tgp pr review --comment -b "Partial pass, more later"`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			n := 0
			var state apigen.CreateReviewState
			for _, x := range []struct {
				on bool
				s  apigen.CreateReviewState
			}{{approve, apigen.CreateReviewStateApprove}, {requestChanges, apigen.CreateReviewStateRequestChanges}, {comment, apigen.CreateReviewStateComment}} {
				if x.on {
					n++
					state = x.s
				}
			}
			if n != 1 {
				return usageErr("pass exactly one of --approve, --request-changes or --comment")
			}
			b, err := a.readBody(body, bodyFile)
			if err != nil {
				return err
			}
			req := apigen.CreateReview{State: state}
			if b != "" {
				req.Body = &b
			}
			if commit != "" {
				req.CommitSha = &commit
			}
			var ics []apigen.InlineCommentInput
			for _, spec := range inline {
				loc, text, ok := strings.Cut(spec, "=")
				m := lineRefRe.FindStringSubmatch(loc)
				side := apigen.InlineCommentInputSideNew
				if m == nil {
					if i := strings.LastIndex(loc, ":-"); i > 0 {
						m = []string{loc, loc[:i], loc[i+2:]}
						side = apigen.InlineCommentInputSideOld
					}
				}
				if !ok || m == nil || strings.TrimSpace(text) == "" {
					return usageErr("invalid --inline %q: use PATH:LINE=TEXT", spec)
				}
				ln, _ := strconv.Atoi(m[2])
				s := side
				ics = append(ics, apigen.InlineCommentInput{Path: m[1], Line: ln, Side: &s, Body: text})
			}
			if len(ics) > 0 {
				req.Comments = &ics
			}
			c, repo, num, err := a.resolvePR(cmd.Context(), repoFlag, prSelector(args))
			if err != nil {
				return err
			}
			r, err := c.CreateReviewWithResponse(cmd.Context(), repo, num, req)
			if err != nil {
				return err
			}
			rv, err := must(r.JSON201, "review")
			if err != nil {
				return err
			}
			return a.print(cmd, rv, func(w io.Writer) error {
				verb := map[apigen.ReviewState]string{"approved": "Approved", "changes_requested": "Requested changes on", "commented": "Reviewed"}[rv.State]
				_, err := fmt.Fprintf(w, "%s %s #%d\n", a.io.SuccessIcon(), verb, num)
				return err
			})
		},
	}
	addRepoFlag(cmd, &repoFlag)
	f := cmd.Flags()
	f.BoolVarP(&approve, "approve", "a", false, "approve")
	f.BoolVarP(&requestChanges, "request-changes", "r", false, "request changes")
	f.BoolVarP(&comment, "comment", "c", false, "comment without a verdict")
	f.StringVarP(&body, "body", "b", "", "review text (markdown)")
	f.StringVarP(&bodyFile, "body-file", "F", "", "read the review text from a `file` (- for stdin)")
	f.StringArrayVar(&inline, "inline", nil, "inline comment `PATH:LINE=TEXT` (repeatable)")
	f.StringVar(&commit, "commit", "", "head commit you reviewed (rejected if the head moved)")
	return withOutput(cmd, "Review")
}

func newPRMergeCmd(a *app) *cobra.Command {
	var (
		repoFlag, subject, body, sha  string
		merge, squash, rebase         bool
		deleteBranch, admin, autoSync bool
	)
	cmd := &cobra.Command{
		Use:   "merge [NUMBER | URL | BRANCH]",
		Short: "Merge a pull request",
		Long: `Merge a pull request with a merge commit (default), --squash or --rebase.
If it can't be merged, the error lists the blockers (also in error.details
with --json). Admins can pass --admin to merge despite missing approvals, change
requests or an out-of-date branch when the protection rule allows it.`,
		Example: `  tgp pr merge 12 --squash --delete-branch
  tgp pr merge --rebase
  tgp pr merge 12 --sha 3f2a9c1   # only if the head is still at that commit`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			req := apigen.MergePullRequest{}
			var m apigen.MergeMethod
			n := 0
			for _, x := range []struct {
				on bool
				m  apigen.MergeMethod
			}{{merge, apigen.Merge}, {squash, apigen.Squash}, {rebase, apigen.Rebase}} {
				if x.on {
					n++
					m = x.m
				}
			}
			if n > 1 {
				return usageErr("pass only one of --merge, --squash or --rebase")
			}
			if n == 1 {
				req.Method = &m
			}
			if subject != "" {
				req.Title = &subject
			}
			if body != "" {
				req.Message = &body
			}
			if sha != "" {
				req.Sha = &sha
			}
			if deleteBranch {
				req.DeleteBranch = &deleteBranch
			}
			if admin {
				req.BypassProtection = &admin
			}
			c, repo, num, err := a.resolvePR(cmd.Context(), repoFlag, prSelector(args))
			if err != nil {
				return err
			}
			r, err := c.MergePullWithResponse(cmd.Context(), repo, num, req)
			if err != nil {
				return err
			}
			pr, err := must(r.JSON200, "pull request")
			if err != nil {
				return err
			}
			if autoSync && pr.Head.Branch == currentBranch() {
				g := exec.Command("git", "checkout", pr.Base.Branch)
				g.Stdout, g.Stderr = a.io.ErrOut, a.io.ErrOut
				_ = g.Run()
				g = exec.Command("git", "pull", "--ff-only")
				g.Stdout, g.Stderr = a.io.ErrOut, a.io.ErrOut
				_ = g.Run()
			}
			return a.print(cmd, pr, func(w io.Writer) error {
				fmt.Fprintf(w, "%s Merged #%d into %s (%s, %s)\n", a.io.SuccessIcon(), pr.Number, pr.Base.Branch, deref(pr.MergeMethod), short(deref(pr.MergeCommit)))
				if deleteBranch && !pr.Head.Exists {
					fmt.Fprintf(w, "%s Deleted branch %s\n", a.io.SuccessIcon(), pr.Head.Branch)
				}
				return nil
			})
		},
	}
	addRepoFlag(cmd, &repoFlag)
	f := cmd.Flags()
	f.BoolVarP(&merge, "merge", "m", false, "merge commit")
	f.BoolVarP(&squash, "squash", "s", false, "squash into one commit")
	f.BoolVarP(&rebase, "rebase", "r", false, "rebase the commits onto the base")
	f.BoolVarP(&deleteBranch, "delete-branch", "d", false, "delete the head branch after merging")
	f.StringVarP(&subject, "subject", "t", "", "merge/squash commit subject")
	f.StringVarP(&body, "body", "b", "", "merge/squash commit body")
	f.StringVar(&sha, "sha", "", "only merge if the head is at this commit")
	f.BoolVar(&admin, "admin", false, "bypass branch protection (admins, when the rule allows it)")
	f.BoolVar(&autoSync, "sync", false, "afterwards, switch the local checkout to the base branch and pull")
	return withOutput(cmd, "PullRequest")
}

func newPRStateCmd(a *app, verb string) *cobra.Command {
	var repoFlag, comment string
	short := "Close a pull request without merging"
	if verb == "reopen" {
		short = "Reopen a closed pull request"
	}
	cmd := &cobra.Command{
		Use:   verb + " [NUMBER | URL | BRANCH]",
		Short: short,
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, repo, n, err := a.resolvePR(cmd.Context(), repoFlag, prSelector(args))
			if err != nil {
				return err
			}
			if comment != "" {
				if _, err := c.CreatePullCommentWithResponse(cmd.Context(), repo, n, apigen.CreateComment{Body: comment}); err != nil {
					return err
				}
			}
			st := apigen.UpdatePullRequestStateClosed
			if verb == "reopen" {
				st = apigen.UpdatePullRequestStateOpen
			}
			r, err := c.UpdatePullWithResponse(cmd.Context(), repo, n, apigen.UpdatePullRequest{State: &st})
			if err != nil {
				return err
			}
			pr, err := must(r.JSON200, "pull request")
			if err != nil {
				return err
			}
			return a.print(cmd, pr, func(w io.Writer) error {
				_, err := fmt.Fprintf(w, "%s %s #%d\n", a.io.SuccessIcon(), map[string]string{"close": "Closed", "reopen": "Reopened"}[verb], n)
				return err
			})
		},
	}
	addRepoFlag(cmd, &repoFlag)
	cmd.Flags().StringVarP(&comment, "comment", "c", "", "leave a comment first")
	return withOutput(cmd, "PullRequest")
}

func newPREditCmd(a *app) *cobra.Command {
	var (
		repoFlag, title, body, bodyFile, base, addRev, rmRev string
		meta                                                 metaFlags
	)
	cmd := &cobra.Command{
		Use:   "edit [NUMBER | URL | BRANCH]",
		Short: "Change a pull request's title, description, base, reviewers, labels, assignees or milestone",
		Example: `  tgp pr edit 12 --title "Shop: add refunds"
  tgp pr edit 12 --add-reviewer tom --remove-reviewer ana
  tgp pr edit 12 --base release`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, repo, n, err := a.resolvePR(cmd.Context(), repoFlag, prSelector(args))
			if err != nil {
				return err
			}
			req := apigen.UpdatePullRequest{}
			if cmd.Flags().Changed("title") {
				req.Title = &title
			}
			if cmd.Flags().Changed("body") || bodyFile != "" {
				b, err := a.readBody(body, bodyFile)
				if err != nil {
					return err
				}
				req.Body = &b
			}
			if base != "" {
				req.Base = &base
			}
			if addRev != "" || rmRev != "" {
				r, err := c.GetPullWithResponse(cmd.Context(), repo, n)
				if err != nil {
					return err
				}
				pr, err := must(r.JSON200, "pull request")
				if err != nil {
					return err
				}
				set := []string{}
				rm := map[string]bool{}
				for _, u := range splitList(rmRev) {
					rm[strings.ToLower(u)] = true
				}
				seen := map[string]bool{}
				for _, u := range pr.RequestedReviewers {
					if !rm[u.Username] && !seen[u.Username] {
						set = append(set, u.Username)
						seen[u.Username] = true
					}
				}
				for _, u := range splitList(addRev) {
					if !seen[strings.ToLower(u)] {
						set = append(set, u)
						seen[strings.ToLower(u)] = true
					}
				}
				req.Reviewers = &set
			}
			req.AddLabels, req.RemoveLabels, req.Labels = listPtr(meta.addLabels), listPtr(meta.removeLabels), setList(cmd, "labels", meta.setLabels)
			req.AddAssignees, req.RemoveAssignees, req.Assignees = listPtr(meta.addAssignees), listPtr(meta.removeAssignees), setList(cmd, "assignees", meta.setAssignees)
			if req.Milestone, err = milestoneID(cmd.Context(), c, repo, meta.milestone); err != nil {
				return err
			}
			if req.Title == nil && req.Body == nil && req.Base == nil && req.Reviewers == nil && !meta.changed(cmd) {
				return usageErr("nothing to change: pass --title, --body, --base, reviewer, label, assignee or milestone flags")
			}
			r, err := c.UpdatePullWithResponse(cmd.Context(), repo, n, req)
			if err != nil {
				return err
			}
			pr, err := must(r.JSON200, "pull request")
			if err != nil {
				return err
			}
			return a.print(cmd, pr, func(w io.Writer) error {
				_, err := fmt.Fprintf(w, "%s Updated #%d\n", a.io.SuccessIcon(), n)
				return err
			})
		},
	}
	addRepoFlag(cmd, &repoFlag)
	f := cmd.Flags()
	f.StringVarP(&title, "title", "t", "", "new title")
	f.StringVarP(&body, "body", "b", "", "new description")
	f.StringVarP(&bodyFile, "body-file", "F", "", "read the description from a `file` (- for stdin)")
	f.StringVarP(&base, "base", "B", "", "new base branch")
	f.StringVar(&addRev, "add-reviewer", "", "comma-separated usernames to request reviews from")
	f.StringVar(&rmRev, "remove-reviewer", "", "comma-separated usernames to stop requesting")
	meta.register(cmd)
	return withOutput(cmd, "PullRequest")
}

func newPRReadyCmd(a *app) *cobra.Command {
	var (
		repoFlag string
		undo     bool
	)
	cmd := &cobra.Command{
		Use:   "ready [NUMBER | URL | BRANCH]",
		Short: "Mark a draft pull request ready for review (--undo: back to draft)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, repo, n, err := a.resolvePR(cmd.Context(), repoFlag, prSelector(args))
			if err != nil {
				return err
			}
			r, err := c.UpdatePullWithResponse(cmd.Context(), repo, n, apigen.UpdatePullRequest{Draft: &undo})
			if err != nil {
				return err
			}
			pr, err := must(r.JSON200, "pull request")
			if err != nil {
				return err
			}
			return a.print(cmd, pr, func(w io.Writer) error {
				msg := "is ready for review"
				if undo {
					msg = "is a draft again"
				}
				_, err := fmt.Fprintf(w, "%s #%d %s\n", a.io.SuccessIcon(), n, msg)
				return err
			})
		},
	}
	addRepoFlag(cmd, &repoFlag)
	cmd.Flags().BoolVar(&undo, "undo", false, "convert back to a draft")
	return withOutput(cmd, "PullRequest")
}

func newPRUpdateBranchCmd(a *app) *cobra.Command {
	var repoFlag string
	cmd := &cobra.Command{
		Use:   "update-branch [NUMBER | URL | BRANCH]",
		Short: "Merge the base branch into the pull request's branch on the server",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, repo, n, err := a.resolvePR(cmd.Context(), repoFlag, prSelector(args))
			if err != nil {
				return err
			}
			r, err := c.UpdatePullBranchWithResponse(cmd.Context(), repo, n, apigen.UpdatePullBranchJSONRequestBody{})
			if err != nil {
				return err
			}
			pr, err := must(r.JSON200, "pull request")
			if err != nil {
				return err
			}
			return a.print(cmd, pr, func(w io.Writer) error {
				_, err := fmt.Fprintf(w, "%s Merged %s into %s (now at %s)\n", a.io.SuccessIcon(), pr.Base.Branch, pr.Head.Branch, short(pr.Head.Sha))
				return err
			})
		},
	}
	addRepoFlag(cmd, &repoFlag)
	return withOutput(cmd, "PullRequest")
}
