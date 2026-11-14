package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/api/apigen"
)

func newWebhookCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "webhook",
		Aliases: []string{"webhooks", "hook"},
		Short:   "Manage webhooks and their deliveries",
		Long: `Webhooks POST JSON to a URL when things happen in a repository (or, with
--org, in any repository). Managing them needs write access to the
repository; organisation webhooks are for admins.`,
	}
	cmd.AddCommand(newWebhookListCmd(a), newWebhookCreateCmd(a), newWebhookViewCmd(a), newWebhookEditCmd(a), newWebhookDeleteCmd(a),
		newWebhookPingCmd(a), newWebhookDeliveriesCmd(a), newWebhookDeliveryCmd(a), newWebhookRedeliverCmd(a), newWebhookEventsCmd(a))
	return group(cmd)
}

func hookID(arg string) (int64, error) {
	id, err := strconv.ParseInt(strings.TrimPrefix(arg, "#"), 10, 64)
	if err != nil || id < 1 {
		return 0, usageErr("invalid webhook id %q (tgp webhook list shows ids)", arg)
	}
	return id, nil
}

func deliveryResult(a *app, d *apigen.WebhookDelivery) string {
	switch d.Status {
	case apigen.WebhookDeliveryStatusSuccess:
		return a.io.paint("32", "✓") + " " + strconv.Itoa(deref(d.ResponseStatus))
	case apigen.WebhookDeliveryStatusPending:
		s := a.io.paint("33", "○") + " pending"
		if d.Attempts > 0 {
			s += fmt.Sprintf(" (attempt %d failed: %s)", d.Attempts, truncate(deref(d.Error), 40))
		}
		return s
	}
	msg := deref(d.Error)
	if d.ResponseStatus != nil {
		msg = strconv.Itoa(*d.ResponseStatus) + " " + msg
	}
	return a.io.paint("31", "✗") + " " + truncate(msg, 50)
}

func eventsLabel(ev []string) string {
	if len(ev) == 1 && ev[0] == "*" {
		return "all events"
	}
	return strings.Join(ev, ", ")
}

func newWebhookListCmd(a *app) *cobra.Command {
	var repoFlag string
	var org bool
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List webhooks",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var hooks []apigen.Webhook
			if org {
				c, err := a.client()
				if err != nil {
					return err
				}
				r, err := c.ListOrgWebhooksWithResponse(cmd.Context())
				if err != nil {
					return err
				}
				l, err := must(r.JSON200, "webhooks")
				if err != nil {
					return err
				}
				hooks = *l
			} else {
				c, repo, err := a.resolveRepo(repoFlag)
				if err != nil {
					return err
				}
				r, err := c.ListRepoWebhooksWithResponse(cmd.Context(), repo)
				if err != nil {
					return err
				}
				l, err := must(r.JSON200, "webhooks")
				if err != nil {
					return err
				}
				hooks = *l
			}
			return a.print(cmd, hooks, func(w io.Writer) error {
				if len(hooks) == 0 {
					_, err := fmt.Fprintln(w, "No webhooks.")
					return err
				}
				rows := make([][]string, len(hooks))
				for i, h := range hooks {
					last := "-"
					if h.LastDelivery != nil {
						last = deliveryResult(a, h.LastDelivery) + " " + ago(&h.LastDelivery.CreatedAt)
					}
					state := "active"
					if !h.Active {
						state = "inactive"
					}
					rows[i] = []string{strconv.FormatInt(h.Id, 10), truncate(h.Url, 50), truncate(eventsLabel(h.Events), 40), state, last}
				}
				return printTable(w, a.io, []string{"id", "url", "events", "", "last delivery"}, rows)
			})
		},
	}
	addRepoFlag(cmd, &repoFlag)
	cmd.Flags().BoolVar(&org, "org", false, "organisation webhooks (admins)")
	return withOutput(cmd, "[]Webhook")
}

// readSecret returns --secret, or stdin with --secret-stdin.
func (a *app) readSecret(secret string, fromStdin bool) (string, error) {
	if !fromStdin {
		return secret, nil
	}
	if secret != "" {
		return "", usageErr("give --secret or --secret-stdin, not both")
	}
	b, err := io.ReadAll(io.LimitReader(a.io.In, 1024))
	if err != nil {
		return "", err
	}
	s := strings.TrimRight(string(b), "\r\n")
	if s == "" {
		return "", usageErr("--secret-stdin got an empty secret")
	}
	return s, nil
}

