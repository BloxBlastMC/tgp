package migrations

import (
	"context"
	"time"

	"github.com/uptrace/bun"
)

type runnersV1 struct {
	bun.BaseModel `bun:"table:runners"`

	ID            int64      `bun:"id,pk,autoincrement"`
	Name          string     `bun:"name,notnull"`
	Labels        string     `bun:"labels,notnull,default:'[]'"`
	TokenHash     string     `bun:"token_hash,notnull,unique"`
	Capacity      int        `bun:"capacity,notnull,default:1"`
	Version       string     `bun:"version,notnull,default:''"`
	OS            string     `bun:"os,notnull,default:''"`
	Arch          string     `bun:"arch,notnull,default:''"`
	CPUPercent    *float64   `bun:"cpu_percent"`
	MemoryPercent *float64   `bun:"memory_percent"`
	Disabled      bool       `bun:"disabled,notnull,default:false"`
	LastSeenAt    *time.Time `bun:"last_seen_at"`
	CreatedByID   *int64     `bun:"created_by_id"`
	CreatedAt     time.Time  `bun:"created_at,notnull"`
}

type runnerTokensV1 struct {
	bun.BaseModel `bun:"table:runner_tokens"`

	ID          int64      `bun:"id,pk,autoincrement"`
	TokenHash   string     `bun:"token_hash,notnull,unique"`
	CreatedByID *int64     `bun:"created_by_id"`
	CreatedAt   time.Time  `bun:"created_at,notnull"`
	ExpiresAt   time.Time  `bun:"expires_at,notnull"`
	UsedAt      *time.Time `bun:"used_at"`
	RunnerID    *int64     `bun:"runner_id"`
}

type workflowRunsV1 struct {
	bun.BaseModel `bun:"table:workflow_runs"`

	ID               int64      `bun:"id,pk,autoincrement"`
	RepoID           int64      `bun:"repo_id,notnull,unique:workflow_runs_repo_number"`
	Number           int64      `bun:"number,notnull,unique:workflow_runs_repo_number"`
	WorkflowPath     string     `bun:"workflow_path,notnull"`
	WorkflowName     string     `bun:"workflow_name,notnull"`
	WorkflowYAML     string     `bun:"workflow_yaml,notnull,default:''"`
	Event            string     `bun:"event,notnull"`
	Ref              string     `bun:"ref,notnull"`
	HeadBranch       string     `bun:"head_branch,notnull,default:''"`
	HeadSHA          string     `bun:"head_sha,notnull"`
	BeforeSHA        string     `bun:"before_sha,notnull,default:''"`
	PullNumber       *int64     `bun:"pull_number"`
	ActorID          *int64     `bun:"actor_id"`
	Title            string     `bun:"title,notnull,default:''"`
	Status           string     `bun:"status,notnull"`
	Conclusion       string     `bun:"conclusion,notnull,default:''"`
	StatusReason     string     `bun:"status_reason,notnull,default:''"`
	Attempt          int        `bun:"attempt,notnull,default:1"`
	ConcurrencyGroup string     `bun:"concurrency_group,notnull,default:''"`
	CancelInProgress bool       `bun:"cancel_in_progress,notnull,default:false"`
	CancelRequested  bool       `bun:"cancel_requested,notnull,default:false"`
	Inputs           string     `bun:"inputs,notnull,default:'{}'"`
	GitHubContext    string     `bun:"github_context,notnull,default:'{}'"`
	CreatedAt        time.Time  `bun:"created_at,notnull"`
	UpdatedAt        time.Time  `bun:"updated_at,notnull"`
	StartedAt        *time.Time `bun:"started_at"`
	CompletedAt      *time.Time `bun:"completed_at"`
}

