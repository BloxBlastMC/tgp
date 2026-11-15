package server

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/uptrace/bun"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/api/apigen"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/apierr"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/audit"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/authz"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/model"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/version"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/webhook"
)

// webhookEvents lists the events webhooks can subscribe to.
var webhookEvents = []apigen.WebhookEventInfo{
	{Name: "push", Description: "Commits or tags were pushed, including merges and tags made by the server.", Actions: []string{}},
	{Name: "create", Description: "A branch or tag was created.", Actions: []string{}},
	{Name: "delete", Description: "A branch or tag was deleted.", Actions: []string{}},
	{Name: "pull_request", Description: "Pull request activity.", Actions: []string{"opened", "edited", "closed", "reopened", "synchronize",
		"ready_for_review", "converted_to_draft", "labeled", "unlabeled", "assigned", "unassigned", "milestoned", "demilestoned",
		"review_requested", "review_request_removed"}},
	{Name: "pull_request_review", Description: "A review was submitted or dismissed.", Actions: []string{"submitted", "dismissed"}},
	{Name: "pull_request_review_comment", Description: "An inline comment on a pull request's diff.", Actions: []string{"created", "edited", "deleted"}},
	{Name: "issues", Description: "Issue activity.", Actions: []string{"opened", "edited", "closed", "reopened", "labeled", "unlabeled",
		"assigned", "unassigned", "milestoned", "demilestoned"}},
	{Name: "issue_comment", Description: "A comment on an issue or a pull request's conversation.", Actions: []string{"created", "edited", "deleted"}},
	{Name: "release", Description: "Release activity.", Actions: []string{"created", "published", "released", "prereleased", "edited", "deleted"}},
	{Name: "workflow_run", Description: "A CI run was requested or completed.", Actions: []string{"requested", "completed"}},
	{Name: "package", Description: "A container image was pushed (published) or deleted.", Actions: []string{"published", "deleted"}},
}

func knownEvent(name string) bool {
	for _, e := range webhookEvents {
		if e.Name == name {
			return true
		}
	}
	return false
}

func (a *api) ListWebhookEvents(context.Context, apigen.ListWebhookEventsRequestObject) (apigen.ListWebhookEventsResponseObject, error) {
	return apigen.ListWebhookEvents200JSONResponse(webhookEvents), nil
}

// ---- delivery engine ----

// Delivery attempts: the first, then retries after these delays.
var retryDelays = []time.Duration{15 * time.Second, time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour}

// deliveryRetention is how long the delivery log is kept.
const deliveryRetention = 30 * 24 * time.Hour

type hookState struct {
	mu     sync.Mutex
	busy   map[int64]bool // webhooks with a delivery in flight
	wake   notifier
	sender *webhook.Sender
	sem    chan struct{}
	wg     sync.WaitGroup
}

func (s *Server) hookSender() *webhook.Sender {
	s.hooksOnce.Do(func() {
		n := s.cfg.Webhooks.Workers
		if n <= 0 {
			n = 4
		}
		s.hooks.sem = make(chan struct{}, n)
		s.hooks.sender = &webhook.Sender{BlockPrivate: s.cfg.WebhooksBlockPrivate(), Timeout: s.cfg.Webhooks.Timeout.Duration,
			UserAgent: "tgp-hookshot/" + version.Version}
	})
	return s.hooks.sender
}

func hookEvents(h *model.Webhook) []string { return jsonStrings(h.Events) }

func hookWants(h *model.Webhook, event string) bool {
	ev := hookEvents(h)
	return slices.Contains(ev, "*") || slices.Contains(ev, event)
}

