package server

import (
	"context"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/api/apigen"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/apierr"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/audit"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/auth"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/model"
)

const recoveryCodeCount = 10

func (a *api) StartTotpSetup(ctx context.Context, _ apigen.StartTotpSetupRequestObject) (apigen.StartTotpSetupResponseObject, error) {
	s := a.s
	u := mustPrincipal(ctx).User
	if u.TOTPEnabled {
		return nil, apierr.Conflict("two-factor authentication is already enabled; disable it first to re-enroll")
	}
	issuer := "tgp"
	if org, err := s.loadOrg(ctx); err == nil && org.Name != "" {
		issuer = org.Name
	}
	secret, url, err := auth.NewTOTPKey(issuer, u.Username)
	if err != nil {
		return nil, err
	}
	u.TOTPPendingEnc = s.box.Seal([]byte(secret), totpAAD(u.ID))
	if _, err := s.db.NewUpdate().Model(u).Column("totp_pending_enc").WherePK().Exec(ctx); err != nil {
		return nil, err
	}
	return apigen.StartTotpSetup200JSONResponse{Secret: secret, OtpauthUrl: url}, nil
}

func (a *api) EnableTotp(ctx context.Context, req apigen.EnableTotpRequestObject) (apigen.EnableTotpResponseObject, error) {
	s := a.s
	_, r := httpFrom(ctx)
	u := mustPrincipal(ctx).User
	if req.Body == nil {
		return nil, apierr.BadRequest("missing request body")
	}
	if u.TOTPEnabled {
		return nil, apierr.Conflict("two-factor authentication is already enabled")
	}
	if u.TOTPPendingEnc == "" {
		return nil, apierr.Conflict("no two-factor setup in progress; start one first")
	}
	secret, err := s.box.Open(u.TOTPPendingEnc, totpAAD(u.ID))
	if err != nil {
		return nil, err
	}
	step, ok := auth.VerifyTOTP(string(secret), req.Body.Code, 0, time.Now())
	if !ok {
		return nil, apierr.Validation("that code doesn't match; check your authenticator app's clock and try the current code")
	}
	var codes []string
	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		u.TOTPSecretEnc = u.TOTPPendingEnc
		u.TOTPPendingEnc = ""
		u.TOTPEnabled = true
		u.TOTPLastStep = step
		u.UpdatedAt = time.Now().UTC()
		if _, err := tx.NewUpdate().Model(u).Column("totp_secret_enc", "totp_pending_enc", "totp_enabled", "totp_last_step", "updated_at").WherePK().Exec(ctx); err != nil {
			return err
		}
		var err error
		if codes, err = replaceRecoveryCodes(ctx, tx, u.ID); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{Actor: u, Action: audit.TOTPEnable, TargetType: "user", Target: u.Username, IP: clientIP(r)})
	})
	if err != nil {
		return nil, err
	}
	return apigen.EnableTotp200JSONResponse{Codes: codes}, nil
}

func replaceRecoveryCodes(ctx context.Context, tx bun.IDB, userID int64) ([]string, error) {
	if _, err := tx.NewDelete().Model((*model.RecoveryCode)(nil)).Where("user_id = ?", userID).Exec(ctx); err != nil {
		return nil, err
	}
	codes := make([]string, recoveryCodeCount)
	rows := make([]model.RecoveryCode, recoveryCodeCount)
	for i := range codes {
		codes[i] = auth.NewRecoveryCode()
		rows[i] = model.RecoveryCode{UserID: userID, CodeHash: auth.HashToken(auth.NormalizeRecoveryCode(codes[i]))}
	}
	if _, err := tx.NewInsert().Model(&rows).Exec(ctx); err != nil {
		return nil, err
	}
	return codes, nil
}

// clearTOTP turns off 2FA for u and deletes their recovery codes.
func clearTOTP(ctx context.Context, tx bun.IDB, u *model.User) error {
	u.TOTPEnabled = false
	u.TOTPSecretEnc = ""
	u.TOTPPendingEnc = ""
	u.TOTPLastStep = 0
	u.UpdatedAt = time.Now().UTC()
	if _, err := tx.NewUpdate().Model(u).Column("totp_enabled", "totp_secret_enc", "totp_pending_enc", "totp_last_step", "updated_at").WherePK().Exec(ctx); err != nil {
		return err
	}
	_, err := tx.NewDelete().Model((*model.RecoveryCode)(nil)).Where("user_id = ?", u.ID).Exec(ctx)
	return err
}

func (a *api) DisableTotp(ctx context.Context, req apigen.DisableTotpRequestObject) (apigen.DisableTotpResponseObject, error) {
	s := a.s
	_, r := httpFrom(ctx)
	u := mustPrincipal(ctx).User
	if req.Body == nil {
		return nil, apierr.BadRequest("missing request body")
	}
	if err := s.requirePassword(u, req.Body.Password); err != nil {
		return nil, err
	}
	if !u.TOTPEnabled {
		return nil, apierr.Conflict("two-factor authentication isn't enabled")
	}
	err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := clearTOTP(ctx, tx, u); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{Actor: u, Action: audit.TOTPDisable, TargetType: "user", Target: u.Username, IP: clientIP(r)})
	})
	if err != nil {
		return nil, err
	}
	return apigen.DisableTotp204Response{}, nil
}

