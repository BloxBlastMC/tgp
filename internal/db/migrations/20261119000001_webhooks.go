package migrations

import (
	"context"
	"time"

	"github.com/uptrace/bun"
)

type webhooksV1 struct {
	bun.BaseModel `bun:"table:webhooks"`

	ID          int64     `bun:"id,pk,autoincrement"`
	RepoID      *int64    `bun:"repo_id"`
	URL         string    `bun:"url,notnull"`
	Secret      string    `bun:"secret,notnull,default:''"`
	Events      string    `bun:"events,notnull"`
	Active      bool      `bun:"active,notnull,default:true"`
	InsecureSSL bool      `bun:"insecure_ssl,notnull,default:false"`
	CreatedByID *int64    `bun:"created_by_id"`
	CreatedAt   time.Time `bun:"created_at,notnull"`
	UpdatedAt   time.Time `bun:"updated_at,notnull"`
}

type webhookDeliveriesV1 struct {
	bun.BaseModel `bun:"table:webhook_deliveries"`

	ID              int64      `bun:"id,pk,autoincrement"`
	WebhookID       int64      `bun:"webhook_id,notnull"`
	GUID            string     `bun:"guid,notnull,unique"`
	Event           string     `bun:"event,notnull"`
	Action          string     `bun:"action,notnull,default:''"`
	Payload         string     `bun:"payload,notnull"`
	Redelivery      bool       `bun:"redelivery,notnull,default:false"`
	Status          string     `bun:"status,notnull"`
	Attempts        int        `bun:"attempts,notnull,default:0"`
	NextAttemptAt   *time.Time `bun:"next_attempt_at"`
	ClaimedUntil    *time.Time `bun:"claimed_until"`
	RequestURL      string     `bun:"request_url,notnull,default:''"`
	RequestHeaders  string     `bun:"request_headers,notnull,default:'{}'"`
	ResponseStatus  *int       `bun:"response_status"`
	ResponseHeaders string     `bun:"response_headers,notnull,default:'{}'"`
	ResponseBody    string     `bun:"response_body,notnull,default:''"`
	ResponseTrunc   bool       `bun:"response_truncated,notnull,default:false"`
	DurationMS      *int       `bun:"duration_ms"`
	Error           string     `bun:"error,notnull,default:''"`
	CreatedAt       time.Time  `bun:"created_at,notnull"`
	DeliveredAt     *time.Time `bun:"delivered_at"`
}

func init() {
	Migrations.MustRegister(func(ctx context.Context, db *bun.DB) error {
		return db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
			fk := func(col, table, onDelete string) string {
				return `("` + col + `") REFERENCES "` + table + `" ("id") ON DELETE ` + onDelete
			}
			if _, err := tx.NewCreateTable().Model((*webhooksV1)(nil)).ForeignKey(fk("repo_id", "repos", "CASCADE")).
				ForeignKey(fk("created_by_id", "users", "SET NULL")).Exec(ctx); err != nil {
				return err
			}
			if _, err := tx.NewCreateTable().Model((*webhookDeliveriesV1)(nil)).ForeignKey(fk("webhook_id", "webhooks", "CASCADE")).Exec(ctx); err != nil {
				return err
			}
			for _, ix := range []struct {
				model any
				name  string
				cols  []string
			}{
				{(*webhooksV1)(nil), "webhooks_repo_idx", []string{"repo_id"}},
				{(*webhookDeliveriesV1)(nil), "webhook_deliveries_hook_idx", []string{"webhook_id", "id"}},
				{(*webhookDeliveriesV1)(nil), "webhook_deliveries_due_idx", []string{"status", "next_attempt_at"}},
				{(*webhookDeliveriesV1)(nil), "webhook_deliveries_created_idx", []string{"created_at"}},
			} {
				if _, err := tx.NewCreateIndex().Model(ix.model).Index(ix.name).Column(ix.cols...).Exec(ctx); err != nil {
					return err
				}
			}
			return nil
		})
	}, func(ctx context.Context, db *bun.DB) error {
		for _, m := range []any{(*webhookDeliveriesV1)(nil), (*webhooksV1)(nil)} {
			if _, err := db.NewDropTable().Model(m).IfExists().Exec(ctx); err != nil {
				return err
			}
		}
		return nil
	})
}