// enqueue stores deliveries of payload for every active webhook of repo
// (and every organisation webhook) that wants event. db may be a
// transaction; deliveries then appear when it commits.
func (s *Server) enqueue(ctx context.Context, db bun.IDB, repoID int64, event, action string, payload map[string]any) error {
	var hooks []*model.Webhook
	if err := db.NewSelect().Model(&hooks).Where("wh.active").Where("wh.repo_id = ? OR wh.repo_id IS NULL", repoID).Scan(ctx); err != nil {
		return err
	}
	var want []*model.Webhook
	for _, h := range hooks {
		if hookWants(h, event) {
			want = append(want, h)
		}
	}
	if len(want) == 0 {
		return nil
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	now := s.now()
	for _, h := range want {
		d := &model.WebhookDelivery{WebhookID: h.ID, GUID: newGUID(), Event: event, Action: action, Payload: string(b),
			Status: model.DeliveryPending, NextAttemptAt: &now, RequestHeaders: "{}", ResponseHeaders: "{}", CreatedAt: now}
		if _, err := db.NewInsert().Model(d).Exec(ctx); err != nil {
			return err
		}
	}
	s.hooks.wake.notify()
	return nil
}

// newGUID returns a random UUID (version 4).
func newGUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

// runHookWorker delivers due webhooks until ctx ends.
func (s *Server) runHookWorker(ctx context.Context) {
	s.hookSender()
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		if _, err := s.dispatchDue(ctx); err != nil && ctx.Err() == nil {
			s.log.Error("webhooks", "err", err)
		}
		select {
		case <-ctx.Done():
			s.hooks.wg.Wait()
			return
		case <-t.C:
		case <-s.hooks.wake.wait():
		}
	}
}

// claim marks a delivery as being sent by this process; false if another
// process (or worker) has it.
func (s *Server) claim(ctx context.Context, id int64) (bool, error) {
	now := s.now()
	until := now.Add(s.hookSender().Timeout + time.Minute)
	res, err := s.db.NewUpdate().Model((*model.WebhookDelivery)(nil)).Set("claimed_until = ?", until).
		Where("id = ? AND status = ?", id, model.DeliveryPending).
		Where("claimed_until IS NULL OR claimed_until < ?", now).Exec(ctx)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// dispatchDue starts due deliveries: in order, one at a time per
// webhook, so receivers see a webhook's events in the order they happened.
// It returns how many it started.
func (s *Server) dispatchDue(ctx context.Context) (int, error) {
	s.hookSender()
	var due []*model.WebhookDelivery
	now := s.now()
	if err := s.db.NewSelect().Model(&due).Column("id", "webhook_id").
		Where("status = ? AND next_attempt_at <= ?", model.DeliveryPending, now).
		Where("claimed_until IS NULL OR claimed_until < ?", now).
		OrderExpr("id").Limit(500).Scan(ctx); err != nil {
		return 0, err
	}
	started := 0
	picked := map[int64]bool{}
	for _, d := range due {
		if picked[d.WebhookID] {
			continue
		}
		picked[d.WebhookID] = true // later ones wait for this one
		s.hooks.mu.Lock()
		busy := s.hooks.busy[d.WebhookID]
		s.hooks.mu.Unlock()
		if busy {
			continue
		}
		select {
		case s.hooks.sem <- struct{}{}:
		case <-ctx.Done():
			return started, nil
		}
		ok, err := s.claim(ctx, d.ID)
		if err != nil || !ok {
			<-s.hooks.sem
			if err != nil {
				return started, err
			}
			continue
		}
		s.hooks.mu.Lock()
		if s.hooks.busy == nil {
			s.hooks.busy = map[int64]bool{}
		}
		s.hooks.busy[d.WebhookID] = true
		s.hooks.mu.Unlock()
		started++
		s.hooks.wg.Add(1)
		go func(id, hookID int64) {
			defer s.hooks.wg.Done()
			defer func() {
				s.hooks.mu.Lock()
				delete(s.hooks.busy, hookID)
				s.hooks.mu.Unlock()
				<-s.hooks.sem
				s.hooks.wake.notify() // the webhook's next delivery may go
			}()
			// Finish an attempt even during shutdown; the claim expires
			// if the process dies.
			actx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.hookSender().Timeout+30*time.Second)
			defer cancel()
			if _, err := s.attempt(actx, id); err != nil {
				s.log.Error("webhook delivery", "id", id, "err", err)
			}
		}(d.ID, d.WebhookID)
	}
	return started, nil
}

// retryable reports whether a failed attempt may succeed later.
func retryable(r *webhook.Result) bool {
	if errors.Is(r.Err, webhook.ErrBlockedAddress) {
		return false
	}
	switch {
	case r.Status == 0:
		return true // network error or timeout
	case r.Status >= 500, r.Status == http.StatusRequestTimeout, r.Status == http.StatusTooManyRequests:
		return true
	}
	return false
}

