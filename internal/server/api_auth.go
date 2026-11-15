package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/api/apigen"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/apierr"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/audit"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/auth"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/model"
)

var errBadCredentials = apierr.Unauthenticated("invalid username or password")

func rateLimited(until time.Time) error {
	secs := int(time.Until(until).Seconds()) + 1
	return apierr.New(http.StatusTooManyRequests, apierr.CodeRateLimited, "too many failed login attempts; try again in %d seconds", secs).
		WithDetails(map[string]any{"retry_after_seconds": secs})
}

func (a *api) Login(ctx context.Context, req apigen.LoginRequestObject) (apigen.LoginResponseObject, error) {
	s := a.s
	w, r := httpFrom(ctx)
	b := req.Body
	if b == nil {
		return nil, apierr.BadRequest("missing request body")
	}
	ip := clientIP(r)
	ident := strings.ToLower(strings.TrimSpace(b.Username))
	userKey, ipKey := "user:"+ident, "ip:"+ip
	if blocked, until := s.userLimiter.Blocked(userKey); blocked {
		return nil, rateLimited(until)
	}
	if blocked, until := s.ipLimiter.Blocked(ipKey); blocked {
		return nil, rateLimited(until)
	}
	fail := func(reason string, u *model.User) error {
		s.userLimiter.Fail(userKey)
		s.ipLimiter.Fail(ipKey)
		e := audit.Entry{Actor: u, ActorName: ident, Action: audit.LoginFailed, TargetType: "user", Target: ident, IP: ip, Details: map[string]any{"reason": reason}}
		if err := audit.Record(ctx, s.db, e); err != nil {
			s.log.Warn("audit login failure", "err", err)
		}
		return errBadCredentials
	}

	u := new(model.User)
	err := s.db.NewSelect().Model(u).Where("username = ? OR (email != '' AND email = ?)", ident, ident).Limit(1).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		auth.BurnPasswordCheck(b.Password)
		return nil, fail("unknown_user", nil)
	} else if err != nil {
		return nil, err
	}
	if err := auth.VerifyPassword(b.Password, u.PasswordHash); err != nil {
		return nil, fail("bad_password", u)
	}
	if u.Disabled {
		return nil, apierr.Forbidden("this account is disabled; contact an admin")
	}
	now := time.Now().UTC()
	if u.MustChangePassword && u.TempPasswordExpiresAt != nil && now.After(*u.TempPasswordExpiresAt) {
		return nil, apierr.Unauthenticated("your temporary password has expired; ask an admin to reset it")
	}

	if u.TOTPEnabled {
		code, recovery := strings.TrimSpace(deref(b.TotpCode)), strings.TrimSpace(deref(b.RecoveryCode))
		switch {
		case code != "":
			ok, err := s.consumeTOTP(ctx, u, code, now)
			if err != nil {
				return nil, err
			}
			if !ok {
				s.userLimiter.Fail(userKey)
				s.ipLimiter.Fail(ipKey)
				return nil, apierr.New(http.StatusUnauthorized, apierr.CodeTOTPRequired, "invalid or already used two-factor code")
			}
		case recovery != "":
			ok, err := s.consumeRecoveryCode(ctx, u, recovery, now)
			if err != nil {
				return nil, err
			}
			if !ok {
				s.userLimiter.Fail(userKey)
				s.ipLimiter.Fail(ipKey)
				return nil, apierr.New(http.StatusUnauthorized, apierr.CodeTOTPRequired, "invalid or already used recovery code")
			}
			_ = audit.Record(ctx, s.db, audit.Entry{Actor: u, Action: audit.RecoveryCodeUsed, TargetType: "user", Target: u.Username, IP: ip})
		default:
			return nil, apierr.New(http.StatusUnauthorized, apierr.CodeTOTPRequired, "two-factor code required")
		}
	}

	if _, err := s.createSession(ctx, w, r, u); err != nil {
		return nil, err
	}
	u.LastLoginAt = &now
	if _, err := s.db.NewUpdate().Model(u).Column("last_login_at").WherePK().Exec(ctx); err != nil {
		return nil, err
	}
	s.userLimiter.Reset(userKey)
	if err := audit.Record(ctx, s.db, audit.Entry{Actor: u, Action: audit.LoginSuccess, TargetType: "user", Target: u.Username, IP: ip}); err != nil {
		return nil, err
	}
	return apigen.Login200JSONResponse(sessionResponse(&Principal{User: u, Method: MethodSession})), nil
}

