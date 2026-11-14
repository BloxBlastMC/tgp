package server

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/api/apigen"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/apierr"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/audit"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/auth"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/model"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/users"
)

const setupTokenKey = "setup_token_hash"

// EnsureSetupToken generates a fresh one-time setup token if setup is still
// pending and returns it (empty when setup is already done). The token
// replaces any previous one, so only the most recently printed token works.
func (s *Server) EnsureSetupToken(ctx context.Context) (string, error) {
	done, err := s.setupComplete(ctx)
	if err != nil || done {
		return "", err
	}
	tok := auth.RandomToken(18)
	row := &model.ServerSetting{Key: setupTokenKey, Value: auth.HashToken(tok), UpdatedAt: time.Now().UTC()}
	_, err = s.db.NewInsert().Model(row).On("CONFLICT (key) DO UPDATE").Set("value = EXCLUDED.value, updated_at = EXCLUDED.updated_at").Exec(ctx)
	if err != nil {
		return "", fmt.Errorf("store setup token: %w", err)
	}
	return tok, nil
}

func (a *api) CompleteSetup(ctx context.Context, req apigen.CompleteSetupRequestObject) (apigen.CompleteSetupResponseObject, error) {
	s := a.s
	w, r := httpFrom(ctx)
	b := req.Body
	if b == nil {
		return nil, apierr.BadRequest("missing request body")
	}
	if done, err := s.setupComplete(ctx); err != nil {
		return nil, err
	} else if done {
		return nil, apierr.Conflict("setup has already been completed")
	}

	ip := clientIP(r)
	if blocked, _ := s.ipLimiter.Blocked("setup:" + ip); blocked {
		return nil, apierr.New(429, apierr.CodeRateLimited, "too many attempts; try again later")
	}
	var stored model.ServerSetting
	err := s.db.NewSelect().Model(&stored).Where("key = ?", setupTokenKey).Scan(ctx)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	given := auth.HashToken(strings.TrimSpace(b.SetupToken))
	if stored.Value == "" || subtle.ConstantTimeCompare([]byte(given), []byte(stored.Value)) != 1 {
		s.ipLimiter.Fail("setup:" + ip)
		return nil, apierr.Forbidden("invalid setup token: copy the token printed in the tgp-server log (restart the server to print a new one)")
	}

	orgName := strings.TrimSpace(b.Org.Name)
	if orgName == "" || len(orgName) > 100 {
		return nil, apierr.Validation("org name is required (max 100 characters)")
	}
	baseURL, err := normalizeBaseURL(deref(b.Org.BaseUrl))
	if err != nil {
		return nil, err
	}

	var (
		admin   *model.User
		created []apigen.UserWithTempPassword
	)
	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Claim setup atomically: only one request can flip the flag.
		now := time.Now().UTC()
		res, err := tx.NewUpdate().Model((*model.OrgSettings)(nil)).
			Set("name = ?", orgName).
			Set("description = ?", strings.TrimSpace(deref(b.Org.Description))).
			Set("base_url = ?", baseURL).
			Set("setup_completed_at = ?", now).
			Set("updated_at = ?", now).
			Where("id = 1").Where("setup_completed_at IS NULL").Exec(ctx)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return apierr.Conflict("setup has already been completed")
		}

		c, err := users.Create(ctx, tx, users.CreateParams{
			Username:    b.Admin.Username,
			DisplayName: deref(b.Admin.DisplayName),
			Email:       deref(b.Admin.Email),
			Role:        model.RoleAdmin,
			Password:    b.Admin.Password,
		})
		if err != nil {
			return prefixErr("admin", err)
		}
		admin = c.User
		if b.Users != nil {
			for i, cu := range *b.Users {
				c, err := users.Create(ctx, tx, users.CreateParams{
					Username:    cu.Username,
					DisplayName: deref(cu.DisplayName),
					Email:       deref(cu.Email),
					Role:        model.Role(cu.Role),
					AllRepos:    deref(cu.AllRepos),
				})
				if err != nil {
					return prefixErr(fmt.Sprintf("user %d (%s)", i+1, cu.Username), err)
				}
				created = append(created, apigen.UserWithTempPassword{
					User:                  toAPIUser(c.User),
					TempPassword:          c.TempPassword,
					TempPasswordExpiresAt: *c.User.TempPasswordExpiresAt,
				})
				if err := audit.Record(ctx, tx, audit.Entry{Actor: admin, Action: audit.UserCreate, TargetType: "user", Target: c.User.Username, IP: ip,
					Details: map[string]any{"role": c.User.Role, "all_repos": c.User.AllRepos}}); err != nil {
					return err
				}
			}
		}
		if _, err := tx.NewDelete().Model((*model.ServerSetting)(nil)).Where("key = ?", setupTokenKey).Exec(ctx); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{Actor: admin, Action: audit.SetupComplete, TargetType: "org", Target: orgName, IP: ip,
			Details: map[string]any{"users_created": len(created)}})
	})
	if err != nil {
		return nil, err
	}
	s.setupDone.Store(true)
	s.log.Info("setup complete", "org", orgName, "admin", admin.Username, "users", len(created))

	if _, err := s.createSession(ctx, w, r, admin); err != nil {
		return nil, err
	}
	if created == nil {
		created = []apigen.UserWithTempPassword{}
	}
	return apigen.CompleteSetup200JSONResponse{Admin: toAPIUser(admin), Users: created}, nil
}

// prefixErr adds context to validation/conflict messages from users.Create.
func prefixErr(prefix string, err error) error {
	var e *apierr.Error
	if errors.As(err, &e) {
		c := *e
		c.Message = prefix + ": " + e.Message
		return &c
	}
	return err
}

func normalizeBaseURL(raw string) (string, error) {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
		return "", apierr.Validation("base URL must be an absolute http(s) URL like https://git.example.com")
	}
	return raw, nil
}
