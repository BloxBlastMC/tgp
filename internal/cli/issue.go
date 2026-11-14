package cli

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/api/apigen"
)

func newIssueCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "issue",
		Aliases: []string{"issues"},
		Short:   "Work with issues",
		Long: `File, triage and close issues. An issue is selected by number ("12" or
"#12") or URL. The repository comes from -R or the current directory's git remote.`,
	}
	cmd.AddCommand(newIssueListCmd(a), newIssueStatusCmd(a), newIssueViewCmd(a), newIssueCreateCmd(a),
		newIssueCommentCmd(a), newIssueEditCmd(a), newIssueStateCmd(a, "close"), newIssueStateCmd(a, "reopen"))
	return group(cmd)
}

var issueURLRe = regexp.MustCompile(`^(https?://.+)/([^/]+)/issues/(\d+)/?$`)

func (a *app) resolveIssue(repoFlag, sel string) (*apiClient, string, int64, error) {
	if m := issueURLRe.FindStringSubmatch(sel); m != nil {
		c, repo, err := a.resolveRepo(m[1] + "/" + m[2])
		if err != nil {
			return nil, "", 0, err
		}
		n, _ := strconv.ParseInt(m[3], 10, 64)
		return c, repo, n, nil
	}
	n, err := strconv.ParseInt(strings.TrimPrefix(sel, "#"), 10, 64)
	if err != nil || n <= 0 {
		return nil, "", 0, usageErr("invalid issue %q: use a number or URL", sel)
	}
	c, repo, err := a.resolveRepo(repoFlag)
	return c, repo, n, err
}

// milestoneID resolves a milestone title or id ("" → nil, "none" → 0).
func milestoneID(ctx context.Context, c *apiClient, repo, sel string) (*int64, error) {
	if sel == "" {
		return nil, nil
	}
	if strings.EqualFold(sel, "none") {
		z := int64(0)
		return &z, nil
	}
	all := apigen.ListMilestonesParamsStateAll
	r, err := c.ListMilestonesWithResponse(ctx, repo, &apigen.ListMilestonesParams{State: &all})
	if err != nil {
		return nil, err
	}
	ms, err := must(r.JSON200, "milestones")
	if err != nil {
		return nil, err
	}
	for _, m := range *ms {
		if strings.EqualFold(m.Title, sel) || strconv.FormatInt(m.Id, 10) == sel {
			id := m.Id
			return &id, nil
		}
	}
	return nil, &Error{Code: "validation_failed", Exit: ExitConflict, Message: fmt.Sprintf("milestone %q doesn't exist in %s", sel, repo),
		Hint: "list them with: tgp milestone list --state all"}
}

func listPtr(s string) *[]string {
	if s == "" {
		return nil
	}
	l := splitList(s)
	return &l
}

func labelChips(ls []apigen.Label) string {
	n := make([]string, len(ls))
	for i, l := range ls {
		n[i] = l.Name
	}
	return strings.Join(n, ", ")
}

func userNames(us []apigen.UserSummary) string {
	n := make([]string, len(us))
	for i, u := range us {
		n[i] = u.Username
	}
	return strings.Join(n, ", ")
}

func issueStateLabel(a *app, state string, reason *apigen.IssueStateReason) string {
	if state == "open" {
		return a.io.Green("open")
	}
	if reason != nil && *reason == apigen.IssueStateReasonNotPlanned {
		return a.io.Dim("closed (not planned)")
	}
	return a.io.paint("35", "closed")
}

func printIssueList(w io.Writer, a *app, issues []apigen.IssueSummary, withRepo bool) error {
	if len(issues) == 0 {
		fmt.Fprintln(a.io.ErrOut, "No issues.")
		return nil
	}
	rows := make([][]string, len(issues))
	for i, is := range issues {
		num := "#" + strconv.FormatInt(is.Number, 10)
		if withRepo {
			num = is.Repo + num
		}
		ms := ""
		if is.Milestone != nil {
			ms = is.Milestone.Title
		}
		rows[i] = []string{num, truncate(is.Title, 60), issueStateLabel(a, string(is.State), is.StateReason), truncate(labelChips(is.Labels), 30),
			userNames(is.Assignees), ms, ago(&is.UpdatedAt)}
	}
	return printTable(w, a.io, []string{"issue", "title", "state", "labels", "assignees", "milestone", "updated"}, rows)
}

