package migrations

import (
	"context"
	"time"

	"github.com/uptrace/bun"
)

// Issues and pull requests share one table and one number sequence per
// repository (like GitHub): a pull request is an issue with a
// pull_requests row. Milestone 6 adds labels and milestones on top.

type issuesV1 struct {
	bun.BaseModel `bun:"table:issues"`

	ID           int64      `bun:"id,pk,autoincrement"`
	RepoID       int64      `bun:"repo_id,notnull,unique:issues_repo_number"`
	Number       int64      `bun:"number,notnull,unique:issues_repo_number"`
	IsPull       bool       `bun:"is_pull,notnull"`
	Title        string     `bun:"title,notnull"`
	Body         string     `bun:"body,notnull,default:''"`
	AuthorID     int64      `bun:"author_id,notnull"`
	State        string     `bun:"state,notnull"` // open, closed
	ClosedAt     *time.Time `bun:"closed_at"`
	ClosedByID   *int64     `bun:"closed_by_id"`
	CommentCount int        `bun:"comment_count,notnull,default:0"`
	CreatedAt    time.Time  `bun:"created_at,notnull"`
	UpdatedAt    time.Time  `bun:"updated_at,notnull"`
}

type pullRequestsV1 struct {
	bun.BaseModel `bun:"table:pull_requests"`

	IssueID          int64      `bun:"issue_id,pk"`
	RepoID           int64      `bun:"repo_id,notnull"`
	HeadBranch       string     `bun:"head_branch,notnull"`
	BaseBranch       string     `bun:"base_branch,notnull"`
	HeadSHA          string     `bun:"head_sha,notnull"`
	HeadDeleted      bool       `bun:"head_deleted,notnull,default:false"`
	Draft            bool       `bun:"draft,notnull,default:false"`
	MergedAt         *time.Time `bun:"merged_at"`
	MergedByID       *int64     `bun:"merged_by_id"`
	MergeCommitSHA   string     `bun:"merge_commit_sha,notnull,default:''"`
	MergeMethod      string     `bun:"merge_method,notnull,default:''"`
	MergeBaseSHA     string     `bun:"merge_base_sha,notnull,default:''"` // frozen when merged
	Mergeable        string     `bun:"mergeable,notnull,default:'unknown'"`
	MergeableBaseSHA string     `bun:"mergeable_base_sha,notnull,default:''"`
	MergeableHeadSHA string     `bun:"mergeable_head_sha,notnull,default:''"`
	ConflictFiles    string     `bun:"conflict_files,notnull,default:''"` // JSON array
}

type reviewRequestsV1 struct {
	bun.BaseModel `bun:"table:review_requests"`

	ID        int64     `bun:"id,pk,autoincrement"`
	IssueID   int64     `bun:"issue_id,notnull,unique:review_requests_issue_user"`
	UserID    int64     `bun:"user_id,notnull,unique:review_requests_issue_user"`
	CreatedAt time.Time `bun:"created_at,notnull"`
}

type reviewsV1 struct {
	bun.BaseModel `bun:"table:reviews"`

	ID             int64      `bun:"id,pk,autoincrement"`
	IssueID        int64      `bun:"issue_id,notnull"`
	AuthorID       int64      `bun:"author_id,notnull"`
	State          string     `bun:"state,notnull"` // approved, changes_requested, commented
	Body           string     `bun:"body,notnull,default:''"`
	CommitSHA      string     `bun:"commit_sha,notnull"`
	Dismissed      bool       `bun:"dismissed,notnull,default:false"`
	DismissedByID  *int64     `bun:"dismissed_by_id"`
	DismissMessage string     `bun:"dismiss_message,notnull,default:''"`
	DismissedAt    *time.Time `bun:"dismissed_at"`
	CreatedAt      time.Time  `bun:"created_at,notnull"`
}

type commentsV1 struct {
	bun.BaseModel `bun:"table:comments"`

	ID           int64      `bun:"id,pk,autoincrement"`
	IssueID      int64      `bun:"issue_id,notnull"`
	AuthorID     int64      `bun:"author_id,notnull"`
	Body         string     `bun:"body,notnull"`
	ReviewID     *int64     `bun:"review_id"`
	ReplyToID    *int64     `bun:"reply_to_id"`
	Path         string     `bun:"path,notnull,default:''"`
	Line         int        `bun:"line,notnull,default:0"`
	Side         string     `bun:"side,notnull,default:''"` // new, old
	CommitSHA    string     `bun:"commit_sha,notnull,default:''"`
	DiffHunk     string     `bun:"diff_hunk,notnull,default:''"`
	Resolved     bool       `bun:"resolved,notnull,default:false"`
	ResolvedByID *int64     `bun:"resolved_by_id"`
	Edited       bool       `bun:"edited,notnull,default:false"`
	CreatedAt    time.Time  `bun:"created_at,notnull"`
	UpdatedAt    time.Time  `bun:"updated_at,notnull"`
	DeletedAt    *time.Time `bun:"deleted_at"`
}

type issueEventsV1 struct {
	bun.BaseModel `bun:"table:issue_events"`

	ID        int64     `bun:"id,pk,autoincrement"`
	IssueID   int64     `bun:"issue_id,notnull"`
	ActorID   *int64    `bun:"actor_id"`
	Type      string    `bun:"type,notnull"`
	Data      string    `bun:"data,notnull,default:'{}'"`
	CreatedAt time.Time `bun:"created_at,notnull"`
}

