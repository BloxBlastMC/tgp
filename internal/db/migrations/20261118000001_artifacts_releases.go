package migrations

import (
	"context"
	"time"

	"github.com/uptrace/bun"
)

type artifactsV1 struct {
	bun.BaseModel `bun:"table:artifacts"`

	ID        int64     `bun:"id,pk,autoincrement"`
	RepoID    int64     `bun:"repo_id,notnull"`
	RunID     int64     `bun:"run_id,notnull,unique:artifacts_run_attempt_name"`
	JobID     *int64    `bun:"job_id"`
	JobName   string    `bun:"job_name,notnull,default:''"`
	Attempt   int       `bun:"attempt,notnull,unique:artifacts_run_attempt_name"`
	Name      string    `bun:"name,notnull,unique:artifacts_run_attempt_name"`
	Size      int64     `bun:"size,notnull"`
	SHA256    string    `bun:"sha256,notnull"`
	BlobKey   string    `bun:"blob_key,notnull"`
	CreatedAt time.Time `bun:"created_at,notnull"`
	ExpiresAt time.Time `bun:"expires_at,notnull"`
}

type ciCachesV1 struct {
	bun.BaseModel `bun:"table:ci_caches"`

	ID         int64     `bun:"id,pk,autoincrement"`
	RepoID     int64     `bun:"repo_id,notnull,unique:ci_caches_scope_key"`
	Ref        string    `bun:"ref,notnull,unique:ci_caches_scope_key"`
	Key        string    `bun:"key,notnull,unique:ci_caches_scope_key"`
	Version    string    `bun:"version,notnull,unique:ci_caches_scope_key"`
	Size       int64     `bun:"size,notnull"`
	BlobKey    string    `bun:"blob_key,notnull"`
	CreatedAt  time.Time `bun:"created_at,notnull"`
	LastUsedAt time.Time `bun:"last_used_at,notnull"`
}

type releasesV1 struct {
	bun.BaseModel `bun:"table:releases"`

	ID          int64      `bun:"id,pk,autoincrement"`
	RepoID      int64      `bun:"repo_id,notnull,unique:releases_repo_tag"`
	Tag         string     `bun:"tag,notnull,unique:releases_repo_tag"`
	Target      string     `bun:"target,notnull,default:''"`
	Name        string     `bun:"name,notnull,default:''"`
	Body        string     `bun:"body,notnull,default:''"`
	Draft       bool       `bun:"draft,notnull,default:false"`
	Prerelease  bool       `bun:"prerelease,notnull,default:false"`
	AuthorID    *int64     `bun:"author_id"`
	CreatedAt   time.Time  `bun:"created_at,notnull"`
	UpdatedAt   time.Time  `bun:"updated_at,notnull"`
	PublishedAt *time.Time `bun:"published_at"`
}

type releaseAssetsV1 struct {
	bun.BaseModel `bun:"table:release_assets"`

	ID          int64     `bun:"id,pk,autoincrement"`
	ReleaseID   int64     `bun:"release_id,notnull,unique:release_assets_release_name"`
	RepoID      int64     `bun:"repo_id,notnull"`
	Name        string    `bun:"name,notnull,unique:release_assets_release_name"`
	Label       string    `bun:"label,notnull,default:''"`
	ContentType string    `bun:"content_type,notnull"`
	Size        int64     `bun:"size,notnull"`
	SHA256      string    `bun:"sha256,notnull"`
	BlobKey     string    `bun:"blob_key,notnull"`
	Downloads   int64     `bun:"downloads,notnull,default:0"`
	UploaderID  *int64    `bun:"uploader_id"`
	CreatedAt   time.Time `bun:"created_at,notnull"`
}

func init() {
	Migrations.MustRegister(func(ctx context.Context, db *bun.DB) error {
		return db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
			fk := func(col, table, onDelete string) string {
				return `("` + col + `") REFERENCES "` + table + `" ("id") ON DELETE ` + onDelete
			}
			for _, q := range []*bun.CreateTableQuery{
				tx.NewCreateTable().Model((*artifactsV1)(nil)).ForeignKey(fk("repo_id", "repos", "CASCADE")).
					ForeignKey(fk("run_id", "workflow_runs", "CASCADE")).ForeignKey(fk("job_id", "jobs", "SET NULL")),
				tx.NewCreateTable().Model((*ciCachesV1)(nil)).ForeignKey(fk("repo_id", "repos", "CASCADE")),
				tx.NewCreateTable().Model((*releasesV1)(nil)).ForeignKey(fk("repo_id", "repos", "CASCADE")).
					ForeignKey(fk("author_id", "users", "SET NULL")),
				tx.NewCreateTable().Model((*releaseAssetsV1)(nil)).ForeignKey(fk("release_id", "releases", "CASCADE")).
					ForeignKey(fk("repo_id", "repos", "CASCADE")).ForeignKey(fk("uploader_id", "users", "SET NULL")),
			} {
				if _, err := q.Exec(ctx); err != nil {
					return err
				}
			}
			for _, ix := range []struct {
				model any
				name  string
				cols  []string
			}{
				{(*artifactsV1)(nil), "artifacts_repo_idx", []string{"repo_id", "created_at"}},
				{(*artifactsV1)(nil), "artifacts_expires_idx", []string{"expires_at"}},
				{(*ciCachesV1)(nil), "ci_caches_repo_used_idx", []string{"repo_id", "last_used_at"}},
				{(*ciCachesV1)(nil), "ci_caches_used_idx", []string{"last_used_at"}},
				{(*releasesV1)(nil), "releases_repo_created_idx", []string{"repo_id", "created_at"}},
			} {
				if _, err := tx.NewCreateIndex().Model(ix.model).Index(ix.name).Column(ix.cols...).Exec(ctx); err != nil {
					return err
				}
			}
			return nil
		})
	}, func(ctx context.Context, db *bun.DB) error {
		for _, m := range []any{(*releaseAssetsV1)(nil), (*releasesV1)(nil), (*ciCachesV1)(nil), (*artifactsV1)(nil)} {
			if _, err := db.NewDropTable().Model(m).IfExists().Exec(ctx); err != nil {
				return err
			}
		}
		return nil
	})
}