// attempt sends a claimed delivery once and records the result.
func (s *Server) attempt(ctx context.Context, id int64) (*model.WebhookDelivery, error) {
	d := new(model.WebhookDelivery)
	if err := s.db.NewSelect().Model(d).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, err
	}
	h := new(model.Webhook)
	if err := s.db.NewSelect().Model(h).Where("id = ?", d.WebhookID).Scan(ctx); err != nil {
		return nil, err
	}
	now := s.now()
	d.ClaimedUntil = nil
	if !h.Active && d.Event != "ping" {
		d.Status, d.Error, d.NextAttemptAt = model.DeliveryFailed, "the webhook was deactivated", nil
		_, err := s.db.NewUpdate().Model(d).Column("status", "error", "next_attempt_at", "claimed_until").WherePK().Exec(ctx)
		return d, err
	}
	headers := map[string]string{
		"Content-Type":      "application/json",
		"X-Tgp-Event":       d.Event,
		"X-Tgp-Delivery":    d.GUID,
		"X-Tgp-Hook-ID":     strconv.FormatInt(h.ID, 10),
		"X-GitHub-Event":    d.Event,
		"X-GitHub-Delivery": d.GUID,
		"X-GitHub-Hook-ID":  strconv.FormatInt(h.ID, 10),
	}
	body := []byte(d.Payload)
	if h.Secret != "" {
		secret, err := s.box.Open(h.Secret, webhookAAD(h.ID))
		if err != nil {
			return nil, fmt.Errorf("unseal webhook secret: %w", err)
		}
		sig := webhook.Sign(string(secret), body)
		headers["X-Tgp-Signature-256"], headers["X-Hub-Signature-256"] = sig, sig
	}
	res := s.hookSender().Send(ctx, webhook.Request{URL: h.URL, Headers: headers, Body: body, Insecure: h.InsecureSSL})
	d.Attempts++
	d.RequestURL = h.URL
	shown := map[string]string{"User-Agent": s.hookSender().UserAgent}
	for k, v := range headers {
		shown[k] = v
	}
	d.RequestHeaders = mustJSON(shown)
	d.ResponseHeaders, d.ResponseBody, d.ResponseTrunc = mustJSON(res.Headers), res.Body, res.Truncated
	d.ResponseStatus = nil
	if res.Status > 0 {
		st := res.Status
		d.ResponseStatus = &st
	}
	ms := int(res.Duration.Milliseconds())
	d.DurationMS = &ms
	d.DeliveredAt = &now
	d.Error = ""
	switch {
	case res.OK():
		d.Status, d.NextAttemptAt = model.DeliverySuccess, nil
	default:
		if res.Err != nil {
			d.Error = res.Err.Error()
		} else {
			d.Error = fmt.Sprintf("the receiver answered %d", res.Status)
		}
		if retryable(res) && d.Attempts <= len(retryDelays) && d.Event != "ping" {
			next := now.Add(retryDelays[d.Attempts-1])
			d.Status, d.NextAttemptAt = model.DeliveryPending, &next
		} else {
			d.Status, d.NextAttemptAt = model.DeliveryFailed, nil
		}
	}
	_, err := s.db.NewUpdate().Model(d).Column("status", "attempts", "next_attempt_at", "claimed_until", "request_url", "request_headers",
		"response_status", "response_headers", "response_body", "response_truncated", "duration_ms", "error", "delivered_at").WherePK().Exec(ctx)
	return d, err
}

// expireDeliveries trims the delivery log.
func (s *Server) expireDeliveries(ctx context.Context) error {
	_, err := s.db.NewDelete().Model((*model.WebhookDelivery)(nil)).
		Where("created_at < ? AND status <> ?", s.now().Add(-deliveryRetention), model.DeliveryPending).Exec(ctx)
	return err
}

// ---- management API ----

func webhookAAD(id int64) string { return "webhook:" + strconv.FormatInt(id, 10) }

// hookFor loads a webhook the caller may manage: write access to its
// repository, or admin (with the admin token scope) for org webhooks.
func (s *Server) hookFor(ctx context.Context, id int64) (*model.Webhook, *model.Repo, error) {
	h := new(model.Webhook)
	if err := s.db.NewSelect().Model(h).Where("id = ?", id).Scan(ctx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, apierr.NotFound("webhook %d not found", id)
		}
		return nil, nil, err
	}
	p := mustPrincipal(ctx)
	if h.RepoID == nil {
		if !p.User.IsAdmin() {
			return nil, nil, apierr.NotFound("webhook %d not found", id)
		}
		if !p.HasScope("admin") {
			return nil, nil, apierr.Forbidden("this token lacks the \"admin\" scope")
		}
		return h, nil, nil
	}
	repo := new(model.Repo)
	if err := s.db.NewSelect().Model(repo).Where("id = ?", *h.RepoID).Scan(ctx); err != nil {
		return nil, nil, err
	}
	perm, err := authz.RepoPerm(ctx, s.db, p.User, repo)
	if err != nil {
		return nil, nil, err
	}
	if perm < authz.Write {
		return nil, nil, apierr.NotFound("webhook %d not found", id)
	}
	return h, repo, nil
}

