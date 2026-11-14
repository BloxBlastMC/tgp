package migrations

import (
	"context"
	"time"

	"github.com/uptrace/bun"
)

type usersV1 struct {
	bun.BaseModel `bun:"table:users"`

	ID                    int64      `bun:"id,pk,autoincrement"`
	Username              string     `bun:"username,notnull,unique"`
	DisplayName           string     `bun:"display_name,notnull,default:''"`
	Email                 string     `bun:"email,notnull,default:''"`
	Role                  string     `bun:"role,notnull"`
	AllRepos              bool       `bun:"all_repos,notnull,default:false"`
	PasswordHash          string     `bun:"password_hash,notnull"`
	MustChangePassword    bool       `bun:"must_change_password,notnull,default:false"`
	TempPasswordExpiresAt *time.Time `bun:"temp_password_expires_at"`
	TOTPEnabled           bool       `bun:"totp_enabled,notnull,default:false"`
	TOTPSecretEnc         string     `bun:"totp_secret_enc,notnull,default:''"`
	TOTPPendingEnc        string     `bun:"totp_pending_enc,notnull,default:''"`
	TOTPLastStep          int64      `bun:"totp_last_step,notnull,default:0"`
	Disabled              bool       `bun:"disabled,notnull,default:false"`
	LastLoginAt           *time.Time `bun:"last_login_at"`
	PasswordChangedAt     *time.Time `bun:"password_changed_at"`
	CreatedAt             time.Time  `bun:"created_at,notnull"`
	UpdatedAt             time.Time  `bun:"updated_at,notnull"`
}

type sessionsV1 struct {
	bun.BaseModel `bun:"table:sessions"`

	ID         string    `bun:"id,pk"` // sha256 of the cookie value
	UserID     int64     `bun:"user_id,notnull"`
	IP         string    `bun:"ip,notnull,default:''"`
	UserAgent  string    `bun:"user_agent,notnull,default:''"`
	CreatedAt  time.Time `bun:"created_at,notnull"`
	LastSeenAt time.Time `bun:"last_seen_at,notnull"`
	ExpiresAt  time.Time `bun:"expires_at,notnull"`
}

type accessTokensV1 struct {
	bun.BaseModel `bun:"table:access_tokens"`

	ID         int64      `bun:"id,pk,autoincrement"`
	UserID     int64      `bun:"user_id,notnull"`
	Name       string     `bun:"name,notnull"`
	TokenHash  string     `bun:"token_hash,notnull,unique"`
	Prefix     string     `bun:"prefix,notnull"`
	Scopes     string     `bun:"scopes,notnull"`
	LastUsedAt *time.Time `bun:"last_used_at"`
	ExpiresAt  *time.Time `bun:"expires_at"`
	CreatedAt  time.Time  `bun:"created_at,notnull"`
}

type recoveryCodesV1 struct {
	bun.BaseModel `bun:"table:recovery_codes"`

	ID       int64      `bun:"id,pk,autoincrement"`
	UserID   int64      `bun:"user_id,notnull"`
	CodeHash string     `bun:"code_hash,notnull"`
	UsedAt   *time.Time `bun:"used_at"`
}

type auditLogV1 struct {
	bun.BaseModel `bun:"table:audit_log"`

	ID         int64     `bun:"id,pk,autoincrement"`
	ActorID    *int64    `bun:"actor_id"`
	ActorName  string    `bun:"actor_name,notnull,default:''"`
	Action     string    `bun:"action,notnull"`
	TargetType string    `bun:"target_type,notnull,default:''"`
	Target     string    `bun:"target,notnull,default:''"`
	IP         string    `bun:"ip,notnull,default:''"`
	Details    string    `bun:"details,notnull,default:'{}'"`
	CreatedAt  time.Time `bun:"created_at,notnull"`
}

type orgLogoV1 struct {
	bun.BaseModel `bun:"table:org_logo"`

	ID          int64     `bun:"id,pk"`
	ContentType string    `bun:"content_type,notnull"`
	Data        []byte    `bun:"data,notnull"`
	UpdatedAt   time.Time `bun:"updated_at,notnull"`
}

func init() {
	Migrations.MustRegister(func(ctx context.Context, db *bun.DB) error {
		return db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
			fkUser := `("user_id") REFERENCES "users" ("id") ON DELETE CASCADE`
			steps := []func() error{
				func() error { _, err := tx.NewCreateTable().Model((*usersV1)(nil)).Exec(ctx); return err },
				func() error {
					_, err := tx.NewCreateTable().Model((*sessionsV1)(nil)).ForeignKey(fkUser).Exec(ctx)
					return err
				},
				func() error {
					_, err := tx.NewCreateTable().Model((*accessTokensV1)(nil)).ForeignKey(fkUser).Exec(ctx)
					return err
				},
				func() error {
					_, err := tx.NewCreateTable().Model((*recoveryCodesV1)(nil)).ForeignKey(fkUser).Exec(ctx)
					return err
				},
				func() error {
					_, err := tx.NewCreateTable().Model((*auditLogV1)(nil)).
						ForeignKey(`("actor_id") REFERENCES "users" ("id") ON DELETE SET NULL`).Exec(ctx)
					return err
				},
				func() error { _, err := tx.NewCreateTable().Model((*orgLogoV1)(nil)).Exec(ctx); return err },
				func() error {
					_, err := tx.NewCreateIndex().Model((*sessionsV1)(nil)).Index("sessions_user_id_idx").Column("user_id").Exec(ctx)
					return err
				},
				func() error {
					_, err := tx.NewCreateIndex().Model((*accessTokensV1)(nil)).Index("access_tokens_user_id_idx").Column("user_id").Exec(ctx)
					return err
				},
				func() error {
					_, err := tx.NewCreateIndex().Model((*recoveryCodesV1)(nil)).Index("recovery_codes_user_id_idx").Column("user_id").Exec(ctx)
					return err
				},
				func() error {
					_, err := tx.NewCreateIndex().Model((*auditLogV1)(nil)).Index("audit_log_created_at_idx").Column("created_at").Exec(ctx)
					return err
				},
			}
			for _, s := range steps {
				if err := s(); err != nil {
					return err
				}
			}
			return nil
		})
	}, func(ctx context.Context, db *bun.DB) error {
		for _, m := range []any{(*orgLogoV1)(nil), (*auditLogV1)(nil), (*recoveryCodesV1)(nil), (*accessTokensV1)(nil), (*sessionsV1)(nil), (*usersV1)(nil)} {
			if _, err := db.NewDropTable().Model(m).IfExists().Exec(ctx); err != nil {
				return err
			}
		}
		return nil
	})
}
