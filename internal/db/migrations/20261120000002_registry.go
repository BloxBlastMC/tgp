package migrations

import (
	"context"
	"time"

	"github.com/uptrace/bun"
)

// The container registry: content-addressed blobs shared by all
// repositories, images (repository + optional name), the blobs each image
// may serve, manifests with their references, tags and resumable uploads.

type registryBlobsV1 struct {
	bun.BaseModel `bun:"table:registry_blobs"`

	Digest    string    `bun:"digest,pk"`
	Size      int64     `bun:"size,notnull"`
	CreatedAt time.Time `bun:"created_at,notnull"`
}

type registryImagesV1 struct {
	bun.BaseModel `bun:"table:registry_images"`

	ID        int64     `bun:"id,pk,autoincrement"`
	RepoID    int64     `bun:"repo_id,notnull"`
	Name      string    `bun:"name,notnull,default:''"`
	CreatedAt time.Time `bun:"created_at,notnull"`
	UpdatedAt time.Time `bun:"updated_at,notnull"`
}

type registryImageBlobsV1 struct {
	bun.BaseModel `bun:"table:registry_image_blobs"`

	ImageID   int64     `bun:"image_id,pk"`
	Digest    string    `bun:"digest,pk"`
	CreatedAt time.Time `bun:"created_at,notnull"`
}

type registryManifestsV1 struct {
	bun.BaseModel `bun:"table:registry_manifests"`

	ID           int64     `bun:"id,pk,autoincrement"`
	ImageID      int64     `bun:"image_id,notnull"`
	Digest       string    `bun:"digest,notnull"`
	MediaType    string    `bun:"media_type,notnull"`
	ArtifactType string    `bun:"artifact_type,notnull,default:''"`
	Size         int64     `bun:"size,notnull"`
	ImageSize    int64     `bun:"image_size,notnull,default:0"`
	Platforms    string    `bun:"platforms,notnull,default:'[]'"`
	Content      []byte    `bun:"content,notnull"`
	PushedByID   *int64    `bun:"pushed_by_id"`
	PushedByJob  *int64    `bun:"pushed_by_job_id"`
	CreatedAt    time.Time `bun:"created_at,notnull"`
}

type registryManifestRefsV1 struct {
	bun.BaseModel `bun:"table:registry_manifest_refs"`

	ManifestID int64  `bun:"manifest_id,pk"`
	Digest     string `bun:"digest,pk"`
	Kind       string `bun:"kind,notnull"` // blob or manifest
}

type registryTagsV1 struct {
	bun.BaseModel `bun:"table:registry_tags"`

	ID         int64     `bun:"id,pk,autoincrement"`
	ImageID    int64     `bun:"image_id,notnull"`
	Name       string    `bun:"name,notnull"`
	ManifestID int64     `bun:"manifest_id,notnull"`
	CreatedAt  time.Time `bun:"created_at,notnull"`
	UpdatedAt  time.Time `bun:"updated_at,notnull"`
}

type registryUploadsV1 struct {
	bun.BaseModel `bun:"table:registry_uploads"`

	ID        string    `bun:"id,pk"`
	ImageID   int64     `bun:"image_id,notnull"`
	Size      int64     `bun:"size,notnull,default:0"`
	Parts     int       `bun:"parts,notnull,default:0"`
	HashState []byte    `bun:"hash_state"`
	CreatedAt time.Time `bun:"created_at,notnull"`
	UpdatedAt time.Time `bun:"updated_at,notnull"`
}

func init() {
	Migrations.MustRegister(func(ctx context.Context, db *bun.DB) error {
		return db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
			fk := func(col, table, onDelete string) string {
				return `("` + col + `") REFERENCES "` + table + `" ("id") ON DELETE ` + onDelete
			}
			tables := []struct {
				model any
				fks   []string
			}{
				{(*registryBlobsV1)(nil), nil},
				{(*registryImagesV1)(nil), []string{fk("repo_id", "repos", "CASCADE")}},
				{(*registryImageBlobsV1)(nil), []string{fk("image_id", "registry_images", "CASCADE")}},
				{(*registryManifestsV1)(nil), []string{fk("image_id", "registry_images", "CASCADE"), fk("pushed_by_id", "users", "SET NULL")}},
				{(*registryManifestRefsV1)(nil), []string{fk("manifest_id", "registry_manifests", "CASCADE")}},
				{(*registryTagsV1)(nil), []string{fk("image_id", "registry_images", "CASCADE"), fk("manifest_id", "registry_manifests", "CASCADE")}},
				{(*registryUploadsV1)(nil), []string{fk("image_id", "registry_images", "CASCADE")}},
			}
			for _, t := range tables {
				q := tx.NewCreateTable().Model(t.model)
				for _, f := range t.fks {
					q = q.ForeignKey(f)
				}
				if _, err := q.Exec(ctx); err != nil {
					return err
				}
			}
			for _, ix := range []struct {
				model  any
				name   string
				unique bool
				cols   []string
			}{
				{(*registryImagesV1)(nil), "registry_images_repo_name_idx", true, []string{"repo_id", "name"}},
				{(*registryImageBlobsV1)(nil), "registry_image_blobs_digest_idx", false, []string{"digest"}},
				{(*registryManifestsV1)(nil), "registry_manifests_image_digest_idx", true, []string{"image_id", "digest"}},
				{(*registryManifestRefsV1)(nil), "registry_manifest_refs_digest_idx", false, []string{"digest"}},
				{(*registryTagsV1)(nil), "registry_tags_image_name_idx", true, []string{"image_id", "name"}},
				{(*registryTagsV1)(nil), "registry_tags_manifest_idx", false, []string{"manifest_id"}},
				{(*registryUploadsV1)(nil), "registry_uploads_updated_idx", false, []string{"updated_at"}},
			} {
				q := tx.NewCreateIndex().Model(ix.model).Index(ix.name).Column(ix.cols...)
				if ix.unique {
					q = q.Unique()
				}
				if _, err := q.Exec(ctx); err != nil {
					return err
				}
			}
			return nil
		})
	}, func(ctx context.Context, db *bun.DB) error {
		for _, m := range []any{(*registryUploadsV1)(nil), (*registryTagsV1)(nil), (*registryManifestRefsV1)(nil),
			(*registryManifestsV1)(nil), (*registryImageBlobsV1)(nil), (*registryImagesV1)(nil), (*registryBlobsV1)(nil)} {
			if _, err := db.NewDropTable().Model(m).IfExists().Exec(ctx); err != nil {
				return err
			}
		}
		return nil
	})
}