func (a *api) RegenerateRecoveryCodes(ctx context.Context, req apigen.RegenerateRecoveryCodesRequestObject) (apigen.RegenerateRecoveryCodesResponseObject, error) {
	s := a.s
	_, r := httpFrom(ctx)
	u := mustPrincipal(ctx).User
	if req.Body == nil {
		return nil, apierr.BadRequest("missing request body")
	}
	if err := s.requirePassword(u, req.Body.Password); err != nil {
		return nil, err
	}
	if !u.TOTPEnabled {
		return nil, apierr.Conflict("two-factor authentication isn't enabled")
	}
	var codes []string
	err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var err error
		if codes, err = replaceRecoveryCodes(ctx, tx, u.ID); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{Actor: u, Action: audit.RecoveryRegenerate, TargetType: "user", Target: u.Username, IP: clientIP(r)})
	})
	if err != nil {
		return nil, err
	}
	return apigen.RegenerateRecoveryCodes200JSONResponse{Codes: codes}, nil
}

func toAPIToken(t *model.AccessToken) apigen.AccessToken {
	scopes := []apigen.TokenScope{}
	for _, sc := range splitScopes(t.Scopes) {
		scopes = append(scopes, apigen.TokenScope(sc))
	}
	return apigen.AccessToken{
		Id:         t.ID,
		Name:       t.Name,
		Prefix:     t.Prefix,
		Scopes:     scopes,
		LastUsedAt: t.LastUsedAt,
		ExpiresAt:  t.ExpiresAt,
		CreatedAt:  t.CreatedAt,
	}
}

func (a *api) ListTokens(ctx context.Context, _ apigen.ListTokensRequestObject) (apigen.ListTokensResponseObject, error) {
	u := mustPrincipal(ctx).User
	var toks []model.AccessToken
	if err := a.s.db.NewSelect().Model(&toks).Where("user_id = ?", u.ID).Order("created_at DESC", "id DESC").Scan(ctx); err != nil {
		return nil, err
	}
	out := make(apigen.ListTokens200JSONResponse, 0, len(toks))
	for i := range toks {
		out = append(out, toAPIToken(&toks[i]))
	}
	return out, nil
}

// scopeOrder fixes the stored order so scope strings are canonical.
var scopeOrder = []string{"read", "write", "admin"}

func (a *api) CreateToken(ctx context.Context, req apigen.CreateTokenRequestObject) (apigen.CreateTokenResponseObject, error) {
	s := a.s
	_, r := httpFrom(ctx)
	p := mustPrincipal(ctx)
	b := req.Body
	if b == nil {
		return nil, apierr.BadRequest("missing request body")
	}
	name := strings.TrimSpace(b.Name)
	if name == "" || len(name) > 100 {
		return nil, apierr.Validation("token name is required (max 100 characters)")
	}
	want := map[string]bool{}
	for _, sc := range b.Scopes {
		if !sc.Valid() {
			return nil, apierr.Validation("unknown scope %q (use read, write or admin)", sc)
		}
		want[string(sc)] = true
	}
	if len(want) == 0 {
		return nil, apierr.Validation("at least one scope is required")
	}
	if want["admin"] && !p.User.IsAdmin() {
		return nil, apierr.Forbidden("only admins can create tokens with the admin scope")
	}
	var scopes []string
	for _, sc := range scopeOrder {
		if !want[sc] {
			continue
		}
		// A token can't mint a token more powerful than itself.
		if p.Method == MethodToken && !p.HasScope(sc) {
			return nil, apierr.Forbidden("the token you're using lacks the %q scope, so it can't create a token with it", sc)
		}
		scopes = append(scopes, sc)
	}
	now := time.Now().UTC()
	raw := auth.NewPAT()
	tok := &model.AccessToken{
		UserID:    p.User.ID,
		Name:      name,
		TokenHash: auth.HashToken(raw),
		Prefix:    raw[:len(auth.PATPrefix)+4],
		Scopes:    strings.Join(scopes, ","),
		CreatedAt: now,
	}
	if b.ExpiresInDays != nil {
		d := *b.ExpiresInDays
		if d < 1 || d > 3650 {
			return nil, apierr.Validation("expires_in_days must be between 1 and 3650")
		}
		exp := now.Add(time.Duration(d) * 24 * time.Hour)
		tok.ExpiresAt = &exp
	}
	if _, err := s.db.NewInsert().Model(tok).Exec(ctx); err != nil {
		return nil, err
	}
	if err := audit.Record(ctx, s.db, audit.Entry{Actor: p.User, Action: audit.TokenCreate, TargetType: "token", Target: name, IP: clientIP(r),
		Details: map[string]any{"id": tok.ID, "scopes": scopes}}); err != nil {
		return nil, err
	}
	return apigen.CreateToken201JSONResponse{Token: raw, AccessToken: toAPIToken(tok)}, nil
}

func (a *api) DeleteToken(ctx context.Context, req apigen.DeleteTokenRequestObject) (apigen.DeleteTokenResponseObject, error) {
	s := a.s
	_, r := httpFrom(ctx)
	u := mustPrincipal(ctx).User
	tok := new(model.AccessToken)
	if err := s.db.NewSelect().Model(tok).Where("id = ?", req.Id).Where("user_id = ?", u.ID).Scan(ctx); err != nil {
		return nil, apierr.NotFound("token %d not found", req.Id)
	}
	if _, err := s.db.NewDelete().Model(tok).WherePK().Exec(ctx); err != nil {
		return nil, err
	}
	_ = audit.Record(ctx, s.db, audit.Entry{Actor: u, Action: audit.TokenDelete, TargetType: "token", Target: tok.Name, IP: clientIP(r), Details: map[string]any{"id": tok.ID}})
	return apigen.DeleteToken204Response{}, nil
}
