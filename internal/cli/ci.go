package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/api/apigen"
)

// ExitPending is returned by tgp pr checks while checks are still running.
const ExitPending = 8

// ---- formatting ----

func runIcon(a *app, status string, conclusion *string) string {
	if status != "completed" {
		switch status {
		case "in_progress":
			return a.io.paint("33", "●")
		case "waiting":
			return a.io.paint("90", "⏸")
		}
		return a.io.paint("33", "○")
	}
	switch deref(conclusion) {
	case "success":
		return a.io.paint("32", "✓")
	case "failure", "timed_out":
		return a.io.paint("31", "✗")
	case "skipped":
		return a.io.paint("90", "-")
	}
	return a.io.paint("90", "⊘")
}

func runResult(status string, conclusion *string) string {
	if status == "completed" && conclusion != nil {
		return string(*conclusion)
	}
	return status
}

func duration(start, end *time.Time) string {
	if start == nil {
		return ""
	}
	e := time.Now()
	if end != nil {
		e = *end
	}
	d := e.Sub(*start).Round(time.Second)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
}

func shortSHA(s string) string {
	if len(s) > 7 {
		return s[:7]
	}
	return s
}

func strConclusion(c *apigen.Conclusion) *string {
	if c == nil {
		return nil
	}
	s := string(*c)
	return &s
}

// ---- tgp run ----

func newRunCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "run",
		Aliases: []string{"runs"},
		Short:   "View, follow, re-run and cancel CI runs",
		Long:    "Runs are selected by their number in the repository (tgp run list shows them).",
	}
	cmd.AddCommand(newRunListCmd(a), newRunViewCmd(a), newRunLogsCmd(a), newRunWatchCmd(a), newRunRerunCmd(a), newRunCancelCmd(a))
	return group(cmd)
}

func newRunListCmd(a *app) *cobra.Command {
	var (
		repoFlag, workflow, branch, event, status, actor, sha string
		pull, limit                                           int
	)
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List recent runs",
		Example: `  tgp run list --status failure --branch main
  tgp run list --pr 12 --json number,status,conclusion`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, repo, err := a.resolveRepo(repoFlag)
			if err != nil {
				return err
			}
			p := &apigen.ListRunsParams{}
			if workflow != "" {
				p.Workflow = &workflow
			}
			if branch != "" {
				p.Branch = &branch
			}
			if event != "" {
				ev := apigen.RunEvent(event)
				p.Event = &ev
			}
			if status != "" {
				st := apigen.ListRunsParamsStatus(status)
				p.Status = &st
			}
			if actor != "" {
				p.Actor = &actor
			}
			if sha != "" {
				p.Sha = &sha
			}
			if pull > 0 {
				n := int64(pull)
				p.Pull = &n
			}
			per := min(max(limit, 1), 100)
			p.PerPage = &per
			r, err := c.ListRunsWithResponse(cmd.Context(), repo, p)
			if err != nil {
				return err
			}
			page, err := must(r.JSON200, "runs")
			if err != nil {
				return err
			}
			return a.print(cmd, page.Runs, func(w io.Writer) error {
				if len(page.Runs) == 0 {
					_, err := fmt.Fprintln(w, "No runs.")
					return err
				}
				rows := make([][]string, len(page.Runs))
				for i, r := range page.Runs {
					ref := deref(r.HeadBranch)
					if r.PullNumber != nil {
						ref = fmt.Sprintf("#%d %s", *r.PullNumber, ref)
					}
					rows[i] = []string{runIcon(a, string(r.Status), strConclusion(r.Conclusion)), "#" + strconv.FormatInt(r.Number, 10),
						truncate(r.Title, 50), r.WorkflowName, string(r.Event), ref, shortSHA(r.HeadSha),
						duration(r.StartedAt, r.CompletedAt), ago(&r.CreatedAt)}
				}
				return printTable(w, a.io, []string{"", "run", "title", "workflow", "event", "ref", "commit", "took", "created"}, rows)
			})
		},
	}
	addRepoFlag(cmd, &repoFlag)
	f := cmd.Flags()
	f.StringVarP(&workflow, "workflow", "w", "", "workflow file, e.g. ci.yml")
	f.StringVarP(&branch, "branch", "b", "", "branch")
	f.StringVarP(&event, "event", "e", "", "push, pull_request, workflow_dispatch or release")
	f.StringVarP(&status, "status", "s", "", "active, queued, in_progress, completed, success, failure, cancelled, skipped or timed_out")
	f.StringVarP(&actor, "user", "u", "", "who started the run (@me for you)")
	f.StringVar(&sha, "commit", "", "commit SHA (prefix)")
	f.IntVar(&pull, "pr", 0, "pull request number")
	f.IntVarP(&limit, "limit", "L", 20, "maximum runs to list (max 100)")
	return withOutput(cmd, "[]WorkflowRun")
}

// resolveRun parses a run number, defaulting to the latest run of the
// current branch (or of the repository).
func (a *app) resolveRun(ctx context.Context, repoFlag string, args []string) (*apiClient, string, int64, error) {
	c, repo, err := a.resolveRepo(repoFlag)
	if err != nil {
		return nil, "", 0, err
	}
	if len(args) > 0 {
		n, err := strconv.ParseInt(strings.TrimPrefix(args[0], "#"), 10, 64)
		if err != nil || n < 1 {
			return nil, "", 0, usageErr("invalid run number %q", args[0])
		}
		return c, repo, n, nil
	}
	p := &apigen.ListRunsParams{}
	one := 1
	p.PerPage = &one
	if b := currentBranch(); b != "" {
		p.Branch = &b
	}
	r, err := c.ListRunsWithResponse(ctx, repo, p)
	if err != nil {
		return nil, "", 0, err
	}
	page, err := must(r.JSON200, "runs")
	if err != nil {
		return nil, "", 0, err
	}
	if len(page.Runs) == 0 {
		return nil, "", 0, &Error{Code: "not_found", Exit: ExitNotFound, Message: "no runs found", Hint: "pass a run number; tgp run list shows them"}
	}
	return c, repo, page.Runs[0].Number, nil
}

