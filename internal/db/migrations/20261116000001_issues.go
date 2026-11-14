package migrations

import (
	"context"
	"strings"
	"time"

	"github.com/uptrace/bun"
)

type labelsV1 struct {
	bun.BaseModel `bun:"table:labels"`

	ID          int64     `bun:"id,pk,autoincrement"`
	RepoID      int64     `bun:"repo_id,notnull,unique:labels_repo_name"`
	Name        string    `bun:"name,notnull"`
	LowerName   string    `bun:"lower_name,notnull,unique:labels_repo_name"`
	Color       string    `bun:"color,notnull"`
	Description string    `bun:"description,notnull,default:''"`
	CreatedAt   time.Time `bun:"created_at,notnull"`
}

type issueLabelsV1 struct {
	bun.BaseModel `bun:"table:issue_labels"`

	IssueID int64 `bun:"issue_id,pk"`
	LabelID int64 `bun:"label_id,pk"`
}

type milestonesV1 struct {
	bun.BaseModel `bun:"table:milestones"`

	ID          int64      `bun:"id,pk,autoincrement"`
	RepoID      int64      `bun:"repo_id,notnull,unique:milestones_repo_title"`
	Title       string     `bun:"title,notnull"`
	LowerTitle  string     `bun:"lower_title,notnull,unique:milestones_repo_title"`
	Description string     `bun:"description,notnull,default:''"`
	DueOn       *time.Time `bun:"due_on"`
	State       string     `bun:"state,notnull"`
	ClosedAt    *time.Time `bun:"closed_at"`
	CreatedAt   time.Time  `bun:"created_at,notnull"`
	UpdatedAt   time.Time  `bun:"updated_at,notnull"`
}

type issueAssigneesV1 struct {
	bun.BaseModel `bun:"table:issue_assignees"`

	IssueID int64 `bun:"issue_id,pk"`
	UserID  int64 `bun:"user_id,pk"`
}

// DefaultLabels are created in every repository. Frozen here for the
// migration; internal/repos has the live copy.
var defaultLabelsV1 = [][3]string{
	{"bug", "d73a4a", "Something isn't working"},
	{"enhancement", "a2eeef", "New feature or request"},
	{"documentation", "0075ca", "Improvements or additions to documentation"},
	{"question", "d876e3", "Further information is requested"},
	{"needs testing", "fbca04", "Ready for a tester to verify"},
	{"duplicate", "cfd3d7", "This issue or pull request already exists"},
	{"wontfix", "ffffff", "This will not be worked on"},
}

func init() {
	Migrations.MustRegister(func(ctx context.Context, db *bun.DB) error {
		return db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
			fk := func(col, table, onDelete string) string {
				return `("` + col + `") REFERENCES "` + table + `" ("id") ON DELETE ` + onDelete
			}
			for _, q := range []*bun.CreateTableQuery{
				tx.NewCreateTable().Model((*labelsV1)(nil)).ForeignKey(fk("repo_id", "repos", "CASCADE")),
				tx.NewCreateTable().Model((*issueLabelsV1)(nil)).
					ForeignKey(fk("issue_id", "issues", "CASCADE")).ForeignKey(fk("label_id", "labels", "CASCADE")),
				tx.NewCreateTable().Model((*milestonesV1)(nil)).ForeignKey(fk("repo_id", "repos", "CASCADE")),
				tx.NewCreateTable().Model((*issueAssigneesV1)(nil)).
					ForeignKey(fk("issue_id", "issues", "CASCADE")).ForeignKey(fk("user_id", "users", "CASCADE")),
			} {
				if _, err := q.Exec(ctx); err != nil {
					return err
				}
			}
			// SQLite can't add a foreign key with ALTER TABLE; the handlers
			// clear milestone_id themselves when a milestone is deleted.
			for _, col := range []string{"milestone_id BIGINT", "state_reason VARCHAR NOT NULL DEFAULT ''"} {
				if _, err := tx.NewAddColumn().Table("issues").ColumnExpr(col).Exec(ctx); err != nil {
					return err
				}
			}
			for _, ix := range []struct {
				model any
				name  string
				cols  []string
			}{
				{(*issueLabelsV1)(nil), "issue_labels_label_idx", []string{"label_id"}},
				{(*issueAssigneesV1)(nil), "issue_assignees_user_idx", []string{"user_id"}},
			} {
				if _, err := tx.NewCreateIndex().Model(ix.model).Index(ix.name).Column(ix.cols...).Exec(ctx); err != nil {
					return err
				}
			}
			if _, err := tx.NewCreateIndex().Table("issues").Index("issues_milestone_idx").Column("milestone_id").Exec(ctx); err != nil {
				return err
			}
			var repoIDs []int64
			if err := tx.NewSelect().Table("repos").Column("id").Scan(ctx, &repoIDs); err != nil {
				return err
			}
			now := time.Now().UTC()
			for _, id := range repoIDs {
				for _, l := range defaultLabelsV1 {
					row := &labelsV1{RepoID: id, Name: l[0], LowerName: strings.ToLower(l[0]), Color: l[1], Description: l[2], CreatedAt: now}
					if _, err := tx.NewInsert().Model(row).Exec(ctx); err != nil {
						return err
					}
				}
			}
			return nil
		})
	}, func(ctx context.Context, db *bun.DB) error {
		for _, m := range []any{(*issueAssigneesV1)(nil), (*issueLabelsV1)(nil), (*milestonesV1)(nil), (*labelsV1)(nil)} {
			if _, err := db.NewDropTable().Model(m).IfExists().Exec(ctx); err != nil {
				return err
			}
		}
		if _, err := db.NewDropIndex().Index("issues_milestone_idx").IfExists().Exec(ctx); err != nil {
			return err
		}
		for _, c := range []string{"milestone_id", "state_reason"} {
			if _, err := db.NewDropColumn().Table("issues").Column(c).Exec(ctx); err != nil {
				return err
			}
		}
		return nil
	})
}