func cleanEvents(in *[]string) ([]string, error) {
	if in == nil {
		return []string{"push"}, nil
	}
	var out []string
	for _, e := range *in {
		e = strings.TrimSpace(e)
		if e == "" || slices.Contains(out, e) {
			continue
		}
		if e != "*" && !knownEvent(e) {
			names := make([]string, len(webhookEvents))
			for i, x := range webhookEvents {
				names[i] = x.Name
			}
			return nil, apierr.Validation("unknown event %q (events: %s, or * for all)", e, strings.Join(names, ", "))
		}
		out = append(out, e)
	}
	if len(out) == 0 {
		return nil, apierr.Validation("choose at least one event")
	}
	if slices.Contains(out, "*") {
		out = []string{"*"}
	}
	return out, nil
}

func (s *Server) toAPIDelivery(d *model.WebhookDelivery) apigen.WebhookDelivery {
	v := apigen.WebhookDelivery{Id: d.ID, Guid: d.GUID, Event: d.Event, Action: strPtrOrNil(d.Action), Status: apigen.WebhookDeliveryStatus(d.Status),
		Redelivery: d.Redelivery, Attempts: d.Attempts, ResponseStatus: d.ResponseStatus, DurationMs: d.DurationMS, Error: strPtrOrNil(d.Error),
		CreatedAt: d.CreatedAt, DeliveredAt: d.DeliveredAt}
	if d.Status == model.DeliveryPending {
		v.NextAttemptAt = d.NextAttemptAt
	}
	return v
}