func getRun(ctx context.Context, c *apiClient, repo string, n int64) (*apigen.WorkflowRunDetail, error) {
	r, err := c.GetRunWithResponse(ctx, repo, n)
	if err != nil {
		return nil, err
	}
	return must(r.JSON200, "run")
}

func (a *app) printRun(w io.Writer, run *apigen.WorkflowRunDetail) error {
	ref := deref(run.HeadBranch)
	if run.PullNumber != nil {
		ref = fmt.Sprintf("pull request #%d (%s)", *run.PullNumber, ref)
	}
	fmt.Fprintf(w, "%s %s #%d: %s\n", runIcon(a, string(run.Status), strConclusion(run.Conclusion)), run.WorkflowName, run.Number, run.Title)
	actor := "-"
	if run.Actor != nil {
		actor = run.Actor.Username
	}
	kv := [][2]string{
		{"result", runResult(string(run.Status), strConclusion(run.Conclusion))},
		{"event", string(run.Event) + " on " + ref},
		{"commit", run.HeadSha},
		{"actor", actor},
		{"attempt", strconv.Itoa(run.Attempt)},
		{"took", duration(run.StartedAt, run.CompletedAt)},
	}
	if run.StatusReason != nil {
		kv = append(kv, [2]string{"reason", *run.StatusReason})
	}
	if len(run.Inputs) > 0 {
		var parts []string
		for _, k := range sortedKeys(run.Inputs) {
			parts = append(parts, k+"="+run.Inputs[k])
		}
		kv = append(kv, [2]string{"inputs", strings.Join(parts, " ")})
	}
	if err := printFields(w, a.io, kv); err != nil {
		return err
	}
	if len(run.Jobs) == 0 {
		return nil
	}
	fmt.Fprintln(w, "\nJOBS")
	for _, j := range run.Jobs {
		fmt.Fprintf(w, "%s %s (id %d) %s\n", runIcon(a, string(j.Status), strConclusion(j.Conclusion)), j.Name, j.Id, a.io.paint("90", duration(j.StartedAt, j.CompletedAt)))
		if j.WaitReason != nil {
			fmt.Fprintf(w, "    %s\n", a.io.paint("33", *j.WaitReason))
		}
		if j.FailureReason != nil {
			fmt.Fprintf(w, "    %s\n", a.io.paint("31", *j.FailureReason))
		}
		for _, s := range j.Steps {
			if s.Status == "completed" && s.Conclusion != nil && *s.Conclusion == "success" {
				continue
			}
			var c *string
			if s.Conclusion != nil {
				v := string(*s.Conclusion)
				c = &v
			}
			if s.Status == "pending" {
				continue
			}
			fmt.Fprintf(w, "    %s %s\n", runIcon(a, string(s.Status), c), s.Name)
		}
	}
	if run.Status == "completed" && deref(strConclusion(run.Conclusion)) != "success" {
		fmt.Fprintf(w, "\nLogs of failed jobs: tgp run logs %d --failed\n", run.Number)
	}
	return nil
}

func newRunViewCmd(a *app) *cobra.Command {
	var repoFlag string
	cmd := &cobra.Command{
		Use:   "view [NUMBER]",
		Short: "Show a run's jobs and steps (default: the latest run of the current branch)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, repo, n, err := a.resolveRun(cmd.Context(), repoFlag, args)
			if err != nil {
				return err
			}
			run, err := getRun(cmd.Context(), c, repo, n)
			if err != nil {
				return err
			}
			return a.print(cmd, run, func(w io.Writer) error { return a.printRun(w, run) })
		},
	}
	addRepoFlag(cmd, &repoFlag)
	return withOutput(cmd, "WorkflowRunDetail")
}

// logLine is one line of `tgp run logs --json`.
type logLine struct {
	JobID    int64     `json:"job_id"`
	Job      string    `json:"job"`
	Step     int       `json:"step"`
	StepName string    `json:"step_name"`
	N        int       `json:"n"`
	T        time.Time `json:"t"`
	Text     string    `json:"text"`
}

func stepName(j apigen.Job, n int) string {
	switch n {
	case 0:
		return "Set up job"
	case -1:
		return "Complete job"
	}
	for _, s := range j.Steps {
		if s.Number == n {
			return s.Name
		}
	}
	return "step " + strconv.Itoa(n)
}

// failedSteps returns the numbers of a job's failed steps.
func failedSteps(j apigen.Job) map[int]bool {
	out := map[int]bool{}
	for _, s := range j.Steps {
		if s.Conclusion != nil && *s.Conclusion == apigen.JobStepConclusionFailure {
			out[s.Number] = true
		}
	}
	return out
}

func jobLogs(ctx context.Context, c *apiClient, repo string, job apigen.Job, after int) ([]apigen.LogLine, int, bool, error) {
	var all []apigen.LogLine
	for {
		limit := 20000
		r, err := c.GetJobLogsWithResponse(ctx, repo, job.Id, &apigen.GetJobLogsParams{After: &after, Limit: &limit})
		if err != nil {
			return nil, after, false, err
		}
		page, err := must(r.JSON200, "logs")
		if err != nil {
			return nil, after, false, err
		}
		all = append(all, page.Lines...)
		done := page.Complete || len(page.Lines) == 0
		after = page.Next
		if done || len(page.Lines) < limit {
			return all, after, page.Complete, nil
		}
	}
}