// consumeTOTP verifies code and records its time step so it can't be reused.
// The conditional update makes concurrent logins with the same code safe.
func (s *Server) consumeTOTP(ctx context.Context, u *model.User, code string, now time.Time) (bool, error) {
	secret, err := s.box.Open(u.TOTPSecretEnc, totpAAD(u.ID))
	if err != nil {
		return false, fmt.Errorf("decrypt totp secret for %s: %w", u.Username, err)
	}
	step, ok := auth.VerifyTOTP(string(secret), code, u.TOTPLastStep, now)
	if !ok {
		return false, nil
	}
	res, err := s.db.NewUpdate().Model((*model.User)(nil)).Set("totp_last_step = ?", step).
		Where("id = ?", u.ID).Where("totp_last_step < ?", step).Exec(ctx)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

func (s *Server) consumeRecoveryCode(ctx context.Context, u *model.User, code string, now time.Time) (bool, error) {
	res, err := s.db.NewUpdate().Model((*model.RecoveryCode)(nil)).Set("used_at = ?", now).
		Where("user_id = ?", u.ID).Where("code_hash = ?", auth.HashToken(auth.NormalizeRecoveryCode(code))).Where("used_at IS NULL").Exec(ctx)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

func totpAAD(userID int64) string { return fmt.Sprintf("totp:%d", userID) }

func (a *api) Logout(ctx context.Context, _ apigen.LogoutRequestObject) (apigen.LogoutResponseObject, error) {
	s := a.s
	w, r := httpFrom(ctx)
	p := mustPrincipal(ctx)
	if p.Session != nil {
		if _, err := s.db.NewDelete().Model(p.Session).WherePK().Exec(ctx); err != nil {
			return nil, err
		}
	}
	s.clearSessionCookie(w)
	_ = audit.Record(ctx, s.db, audit.Entry{Actor: p.User, Action: audit.Logout, TargetType: "user", Target: p.User.Username, IP: clientIP(r)})
	return apigen.Logout204Response{}, nil
}

func sessionResponse(p *Principal) apigen.Session {
	out := apigen.Session{
		User:               toAPIUser(p.User),
		AuthMethod:         apigen.SessionAuthMethod(p.Method),
		MustChangePassword: p.User.MustChangePassword,
	}
	if p.Method == MethodToken || p.Method == MethodJob {
		sc := append([]string{}, p.Scopes...)
		out.TokenScopes = &sc
	}
	return out
}

func (a *api) GetSession(ctx context.Context, _ apigen.GetSessionRequestObject) (apigen.GetSessionResponseObject, error) {
	return apigen.GetSession200JSONResponse(sessionResponse(mustPrincipal(ctx))), nil
}

func (a *api) ChangePassword(ctx context.Context, req apigen.ChangePasswordRequestObject) (apigen.ChangePasswordResponseObject, error) {
	s := a.s
	_, r := httpFrom(ctx)
	p := mustPrincipal(ctx)
	b := req.Body
	if b == nil {
		return nil, apierr.BadRequest("missing request body")
	}
	u := p.User
	key := "user:" + u.Username
	if blocked, until := s.userLimiter.Blocked(key); blocked {
		return nil, rateLimited(until)
	}
	if err := auth.VerifyPassword(b.CurrentPassword, u.PasswordHash); err != nil {
		s.userLimiter.Fail(key)
		return nil, apierr.Validation("current password is incorrect")
	}
	if err := auth.ValidatePassword(b.NewPassword); err != nil {
		return nil, apierr.Validation("%v", err)
	}
	if b.NewPassword == b.CurrentPassword {
		return nil, apierr.Validation("new password must be different from the current one")
	}
	hash, err := auth.HashPassword(b.NewPassword)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	wasTemp := u.MustChangePassword
	u.PasswordHash = hash
	u.MustChangePassword = false
	u.TempPasswordExpiresAt = nil
	u.PasswordChangedAt = &now
	u.UpdatedAt = now
	if _, err := s.db.NewUpdate().Model(u).Column("password_hash", "must_change_password", "temp_password_expires_at", "password_changed_at", "updated_at").WherePK().Exec(ctx); err != nil {
		return nil, err
	}
	// Sign out every other session.
	q := s.db.NewDelete().Model((*model.Session)(nil)).Where("user_id = ?", u.ID)
	if p.Session != nil {
		q = q.Where("id != ?", p.Session.ID)
	}
	if _, err := q.Exec(ctx); err != nil {
		return nil, err
	}
	if err := audit.Record(ctx, s.db, audit.Entry{Actor: u, Action: audit.PasswordChange, TargetType: "user", Target: u.Username, IP: clientIP(r),
		Details: map[string]any{"was_temporary": wasTemp}}); err != nil {
		return nil, err
	}
	return apigen.ChangePassword200JSONResponse(toAPIUser(u)), nil
}

func (a *api) ListUsers(ctx context.Context, _ apigen.ListUsersRequestObject) (apigen.ListUsersResponseObject, error) {
	var us []model.User
	if err := a.s.db.NewSelect().Model(&us).Where("disabled = ?", false).Order("username ASC").Scan(ctx); err != nil {
		return nil, err
	}
	out := make(apigen.ListUsers200JSONResponse, 0, len(us))
	for _, u := range us {
		out = append(out, apigen.UserSummary{Id: u.ID, Username: u.Username, DisplayName: u.DisplayName, Role: apigen.Role(u.Role)})
	}
	return out, nil
}

// requirePassword re-checks the caller's password for sensitive actions.
func (s *Server) requirePassword(u *model.User, pw string) error {
	key := "user:" + u.Username
	if blocked, until := s.userLimiter.Blocked(key); blocked {
		return rateLimited(until)
	}
	if err := auth.VerifyPassword(pw, u.PasswordHash); err != nil {
		s.userLimiter.Fail(key)
		return apierr.Validation("password is incorrect")
	}
	return nil
}