type jobsV1 struct {
	bun.BaseModel `bun:"table:jobs"`

	ID              int64      `bun:"id,pk,autoincrement"`
	RunID           int64      `bun:"run_id,notnull"`
	RepoID          int64      `bun:"repo_id,notnull"`
	Attempt         int        `bun:"attempt,notnull"`
	Key             string     `bun:"key,notnull"`
	Name            string     `bun:"name,notnull"`
	CheckName       string     `bun:"check_name,notnull"`
	Matrix          string     `bun:"matrix,notnull,default:''"`
	Needs           string     `bun:"needs,notnull,default:'[]'"`
	Labels          string     `bun:"labels,notnull,default:'[]'"`
	Image           string     `bun:"image,notnull"`
	CPUs            float64    `bun:"cpus,notnull"`
	MemoryBytes     int64      `bun:"memory_bytes,notnull"`
	Pids            int        `bun:"pids,notnull"`
	TimeoutMinutes  int        `bun:"timeout_minutes,notnull"`
	MaxParallel     int        `bun:"max_parallel,notnull,default:0"`
	Definition      string     `bun:"definition,notnull"`
	Status          string     `bun:"status,notnull"`
	Conclusion      string     `bun:"conclusion,notnull,default:''"`
	FailureReason   string     `bun:"failure_reason,notnull,default:''"`
	Outputs         string     `bun:"outputs,notnull,default:'{}'"`
	RunnerID        *int64     `bun:"runner_id"`
	TokenHash       *string    `bun:"token_hash,unique"`
	CancelRequested bool       `bun:"cancel_requested,notnull,default:false"`
	ReusedFromID    *int64     `bun:"reused_from_id"`
	LogLines        int64      `bun:"log_lines,notnull,default:0"`
	LogBytes        int64      `bun:"log_bytes,notnull,default:0"`
	LogTruncated    bool       `bun:"log_truncated,notnull,default:false"`
	LogsDeleted     bool       `bun:"logs_deleted,notnull,default:false"`
	CreatedAt       time.Time  `bun:"created_at,notnull"`
	QueuedAt        *time.Time `bun:"queued_at"`
	StartedAt       *time.Time `bun:"started_at"`
	CompletedAt     *time.Time `bun:"completed_at"`
	HeartbeatAt     *time.Time `bun:"heartbeat_at"`
	CancelledAt     *time.Time `bun:"cancelled_at"`
}

type jobStepsV1 struct {
	bun.BaseModel `bun:"table:job_steps"`

	ID          int64      `bun:"id,pk,autoincrement"`
	JobID       int64      `bun:"job_id,notnull,unique:job_steps_job_number"`
	Number      int        `bun:"number,notnull,unique:job_steps_job_number"`
	Name        string     `bun:"name,notnull"`
	Status      string     `bun:"status,notnull"`
	Conclusion  string     `bun:"conclusion,notnull,default:''"`
	StartedAt   *time.Time `bun:"started_at"`
	CompletedAt *time.Time `bun:"completed_at"`
}

type jobLogChunksV1 struct {
	bun.BaseModel `bun:"table:job_log_chunks"`

	JobID     int64     `bun:"job_id,pk"`
	Seq       int       `bun:"seq,pk"`
	FirstLine int64     `bun:"first_line,notnull"`
	LineCount int       `bun:"line_count,notnull"`
	Bytes     int64     `bun:"bytes,notnull"`
	CreatedAt time.Time `bun:"created_at,notnull"`
}

type secretsV1 struct {
	bun.BaseModel `bun:"table:secrets"`

	ID          int64     `bun:"id,pk,autoincrement"`
	RepoID      *int64    `bun:"repo_id"`
	Name        string    `bun:"name,notnull"`
	Value       string    `bun:"value,notnull"`
	CreatedAt   time.Time `bun:"created_at,notnull"`
	UpdatedAt   time.Time `bun:"updated_at,notnull"`
	UpdatedByID *int64    `bun:"updated_by_id"`
}