func newRunLogsCmd(a *app) *cobra.Command {
	var (
		repoFlag, jobSel string
		failed, follow   bool
		tail             int
	)
	cmd := &cobra.Command{
		Use:   "logs [NUMBER]",
		Short: "Print job logs (default: the latest run of the current branch)",
		Long: `Prints the logs of a run's jobs, each line prefixed with the job and step.
--failed limits output to failed jobs (and, within them, failed steps), which is
usually what you need to fix a build. --follow streams a running job's log.`,
		Example: `  tgp run logs 34 --failed --tail 50
  tgp run logs --job test --follow`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			c, repo, n, err := a.resolveRun(ctx, repoFlag, args)
			if err != nil {
				return err
			}
			run, err := getRun(ctx, c, repo, n)
			if err != nil {
				return err
			}
			var jobs []apigen.Job
			for _, j := range run.Jobs {
				switch {
				case jobSel != "":
					if strconv.FormatInt(j.Id, 10) == jobSel || strings.EqualFold(j.Name, jobSel) || strings.EqualFold(j.Key, jobSel) {
						jobs = append(jobs, j)
					}
				case failed:
					if j.Conclusion != nil && (*j.Conclusion == apigen.ConclusionFailure || *j.Conclusion == apigen.ConclusionTimedOut) {
						jobs = append(jobs, j)
					}
				default:
					if j.Status != apigen.JobStatusPending && j.Status != apigen.JobStatusQueued {
						jobs = append(jobs, j)
					}
				}
			}
			if len(jobs) == 0 {
				what := "no job has started yet"
				if jobSel != "" {
					what = fmt.Sprintf("no job %q in run #%d", jobSel, n)
				} else if failed {
					what = fmt.Sprintf("no failed jobs in run #%d (%s)", n, runResult(string(run.Status), strConclusion(run.Conclusion)))
				}
				return &Error{Code: "not_found", Exit: ExitNotFound, Message: what}
			}
			if follow && len(jobs) > 1 {
				// Follow the first unfinished job, or the last one.
				pick := jobs[len(jobs)-1]
				for _, j := range jobs {
					if j.Status != apigen.JobStatusCompleted {
						pick = j
						break
					}
				}
				jobs = []apigen.Job{pick}
			}
			var out []logLine
			emit := func(w io.Writer, j apigen.Job, lines []apigen.LogLine) {
				only := map[int]bool(nil)
				if failed {
					if fs := failedSteps(j); len(fs) > 0 {
						only = fs
					}
				}
				var keep []apigen.LogLine
				for _, l := range lines {
					if only == nil || only[l.Step] {
						keep = append(keep, l)
					}
				}
				if tail > 0 && len(keep) > tail {
					keep = keep[len(keep)-tail:]
				}
				for _, l := range keep {
					ll := logLine{JobID: j.Id, Job: j.Name, Step: l.Step, StepName: stepName(j, l.Step), N: l.N, T: l.T, Text: l.Text}
					if a.out.jsonMode() {
						out = append(out, ll)
						continue
					}
					fmt.Fprintf(w, "%s\t%s\t%s\n", a.io.paint("36", j.Name), a.io.paint("90", ll.StepName), l.Text)
				}
			}
			w := a.io.Out
			for _, j := range jobs {
				lines, next, complete, err := jobLogs(ctx, c, repo, j, 0)
				if err != nil {
					return err
				}
				emit(w, j, lines)
				for follow && !complete {
					select {
					case <-ctx.Done():
						return ctx.Err()
					case <-time.After(2 * time.Second):
					}
					var more []apigen.LogLine
					more, next, complete, err = jobLogs(ctx, c, repo, j, next)
					if err != nil {
						return err
					}
					tail = 0 // only the initial batch is tailed
					emit(w, j, more)
				}
			}
			if a.out.jsonMode() {
				if out == nil {
					out = []logLine{}
				}
				return a.printJSON(cmd, out)
			}
			return nil
		},
	}
	addRepoFlag(cmd, &repoFlag)
	f := cmd.Flags()
	f.StringVarP(&jobSel, "job", "j", "", "only this job (id, name or key)")
	f.BoolVar(&failed, "failed", false, "only failed jobs (and their failed steps)")
	f.IntVarP(&tail, "tail", "n", 0, "only the last N lines per job")
	f.BoolVarP(&follow, "follow", "f", false, "keep printing new lines until the job finishes")
	return withOutput(cmd, "[]RunLogLine")
}

func newRunWatchCmd(a *app) *cobra.Command {
	var (
		repoFlag   string
		interval   time.Duration
		exitStatus bool
	)
	cmd := &cobra.Command{
		Use:   "watch [NUMBER]",
		Short: "Wait for a run to finish, showing progress",
		Long:  "Waits until the run completes. With --exit-status the exit code is 1 unless it succeeded.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			c, repo, n, err := a.resolveRun(ctx, repoFlag, args)
			if err != nil {
				return err
			}
			last := ""
			for {
				run, err := getRun(ctx, c, repo, n)
				if err != nil {
					return err
				}
				if !a.out.jsonMode() {
					var parts []string
					for _, j := range run.Jobs {
						parts = append(parts, runIcon(a, string(j.Status), strConclusion(j.Conclusion))+" "+j.Name)
					}
					line := fmt.Sprintf("#%d %s: %s", run.Number, runResult(string(run.Status), strConclusion(run.Conclusion)), strings.Join(parts, "  "))
					if line != last {
						fmt.Fprintf(a.io.ErrOut, "%s %s\n", time.Now().Format("15:04:05"), line)
						last = line
					}
				}
				if run.Status == apigen.RunStatusCompleted {
					if err := a.print(cmd, run, func(w io.Writer) error { return a.printRun(w, run) }); err != nil {
						return err
					}
					if exitStatus && (run.Conclusion == nil || *run.Conclusion != apigen.ConclusionSuccess) {
						return &Error{Code: "run_failed", Exit: ExitError, Message: fmt.Sprintf("run #%d %s", run.Number, *run.Conclusion), silent: !a.out.jsonMode()}
					}
					return nil
				}
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(interval):
				}
			}
		},
	}
	addRepoFlag(cmd, &repoFlag)
	cmd.Flags().DurationVarP(&interval, "interval", "i", 3*time.Second, "polling interval")
	cmd.Flags().BoolVar(&exitStatus, "exit-status", false, "exit with 1 unless the run succeeded")
	return withOutput(cmd, "WorkflowRunDetail")
}

