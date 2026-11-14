package cli

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/spf13/cobra"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/api/apigen"
)

func newLabelCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "label",
		Aliases: []string{"labels"},
		Short:   "Manage labels (changing them needs write access)",
	}
	cmd.AddCommand(newLabelListCmd(a), newLabelCreateCmd(a), newLabelEditCmd(a), newLabelDeleteCmd(a))
	return group(cmd)
}

func findLabel(ctx context.Context, c *apiClient, repo, name string) (*apigen.Label, error) {
	r, err := c.ListLabelsWithResponse(ctx, repo)
	if err != nil {
		return nil, err
	}
	ls, err := must(r.JSON200, "labels")
	if err != nil {
		return nil, err
	}
	for _, l := range *ls {
		if strings.EqualFold(l.Name, name) {
			return &l, nil
		}
	}
	return nil, &Error{Code: "not_found", Exit: ExitNotFound, Message: fmt.Sprintf("label %q not found in %s", name, repo)}
}

func newLabelListCmd(a *app) *cobra.Command {
	var repoFlag string
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List labels with their open issue counts",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, repo, err := a.resolveRepo(repoFlag)
			if err != nil {
				return err
			}
			r, err := c.ListLabelsWithResponse(cmd.Context(), repo)
			if err != nil {
				return err
			}
			ls, err := must(r.JSON200, "labels")
			if err != nil {
				return err
			}
			return a.print(cmd, ls, func(w io.Writer) error {
				rows := make([][]string, len(*ls))
				for i, l := range *ls {
					rows[i] = []string{l.Name, "#" + l.Color, strconv.Itoa(deref(l.OpenIssues)), l.Description}
				}
				return printTable(w, a.io, []string{"name", "color", "open", "description"}, rows)
			})
		},
	}
	addRepoFlag(cmd, &repoFlag)
	return withOutput(cmd, "[]Label")
}

func newLabelCreateCmd(a *app) *cobra.Command {
	var repoFlag, color, desc string
	cmd := &cobra.Command{
		Use:     "create NAME",
		Short:   "Create a label",
		Example: `  tgp label create P1 --color b60205 -d "Fix this week"`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, repo, err := a.resolveRepo(repoFlag)
			if err != nil {
				return err
			}
			if color == "" {
				color = fmt.Sprintf("%06x", time.Now().UnixNano()&0xffffff)
			}
			req := apigen.LabelInput{Name: args[0], Color: color}
			if desc != "" {
				req.Description = &desc
			}
			r, err := c.CreateLabelWithResponse(cmd.Context(), repo, req)
			if err != nil {
				return err
			}
			l, err := must(r.JSON201, "label")
			if err != nil {
				return err
			}
			return a.print(cmd, l, func(w io.Writer) error {
				_, err := fmt.Fprintf(w, "%s Created label %s (#%s)\n", a.io.SuccessIcon(), l.Name, l.Color)
				return err
			})
		},
	}
	addRepoFlag(cmd, &repoFlag)
	cmd.Flags().StringVarP(&color, "color", "c", "", "six hex digits, e.g. d73a4a (default: random)")
	cmd.Flags().StringVarP(&desc, "description", "d", "", "description")
	return withOutput(cmd, "Label")
}

func newLabelEditCmd(a *app) *cobra.Command {
	var repoFlag, name, color, desc string
	cmd := &cobra.Command{
		Use:   "edit NAME",
		Short: "Rename, recolour or describe a label",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, repo, err := a.resolveRepo(repoFlag)
			if err != nil {
				return err
			}
			l, err := findLabel(cmd.Context(), c, repo, args[0])
			if err != nil {
				return err
			}
			req := apigen.LabelPatch{}
			f := cmd.Flags()
			if f.Changed("name") {
				req.Name = &name
			}
			if f.Changed("color") {
				req.Color = &color
			}
			if f.Changed("description") {
				req.Description = &desc
			}
			if req == (apigen.LabelPatch{}) {
				return usageErr("nothing to change: pass --name, --color or --description")
			}
			r, err := c.UpdateLabelWithResponse(cmd.Context(), repo, l.Id, req)
			if err != nil {
				return err
			}
			out, err := must(r.JSON200, "label")
			if err != nil {
				return err
			}
			return a.print(cmd, out, func(w io.Writer) error {
				_, err := fmt.Fprintf(w, "%s Updated label %s\n", a.io.SuccessIcon(), out.Name)
				return err
			})
		},
	}
	addRepoFlag(cmd, &repoFlag)
	cmd.Flags().StringVarP(&name, "name", "n", "", "new name")
	cmd.Flags().StringVarP(&color, "color", "c", "", "new color")
	cmd.Flags().StringVarP(&desc, "description", "d", "", "new description")
	return withOutput(cmd, "Label")
}