func init() {
	Migrations.MustRegister(func(ctx context.Context, db *bun.DB) error {
		return db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
			fk := func(col, table, onDelete string) string {
				return `("` + col + `") REFERENCES "` + table + `" ("id") ON DELETE ` + onDelete
			}
			for _, q := range []*bun.CreateTableQuery{
				tx.NewCreateTable().Model((*runnersV1)(nil)).ForeignKey(fk("created_by_id", "users", "SET NULL")),
				tx.NewCreateTable().Model((*runnerTokensV1)(nil)).
					ForeignKey(fk("created_by_id", "users", "SET NULL")).ForeignKey(fk("runner_id", "runners", "SET NULL")),
				tx.NewCreateTable().Model((*workflowRunsV1)(nil)).
					ForeignKey(fk("repo_id", "repos", "CASCADE")).ForeignKey(fk("actor_id", "users", "SET NULL")),
				tx.NewCreateTable().Model((*jobsV1)(nil)).
					ForeignKey(fk("run_id", "workflow_runs", "CASCADE")).ForeignKey(fk("repo_id", "repos", "CASCADE")).
					ForeignKey(fk("runner_id", "runners", "SET NULL")).ForeignKey(fk("reused_from_id", "jobs", "SET NULL")),
				tx.NewCreateTable().Model((*jobStepsV1)(nil)).ForeignKey(fk("job_id", "jobs", "CASCADE")),
				tx.NewCreateTable().Model((*jobLogChunksV1)(nil)).ForeignKey(fk("job_id", "jobs", "CASCADE")),
				tx.NewCreateTable().Model((*secretsV1)(nil)).
					ForeignKey(fk("repo_id", "repos", "CASCADE")).ForeignKey(fk("updated_by_id", "users", "SET NULL")),
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
				{(*workflowRunsV1)(nil), "workflow_runs_repo_status_idx", []string{"repo_id", "status"}},
				{(*workflowRunsV1)(nil), "workflow_runs_repo_sha_idx", []string{"repo_id", "head_sha"}},
				{(*workflowRunsV1)(nil), "workflow_runs_status_idx", []string{"status"}},
				{(*jobsV1)(nil), "jobs_run_idx", []string{"run_id", "attempt"}},
				{(*jobsV1)(nil), "jobs_status_idx", []string{"status", "queued_at"}},
				{(*jobsV1)(nil), "jobs_runner_idx", []string{"runner_id", "status"}},
				{(*jobsV1)(nil), "jobs_completed_idx", []string{"completed_at"}},
			} {
				if _, err := tx.NewCreateIndex().Model(ix.model).Index(ix.name).Column(ix.cols...).Exec(ctx); err != nil {
					return err
				}
			}
			// One secret per name per scope; org secrets have no repo.
			if _, err := tx.NewCreateIndex().Model((*secretsV1)(nil)).Index("secrets_scope_name_idx").Unique().
				ColumnExpr("COALESCE(repo_id, 0)").Column("name").Exec(ctx); err != nil {
				return err
			}
			for _, c := range []struct{ table, col string }{
				{"repos", "next_run_number BIGINT NOT NULL DEFAULT 1"},
				{"branch_protections", "required_checks VARCHAR NOT NULL DEFAULT '[]'"},
			} {
				if _, err := tx.NewAddColumn().Table(c.table).ColumnExpr(c.col).Exec(ctx); err != nil {
					return err
				}
			}
			return nil
		})
	}, func(ctx context.Context, db *bun.DB) error {
		for _, m := range []any{(*secretsV1)(nil), (*jobLogChunksV1)(nil), (*jobStepsV1)(nil), (*jobsV1)(nil),
			(*workflowRunsV1)(nil), (*runnerTokensV1)(nil), (*runnersV1)(nil)} {
			if _, err := db.NewDropTable().Model(m).IfExists().Exec(ctx); err != nil {
				return err
			}
		}
		for _, c := range []struct{ table, col string }{{"repos", "next_run_number"}, {"branch_protections", "required_checks"}} {
			if _, err := db.NewDropColumn().Table(c.table).Column(c.col).Exec(ctx); err != nil {
				return err
			}
		}
		return nil
	})
}