func newWebhookCreateCmd(a *app) *cobra.Command {
	var (
		repoFlag, secret        string
		events                  []string
		org, inactive, insecure bool
		secretStdin             bool
	)
	cmd := &cobra.Command{
		Use:   "create URL",
		Short: "Add a webhook (it gets a ping right away)",
		Example: `  tgp webhook create https://ci.example.com/hook -e push -e pull_request --secret-stdin < secret.txt
  tgp webhook create https://chat.example.com/tgp -e '*' --org`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sec, err := a.readSecret(secret, secretStdin)
			if err != nil {
				return err
			}
			in := apigen.WebhookInput{Url: args[0], Active: ptr(!inactive), InsecureSsl: &insecure}
			if len(events) > 0 {
				ev := splitEvents(events)
				in.Events = &ev
			}
			if sec != "" {
				in.Secret = &sec
			}
			var h *apigen.Webhook
			if org {
				c, err := a.client()
				if err != nil {
					return err
				}
				r, err := c.CreateOrgWebhookWithResponse(cmd.Context(), in)
				if err != nil {
					return err
				}
				if h, err = must(r.JSON201, "webhook"); err != nil {
					return err
				}
			} else {
				c, repo, err := a.resolveRepo(repoFlag)
				if err != nil {
					return err
				}
				r, err := c.CreateRepoWebhookWithResponse(cmd.Context(), repo, in)
				if err != nil {
					return err
				}
				if h, err = must(r.JSON201, "webhook"); err != nil {
					return err
				}
			}
			return a.print(cmd, h, func(w io.Writer) error {
				fmt.Fprintf(w, "%s Added webhook %d for %s\n", a.io.SuccessIcon(), h.Id, eventsLabel(h.Events))
				if h.LastDelivery != nil {
					fmt.Fprintf(w, "Ping: %s\n", deliveryResult(a, h.LastDelivery))
				}
				return nil
			})
		},
	}
	addRepoFlag(cmd, &repoFlag)
	f := cmd.Flags()
	f.StringArrayVarP(&events, "event", "e", nil, "event to send (repeatable or comma-separated; * for all; default push)")
	f.StringVarP(&secret, "secret", "s", "", "signing secret (visible in shell history; prefer --secret-stdin)")
	f.BoolVar(&secretStdin, "secret-stdin", false, "read the signing secret from stdin")
	f.BoolVar(&org, "org", false, "an organisation webhook for every repository (admins)")
	f.BoolVar(&inactive, "inactive", false, "create it switched off")
	f.BoolVar(&insecure, "insecure-ssl", false, "don't verify the receiver's TLS certificate")
	return withOutput(cmd, "Webhook")
}

func splitEvents(in []string) []string {
	var out []string
	for _, e := range in {
		for _, p := range strings.Split(e, ",") {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}

func ptr[T any](v T) *T { return &v }

func newWebhookViewCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "view ID",
		Short: "Show a webhook and its recent deliveries",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := hookID(args[0])
			if err != nil {
				return err
			}
			c, err := a.client()
			if err != nil {
				return err
			}
			r, err := c.GetWebhookWithResponse(cmd.Context(), id)
			if err != nil {
				return err
			}
			h, err := must(r.JSON200, "webhook")
			if err != nil {
				return err
			}
			return a.print(cmd, h, func(w io.Writer) error {
				scope := "organisation (every repository)"
				if h.Repo != nil {
					scope = *h.Repo
				}
				fields := [][2]string{{"url", h.Url}, {"scope", scope}, {"events", eventsLabel(h.Events)},
					{"active", strconv.FormatBool(h.Active)}, {"signed", strconv.FormatBool(h.HasSecret)}}
				if h.InsecureSsl {
					fields = append(fields, [2]string{"tls", "not verified"})
				}
				if err := printFields(w, a.io, fields); err != nil {
					return err
				}
				dr, err := c.ListWebhookDeliveriesWithResponse(cmd.Context(), id, &apigen.ListWebhookDeliveriesParams{PerPage: ptr(10)})
				if err != nil {
					return err
				}
				if dr.JSON200 != nil && len(dr.JSON200.Deliveries) > 0 {
					fmt.Fprintln(w)
					return printDeliveries(a, w, dr.JSON200.Deliveries)
				}
				return nil
			})
		},
	}
	return withOutput(cmd, "Webhook")
}

func printDeliveries(a *app, w io.Writer, ds []apigen.WebhookDelivery) error {
	rows := make([][]string, len(ds))
	for i, d := range ds {
		ev := d.Event
		if d.Action != nil {
			ev += "." + *d.Action
		}
		if d.Redelivery {
			ev += " (redelivery)"
		}
		took := ""
		if d.DurationMs != nil {
			took = strconv.Itoa(*d.DurationMs) + "ms"
		}
		rows[i] = []string{strconv.FormatInt(d.Id, 10), ev, deliveryResult(a, &d), took, ago(&d.CreatedAt)}
	}
	return printTable(w, a.io, []string{"delivery", "event", "result", "took", "when"}, rows)
}