func newLabelDeleteCmd(a *app) *cobra.Command {
	var repoFlag string
	cmd := &cobra.Command{
		Use:   "delete NAME",
		Short: "Delete a label (removes it from issues and pull requests)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, repo, err := a.resolveRepo(repoFlag)
			if err != nil {
				return err
			}
			l, err := findLabel(cmd.Context(), c, repo, args[0])
			if err != nil {
				return err
			}
			if _, err := c.DeleteLabelWithResponse(cmd.Context(), repo, l.Id); err != nil {
				return err
			}
			return a.print(cmd, map[string]any{"name": l.Name, "deleted": true}, func(w io.Writer) error {
				_, err := fmt.Fprintf(w, "%s Deleted label %s\n", a.io.SuccessIcon(), l.Name)
				return err
			})
		},
	}
	addRepoFlag(cmd, &repoFlag)
	return withOutput(cmd, "Deleted")
}

// ---- milestones ----

func newMilestoneCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "milestone",
		Aliases: []string{"milestones"},
		Short:   "Manage milestones (changing them needs write access)",
		Long:    "Milestones are selected by title or id.",
	}
	cmd.AddCommand(newMilestoneListCmd(a), newMilestoneViewCmd(a), newMilestoneCreateCmd(a), newMilestoneEditCmd(a),
		newMilestoneStateCmd(a, "close"), newMilestoneStateCmd(a, "reopen"), newMilestoneDeleteCmd(a))
	return group(cmd)
}

func findMilestone(ctx context.Context, c *apiClient, repo, sel string) (*apigen.Milestone, error) {
	id, err := milestoneID(ctx, c, repo, sel)
	if err != nil {
		return nil, err
	}
	if id == nil || *id == 0 {
		return nil, usageErr("give a milestone title or id")
	}
	r, err := c.GetMilestoneWithResponse(ctx, repo, *id)
	if err != nil {
		return nil, err
	}
	return must(r.JSON200, "milestone")
}

func parseDate(s string) (*openapi_types.Date, error) {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return nil, usageErr("invalid date %q: use YYYY-MM-DD", s)
	}
	return &openapi_types.Date{Time: t}, nil
}

func milestoneDue(m *apigen.Milestone) string {
	if m.DueOn == nil {
		return "-"
	}
	return m.DueOn.String()
}

func milestoneProgress(m *apigen.Milestone) string {
	total := m.OpenIssues + m.ClosedIssues
	if total == 0 {
		return "0%"
	}
	return fmt.Sprintf("%d%% (%d/%d)", m.ClosedIssues*100/total, m.ClosedIssues, total)
}

func newMilestoneListCmd(a *app) *cobra.Command {
	var repoFlag, state string
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List milestones with progress",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, repo, err := a.resolveRepo(repoFlag)
			if err != nil {
				return err
			}
			st := apigen.ListMilestonesParamsState(state)
			if st != "open" && st != "closed" && st != "all" {
				return usageErr("invalid --state %q: use open, closed or all", state)
			}
			r, err := c.ListMilestonesWithResponse(cmd.Context(), repo, &apigen.ListMilestonesParams{State: &st})
			if err != nil {
				return err
			}
			ms, err := must(r.JSON200, "milestones")
			if err != nil {
				return err
			}
			return a.print(cmd, ms, func(w io.Writer) error {
				if len(*ms) == 0 {
					fmt.Fprintln(a.io.ErrOut, "No milestones.")
					return nil
				}
				rows := make([][]string, len(*ms))
				for i := range *ms {
					m := &(*ms)[i]
					rows[i] = []string{strconv.FormatInt(m.Id, 10), m.Title, string(m.State), milestoneDue(m), milestoneProgress(m)}
				}
				return printTable(w, a.io, []string{"id", "title", "state", "due", "done"}, rows)
			})
		},
	}
	addRepoFlag(cmd, &repoFlag)
	cmd.Flags().StringVarP(&state, "state", "s", "open", "open, closed or all")
	return withOutput(cmd, "[]Milestone")
}

func newMilestoneViewCmd(a *app) *cobra.Command {
	var repoFlag string
	cmd := &cobra.Command{
		Use:   "view TITLE | ID",
		Short: "Show a milestone",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, repo, err := a.resolveRepo(repoFlag)
			if err != nil {
				return err
			}
			m, err := findMilestone(cmd.Context(), c, repo, args[0])
			if err != nil {
				return err
			}
			return a.print(cmd, m, func(w io.Writer) error {
				if err := printFields(w, a.io, [][2]string{{"title", a.io.Bold(m.Title)}, {"state", string(m.State)}, {"due", milestoneDue(m)}, {"done", milestoneProgress(m)}}); err != nil {
					return err
				}
				if m.Description != "" {
					fmt.Fprintln(w)
					fmt.Fprintln(w, m.Description)
				}
				return nil
			})
		},
	}
	addRepoFlag(cmd, &repoFlag)
	return withOutput(cmd, "Milestone")
}