func (s *Server) toAPIWebhooks(ctx context.Context, hooks []*model.Webhook) ([]apigen.Webhook, error) {
	out := make([]apigen.Webhook, len(hooks))
	uc := s.users()
	repoNames := map[int64]string{}
	for i, h := range hooks {
		v := apigen.Webhook{Id: h.ID, Scope: apigen.WebhookScopeOrg, Url: h.URL, Events: hookEvents(h), Active: h.Active, HasSecret: h.Secret != "",
			InsecureSsl: h.InsecureSSL, CreatedBy: uc.summaryPtr(ctx, h.CreatedByID), CreatedAt: h.CreatedAt, UpdatedAt: h.UpdatedAt}
		if h.RepoID != nil {
			v.Scope = apigen.WebhookScopeRepo
			name, ok := repoNames[*h.RepoID]
			if !ok {
				_ = s.db.NewSelect().Model((*model.Repo)(nil)).Column("name").Where("id = ?", *h.RepoID).Scan(ctx, &name)
				repoNames[*h.RepoID] = name
			}
			v.Repo = &name
		}
		last := new(model.WebhookDelivery)
		err := s.db.NewSelect().Model(last).Where("webhook_id = ?", h.ID).OrderExpr("id DESC").Limit(1).Scan(ctx)
		switch {
		case err == nil:
			d := s.toAPIDelivery(last)
			v.LastDelivery = &d
		case !errors.Is(err, sql.ErrNoRows):
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}

func (s *Server) toAPIWebhook(ctx context.Context, h *model.Webhook) (apigen.Webhook, error) {
	l, err := s.toAPIWebhooks(ctx, []*model.Webhook{h})
	if err != nil {
		return apigen.Webhook{}, err
	}
	return l[0], nil
}

func (s *Server) listHooks(ctx context.Context, repoID *int64) ([]apigen.Webhook, error) {
	var hooks []*model.Webhook
	q := s.db.NewSelect().Model(&hooks).OrderExpr("wh.id")
	if repoID == nil {
		q = q.Where("wh.repo_id IS NULL")
	} else {
		q = q.Where("wh.repo_id = ?", *repoID)
	}
	if err := q.Scan(ctx); err != nil {
		return nil, err
	}
	return s.toAPIWebhooks(ctx, hooks)
}

func (a *api) ListRepoWebhooks(ctx context.Context, req apigen.ListRepoWebhooksRequestObject) (apigen.ListRepoWebhooksResponseObject, error) {
	repo, _, err := a.s.repoFor(ctx, req.Repo, authz.Write)
	if err != nil {
		return nil, err
	}
	l, err := a.s.listHooks(ctx, &repo.ID)
	if err != nil {
		return nil, err
	}
	return apigen.ListRepoWebhooks200JSONResponse(l), nil
}

func (a *api) ListOrgWebhooks(ctx context.Context, _ apigen.ListOrgWebhooksRequestObject) (apigen.ListOrgWebhooksResponseObject, error) {
	l, err := a.s.listHooks(ctx, nil)
	if err != nil {
		return nil, err
	}
	return apigen.ListOrgWebhooks200JSONResponse(l), nil
}

// createHook stores a webhook and sends it a ping.
func (s *Server) createHook(ctx context.Context, repo *model.Repo, in *apigen.WebhookInput) (*model.Webhook, error) {
	if in == nil {
		return nil, apierr.BadRequest("missing request body")
	}
	u := strings.TrimSpace(in.Url)
	if err := webhook.ValidateURL(u); err != nil {
		return nil, apierr.Validation("%v", err)
	}
	events, err := cleanEvents(in.Events)
	if err != nil {
		return nil, err
	}
	user := mustPrincipal(ctx).User
	now := s.now()
	h := &model.Webhook{URL: u, Events: mustJSON(events), Active: in.Active == nil || *in.Active, InsecureSSL: deref(in.InsecureSsl),
		CreatedByID: &user.ID, CreatedAt: now, UpdatedAt: now}
	target := "org"
	if repo != nil {
		h.RepoID, target = &repo.ID, repo.Name
	}
	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewInsert().Model(h).Exec(ctx); err != nil {
			return err
		}
		if sec := deref(in.Secret); sec != "" {
			h.Secret = s.box.Seal([]byte(sec), webhookAAD(h.ID))
			if _, err := tx.NewUpdate().Model(h).Column("secret").WherePK().Exec(ctx); err != nil {
				return err
			}
		}
		return s.record(ctx, tx, audit.WebhookCreate, "webhook", target, map[string]any{"id": h.ID, "url": u, "events": events})
	})
	if err != nil {
		return nil, err
	}
	if h.Active {
		if _, err := s.sendPing(ctx, h, repo); err != nil {
			s.log.Warn("webhook ping", "id", h.ID, "err", err)
		}
	}
	return h, nil
}

func (a *api) CreateRepoWebhook(ctx context.Context, req apigen.CreateRepoWebhookRequestObject) (apigen.CreateRepoWebhookResponseObject, error) {
	repo, _, err := a.s.repoFor(ctx, req.Repo, authz.Write)
	if err != nil {
		return nil, err
	}
	h, err := a.s.createHook(ctx, repo, req.Body)
	if err != nil {
		return nil, err
	}
	v, err := a.s.toAPIWebhook(ctx, h)
	if err != nil {
		return nil, err
	}
	return apigen.CreateRepoWebhook201JSONResponse(v), nil
}

func (a *api) CreateOrgWebhook(ctx context.Context, req apigen.CreateOrgWebhookRequestObject) (apigen.CreateOrgWebhookResponseObject, error) {
	h, err := a.s.createHook(ctx, nil, req.Body)
	if err != nil {
		return nil, err
	}
	v, err := a.s.toAPIWebhook(ctx, h)
	if err != nil {
		return nil, err
	}
	return apigen.CreateOrgWebhook201JSONResponse(v), nil
}