func newWebhookEditCmd(a *app) *cobra.Command {
	var (
		url, secret                   string
		events                        []string
		active, insecure, secretStdin bool
		removeSecret                  bool
	)
	cmd := &cobra.Command{
		Use:   "edit ID",
		Short: "Change a webhook",
		Example: `  tgp webhook edit 3 --active=false
  tgp webhook edit 3 -e push -e release --secret-stdin < new-secret.txt`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := hookID(args[0])
			if err != nil {
				return err
			}
			f := cmd.Flags()
			var p apigen.WebhookPatch
			if f.Changed("url") {
				p.Url = &url
			}
			if f.Changed("event") {
				ev := splitEvents(events)
				p.Events = &ev
			}
			if f.Changed("active") {
				p.Active = &active
			}
			if f.Changed("insecure-ssl") {
				p.InsecureSsl = &insecure
			}
			switch {
			case removeSecret:
				p.Secret = ptr("")
			case secret != "" || secretStdin:
				sec, err := a.readSecret(secret, secretStdin)
				if err != nil {
					return err
				}
				p.Secret = &sec
			}
			c, err := a.client()
			if err != nil {
				return err
			}
			r, err := c.UpdateWebhookWithResponse(cmd.Context(), id, p)
			if err != nil {
				return err
			}
			h, err := must(r.JSON200, "webhook")
			if err != nil {
				return err
			}
			return a.print(cmd, h, func(w io.Writer) error {
				_, err := fmt.Fprintf(w, "%s Updated webhook %d\n", a.io.SuccessIcon(), h.Id)
				return err
			})
		},
	}
	f := cmd.Flags()
	f.StringVar(&url, "url", "", "new URL")
	f.StringArrayVarP(&events, "event", "e", nil, "replace the events (repeatable; * for all)")
	f.StringVarP(&secret, "secret", "s", "", "new signing secret")
	f.BoolVar(&secretStdin, "secret-stdin", false, "read the new secret from stdin")
	f.BoolVar(&removeSecret, "remove-secret", false, "stop signing deliveries")
	f.BoolVar(&active, "active", true, "switch on (--active=false switches off)")
	f.BoolVar(&insecure, "insecure-ssl", false, "don't verify TLS certificates (--insecure-ssl=false to verify)")
	return withOutput(cmd, "Webhook")
}

func newWebhookDeleteCmd(a *app) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete ID",
		Short: "Delete a webhook and its delivery log",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := hookID(args[0])
			if err != nil {
				return err
			}
			if err := a.confirmOrYes(yes, fmt.Sprintf("Delete webhook %d?", id)); err != nil {
				return err
			}
			c, err := a.client()
			if err != nil {
				return err
			}
			if _, err := c.DeleteWebhookWithResponse(cmd.Context(), id); err != nil {
				return err
			}
			return a.print(cmd, map[string]any{"name": args[0], "deleted": true}, func(w io.Writer) error {
				_, err := fmt.Fprintf(w, "%s Deleted webhook %d\n", a.io.SuccessIcon(), id)
				return err
			})
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "don't ask for confirmation")
	return withOutput(cmd, "Deleted")
}

func newWebhookPingCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ping ID",
		Short: "Send a ping and show the result (exit 1 if it failed)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := hookID(args[0])
			if err != nil {
				return err
			}
			c, err := a.client()
			if err != nil {
				return err
			}
			r, err := c.PingWebhookWithResponse(cmd.Context(), id)
			if err != nil {
				return err
			}
			d, err := must(r.JSON200, "delivery")
			if err != nil {
				return err
			}
			if err := a.print(cmd, d, func(w io.Writer) error {
				_, err := fmt.Fprintf(w, "Ping %s\n", deliveryResult(a, d))
				return err
			}); err != nil {
				return err
			}
			if d.Status != apigen.WebhookDeliveryStatusSuccess {
				return &Error{Code: "delivery_failed", Exit: ExitError, Message: "the ping wasn't accepted: " + strings.TrimSpace(deref(d.Error)), silent: true}
			}
			return nil
		},
	}
	return withOutput(cmd, "WebhookDelivery")
}