func newIssueListCmd(a *app) *cobra.Command {
	var (
		repoFlag, state, labels, milestone, assignee, author, search, sortBy string
		limit                                                                int
	)
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List issues in a repository",
		Example: `  tgp issue list --label bug --assignee none
  tgp issue list --milestone v1.0 --state all
  tgp issue list --json=number,title,labels --jq '.[] | select(.labels | length == 0) | .number'`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, repo, err := a.resolveRepo(repoFlag)
			if err != nil {
				return err
			}
			st := apigen.ListIssuesParamsState(state)
			if st != "open" && st != "closed" && st != "all" {
				return usageErr("invalid --state %q: use open, closed or all", state)
			}
			so := apigen.ListIssuesParamsSort(sortBy)
			if so != "updated" && so != "created" && so != "comments" {
				return usageErr("invalid --sort %q: use updated, created or comments", sortBy)
			}
			var all []apigen.IssueSummary
			for page := 1; ; page++ {
				per := 100
				p := &apigen.ListIssuesParams{State: &st, Sort: &so, Page: &page, PerPage: &per}
				for _, x := range []struct {
					v   string
					dst **string
				}{{labels, &p.Labels}, {milestone, &p.Milestone}, {assignee, &p.Assignee}, {author, &p.Author}, {search, &p.Q}} {
					if x.v != "" {
						v := x.v
						*x.dst = &v
					}
				}
				r, err := c.ListIssuesWithResponse(cmd.Context(), repo, p)
				if err != nil {
					return err
				}
				pg, err := must(r.JSON200, "issue list")
				if err != nil {
					return err
				}
				all = append(all, pg.Issues...)
				if len(pg.Issues) < per || (limit > 0 && len(all) >= limit) {
					break
				}
			}
			if limit > 0 && len(all) > limit {
				all = all[:limit]
			}
			if all == nil {
				all = []apigen.IssueSummary{}
			}
			return a.print(cmd, all, func(w io.Writer) error { return printIssueList(w, a, all, false) })
		},
	}
	addRepoFlag(cmd, &repoFlag)
	f := cmd.Flags()
	f.StringVarP(&state, "state", "s", "open", "open, closed or all")
	f.StringVarP(&labels, "label", "l", "", "comma-separated labels (all must match)")
	f.StringVarP(&milestone, "milestone", "m", "", "milestone title, none or *")
	f.StringVarP(&assignee, "assignee", "a", "", "username, none or *")
	f.StringVarP(&author, "author", "A", "", "author username")
	f.StringVarP(&search, "search", "q", "", "only titles containing `text`")
	f.StringVar(&sortBy, "sort", "updated", "updated, created or comments")
	f.IntVarP(&limit, "limit", "L", 30, "show at most `n` (0 = all)")
	return withOutput(cmd, "[]IssueSummary")
}

func newIssueStatusCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Open issues assigned to you, and opened by you, across all repositories",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := a.client()
			if err != nil {
				return err
			}
			get := func(f apigen.ListMyIssuesParamsFilter) ([]apigen.IssueSummary, error) {
				per := 100
				r, err := c.ListMyIssuesWithResponse(cmd.Context(), &apigen.ListMyIssuesParams{Filter: &f, PerPage: &per})
				if err != nil {
					return nil, err
				}
				pg, err := must(r.JSON200, "issues")
				if err != nil {
					return nil, err
				}
				return pg.Issues, nil
			}
			assigned, err := get(apigen.ListMyIssuesParamsFilterAssigned)
			if err != nil {
				return err
			}
			created, err := get(apigen.ListMyIssuesParamsFilterCreated)
			if err != nil {
				return err
			}
			out := map[string]any{"assigned": assigned, "created": created}
			return a.print(cmd, out, func(w io.Writer) error {
				for _, sec := range []struct {
					title string
					list  []apigen.IssueSummary
				}{{"Assigned to you", assigned}, {"Opened by you", created}} {
					fmt.Fprintln(w, a.io.Bold(sec.title))
					if len(sec.list) == 0 {
						fmt.Fprintln(w, a.io.Dim("  nothing"))
					} else if err := printIssueList(w, a, sec.list, true); err != nil {
						return err
					}
					fmt.Fprintln(w)
				}
				return nil
			})
		},
	}
	return withOutput(cmd, "IssueStatus")
}