func newRunRerunCmd(a *app) *cobra.Command {
	var (
		repoFlag, jobSel string
		failed           bool
	)
	cmd := &cobra.Command{
		Use:   "rerun NUMBER",
		Short: "Re-run a finished run, its failed jobs, or one job (testers can too)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			c, repo, n, err := a.resolveRun(ctx, repoFlag, args)
			if err != nil {
				return err
			}
			var run *apigen.WorkflowRunDetail
			if jobSel != "" {
				cur, err := getRun(ctx, c, repo, n)
				if err != nil {
					return err
				}
				var id int64
				for _, j := range cur.Jobs {
					if strconv.FormatInt(j.Id, 10) == jobSel || strings.EqualFold(j.Name, jobSel) {
						id = j.Id
					}
				}
				if id == 0 {
					return &Error{Code: "not_found", Exit: ExitNotFound, Message: fmt.Sprintf("no job %q in run #%d", jobSel, n)}
				}
				r, err := c.RerunJobWithResponse(ctx, repo, id)
				if err != nil {
					return err
				}
				if run, err = must(r.JSON200, "run"); err != nil {
					return err
				}
			} else {
				r, err := c.RerunRunWithResponse(ctx, repo, n, apigen.RerunRequest{FailedOnly: &failed})
				if err != nil {
					return err
				}
				if run, err = must(r.JSON200, "run"); err != nil {
					return err
				}
			}
			return a.print(cmd, run, func(w io.Writer) error {
				_, err := fmt.Fprintf(w, "%s Started attempt %d of run #%d. Follow it with: tgp run watch %d\n", a.io.SuccessIcon(), run.Attempt, run.Number, run.Number)
				return err
			})
		},
	}
	addRepoFlag(cmd, &repoFlag)
	cmd.Flags().BoolVar(&failed, "failed", false, "only re-run failed or cancelled jobs (and the jobs that need them)")
	cmd.Flags().StringVarP(&jobSel, "job", "j", "", "re-run one job (id or name) and the jobs that need it")
	return withOutput(cmd, "WorkflowRunDetail")
}

func newRunCancelCmd(a *app) *cobra.Command {
	var repoFlag string
	cmd := &cobra.Command{
		Use:   "cancel NUMBER",
		Short: "Cancel a run (write access)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, repo, n, err := a.resolveRun(cmd.Context(), repoFlag, args)
			if err != nil {
				return err
			}
			r, err := c.CancelRunWithResponse(cmd.Context(), repo, n)
			if err != nil {
				return err
			}
			run, err := must(r.JSON200, "run")
			if err != nil {
				return err
			}
			return a.print(cmd, run, func(w io.Writer) error {
				_, err := fmt.Fprintf(w, "%s Cancelled run #%d\n", a.io.SuccessIcon(), run.Number)
				return err
			})
		},
	}
	addRepoFlag(cmd, &repoFlag)
	return withOutput(cmd, "WorkflowRunDetail")
}

// ---- tgp workflow ----

func newWorkflowCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "workflow",
		Aliases: []string{"workflows"},
		Short:   "List, check and run workflows",
	}
	cmd.AddCommand(newWorkflowListCmd(a), newWorkflowRunCmd(a), newWorkflowLintCmd(a))
	return group(cmd)
}

func newWorkflowListCmd(a *app) *cobra.Command {
	var repoFlag, ref string
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List workflow files, with their triggers and any errors",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, repo, err := a.resolveRepo(repoFlag)
			if err != nil {
				return err
			}
			p := &apigen.ListWorkflowsParams{}
			if ref != "" {
				p.Ref = &ref
			}
			r, err := c.ListWorkflowsWithResponse(cmd.Context(), repo, p)
			if err != nil {
				return err
			}
			wfs, err := must(r.JSON200, "workflows")
			if err != nil {
				return err
			}
			return a.print(cmd, wfs, func(w io.Writer) error {
				if len(*wfs) == 0 {
					_, err := fmt.Fprintf(w, "No workflows. Add YAML files under .tgp/workflows/ (see docs/ci.md).\n")
					return err
				}
				for _, wf := range *wfs {
					var trig []string
					for _, t := range wf.Triggers {
						trig = append(trig, string(t))
					}
					icon := a.io.SuccessIcon()
					if !wf.Valid {
						icon = a.io.paint("31", "✗")
					}
					fmt.Fprintf(w, "%s %s  %s  %s\n", icon, wf.File, wf.Name, a.io.paint("90", strings.Join(trig, ", ")))
					for _, e := range wf.Errors {
						pos := ""
						if e.Line != nil {
							pos = fmt.Sprintf("%d:%d: ", *e.Line, deref(e.Column))
						}
						fmt.Fprintf(w, "    %s%s\n", pos, a.io.paint("31", e.Message))
					}
					for _, in := range wf.DispatchInputs {
						req := ""
						if in.Required {
							req = " (required)"
						}
						fmt.Fprintf(w, "    input %s: %s%s %s\n", in.Name, in.Type, req, a.io.paint("90", in.Description))
					}
				}
				return nil
			})
		},
	}
	addRepoFlag(cmd, &repoFlag)
	cmd.Flags().StringVar(&ref, "ref", "", "branch, tag or commit (default: the default branch)")
	return withOutput(cmd, "[]Workflow")
}