func (a *api) GetWebhook(ctx context.Context, req apigen.GetWebhookRequestObject) (apigen.GetWebhookResponseObject, error) {
	h, _, err := a.s.hookFor(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	v, err := a.s.toAPIWebhook(ctx, h)
	if err != nil {
		return nil, err
	}
	return apigen.GetWebhook200JSONResponse(v), nil
}

func (a *api) UpdateWebhook(ctx context.Context, req apigen.UpdateWebhookRequestObject) (apigen.UpdateWebhookResponseObject, error) {
	s := a.s
	h, repo, err := s.hookFor(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, apierr.BadRequest("missing request body")
	}
	b := req.Body
	if b.Url != nil {
		u := strings.TrimSpace(*b.Url)
		if err := webhook.ValidateURL(u); err != nil {
			return nil, apierr.Validation("%v", err)
		}
		h.URL = u
	}
	if b.Events != nil {
		ev, err := cleanEvents(b.Events)
		if err != nil {
			return nil, err
		}
		h.Events = mustJSON(ev)
	}
	if b.Secret != nil {
		h.Secret = ""
		if *b.Secret != "" {
			h.Secret = s.box.Seal([]byte(*b.Secret), webhookAAD(h.ID))
		}
	}
	if b.Active != nil {
		h.Active = *b.Active
	}
	if b.InsecureSsl != nil {
		h.InsecureSSL = *b.InsecureSsl
	}
	h.UpdatedAt = s.now()
	target := "org"
	if repo != nil {
		target = repo.Name
	}
	if err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewUpdate().Model(h).Column("url", "events", "secret", "active", "insecure_ssl", "updated_at").WherePK().Exec(ctx); err != nil {
			return err
		}
		return s.record(ctx, tx, audit.WebhookUpdate, "webhook", target, map[string]any{"id": h.ID, "url": h.URL, "events": hookEvents(h),
			"active": h.Active, "secret_changed": b.Secret != nil})
	}); err != nil {
		return nil, err
	}
	v, err := s.toAPIWebhook(ctx, h)
	if err != nil {
		return nil, err
	}
	return apigen.UpdateWebhook200JSONResponse(v), nil
}

func (a *api) DeleteWebhook(ctx context.Context, req apigen.DeleteWebhookRequestObject) (apigen.DeleteWebhookResponseObject, error) {
	s := a.s
	h, repo, err := s.hookFor(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	target := "org"
	if repo != nil {
		target = repo.Name
	}
	if err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewDelete().Model((*model.WebhookDelivery)(nil)).Where("webhook_id = ?", h.ID).Exec(ctx); err != nil {
			return err
		}
		if _, err := tx.NewDelete().Model(h).WherePK().Exec(ctx); err != nil {
			return err
		}
		return s.record(ctx, tx, audit.WebhookDelete, "webhook", target, map[string]any{"id": h.ID, "url": h.URL})
	}); err != nil {
		return nil, err
	}
	return apigen.DeleteWebhook204Response{}, nil
}

// sendPing creates a ping delivery and sends it now.
func (s *Server) sendPing(ctx context.Context, h *model.Webhook, repo *model.Repo) (*model.WebhookDelivery, error) {
	user := mustPrincipal(ctx).User
	payload := map[string]any{
		"zen":     "Small steps, often merged.",
		"hook_id": h.ID,
		"hook":    map[string]any{"id": h.ID, "type": map[bool]string{true: "Repository", false: "Organization"}[repo != nil], "events": hookEvents(h), "active": h.Active, "config": map[string]any{"url": h.URL, "content_type": "json", "insecure_ssl": map[bool]string{true: "1", false: "0"}[h.InsecureSSL]}},
		"sender":  s.senderPayload(user),
	}
	oi := s.loadOrgInfo(ctx)
	if repo != nil {
		payload["repository"] = s.repoPayload(oi, repo)
	}
	payload["organization"] = map[string]any{"login": oi.owner}
	b, _ := json.Marshal(payload)
	now := s.now()
	d := &model.WebhookDelivery{WebhookID: h.ID, GUID: newGUID(), Event: "ping", Payload: string(b), Status: model.DeliveryPending,
		NextAttemptAt: &now, RequestHeaders: "{}", ResponseHeaders: "{}", CreatedAt: now}
	if _, err := s.db.NewInsert().Model(d).Exec(ctx); err != nil {
		return nil, err
	}
	if ok, err := s.claim(ctx, d.ID); err != nil || !ok {
		return d, err
	}
	return s.attempt(context.WithoutCancel(ctx), d.ID)
}

