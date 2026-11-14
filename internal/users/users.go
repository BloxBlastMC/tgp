// Package users holds user account operations shared by first-start setup,
// the admin API and tooling.
package users

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/apierr"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/auth"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/model"
)

// TempPasswordTTL is how long a temporary password works.
const TempPasswordTTL = 72 * time.Hour

var usernameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,38}$`)

// reserved usernames would clash with web or API routes.
var reserved = map[string]bool{
	"admin": false, // allowed: a natural first-admin name
	"api":   true, "assets": true, "login": true, "logout": true, "setup": true,
	"settings": true, "new": true, "healthz": true, "readyz": true, "git": true,
}

// NormalizeUsername lowercases and trims a username.
func NormalizeUsername(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// ValidateUsername checks a normalized username.
func ValidateUsername(u string) error {
	if !usernameRe.MatchString(u) {
		return apierr.Validation("username %q is invalid: use 1-39 lowercase letters, digits, '.', '-' or '_', starting with a letter or digit", u)
	}
	if reserved[u] {
		return apierr.Validation("username %q is reserved", u)
	}
	return nil
}

// NormalizeEmail trims and validates an optional email address.
func NormalizeEmail(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	a, err := mail.ParseAddress(s)
	if err != nil || a.Address != s {
		return "", apierr.Validation("email %q is invalid", s)
	}
	return strings.ToLower(s), nil
}

// CreateParams describes a new user.
type CreateParams struct {
	Username    string
	DisplayName string
	Email       string
	Role        model.Role
	AllRepos    bool
	// Password, if set, becomes the user's real password. Otherwise a
	// temporary password is generated and must be changed at first login.
	Password string
}

// Created is the result of Create.
type Created struct {
	User         *model.User
	TempPassword string // empty when Password was given
}

// Create validates and inserts a user.
func Create(ctx context.Context, db bun.IDB, p CreateParams) (*Created, error) {
	username := NormalizeUsername(p.Username)
	if err := ValidateUsername(username); err != nil {
		return nil, err
	}
	if !p.Role.Valid() {
		return nil, apierr.Validation("role must be admin, maintainer or tester")
	}
	email, err := NormalizeEmail(p.Email)
	if err != nil {
		return nil, err
	}
	displayName := strings.TrimSpace(p.DisplayName)
	if len(displayName) > 100 {
		return nil, apierr.Validation("display name is too long (max 100)")
	}
	if displayName == "" {
		displayName = username
	}
	if err := ensureUnique(ctx, db, username, email, 0); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	u := &model.User{
		Username:    username,
		DisplayName: displayName,
		Email:       email,
		Role:        p.Role,
		AllRepos:    p.AllRepos || p.Role == model.RoleAdmin,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	out := &Created{User: u}
	pw := p.Password
	if pw == "" {
		pw = auth.NewTempPassword()
		out.TempPassword = pw
		exp := now.Add(TempPasswordTTL)
		u.MustChangePassword = true
		u.TempPasswordExpiresAt = &exp
	} else {
		if err := auth.ValidatePassword(pw); err != nil {
			return nil, apierr.Validation("%v", err)
		}
		u.PasswordChangedAt = &now
	}
	if u.PasswordHash, err = auth.HashPassword(pw); err != nil {
		return nil, err
	}
	if _, err := db.NewInsert().Model(u).Exec(ctx); err != nil {
		if IsUniqueViolation(err) {
			return nil, apierr.Conflict("username %q is already taken", username)
		}
		return nil, err
	}
	return out, nil
}

func ensureUnique(ctx context.Context, db bun.IDB, username, email string, exceptID int64) error {
	if username != "" {
		n, err := db.NewSelect().Model((*model.User)(nil)).Where("username = ?", username).Where("id != ?", exceptID).Count(ctx)
		if err != nil {
			return err
		}
		if n > 0 {
			return apierr.Conflict("username %q is already taken", username)
		}
	}
	if email != "" {
		n, err := db.NewSelect().Model((*model.User)(nil)).Where("email = ?", email).Where("id != ?", exceptID).Count(ctx)
		if err != nil {
			return err
		}
		if n > 0 {
			return apierr.Conflict("email %q is already used by another user", email)
		}
	}
	return nil
}

// EnsureEmailFree checks no other user has email.
func EnsureEmailFree(ctx context.Context, db bun.IDB, email string, exceptID int64) error {
	return ensureUnique(ctx, db, "", email, exceptID)
}

// ResetTempPassword gives u a new temporary password, forces a change at
// next login and signs them out everywhere.
func ResetTempPassword(ctx context.Context, db bun.IDB, u *model.User) (string, error) {
	pw := auth.NewTempPassword()
	hash, err := auth.HashPassword(pw)
	if err != nil {
		return "", err
	}
	now := time.Now().UTC()
	exp := now.Add(TempPasswordTTL)
	u.PasswordHash = hash
	u.MustChangePassword = true
	u.TempPasswordExpiresAt = &exp
	u.UpdatedAt = now
	if _, err := db.NewUpdate().Model(u).Column("password_hash", "must_change_password", "temp_password_expires_at", "updated_at").WherePK().Exec(ctx); err != nil {
		return "", err
	}
	if _, err := db.NewDelete().Model((*model.Session)(nil)).Where("user_id = ?", u.ID).Exec(ctx); err != nil {
		return "", err
	}
	return pw, nil
}

// GetByUsername loads a user, returning a not_found API error if missing.
func GetByUsername(ctx context.Context, db bun.IDB, username string) (*model.User, error) {
	u := new(model.User)
	err := db.NewSelect().Model(u).Where("username = ?", NormalizeUsername(username)).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apierr.NotFound("user %q not found", username)
	}
	return u, err
}

// CountActiveAdmins returns the number of enabled admins, excluding exceptID.
func CountActiveAdmins(ctx context.Context, db bun.IDB, exceptID int64) (int64, error) {
	return db.NewSelect().Model((*model.User)(nil)).
		Where("role = ?", model.RoleAdmin).Where("disabled = ?", false).Where("id != ?", exceptID).Count(ctx)
}

// IsUniqueViolation reports whether err is a unique constraint violation on
// SQLite or Postgres.
func IsUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "UNIQUE constraint failed") || strings.Contains(s, "SQLSTATE=23505") || strings.Contains(s, "duplicate key value")
}

// String renders a user for logs.
func String(u *model.User) string { return fmt.Sprintf("%s(%d)", u.Username, u.ID) }