func newWorkflowRunCmd(a *app) *cobra.Command {
	var (
		repoFlag, ref string
		fields        []string
	)
	cmd := &cobra.Command{
		Use:   "run WORKFLOW",
		Short: "Start a workflow that has a workflow_dispatch trigger (write access)",
		Example: `  tgp workflow run deploy.yml -f env=staging -f dry=false
  tgp workflow run deploy.yml --ref release/1.2`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, repo, err := a.resolveRepo(repoFlag)
			if err != nil {
				return err
			}
			body := apigen.DispatchRequest{}
			if ref != "" {
				body.Ref = &ref
			}
			inputs := map[string]string{}
			for _, kv := range fields {
				k, v, ok := strings.Cut(kv, "=")
				if !ok || k == "" {
					return usageErr("-f takes key=value, got %q", kv)
				}
				inputs[k] = v
			}
			if len(inputs) > 0 {
				body.Inputs = &inputs
			}
			r, err := c.DispatchWorkflowWithResponse(cmd.Context(), repo, args[0], body)
			if err != nil {
				return err
			}
			run, err := must(r.JSON201, "run")
			if err != nil {
				return err
			}
			return a.print(cmd, run, func(w io.Writer) error {
				_, err := fmt.Fprintf(w, "%s Started run #%d of %s. Follow it with: tgp run watch %d\n", a.io.SuccessIcon(), run.Number, run.WorkflowName, run.Number)
				return err
			})
		},
	}
	addRepoFlag(cmd, &repoFlag)
	cmd.Flags().StringVar(&ref, "ref", "", "branch or tag to run on (default: the default branch)")
	cmd.Flags().StringArrayVarP(&fields, "field", "f", nil, "input as key=value (repeatable)")
	return withOutput(cmd, "WorkflowRunDetail")
}

// ---- tgp secret ----

func newSecretCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "secret",
		Aliases: []string{"secrets"},
		Short:   "Manage CI secrets (repository: write access; --org: admins)",
		Long: `Secrets are available to workflows as ${{ secrets.NAME }} and are masked in logs.
Repository secrets override organisation secrets of the same name. Values can
be set but never read back.`,
	}
	cmd.AddCommand(newSecretListCmd(a), newSecretSetCmd(a), newSecretDeleteCmd(a))
	return group(cmd)
}

func newSecretListCmd(a *app) *cobra.Command {
	var repoFlag string
	var org bool
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List secret names",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var list *[]apigen.Secret
			if org {
				c, err := a.client()
				if err != nil {
					return err
				}
				r, err := c.ListOrgSecretsWithResponse(cmd.Context())
				if err != nil {
					return err
				}
				if list, err = must(r.JSON200, "secrets"); err != nil {
					return err
				}
			} else {
				c, repo, err := a.resolveRepo(repoFlag)
				if err != nil {
					return err
				}
				r, err := c.ListRepoSecretsWithResponse(cmd.Context(), repo)
				if err != nil {
					return err
				}
				if list, err = must(r.JSON200, "secrets"); err != nil {
					return err
				}
			}
			return a.print(cmd, list, func(w io.Writer) error {
				if len(*list) == 0 {
					_, err := fmt.Fprintln(w, "No secrets.")
					return err
				}
				rows := make([][]string, len(*list))
				for i, s := range *list {
					by := ""
					if s.UpdatedBy != nil {
						by = s.UpdatedBy.Username
					}
					rows[i] = []string{s.Name, string(s.Scope), ago(&s.UpdatedAt), by}
				}
				return printTable(w, a.io, []string{"name", "scope", "updated", "by"}, rows)
			})
		},
	}
	addRepoFlag(cmd, &repoFlag)
	cmd.Flags().BoolVar(&org, "org", false, "organisation secrets (admins)")
	return withOutput(cmd, "[]Secret")
}

func newSecretSetCmd(a *app) *cobra.Command {
	var repoFlag, body string
	var org bool
	cmd := &cobra.Command{
		Use:   "set NAME",
		Short: "Create or update a secret",
		Long: `Sets a secret's value from --body, from standard input when it isn't a
terminal, or from a hidden prompt.`,
		Example: `  tgp secret set DEPLOY_KEY < key.pem
  printf %s "$TOKEN" | tgp secret set NPM_TOKEN --org`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			value := body
			if !cmd.Flags().Changed("body") {
				switch {
				case !a.io.stdinTTY:
					b, err := io.ReadAll(io.LimitReader(a.io.In, 1<<20))
					if err != nil {
						return err
					}
					value = strings.TrimRight(string(b), "\r\n")
				case a.io.CanPrompt() && !a.noInput:
					v, err := a.io.PromptPassword("Value for " + args[0])
					if err != nil {
						return err
					}
					value = v
				default:
					return a.io.requirePrompt("the secret value", "--body")
				}
			}
			if value == "" {
				return usageErr("the secret value is empty")
			}
			var s *apigen.Secret
			if org {
				c, err := a.client()
				if err != nil {
					return err
				}
				r, err := c.SetOrgSecretWithResponse(cmd.Context(), args[0], apigen.SecretValue{Value: value})
				if err != nil {
					return err
				}
				if s, err = must(r.JSON200, "secret"); err != nil {
					return err
				}
			} else {
				c, repo, err := a.resolveRepo(repoFlag)
				if err != nil {
					return err
				}
				r, err := c.SetRepoSecretWithResponse(cmd.Context(), repo, args[0], apigen.SecretValue{Value: value})
				if err != nil {
					return err
				}
				if s, err = must(r.JSON200, "secret"); err != nil {
					return err
				}
			}
			return a.print(cmd, s, func(w io.Writer) error {
				_, err := fmt.Fprintf(w, "%s Set %s secret %s\n", a.io.SuccessIcon(), s.Scope, s.Name)
				return err
			})
		},
	}
	addRepoFlag(cmd, &repoFlag)
	cmd.Flags().StringVarP(&body, "body", "b", "", "the value (visible in shell history; prefer stdin)")
	cmd.Flags().BoolVar(&org, "org", false, "an organisation secret (admins)")
	return withOutput(cmd, "Secret")
}