type branchProtectionsV1 struct {
	bun.BaseModel `bun:"table:branch_protections"`

	ID                    int64     `bun:"id,pk,autoincrement"`
	RepoID                int64     `bun:"repo_id,notnull,unique:branch_protections_repo_pattern"`
	Pattern               string    `bun:"pattern,notnull,unique:branch_protections_repo_pattern"`
	RequirePullRequest    bool      `bun:"require_pull_request,notnull"`
	RequiredApprovals     int       `bun:"required_approvals,notnull"`
	DismissStaleApprovals bool      `bun:"dismiss_stale_approvals,notnull"`
	RequireUpToDate       bool      `bun:"require_up_to_date,notnull"`
	BlockForcePush        bool      `bun:"block_force_push,notnull"`
	AllowDeletion         bool      `bun:"allow_deletion,notnull"`
	AdminsBypass          bool      `bun:"admins_bypass,notnull"`
	CreatedAt             time.Time `bun:"created_at,notnull"`
	UpdatedAt             time.Time `bun:"updated_at,notnull"`
}

func init() {
	Migrations.MustRegister(func(ctx context.Context, db *bun.DB) error {
		return db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
			if _, err := tx.NewAddColumn().Table("repos").ColumnExpr("next_number BIGINT NOT NULL DEFAULT 1").Exec(ctx); err != nil {
				return err
			}
			fk := func(col, table, onDelete string) string {
				return `("` + col + `") REFERENCES "` + table + `" ("id") ON DELETE ` + onDelete
			}
			steps := []*bun.CreateTableQuery{
				tx.NewCreateTable().Model((*issuesV1)(nil)).
					ForeignKey(fk("repo_id", "repos", "CASCADE")).
					ForeignKey(fk("author_id", "users", "RESTRICT")).
					ForeignKey(fk("closed_by_id", "users", "SET NULL")),
				tx.NewCreateTable().Model((*pullRequestsV1)(nil)).
					ForeignKey(fk("issue_id", "issues", "CASCADE")).
					ForeignKey(fk("repo_id", "repos", "CASCADE")).
					ForeignKey(fk("merged_by_id", "users", "SET NULL")),
				tx.NewCreateTable().Model((*reviewRequestsV1)(nil)).
					ForeignKey(fk("issue_id", "issues", "CASCADE")).
					ForeignKey(fk("user_id", "users", "CASCADE")),
				tx.NewCreateTable().Model((*reviewsV1)(nil)).
					ForeignKey(fk("issue_id", "issues", "CASCADE")).
					ForeignKey(fk("author_id", "users", "RESTRICT")).
					ForeignKey(fk("dismissed_by_id", "users", "SET NULL")),
				tx.NewCreateTable().Model((*commentsV1)(nil)).
					ForeignKey(fk("issue_id", "issues", "CASCADE")).
					ForeignKey(fk("author_id", "users", "RESTRICT")).
					ForeignKey(fk("review_id", "reviews", "CASCADE")).
					ForeignKey(fk("reply_to_id", "comments", "CASCADE")).
					ForeignKey(fk("resolved_by_id", "users", "SET NULL")),
				tx.NewCreateTable().Model((*issueEventsV1)(nil)).
					ForeignKey(fk("issue_id", "issues", "CASCADE")).
					ForeignKey(fk("actor_id", "users", "SET NULL")),
				tx.NewCreateTable().Model((*branchProtectionsV1)(nil)).
					ForeignKey(fk("repo_id", "repos", "CASCADE")),
			}
			for _, q := range steps {
				if _, err := q.Exec(ctx); err != nil {
					return err
				}
			}
			indexes := []struct {
				model any
				name  string
				cols  []string
			}{
				{(*issuesV1)(nil), "issues_repo_pull_state_idx", []string{"repo_id", "is_pull", "state"}},
				{(*issuesV1)(nil), "issues_author_idx", []string{"author_id"}},
				{(*pullRequestsV1)(nil), "pull_requests_repo_head_idx", []string{"repo_id", "head_branch"}},
				{(*pullRequestsV1)(nil), "pull_requests_repo_base_idx", []string{"repo_id", "base_branch"}},
				{(*reviewRequestsV1)(nil), "review_requests_user_idx", []string{"user_id"}},
				{(*reviewsV1)(nil), "reviews_issue_idx", []string{"issue_id"}},
				{(*commentsV1)(nil), "comments_issue_idx", []string{"issue_id"}},
				{(*issueEventsV1)(nil), "issue_events_issue_idx", []string{"issue_id"}},
			}
			for _, ix := range indexes {
				if _, err := tx.NewCreateIndex().Model(ix.model).Index(ix.name).Column(ix.cols...).Exec(ctx); err != nil {
					return err
				}
			}
			return nil
		})
	}, func(ctx context.Context, db *bun.DB) error {
		for _, m := range []any{(*branchProtectionsV1)(nil), (*issueEventsV1)(nil), (*commentsV1)(nil), (*reviewsV1)(nil),
			(*reviewRequestsV1)(nil), (*pullRequestsV1)(nil), (*issuesV1)(nil)} {
			if _, err := db.NewDropTable().Model(m).IfExists().Exec(ctx); err != nil {
				return err
			}
		}
		_, err := db.NewDropColumn().Table("repos").Column("next_number").Exec(ctx)
		return err
	})
}
