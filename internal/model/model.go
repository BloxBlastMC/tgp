// Package model holds the bun models for tgp's tables. Migrations keep their
// own frozen copies; these track the current schema.
package model

import (
	"time"

	"github.com/uptrace/bun"
)

// Role is a user's org-wide role.
type Role string

const (
	RoleAdmin      Role = "admin"
	RoleMaintainer Role = "maintainer"
	RoleTester     Role = "tester"
)

// Valid reports whether r is a known role.
func (r Role) Valid() bool {
	switch r {
	case RoleAdmin, RoleMaintainer, RoleTester:
		return true
	}
	return false
}

type OrgSettings struct {
	bun.BaseModel `bun:"table:org_settings"`

	ID               int64      `bun:"id,pk"`
	Name             string     `bun:"name"`
	Description      string     `bun:"description"`
	LogoPath         string     `bun:"logo_path"`
	BaseURL          string     `bun:"base_url"`
	SetupCompletedAt *time.Time `bun:"setup_completed_at"`
	CreatedAt        time.Time  `bun:"created_at"`
	UpdatedAt        time.Time  `bun:"updated_at"`
}

type ServerSetting struct {
	bun.BaseModel `bun:"table:server_settings"`

	Key       string    `bun:"key,pk"`
	Value     string    `bun:"value"`
	UpdatedAt time.Time `bun:"updated_at"`
}

type User struct {
	bun.BaseModel `bun:"table:users"`

	ID                    int64      `bun:"id,pk,autoincrement"`
	Username              string     `bun:"username"`
	DisplayName           string     `bun:"display_name"`
	Email                 string     `bun:"email"`
	Role                  Role       `bun:"role"`
	AllRepos              bool       `bun:"all_repos"`
	PasswordHash          string     `bun:"password_hash"`
	MustChangePassword    bool       `bun:"must_change_password"`
	TempPasswordExpiresAt *time.Time `bun:"temp_password_expires_at"`
	TOTPEnabled           bool       `bun:"totp_enabled"`
	TOTPSecretEnc         string     `bun:"totp_secret_enc"`
	TOTPPendingEnc        string     `bun:"totp_pending_enc"`
	TOTPLastStep          int64      `bun:"totp_last_step"`
	Disabled              bool       `bun:"disabled"`
	LastLoginAt           *time.Time `bun:"last_login_at"`
	PasswordChangedAt     *time.Time `bun:"password_changed_at"`
	CreatedAt             time.Time  `bun:"created_at"`
	UpdatedAt             time.Time  `bun:"updated_at"`
}

// IsAdmin reports whether the user is an admin.
func (u *User) IsAdmin() bool { return u.Role == RoleAdmin }

type Session struct {
	bun.BaseModel `bun:"table:sessions"`

	ID         string    `bun:"id,pk"`
	UserID     int64     `bun:"user_id"`
	IP         string    `bun:"ip"`
	UserAgent  string    `bun:"user_agent"`
	CreatedAt  time.Time `bun:"created_at"`
	LastSeenAt time.Time `bun:"last_seen_at"`
	ExpiresAt  time.Time `bun:"expires_at"`
}

type AccessToken struct {
	bun.BaseModel `bun:"table:access_tokens"`

	ID         int64      `bun:"id,pk,autoincrement"`
	UserID     int64      `bun:"user_id"`
	Name       string     `bun:"name"`
	TokenHash  string     `bun:"token_hash"`
	Prefix     string     `bun:"prefix"`
	Scopes     string     `bun:"scopes"`
	LastUsedAt *time.Time `bun:"last_used_at"`
	ExpiresAt  *time.Time `bun:"expires_at"`
	CreatedAt  time.Time  `bun:"created_at"`
}

type RecoveryCode struct {
	bun.BaseModel `bun:"table:recovery_codes"`

	ID       int64      `bun:"id,pk,autoincrement"`
	UserID   int64      `bun:"user_id"`
	CodeHash string     `bun:"code_hash"`
	UsedAt   *time.Time `bun:"used_at"`
}

type AuditEntry struct {
	bun.BaseModel `bun:"table:audit_log"`

	ID         int64     `bun:"id,pk,autoincrement"`
	ActorID    *int64    `bun:"actor_id"`
	ActorName  string    `bun:"actor_name"`
	Action     string    `bun:"action"`
	TargetType string    `bun:"target_type"`
	Target     string    `bun:"target"`
	IP         string    `bun:"ip"`
	Details    string    `bun:"details"`
	CreatedAt  time.Time `bun:"created_at"`
}

type OrgLogo struct {
	bun.BaseModel `bun:"table:org_logo"`

	ID          int64     `bun:"id,pk"`
	ContentType string    `bun:"content_type"`
	Data        []byte    `bun:"data"`
	UpdatedAt   time.Time `bun:"updated_at"`
}

type Repo struct {
	bun.BaseModel `bun:"table:repos,alias:r"`

	ID            int64      `bun:"id,pk,autoincrement"`
	Name          string     `bun:"name"`
	LowerName     string     `bun:"lower_name"`
	Description   string     `bun:"description"`
	DefaultBranch string     `bun:"default_branch"`
	Archived      bool       `bun:"archived"`
	CreatedByID   *int64     `bun:"created_by_id"`
	CreatedAt     time.Time  `bun:"created_at"`
	UpdatedAt     time.Time  `bun:"updated_at"`
	PushedAt      *time.Time `bun:"pushed_at"`
}

type RepoGrant struct {
	bun.BaseModel `bun:"table:repo_grants"`

	ID        int64     `bun:"id,pk,autoincrement"`
	RepoID    int64     `bun:"repo_id"`
	UserID    int64     `bun:"user_id"`
	CreatedAt time.Time `bun:"created_at"`
}