func newSecretDeleteCmd(a *app) *cobra.Command {
	var repoFlag string
	var org bool
	cmd := &cobra.Command{
		Use:   "delete NAME",
		Short: "Delete a secret",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := strings.ToUpper(args[0])
			if org {
				c, err := a.client()
				if err != nil {
					return err
				}
				if _, err := c.DeleteOrgSecretWithResponse(cmd.Context(), name); err != nil {
					return err
				}
			} else {
				c, repo, err := a.resolveRepo(repoFlag)
				if err != nil {
					return err
				}
				if _, err := c.DeleteRepoSecretWithResponse(cmd.Context(), repo, name); err != nil {
					return err
				}
			}
			return a.print(cmd, map[string]any{"name": name, "deleted": true}, func(w io.Writer) error {
				_, err := fmt.Fprintf(w, "%s Deleted secret %s\n", a.io.SuccessIcon(), name)
				return err
			})
		},
	}
	addRepoFlag(cmd, &repoFlag)
	cmd.Flags().BoolVar(&org, "org", false, "an organisation secret (admins)")
	return withOutput(cmd, "Deleted")
}

// ---- tgp pr checks ----

func newPRChecksCmd(a *app) *cobra.Command {
	var (
		repoFlag string
		watch    bool
		required bool
		interval time.Duration
	)
	cmd := &cobra.Command{
		Use:   "checks [NUMBER | URL | BRANCH]",
		Short: "Show CI checks for a pull request's head commit",
		Long: `Shows the CI checks of a pull request. Exit status: 0 when all checks passed
(or there are none), 1 when one failed, 8 while checks are still pending.`,
		Example: `  tgp pr checks 12 --watch
  tgp pr checks --json state,checks --jq '.checks[] | select(.conclusion=="failure") | .name'`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			c, repo, n, err := a.resolvePR(ctx, repoFlag, prSelector(args))
			if err != nil {
				return err
			}
			for {
				r, err := c.GetPullWithResponse(ctx, repo, n)
				if err != nil {
					return err
				}
				pr, err := must(r.JSON200, "pull request")
				if err != nil {
					return err
				}
				cs := pr.Checks
				if required {
					var keep []apigen.Check
					for _, ch := range cs.Checks {
						if ch.Required {
							keep = append(keep, ch)
						}
					}
					if keep == nil {
						keep = []apigen.Check{}
					}
					cs.Checks = keep
				}
				if watch && cs.State == apigen.CheckSummaryStatePending {
					if !a.out.jsonMode() {
						fmt.Fprintf(a.io.ErrOut, "%s %d/%d checks done, waiting...\n", time.Now().Format("15:04:05"), cs.Total-cs.Pending, cs.Total)
					}
					select {
					case <-ctx.Done():
						return ctx.Err()
					case <-time.After(interval):
					}
					continue
				}
				if err := a.print(cmd, cs, func(w io.Writer) error {
					if cs.Total == 0 && len(cs.MissingRequired) == 0 {
						_, err := fmt.Fprintf(w, "No checks for pull request #%d.\n", n)
						return err
					}
					fmt.Fprintf(w, "%s: %d passed, %d failed, %d pending, %d skipped\n", cs.State, cs.Succeeded, cs.Failed, cs.Pending, cs.Skipped)
					rows := [][]string{}
					for _, ch := range cs.Checks {
						req := ""
						if ch.Required {
							req = "required"
						}
						rows = append(rows, []string{runIcon(a, string(ch.Status), strConclusion(ch.Conclusion)), ch.Name, runResult(string(ch.Status), strConclusion(ch.Conclusion)),
							duration(ch.StartedAt, ch.CompletedAt), req, fmt.Sprintf("tgp run logs %d --job %d", ch.RunNumber, ch.JobId)})
					}
					for _, m := range cs.MissingRequired {
						rows = append(rows, []string{runIcon(a, "queued", nil), m, "expected", "", "required", ""})
					}
					return printTable(w, a.io, []string{"", "check", "result", "took", "", "logs"}, rows)
				}); err != nil {
					return err
				}
				switch cs.State {
				case apigen.CheckSummaryStateFailure:
					return &Error{Code: "checks_failed", Exit: ExitError, Message: fmt.Sprintf("%d check(s) failed", cs.Failed), silent: !a.out.jsonMode()}
				case apigen.CheckSummaryStatePending:
					return &Error{Code: "checks_pending", Exit: ExitPending, Message: "checks are still running", silent: !a.out.jsonMode()}
				}
				return nil
			}
		},
	}
	addRepoFlag(cmd, &repoFlag)
	cmd.Flags().BoolVarP(&watch, "watch", "w", false, "wait until no check is pending")
	cmd.Flags().BoolVar(&required, "required", false, "only required checks")
	cmd.Flags().DurationVarP(&interval, "interval", "i", 5*time.Second, "polling interval with --watch")
	return withOutput(cmd, "CheckSummary")
}

// ---- tgp admin runner / ci ----

func newAdminRunnerCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "runner",
		Aliases: []string{"runners"},
		Short:   "Manage CI runners",
	}
	group(cmd)
	list := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List runners with status, load and running jobs",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := a.client()
			if err != nil {
				return err
			}
			r, err := c.ListRunnersWithResponse(cmd.Context())
			if err != nil {
				return err
			}
			l, err := must(r.JSON200, "runners")
			if err != nil {
				return err
			}
			return a.print(cmd, l, func(w io.Writer) error {
				fmt.Fprintf(w, "%d jobs running, %d queued\n\n", l.RunningJobs, l.QueuedJobs)
				if len(l.Runners) == 0 {
					_, err := fmt.Fprintln(w, "No runners. Add one with: tgp admin runner token")
					return err
				}
				rows := make([][]string, len(l.Runners))
				for i, rn := range l.Runners {
					load := ""
					if rn.CpuPercent != nil {
						load = fmt.Sprintf("cpu %.0f%%", *rn.CpuPercent)
						if rn.MemoryPercent != nil {
							load += fmt.Sprintf(" mem %.0f%%", *rn.MemoryPercent)
						}
						if deref(rn.Overloaded) {
							load += " (overloaded)"
						}
					}
					rows[i] = []string{strconv.FormatInt(rn.Id, 10), rn.Name, string(rn.Status), fmt.Sprintf("%d/%d", rn.Busy, rn.Capacity),
						strings.Join(rn.Labels, ","), load, ago(rn.LastSeenAt)}
				}
				return printTable(w, a.io, []string{"id", "name", "status", "busy", "labels", "load", "seen"}, rows)
			})
		},
	}
	view := &cobra.Command{
		Use:   "view ID",
		Short: "Show a runner",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return usageErr("invalid runner id %q", args[0])
			}
			c, err := a.client()
			if err != nil {
				return err
			}
			r, err := c.GetRunnerWithResponse(cmd.Context(), id)
			if err != nil {
				return err
			}
			rn, err := must(r.JSON200, "runner")
			if err != nil {
				return err
			}
			return a.print(cmd, rn, func(w io.Writer) error { return printRunner(a, w, rn) })
		},
	}
	var hours int
	token := &cobra.Command{
		Use:   "token",
		Short: "Create a one-time registration token for a new runner",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := a.client()
			if err != nil {
				return err
			}
			r, err := c.CreateRunnerTokenWithResponse(cmd.Context(), apigen.CreateRunnerTokenJSONRequestBody{ExpiresInHours: &hours})
			if err != nil {
				return err
			}
			t, err := must(r.JSON201, "token")
			if err != nil {
				return err
			}
			return a.print(cmd, t, func(w io.Writer) error {
				_, err := fmt.Fprintf(w, "Registration token (single use, expires %s):\n\n  %s\n\nOn the runner host (with Docker installed):\n\n  %s\n  tgp-runner run\n",
					t.ExpiresAt.Local().Format(time.RFC1123), t.Token, t.Command)
				return err
			})
		},
	}
	token.Flags().IntVar(&hours, "expires-in", 24, "hours until the token expires (1-720)")
	var name string
	var disable, enable bool
	edit := &cobra.Command{
		Use:   "edit ID",
		Short: "Rename, disable or enable a runner",
		Long:  "A disabled runner finishes its current jobs but gets no new ones.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return usageErr("invalid runner id %q", args[0])
			}
			body := apigen.UpdateRunnerJSONRequestBody{}
			if cmd.Flags().Changed("name") {
				body.Name = &name
			}
			switch {
			case disable && enable:
				return usageErr("--disable and --enable conflict")
			case disable:
				t := true
				body.Disabled = &t
			case enable:
				f := false
				body.Disabled = &f
			}
			if body.Name == nil && body.Disabled == nil {
				return usageErr("nothing to change; use --name, --disable or --enable")
			}
			c, err := a.client()
			if err != nil {
				return err
			}
			r, err := c.UpdateRunnerWithResponse(cmd.Context(), id, body)
			if err != nil {
				return err
			}
			rn, err := must(r.JSON200, "runner")
			if err != nil {
				return err
			}
			return a.print(cmd, rn, func(w io.Writer) error {
				_, err := fmt.Fprintf(w, "%s Runner %s is %s\n", a.io.SuccessIcon(), rn.Name, rn.Status)
				return err
			})
		},
	}
	edit.Flags().StringVar(&name, "name", "", "new name")
	edit.Flags().BoolVar(&disable, "disable", false, "stop giving it jobs")
	edit.Flags().BoolVar(&enable, "enable", false, "give it jobs again")
	var yes bool
	del := &cobra.Command{
		Use:   "delete ID",
		Short: "Remove a runner (its running jobs fail)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return usageErr("invalid runner id %q", args[0])
			}
			if !yes {
				if !a.io.CanPrompt() || a.noInput {
					return a.io.requirePrompt("confirmation", "--yes")
				}
				ok, err := a.io.Confirm(fmt.Sprintf("Remove runner %d? Its running jobs fail", id), false)
				if err != nil || !ok {
					return &Error{Code: CodeCancelled, Exit: ExitError, Message: "cancelled"}
				}
			}
			c, err := a.client()
			if err != nil {
				return err
			}
			if _, err := c.DeleteRunnerWithResponse(cmd.Context(), id); err != nil {
				return err
			}
			return a.print(cmd, map[string]any{"name": args[0], "deleted": true}, func(w io.Writer) error {
				_, err := fmt.Fprintf(w, "%s Removed runner %d\n", a.io.SuccessIcon(), id)
				return err
			})
		},
	}
	del.Flags().BoolVarP(&yes, "yes", "y", false, "don't ask for confirmation")
	cmd.AddCommand(withOutput(list, "RunnerList"), withOutput(view, "Runner"), withOutput(token, "RunnerRegistrationToken"),
		withOutput(edit, "Runner"), withOutput(del, "Deleted"))
	return cmd
}

func printRunner(a *app, w io.Writer, rn *apigen.Runner) error {
	kv := [][2]string{
		{"name", rn.Name}, {"status", string(rn.Status)}, {"labels", strings.Join(rn.Labels, ", ")},
		{"busy", fmt.Sprintf("%d of %d", rn.Busy, rn.Capacity)}, {"version", rn.Version}, {"platform", rn.Os + "/" + rn.Arch},
		{"last seen", ago(rn.LastSeenAt)},
	}
	if rn.HostMode != nil && *rn.HostMode != apigen.RunnerHostModeOff {
		kv = append(kv, [2]string{"host jobs", string(*rn.HostMode)})
	}
	if rn.CpuPercent != nil {
		kv = append(kv, [2]string{"cpu", fmt.Sprintf("%.0f%%", *rn.CpuPercent)})
	}
	if rn.MemoryPercent != nil {
		kv = append(kv, [2]string{"memory", fmt.Sprintf("%.0f%%", *rn.MemoryPercent)})
	}
	if err := printFields(w, a.io, kv); err != nil {
		return err
	}
	for _, j := range rn.Jobs {
		fmt.Fprintf(w, "  running: %s run #%d %s (job %d)\n", j.Repo, j.RunNumber, j.Name, j.Id)
	}
	return nil
}