func newWebhookDeliveriesCmd(a *app) *cobra.Command {
	var status, event string
	var limit int
	cmd := &cobra.Command{
		Use:   "deliveries ID",
		Short: "List a webhook's recent deliveries",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := hookID(args[0])
			if err != nil {
				return err
			}
			p := &apigen.ListWebhookDeliveriesParams{PerPage: ptr(min(max(limit, 1), 100))}
			if status != "" {
				st := apigen.WebhookDeliveryStatus(status)
				p.Status = &st
			}
			if event != "" {
				p.Event = &event
			}
			c, err := a.client()
			if err != nil {
				return err
			}
			r, err := c.ListWebhookDeliveriesWithResponse(cmd.Context(), id, p)
			if err != nil {
				return err
			}
			page, err := must(r.JSON200, "deliveries")
			if err != nil {
				return err
			}
			return a.print(cmd, page.Deliveries, func(w io.Writer) error {
				if len(page.Deliveries) == 0 {
					_, err := fmt.Fprintln(w, "No deliveries.")
					return err
				}
				return printDeliveries(a, w, page.Deliveries)
			})
		},
	}
	f := cmd.Flags()
	f.StringVarP(&status, "status", "s", "", "pending, success or failed")
	f.StringVarP(&event, "event", "e", "", "only this event")
	f.IntVarP(&limit, "limit", "L", 30, "maximum deliveries (max 100)")
	return withOutput(cmd, "[]WebhookDelivery")
}

func newWebhookDeliveryCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delivery ID DELIVERY",
		Short: "Show a delivery's request and response",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := hookID(args[0])
			if err != nil {
				return err
			}
			did, err := strconv.ParseInt(args[1], 10, 64)
			if err != nil {
				return usageErr("invalid delivery id %q", args[1])
			}
			c, err := a.client()
			if err != nil {
				return err
			}
			r, err := c.GetWebhookDeliveryWithResponse(cmd.Context(), id, did)
			if err != nil {
				return err
			}
			d, err := must(r.JSON200, "delivery")
			if err != nil {
				return err
			}
			return a.print(cmd, d, func(w io.Writer) error {
				var base apigen.WebhookDelivery
				_ = remarshal(d, &base)
				ev := d.Event
				if d.Action != nil {
					ev += "." + *d.Action
				}
				fmt.Fprintf(w, "%s %s  %s\n", a.io.Bold(ev), d.Guid, deliveryResult(a, &base))
				fmt.Fprintf(w, "\nPOST %s\n", d.Request.Url)
				for _, k := range sortedKeys(d.Request.Headers) {
					fmt.Fprintf(w, "%s: %s\n", k, d.Request.Headers[k])
				}
				b, _ := json.MarshalIndent(d.Request.Payload, "", "  ")
				fmt.Fprintf(w, "\n%s\n", b)
				if d.Response != nil {
					fmt.Fprintf(w, "\nResponse %d\n", deref(d.ResponseStatus))
					for _, k := range sortedKeys(d.Response.Headers) {
						fmt.Fprintf(w, "%s: %s\n", k, d.Response.Headers[k])
					}
					fmt.Fprintf(w, "\n%s\n", d.Response.Body)
					if d.Response.Truncated {
						fmt.Fprintln(w, "(truncated)")
					}
				}
				return nil
			})
		},
	}
	return withOutput(cmd, "WebhookDeliveryDetail")
}

func newWebhookRedeliverCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "redeliver ID DELIVERY",
		Short: "Send a delivery's payload again",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := hookID(args[0])
			if err != nil {
				return err
			}
			did, err := strconv.ParseInt(args[1], 10, 64)
			if err != nil {
				return usageErr("invalid delivery id %q", args[1])
			}
			c, err := a.client()
			if err != nil {
				return err
			}
			r, err := c.RedeliverWebhookDeliveryWithResponse(cmd.Context(), id, did)
			if err != nil {
				return err
			}
			d, err := must(r.JSON201, "delivery")
			if err != nil {
				return err
			}
			return a.print(cmd, d, func(w io.Writer) error {
				_, err := fmt.Fprintf(w, "%s Queued delivery %d (tgp webhook deliveries %d shows the result)\n", a.io.SuccessIcon(), d.Id, id)
				return err
			})
		},
	}
	return withOutput(cmd, "WebhookDelivery")
}

func newWebhookEventsCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "events",
		Short: "List the events webhooks can subscribe to",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := a.client()
			if err != nil {
				return err
			}
			r, err := c.ListWebhookEventsWithResponse(cmd.Context())
			if err != nil {
				return err
			}
			l, err := must(r.JSON200, "events")
			if err != nil {
				return err
			}
			return a.print(cmd, *l, func(w io.Writer) error {
				rows := make([][]string, len(*l))
				for i, e := range *l {
					rows[i] = []string{e.Name, e.Description, strings.Join(e.Actions, ", ")}
				}
				return printTable(w, a.io, []string{"event", "", "actions"}, rows)
			})
		},
	}
	return withOutput(cmd, "[]WebhookEventInfo")
}