func newMilestoneCreateCmd(a *app) *cobra.Command {
	var repoFlag, desc, due string
	cmd := &cobra.Command{
		Use:     "create TITLE",
		Short:   "Create a milestone",
		Example: `  tgp milestone create "v1.0" --due 2027-01-31 -d "First public release"`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, repo, err := a.resolveRepo(repoFlag)
			if err != nil {
				return err
			}
			req := apigen.MilestoneInput{Title: args[0]}
			if desc != "" {
				req.Description = &desc
			}
			if due != "" {
				if req.DueOn, err = parseDate(due); err != nil {
					return err
				}
			}
			r, err := c.CreateMilestoneWithResponse(cmd.Context(), repo, req)
			if err != nil {
				return err
			}
			m, err := must(r.JSON201, "milestone")
			if err != nil {
				return err
			}
			return a.print(cmd, m, func(w io.Writer) error {
				_, err := fmt.Fprintf(w, "%s Created milestone %s (id %d)\n", a.io.SuccessIcon(), m.Title, m.Id)
				return err
			})
		},
	}
	addRepoFlag(cmd, &repoFlag)
	cmd.Flags().StringVarP(&desc, "description", "d", "", "description (markdown)")
	cmd.Flags().StringVar(&due, "due", "", "due date, YYYY-MM-DD")
	return withOutput(cmd, "Milestone")
}

func newMilestoneEditCmd(a *app) *cobra.Command {
	var repoFlag, title, desc, due string
	cmd := &cobra.Command{
		Use:   "edit TITLE | ID",
		Short: "Change a milestone's title, description or due date",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, repo, err := a.resolveRepo(repoFlag)
			if err != nil {
				return err
			}
			m, err := findMilestone(cmd.Context(), c, repo, args[0])
			if err != nil {
				return err
			}
			req := apigen.MilestonePatch{}
			f := cmd.Flags()
			if f.Changed("title") {
				req.Title = &title
			}
			if f.Changed("description") {
				req.Description = &desc
			}
			if f.Changed("due") {
				if due == "" || due == "none" {
					t := true
					req.ClearDueOn = &t
				} else if req.DueOn, err = parseDate(due); err != nil {
					return err
				}
			}
			if req == (apigen.MilestonePatch{}) {
				return usageErr("nothing to change: pass --title, --description or --due")
			}
			return a.patchMilestone(cmd, c, repo, m.Id, req, "Updated")
		},
	}
	addRepoFlag(cmd, &repoFlag)
	cmd.Flags().StringVarP(&title, "title", "t", "", "new title")
	cmd.Flags().StringVarP(&desc, "description", "d", "", "new description")
	cmd.Flags().StringVar(&due, "due", "", "new due date (YYYY-MM-DD, or none)")
	return withOutput(cmd, "Milestone")
}

func (a *app) patchMilestone(cmd *cobra.Command, c *apiClient, repo string, id int64, req apigen.MilestonePatch, verb string) error {
	r, err := c.UpdateMilestoneWithResponse(cmd.Context(), repo, id, req)
	if err != nil {
		return err
	}
	m, err := must(r.JSON200, "milestone")
	if err != nil {
		return err
	}
	return a.print(cmd, m, func(w io.Writer) error {
		_, err := fmt.Fprintf(w, "%s %s milestone %s\n", a.io.SuccessIcon(), verb, m.Title)
		return err
	})
}

func newMilestoneStateCmd(a *app, verb string) *cobra.Command {
	var repoFlag string
	cmd := &cobra.Command{
		Use:   verb + " TITLE | ID",
		Short: map[string]string{"close": "Close a milestone", "reopen": "Reopen a milestone"}[verb],
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, repo, err := a.resolveRepo(repoFlag)
			if err != nil {
				return err
			}
			m, err := findMilestone(cmd.Context(), c, repo, args[0])
			if err != nil {
				return err
			}
			st := apigen.MilestonePatchStateClosed
			v := "Closed"
			if verb == "reopen" {
				st, v = apigen.MilestonePatchStateOpen, "Reopened"
			}
			return a.patchMilestone(cmd, c, repo, m.Id, apigen.MilestonePatch{State: &st}, v)
		},
	}
	addRepoFlag(cmd, &repoFlag)
	return withOutput(cmd, "Milestone")
}

func newMilestoneDeleteCmd(a *app) *cobra.Command {
	var repoFlag string
	cmd := &cobra.Command{
		Use:   "delete TITLE | ID",
		Short: "Delete a milestone (its issues stay, without a milestone)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, repo, err := a.resolveRepo(repoFlag)
			if err != nil {
				return err
			}
			m, err := findMilestone(cmd.Context(), c, repo, args[0])
			if err != nil {
				return err
			}
			if _, err := c.DeleteMilestoneWithResponse(cmd.Context(), repo, m.Id); err != nil {
				return err
			}
			return a.print(cmd, map[string]any{"name": m.Title, "deleted": true}, func(w io.Writer) error {
				_, err := fmt.Fprintf(w, "%s Deleted milestone %s\n", a.io.SuccessIcon(), m.Title)
				return err
			})
		},
	}
	addRepoFlag(cmd, &repoFlag)
	return withOutput(cmd, "Deleted")
}