type issueView struct {
	apigen.Issue
	CommentsList []apigen.Comment `json:"comment_list"`
}

func newIssueViewCmd(a *app) *cobra.Command {
	var (
		repoFlag string
		comments bool
	)
	cmd := &cobra.Command{
		Use:   "view NUMBER | URL",
		Short: "Show an issue (with --json: including all comments)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, repo, n, err := a.resolveIssue(repoFlag, args[0])
			if err != nil {
				return err
			}
			r, err := c.GetIssueWithResponse(cmd.Context(), repo, n)
			if err != nil {
				return pullHint(err, repo, n)
			}
			is, err := must(r.JSON200, "issue")
			if err != nil {
				return err
			}
			cr, err := c.ListIssueCommentsWithResponse(cmd.Context(), repo, n)
			if err != nil {
				return err
			}
			cs, err := must(cr.JSON200, "comments")
			if err != nil {
				return err
			}
			v := &issueView{Issue: *is, CommentsList: *cs}
			return a.print(cmd, v, func(w io.Writer) error {
				fmt.Fprintf(w, "%s %s\n", a.io.Bold(is.Title), a.io.Dim("#"+strconv.FormatInt(is.Number, 10)))
				fmt.Fprintf(w, "%s · opened by %s %s · %d comment(s)\n", issueStateLabel(a, string(is.State), is.StateReason), is.Author.Username, ago(&is.CreatedAt), is.Comments)
				kv := [][2]string{}
				if len(is.Labels) > 0 {
					kv = append(kv, [2]string{"labels", labelChips(is.Labels)})
				}
				if len(is.Assignees) > 0 {
					kv = append(kv, [2]string{"assignees", userNames(is.Assignees)})
				}
				if is.Milestone != nil {
					kv = append(kv, [2]string{"milestone", is.Milestone.Title})
				}
				if err := printFields(w, a.io, kv); err != nil {
					return err
				}
				if body := strings.TrimSpace(is.Body); body != "" {
					fmt.Fprintln(w)
					fmt.Fprintln(w, body)
				}
				if comments {
					for _, cm := range *cs {
						fmt.Fprintf(w, "\n%s %s %s\n%s\n", a.io.Bold(cm.Author.Username), a.io.Dim(ago(&cm.CreatedAt)), a.io.Dim(fmt.Sprintf("[id %d]", cm.Id)), indent(cm.Body, "  "))
					}
				}
				return nil
			})
		},
	}
	addRepoFlag(cmd, &repoFlag)
	cmd.Flags().BoolVarP(&comments, "comments", "c", false, "also show comments")
	return withOutput(cmd, "IssueView")
}

// pullHint points at `tgp pr` when an issue number belongs to a pull request.
func pullHint(err error, repo string, n int64) error {
	if e, ok := err.(*Error); ok && e.Details != nil && e.Details["is_pull"] == true {
		e.Hint = fmt.Sprintf("use: tgp pr view %d -R %s", n, repo)
	}
	return err
}