func (a *api) PingWebhook(ctx context.Context, req apigen.PingWebhookRequestObject) (apigen.PingWebhookResponseObject, error) {
	h, repo, err := a.s.hookFor(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	d, err := a.s.sendPing(ctx, h, repo)
	if err != nil {
		return nil, err
	}
	return apigen.PingWebhook200JSONResponse(a.s.toAPIDelivery(d)), nil
}

func (a *api) ListWebhookDeliveries(ctx context.Context, req apigen.ListWebhookDeliveriesRequestObject) (apigen.ListWebhookDeliveriesResponseObject, error) {
	s := a.s
	h, _, err := s.hookFor(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	limit, offset := pageParams(req.Params.Page, req.Params.PerPage)
	var ds []*model.WebhookDelivery
	q := s.db.NewSelect().Model(&ds).ExcludeColumn("payload", "response_body", "request_headers", "response_headers").
		Where("webhook_id = ?", h.ID)
	if req.Params.Status != nil {
		q = q.Where("status = ?", string(*req.Params.Status))
	}
	if ev := deref(req.Params.Event); ev != "" {
		q = q.Where("event = ?", ev)
	}
	total, err := q.OrderExpr("id DESC").Limit(int64(limit)).Offset(int64(offset)).ScanAndCount(ctx)
	if err != nil {
		return nil, err
	}
	out := apigen.WebhookDeliveryPage{Deliveries: make([]apigen.WebhookDelivery, len(ds)), Total: int(total)}
	for i, d := range ds {
		out.Deliveries[i] = s.toAPIDelivery(d)
	}
	return apigen.ListWebhookDeliveries200JSONResponse(out), nil
}

func (s *Server) loadDelivery(ctx context.Context, hookID, id int64) (*model.WebhookDelivery, error) {
	d := new(model.WebhookDelivery)
	if err := s.db.NewSelect().Model(d).Where("id = ? AND webhook_id = ?", id, hookID).Scan(ctx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apierr.NotFound("delivery %d not found", id)
		}
		return nil, err
	}
	return d, nil
}

func (a *api) GetWebhookDelivery(ctx context.Context, req apigen.GetWebhookDeliveryRequestObject) (apigen.GetWebhookDeliveryResponseObject, error) {
	s := a.s
	h, _, err := s.hookFor(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	d, err := s.loadDelivery(ctx, h.ID, req.Delivery)
	if err != nil {
		return nil, err
	}
	var out apigen.WebhookDeliveryDetail
	if err := remarshalInto(s.toAPIDelivery(d), &out); err != nil {
		return nil, err
	}
	out.Request.Url = d.RequestURL
	if out.Request.Url == "" {
		out.Request.Url = h.URL
	}
	out.Request.Headers = jsonStringMap(d.RequestHeaders)
	if out.Request.Headers == nil {
		out.Request.Headers = map[string]string{}
	}
	out.Request.Payload = map[string]any{}
	_ = json.Unmarshal([]byte(d.Payload), &out.Request.Payload)
	if d.ResponseStatus != nil {
		hdr := jsonStringMap(d.ResponseHeaders)
		if hdr == nil {
			hdr = map[string]string{}
		}
		out.Response = &struct {
			Body      string            `json:"body"`
			Headers   map[string]string `json:"headers"`
			Truncated bool              `json:"truncated"`
		}{Body: d.ResponseBody, Headers: hdr, Truncated: d.ResponseTrunc}
	}
	return apigen.GetWebhookDelivery200JSONResponse(out), nil
}

func remarshalInto(v, out any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

func (a *api) RedeliverWebhookDelivery(ctx context.Context, req apigen.RedeliverWebhookDeliveryRequestObject) (apigen.RedeliverWebhookDeliveryResponseObject, error) {
	s := a.s
	h, _, err := s.hookFor(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	old, err := s.loadDelivery(ctx, h.ID, req.Delivery)
	if err != nil {
		return nil, err
	}
	now := s.now()
	d := &model.WebhookDelivery{WebhookID: h.ID, GUID: newGUID(), Event: old.Event, Action: old.Action, Payload: old.Payload, Redelivery: true,
		Status: model.DeliveryPending, NextAttemptAt: &now, RequestHeaders: "{}", ResponseHeaders: "{}", CreatedAt: now}
	if _, err := s.db.NewInsert().Model(d).Exec(ctx); err != nil {
		return nil, err
	}
	s.hooks.wake.notify()
	return apigen.RedeliverWebhookDelivery201JSONResponse(s.toAPIDelivery(d)), nil
}

// DeliverWebhooks sends every due delivery and waits for them (tests and
// tools; servers run the background worker).
func (s *Server) DeliverWebhooks(ctx context.Context) error {
	for {
		n, err := s.dispatchDue(ctx)
		if err != nil {
			return err
		}
		s.hooks.wg.Wait()
		if n == 0 {
			return nil
		}
	}
}