// ciSettingKeys maps setting names to whether they are numbers with
// decimals (cpus), whole numbers or other types.
var ciSettingKeys = map[string]string{
	"max_concurrent_jobs": "int", "max_jobs_per_repo": "int", "max_queued_runs_per_repo": "int",
	"runner_max_cpu_percent": "int", "runner_offline_after_seconds": "int", "default_image": "string",
	"default_cpus": "float", "max_cpus": "float", "default_memory_mb": "int", "max_memory_mb": "int",
	"default_pids": "int", "max_pids": "int", "default_timeout_minutes": "int", "max_timeout_minutes": "int",
	"max_log_mb": "int", "log_retention_days": "int", "cancel_superseded": "bool",
	"artifact_max_mb": "int", "artifact_retention_days": "int", "cache_quota_mb": "int", "cache_retention_days": "int",
	"release_asset_max_mb": "int",
}

func newAdminCICmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ci",
		Short: "View and change CI limits and defaults",
	}
	group(cmd)
	view := &cobra.Command{
		Use:   "view",
		Short: "Show CI limits and defaults",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := a.client()
			if err != nil {
				return err
			}
			r, err := c.GetCISettingsWithResponse(cmd.Context())
			if err != nil {
				return err
			}
			s, err := must(r.JSON200, "settings")
			if err != nil {
				return err
			}
			return a.print(cmd, s, func(w io.Writer) error { return printCISettings(a, w, s) })
		},
	}
	set := &cobra.Command{
		Use:   "set KEY=VALUE...",
		Short: "Change CI settings",
		Long:  "Keys: " + strings.Join(sortedKeys(ciSettingKeys), ", ") + ". Limits of 0 mean no limit.",
		Example: `  tgp admin ci set max_jobs_per_repo=2 max_concurrent_jobs=8
  tgp admin ci set default_image=node:22-bookworm max_memory_mb=16384`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body := map[string]any{}
			for _, kv := range args {
				k, v, ok := strings.Cut(kv, "=")
				typ, known := ciSettingKeys[k]
				if !ok || !known {
					return usageErr("unknown setting %q; keys: %s", kv, strings.Join(sortedKeys(ciSettingKeys), ", "))
				}
				switch typ {
				case "int":
					n, err := strconv.Atoi(v)
					if err != nil {
						return usageErr("%s must be a whole number", k)
					}
					body[k] = n
				case "float":
					f, err := strconv.ParseFloat(v, 64)
					if err != nil {
						return usageErr("%s must be a number", k)
					}
					body[k] = f
				case "bool":
					b, err := strconv.ParseBool(v)
					if err != nil {
						return usageErr("%s must be true or false", k)
					}
					body[k] = b
				default:
					body[k] = v
				}
			}
			c, err := a.client()
			if err != nil {
				return err
			}
			var patch apigen.CISettingsPatch
			if err := remarshal(body, &patch); err != nil {
				return err
			}
			r, err := c.UpdateCISettingsWithResponse(cmd.Context(), patch)
			if err != nil {
				return err
			}
			s, err := must(r.JSON200, "settings")
			if err != nil {
				return err
			}
			return a.print(cmd, s, func(w io.Writer) error { return printCISettings(a, w, s) })
		},
	}
	cmd.AddCommand(withOutput(view, "CISettings"), withOutput(set, "CISettings"))
	return cmd
}

func printCISettings(a *app, w io.Writer, s *apigen.CISettings) error {
	lim := func(n int) string {
		if n == 0 {
			return "no limit"
		}
		return strconv.Itoa(n)
	}
	return printFields(w, a.io, [][2]string{
		{"max_concurrent_jobs", lim(s.MaxConcurrentJobs)},
		{"max_jobs_per_repo", lim(s.MaxJobsPerRepo)},
		{"max_queued_runs_per_repo", lim(s.MaxQueuedRunsPerRepo)},
		{"cancel_superseded", strconv.FormatBool(s.CancelSuperseded)},
		{"runner_max_cpu_percent", lim(s.RunnerMaxCpuPercent)},
		{"runner_offline_after_seconds", strconv.Itoa(s.RunnerOfflineAfterSeconds)},
		{"default_image", s.DefaultImage},
		{"cpus (default/max)", fmt.Sprintf("%g / %g", s.DefaultCpus, s.MaxCpus)},
		{"memory_mb (default/max)", fmt.Sprintf("%d / %d", s.DefaultMemoryMb, s.MaxMemoryMb)},
		{"pids (default/max)", fmt.Sprintf("%d / %d", s.DefaultPids, s.MaxPids)},
		{"timeout_minutes (default/max)", fmt.Sprintf("%d / %d", s.DefaultTimeoutMinutes, s.MaxTimeoutMinutes)},
		{"max_log_mb", strconv.Itoa(s.MaxLogMb)},
		{"log_retention_days", lim(s.LogRetentionDays)},
		{"artifact_max_mb", strconv.Itoa(s.ArtifactMaxMb)},
		{"artifact_retention_days", strconv.Itoa(s.ArtifactRetentionDays)},
		{"cache_quota_mb (per repo)", lim(s.CacheQuotaMb)},
		{"cache_retention_days (unused)", strconv.Itoa(s.CacheRetentionDays)},
		{"release_asset_max_mb", strconv.Itoa(s.ReleaseAssetMaxMb)},
	})
}

// remarshal converts v to out through JSON.
func remarshal(v, out any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}