func newIssueCreateCmd(a *app) *cobra.Command {
	var repoFlag, title, body, bodyFile, labels, assignees, milestone string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Open an issue",
		Example: `  tgp issue create -t "Crash on join" -b "Steps: ..." -l bug -a mia
  tgp issue create -t "Shop UI" -F spec.md -m "v1.0"`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, repo, err := a.resolveRepo(repoFlag)
			if err != nil {
				return err
			}
			b, err := a.readBody(body, bodyFile)
			if err != nil {
				return err
			}
			if title == "" {
				if err := a.io.requirePrompt("a title", "--title"); err != nil {
					return err
				}
				if title, err = a.io.Prompt("Title", ""); err != nil {
					return err
				}
			}
			req := apigen.CreateIssue{Title: title, Labels: listPtr(labels), Assignees: listPtr(assignees)}
			if b != "" {
				req.Body = &b
			}
			if req.Milestone, err = milestoneID(cmd.Context(), c, repo, milestone); err != nil {
				return err
			}
			r, err := c.CreateIssueWithResponse(cmd.Context(), repo, req)
			if err != nil {
				return err
			}
			is, err := must(r.JSON201, "issue")
			if err != nil {
				return err
			}
			return a.print(cmd, is, func(w io.Writer) error {
				fmt.Fprintf(w, "%s Opened issue #%d: %s\n", a.io.SuccessIcon(), is.Number, is.Title)
				fmt.Fprintf(w, "  %s/%s/issues/%d\n", c.t.Base, repo, is.Number)
				return nil
			})
		},
	}
	addRepoFlag(cmd, &repoFlag)
	f := cmd.Flags()
	f.StringVarP(&title, "title", "t", "", "title")
	f.StringVarP(&body, "body", "b", "", "description (markdown)")
	f.StringVarP(&bodyFile, "body-file", "F", "", "read the description from a `file` (- for stdin)")
	f.StringVarP(&labels, "label", "l", "", "comma-separated labels")
	f.StringVarP(&assignees, "assignee", "a", "", "comma-separated usernames")
	f.StringVarP(&milestone, "milestone", "m", "", "milestone title or id")
	return withOutput(cmd, "Issue")
}

