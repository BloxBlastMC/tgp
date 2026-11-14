// Package audit records security-relevant actions in the audit_log table.
package audit

import (
	"context"
	"encoding/json"
	"time"

	"github.com/uptrace/bun"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/model"
)

// Well-known actions. Keep names stable: admins filter on them.
const (
	SetupComplete      = "setup.complete"
	LoginSuccess       = "auth.login"
	LoginFailed        = "auth.login_failed"
	Logout             = "auth.logout"
	PasswordChange     = "account.password_change"
	TOTPEnable         = "account.totp_enable"
	TOTPDisable        = "account.totp_disable"
	RecoveryRegenerate = "account.recovery_codes_regenerate"
	RecoveryCodeUsed   = "account.recovery_code_used"
	TokenCreate        = "token.create"
	TokenDelete        = "token.delete"
	UserCreate         = "user.create"
	UserUpdate         = "user.update"
	UserResetPassword  = "user.reset_password"
	UserResetTOTP      = "user.reset_totp"
	OrgUpdate          = "org.update"
	RepoCreate         = "repo.create"
	RepoUpdate         = "repo.update"
	RepoDelete         = "repo.delete"
	UserRepoAccess     = "user.repo_access"
	CISettingsUpdate   = "ci.settings_update"
	SecretSet          = "secret.set"
	SecretDelete       = "secret.delete"
	RunnerTokenCreate  = "runner.token_create"
	RunnerRegister     = "runner.register"
	RunnerUpdate       = "runner.update"
	RunnerDelete       = "runner.delete"
	ReleaseDelete      = "release.delete"
)

// Entry is one audit record.
type Entry struct {
	Actor      *model.User // nil for anonymous actions
	ActorName  string      // used when Actor is nil (e.g. attempted username)
	Action     string
	TargetType string
	Target     string
	IP         string
	Details    map[string]any
}

// Record writes e using db (which may be a transaction).
func Record(ctx context.Context, db bun.IDB, e Entry) error {
	row := &model.AuditEntry{
		ActorName:  e.ActorName,
		Action:     e.Action,
		TargetType: e.TargetType,
		Target:     e.Target,
		IP:         e.IP,
		Details:    "{}",
		CreatedAt:  time.Now().UTC(),
	}
	if e.Actor != nil {
		id := e.Actor.ID
		row.ActorID = &id
		row.ActorName = e.Actor.Username
	}
	if len(e.Details) > 0 {
		b, err := json.Marshal(e.Details)
		if err != nil {
			return err
		}
		row.Details = string(b)
	}
	_, err := db.NewInsert().Model(row).Exec(ctx)
	return err
}