func newIssueCommentCmd(a *app) *cobra.Command {
	var repoFlag, body, bodyFile string
	cmd := &cobra.Command{
		Use:   "comment NUMBER | URL",
		Short: "Comment on an issue",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			b, err := a.readBody(body, bodyFile)
			if err != nil {
				return err
			}
			if strings.TrimSpace(b) == "" {
				return usageErr("--body (or --body-file) is required")
			}
			c, repo, n, err := a.resolveIssue(repoFlag, args[0])
			if err != nil {
				return err
			}
			r, err := c.CreateIssueCommentWithResponse(cmd.Context(), repo, n, apigen.CreateIssueCommentJSONRequestBody{Body: b})
			if err != nil {
				return pullHint(err, repo, n)
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
	cmd.Flags().StringVarP(&body, "body", "b", "", "comment text (markdown)")
	cmd.Flags().StringVarP(&bodyFile, "body-file", "F", "", "read the comment from a `file` (- for stdin)")
	return withOutput(cmd, "Comment")
}

// metaFlags are the label/assignee/milestone flags shared by issue edit
// and pr edit.
type metaFlags struct {
	addLabels, removeLabels, setLabels          string
	addAssignees, removeAssignees, setAssignees string
	milestone                                   string
}

func (m *metaFlags) register(cmd *cobra.Command) {
	f := cmd.Flags()
	f.StringVar(&m.addLabels, "add-label", "", "comma-separated labels to add")
	f.StringVar(&m.removeLabels, "remove-label", "", "comma-separated labels to remove")
	f.StringVar(&m.setLabels, "labels", "", "replace all labels with this comma-separated list (\"\" with --labels= clears)")
	f.StringVar(&m.addAssignees, "add-assignee", "", "comma-separated usernames to assign")
	f.StringVar(&m.removeAssignees, "remove-assignee", "", "comma-separated usernames to unassign")
	f.StringVar(&m.setAssignees, "assignees", "", "replace all assignees with this comma-separated list")
	f.StringVarP(&m.milestone, "milestone", "m", "", "milestone title or id; none removes it")
}

func (m *metaFlags) changed(cmd *cobra.Command) bool {
	for _, n := range []string{"add-label", "remove-label", "labels", "add-assignee", "remove-assignee", "assignees", "milestone"} {
		if cmd.Flags().Changed(n) {
			return true
		}
	}
	return false
}

func setList(cmd *cobra.Command, name, v string) *[]string {
	if !cmd.Flags().Changed(name) {
		return nil
	}
	l := splitList(v)
	if l == nil {
		l = []string{}
	}
	return &l
}

func newIssueEditCmd(a *app) *cobra.Command {
	var (
		repoFlag, title, body, bodyFile string
		meta                            metaFlags
	)
	cmd := &cobra.Command{
		Use:   "edit NUMBER | URL",
		Short: "Change an issue's title, description, labels, assignees or milestone",
		Example: `  tgp issue edit 12 --add-label bug,P1 --add-assignee mia
  tgp issue edit 12 --milestone none --remove-label needs-testing
  tgp issue edit 12 --title "Crash on join (1.20)"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, repo, n, err := a.resolveIssue(repoFlag, args[0])
			if err != nil {
				return err
			}
			req := apigen.UpdateIssue{
				AddLabels: listPtr(meta.addLabels), RemoveLabels: listPtr(meta.removeLabels), Labels: setList(cmd, "labels", meta.setLabels),
				AddAssignees: listPtr(meta.addAssignees), RemoveAssignees: listPtr(meta.removeAssignees), Assignees: setList(cmd, "assignees", meta.setAssignees),
			}
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
			if req.Milestone, err = milestoneID(cmd.Context(), c, repo, meta.milestone); err != nil {
				return err
			}
			if req.Title == nil && req.Body == nil && !meta.changed(cmd) {
				return usageErr("nothing to change: pass --title, --body or label/assignee/milestone flags")
			}
			r, err := c.UpdateIssueWithResponse(cmd.Context(), repo, n, req)
			if err != nil {
				return pullHint(err, repo, n)
			}
			is, err := must(r.JSON200, "issue")
			if err != nil {
				return err
			}
			return a.print(cmd, is, func(w io.Writer) error {
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
	meta.register(cmd)
	return withOutput(cmd, "Issue")
}

func newIssueStateCmd(a *app, verb string) *cobra.Command {
	var (
		repoFlag, comment string
		notPlanned        bool
	)
	short := "Close an issue"
	if verb == "reopen" {
		short = "Reopen a closed issue"
	}
	cmd := &cobra.Command{
		Use:   verb + " NUMBER | URL",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, repo, n, err := a.resolveIssue(repoFlag, args[0])
			if err != nil {
				return err
			}
			if comment != "" {
				if _, err := c.CreateIssueCommentWithResponse(cmd.Context(), repo, n, apigen.CreateIssueCommentJSONRequestBody{Body: comment}); err != nil {
					return pullHint(err, repo, n)
				}
			}
			st := apigen.UpdateIssueStateClosed
			if verb == "reopen" {
				st = apigen.UpdateIssueStateOpen
			}
			req := apigen.UpdateIssue{State: &st}
			if notPlanned {
				r := apigen.IssueStateReasonNotPlanned
				req.StateReason = &r
			}
			r, err := c.UpdateIssueWithResponse(cmd.Context(), repo, n, req)
			if err != nil {
				return pullHint(err, repo, n)
			}
			is, err := must(r.JSON200, "issue")
			if err != nil {
				return err
			}
			return a.print(cmd, is, func(w io.Writer) error {
				_, err := fmt.Fprintf(w, "%s %s #%d\n", a.io.SuccessIcon(), map[string]string{"close": "Closed", "reopen": "Reopened"}[verb], n)
				return err
			})
		},
	}
	addRepoFlag(cmd, &repoFlag)
	cmd.Flags().StringVarP(&comment, "comment", "c", "", "leave a comment first")
	if verb == "close" {
		cmd.Flags().BoolVar(&notPlanned, "not-planned", false, "close as not planned (won't fix, duplicate…) instead of completed")
	}
	return withOutput(cmd, "Issue")
}
