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
	NextNumber    int64      `bun:"next_number,scanonly"`     // advanced only by nextNumber
	NextRunNumber int64      `bun:"next_run_number,scanonly"` // advanced only by nextRunNumber
}

type RepoGrant struct {
	bun.BaseModel `bun:"table:repo_grants"`

	ID        int64     `bun:"id,pk,autoincrement"`
	RepoID    int64     `bun:"repo_id"`
	UserID    int64     `bun:"user_id"`
	CreatedAt time.Time `bun:"created_at"`
}

// Issue is an issue or (with IsPull) the issue half of a pull request.
type Issue struct {
	bun.BaseModel `bun:"table:issues,alias:i"`

	ID           int64      `bun:"id,pk,autoincrement"`
	RepoID       int64      `bun:"repo_id"`
	Number       int64      `bun:"number"`
	IsPull       bool       `bun:"is_pull"`
	Title        string     `bun:"title"`
	Body         string     `bun:"body"`
	AuthorID     int64      `bun:"author_id"`
	State        string     `bun:"state"`
	ClosedAt     *time.Time `bun:"closed_at"`
	ClosedByID   *int64     `bun:"closed_by_id"`
	CommentCount int        `bun:"comment_count"`
	MilestoneID  *int64     `bun:"milestone_id"`
	StateReason  string     `bun:"state_reason"` // completed, not_planned (closed issues)
	CreatedAt    time.Time  `bun:"created_at"`
	UpdatedAt    time.Time  `bun:"updated_at"`
}

// Issue states.
const (
	StateOpen   = "open"
	StateClosed = "closed"
)

// PullRequest holds the pull-request-specific fields of an issue.
type PullRequest struct {
	bun.BaseModel `bun:"table:pull_requests,alias:pr"`

	IssueID          int64      `bun:"issue_id,pk"`
	RepoID           int64      `bun:"repo_id"`
	HeadBranch       string     `bun:"head_branch"`
	BaseBranch       string     `bun:"base_branch"`
	HeadSHA          string     `bun:"head_sha"`
	HeadDeleted      bool       `bun:"head_deleted"`
	Draft            bool       `bun:"draft"`
	MergedAt         *time.Time `bun:"merged_at"`
	MergedByID       *int64     `bun:"merged_by_id"`
	MergeCommitSHA   string     `bun:"merge_commit_sha"`
	MergeMethod      string     `bun:"merge_method"`
	MergeBaseSHA     string     `bun:"merge_base_sha"`
	Mergeable        string     `bun:"mergeable"`
	MergeableBaseSHA string     `bun:"mergeable_base_sha"`
	MergeableHeadSHA string     `bun:"mergeable_head_sha"`
	ConflictFiles    string     `bun:"conflict_files"`
}

type ReviewRequest struct {
	bun.BaseModel `bun:"table:review_requests"`

	ID        int64     `bun:"id,pk,autoincrement"`
	IssueID   int64     `bun:"issue_id"`
	UserID    int64     `bun:"user_id"`
	CreatedAt time.Time `bun:"created_at"`
}

// Review states.
const (
	ReviewApproved         = "approved"
	ReviewChangesRequested = "changes_requested"
	ReviewCommented        = "commented"
)

type Review struct {
	bun.BaseModel `bun:"table:reviews"`

	ID             int64      `bun:"id,pk,autoincrement"`
	IssueID        int64      `bun:"issue_id"`
	AuthorID       int64      `bun:"author_id"`
	State          string     `bun:"state"`
	Body           string     `bun:"body"`
	CommitSHA      string     `bun:"commit_sha"`
	Dismissed      bool       `bun:"dismissed"`
	DismissedByID  *int64     `bun:"dismissed_by_id"`
	DismissMessage string     `bun:"dismiss_message"`
	DismissedAt    *time.Time `bun:"dismissed_at"`
	CreatedAt      time.Time  `bun:"created_at"`
}

// Comment is a conversation comment or (with Path) an inline code comment.
type Comment struct {
	bun.BaseModel `bun:"table:comments"`

	ID           int64      `bun:"id,pk,autoincrement"`
	IssueID      int64      `bun:"issue_id"`
	AuthorID     int64      `bun:"author_id"`
	Body         string     `bun:"body"`
	ReviewID     *int64     `bun:"review_id"`
	ReplyToID    *int64     `bun:"reply_to_id"`
	Path         string     `bun:"path"`
	Line         int        `bun:"line"`
	Side         string     `bun:"side"`
	CommitSHA    string     `bun:"commit_sha"`
	DiffHunk     string     `bun:"diff_hunk"`
	Resolved     bool       `bun:"resolved"`
	ResolvedByID *int64     `bun:"resolved_by_id"`
	Edited       bool       `bun:"edited"`
	CreatedAt    time.Time  `bun:"created_at"`
	UpdatedAt    time.Time  `bun:"updated_at"`
	DeletedAt    *time.Time `bun:"deleted_at"`
}

type IssueEvent struct {
	bun.BaseModel `bun:"table:issue_events"`

	ID        int64     `bun:"id,pk,autoincrement"`
	IssueID   int64     `bun:"issue_id"`
	ActorID   *int64    `bun:"actor_id"`
	Type      string    `bun:"type"`
	Data      string    `bun:"data"`
	CreatedAt time.Time `bun:"created_at"`
}

type BranchProtection struct {
	bun.BaseModel `bun:"table:branch_protections"`

	ID                    int64     `bun:"id,pk,autoincrement"`
	RepoID                int64     `bun:"repo_id"`
	Pattern               string    `bun:"pattern"`
	RequirePullRequest    bool      `bun:"require_pull_request"`
	RequiredApprovals     int       `bun:"required_approvals"`
	DismissStaleApprovals bool      `bun:"dismiss_stale_approvals"`
	RequireUpToDate       bool      `bun:"require_up_to_date"`
	BlockForcePush        bool      `bun:"block_force_push"`
	AllowDeletion         bool      `bun:"allow_deletion"`
	AdminsBypass          bool      `bun:"admins_bypass"`
	RequiredChecks        string    `bun:"required_checks"` // JSON array of check names
	CreatedAt             time.Time `bun:"created_at"`
	UpdatedAt             time.Time `bun:"updated_at"`
}

type Label struct {
	bun.BaseModel `bun:"table:labels,alias:l"`

	ID          int64     `bun:"id,pk,autoincrement"`
	RepoID      int64     `bun:"repo_id"`
	Name        string    `bun:"name"`
	LowerName   string    `bun:"lower_name"`
	Color       string    `bun:"color"`
	Description string    `bun:"description"`
	CreatedAt   time.Time `bun:"created_at"`
}

type IssueLabel struct {
	bun.BaseModel `bun:"table:issue_labels"`

	IssueID int64 `bun:"issue_id,pk"`
	LabelID int64 `bun:"label_id,pk"`
}

type Milestone struct {
	bun.BaseModel `bun:"table:milestones,alias:m"`

	ID          int64      `bun:"id,pk,autoincrement"`
	RepoID      int64      `bun:"repo_id"`
	Title       string     `bun:"title"`
	LowerTitle  string     `bun:"lower_title"`
	Description string     `bun:"description"`
	DueOn       *time.Time `bun:"due_on"`
	State       string     `bun:"state"`
	ClosedAt    *time.Time `bun:"closed_at"`
	CreatedAt   time.Time  `bun:"created_at"`
	UpdatedAt   time.Time  `bun:"updated_at"`
}

type IssueAssignee struct {
	bun.BaseModel `bun:"table:issue_assignees"`

	IssueID int64 `bun:"issue_id,pk"`
	UserID  int64 `bun:"user_id,pk"`
}

// ---- CI ----

// Run statuses.
const (
	RunWaiting    = "waiting"
	RunQueued     = "queued"
	RunInProgress = "in_progress"
	RunCompleted  = "completed"
)

// Job statuses (conclusions are shared with runs and steps).
const (
	JobPending    = "pending"
	JobQueued     = "queued"
	JobInProgress = "in_progress"
	JobCompleted  = "completed"

	ConclusionSuccess   = "success"
	ConclusionFailure   = "failure"
	ConclusionCancelled = "cancelled"
	ConclusionSkipped   = "skipped"
	ConclusionTimedOut  = "timed_out"
)

type Runner struct {
	bun.BaseModel `bun:"table:runners,alias:rn"`

	ID            int64      `bun:"id,pk,autoincrement"`
	Name          string     `bun:"name"`
	Labels        string     `bun:"labels"` // JSON array
	TokenHash     string     `bun:"token_hash"`
	Capacity      int        `bun:"capacity"`
	Version       string     `bun:"version"`
	OS            string     `bun:"os"`
	Arch          string     `bun:"arch"`
	CPUPercent    *float64   `bun:"cpu_percent"`
	MemoryPercent *float64   `bun:"memory_percent"`
	Disabled      bool       `bun:"disabled"`
	LastSeenAt    *time.Time `bun:"last_seen_at"`
	CreatedByID   *int64     `bun:"created_by_id"`
	CreatedAt     time.Time  `bun:"created_at"`
}

type RunnerToken struct {
	bun.BaseModel `bun:"table:runner_tokens"`

	ID          int64      `bun:"id,pk,autoincrement"`
	TokenHash   string     `bun:"token_hash"`
	CreatedByID *int64     `bun:"created_by_id"`
	CreatedAt   time.Time  `bun:"created_at"`
	ExpiresAt   time.Time  `bun:"expires_at"`
	UsedAt      *time.Time `bun:"used_at"`
	RunnerID    *int64     `bun:"runner_id"`
}

type WorkflowRun struct {
	bun.BaseModel `bun:"table:workflow_runs,alias:wr"`

	ID               int64      `bun:"id,pk,autoincrement"`
	RepoID           int64      `bun:"repo_id"`
	Number           int64      `bun:"number"`
	WorkflowPath     string     `bun:"workflow_path"`
	WorkflowName     string     `bun:"workflow_name"`
	WorkflowYAML     string     `bun:"workflow_yaml"`
	Event            string     `bun:"event"`
	Ref              string     `bun:"ref"`
	HeadBranch       string     `bun:"head_branch"`
	HeadSHA          string     `bun:"head_sha"`
	BeforeSHA        string     `bun:"before_sha"`
	PullNumber       *int64     `bun:"pull_number"`
	ActorID          *int64     `bun:"actor_id"`
	Title            string     `bun:"title"`
	Status           string     `bun:"status"`
	Conclusion       string     `bun:"conclusion"`
	StatusReason     string     `bun:"status_reason"`
	Attempt          int        `bun:"attempt"`
	ConcurrencyGroup string     `bun:"concurrency_group"`
	CancelInProgress bool       `bun:"cancel_in_progress"`
	CancelRequested  bool       `bun:"cancel_requested"`
	Inputs           string     `bun:"inputs"`         // JSON object
	GitHubContext    string     `bun:"github_context"` // JSON object
	CreatedAt        time.Time  `bun:"created_at"`
	UpdatedAt        time.Time  `bun:"updated_at"`
	StartedAt        *time.Time `bun:"started_at"`
	CompletedAt      *time.Time `bun:"completed_at"`
}

type Job struct {
	bun.BaseModel `bun:"table:jobs,alias:j"`

	ID              int64      `bun:"id,pk,autoincrement"`
	RunID           int64      `bun:"run_id"`
	RepoID          int64      `bun:"repo_id"`
	Attempt         int        `bun:"attempt"`
	Key             string     `bun:"key"`
	Name            string     `bun:"name"`
	CheckName       string     `bun:"check_name"`
	Matrix          string     `bun:"matrix"` // JSON object or ""
	Needs           string     `bun:"needs"`  // JSON array
	Labels          string     `bun:"labels"` // JSON array
	Image           string     `bun:"image"`
	CPUs            float64    `bun:"cpus"`
	MemoryBytes     int64      `bun:"memory_bytes"`
	Pids            int        `bun:"pids"`
	TimeoutMinutes  int        `bun:"timeout_minutes"`
	MaxParallel     int        `bun:"max_parallel"`
	Definition      string     `bun:"definition"` // JSON jobDef
	Status          string     `bun:"status"`
	Conclusion      string     `bun:"conclusion"`
	FailureReason   string     `bun:"failure_reason"`
	Outputs         string     `bun:"outputs"` // JSON object
	RunnerID        *int64     `bun:"runner_id"`
	TokenHash       *string    `bun:"token_hash"`
	CancelRequested bool       `bun:"cancel_requested"`
	ReusedFromID    *int64     `bun:"reused_from_id"`
	LogLines        int64      `bun:"log_lines"`
	LogBytes        int64      `bun:"log_bytes"`
	LogTruncated    bool       `bun:"log_truncated"`
	LogsDeleted     bool       `bun:"logs_deleted"`
	CreatedAt       time.Time  `bun:"created_at"`
	QueuedAt        *time.Time `bun:"queued_at"`
	StartedAt       *time.Time `bun:"started_at"`
	CompletedAt     *time.Time `bun:"completed_at"`
	HeartbeatAt     *time.Time `bun:"heartbeat_at"`
	CancelledAt     *time.Time `bun:"cancelled_at"`
}

type JobStep struct {
	bun.BaseModel `bun:"table:job_steps,alias:js"`

	ID          int64      `bun:"id,pk,autoincrement"`
	JobID       int64      `bun:"job_id"`
	Number      int        `bun:"number"`
	Name        string     `bun:"name"`
	Status      string     `bun:"status"`
	Conclusion  string     `bun:"conclusion"`
	StartedAt   *time.Time `bun:"started_at"`
	CompletedAt *time.Time `bun:"completed_at"`
}

type JobLogChunk struct {
	bun.BaseModel `bun:"table:job_log_chunks,alias:lc"`

	JobID     int64     `bun:"job_id,pk"`
	Seq       int       `bun:"seq,pk"`
	FirstLine int64     `bun:"first_line"`
	LineCount int       `bun:"line_count"`
	Bytes     int64     `bun:"bytes"`
	CreatedAt time.Time `bun:"created_at"`
}

type Secret struct {
	bun.BaseModel `bun:"table:secrets,alias:sec"`

	ID          int64     `bun:"id,pk,autoincrement"`
	RepoID      *int64    `bun:"repo_id"`
	Name        string    `bun:"name"`
	Value       string    `bun:"value"` // sealed with the master key
	CreatedAt   time.Time `bun:"created_at"`
	UpdatedAt   time.Time `bun:"updated_at"`
	UpdatedByID *int64    `bun:"updated_by_id"`
}

// Artifact is a zip file uploaded by a job (upload-artifact).
type Artifact struct {
	bun.BaseModel `bun:"table:artifacts,alias:art"`

	ID        int64     `bun:"id,pk,autoincrement"`
	RepoID    int64     `bun:"repo_id"`
	RunID     int64     `bun:"run_id"`
	JobID     *int64    `bun:"job_id"`
	JobName   string    `bun:"job_name"`
	Attempt   int       `bun:"attempt"`
	Name      string    `bun:"name"`
	Size      int64     `bun:"size"`
	SHA256    string    `bun:"sha256"`
	BlobKey   string    `bun:"blob_key"`
	CreatedAt time.Time `bun:"created_at"`
	ExpiresAt time.Time `bun:"expires_at"`
}

// CacheEntry is a CI cache archive (the cache built-in step).
type CacheEntry struct {
	bun.BaseModel `bun:"table:ci_caches,alias:cc"`

	ID         int64     `bun:"id,pk,autoincrement"`
	RepoID     int64     `bun:"repo_id"`
	Ref        string    `bun:"ref"`
	Key        string    `bun:"key"`
	Version    string    `bun:"version"`
	Size       int64     `bun:"size"`
	BlobKey    string    `bun:"blob_key"`
	CreatedAt  time.Time `bun:"created_at"`
	LastUsedAt time.Time `bun:"last_used_at"`
}

// Release is a named, tagged version with notes and files.
type Release struct {
	bun.BaseModel `bun:"table:releases,alias:rel"`

	ID          int64      `bun:"id,pk,autoincrement"`
	RepoID      int64      `bun:"repo_id"`
	Tag         string     `bun:"tag"`
	Target      string     `bun:"target"`
	Name        string     `bun:"name"`
	Body        string     `bun:"body"`
	Draft       bool       `bun:"draft"`
	Prerelease  bool       `bun:"prerelease"`
	AuthorID    *int64     `bun:"author_id"`
	CreatedAt   time.Time  `bun:"created_at"`
	UpdatedAt   time.Time  `bun:"updated_at"`
	PublishedAt *time.Time `bun:"published_at"`
}

// ReleaseAsset is a file attached to a release.
type ReleaseAsset struct {
	bun.BaseModel `bun:"table:release_assets,alias:ra"`

	ID          int64     `bun:"id,pk,autoincrement"`
	ReleaseID   int64     `bun:"release_id"`
	RepoID      int64     `bun:"repo_id"`
	Name        string    `bun:"name"`
	Label       string    `bun:"label"`
	ContentType string    `bun:"content_type"`
	Size        int64     `bun:"size"`
	SHA256      string    `bun:"sha256"`
	BlobKey     string    `bun:"blob_key"`
	Downloads   int64     `bun:"downloads"`
	UploaderID  *int64    `bun:"uploader_id"`
	CreatedAt   time.Time `bun:"created_at"`
}

// Webhook sends events of one repository (RepoID set) or of every
// repository (organisation webhook) to a URL.
type Webhook struct {
	bun.BaseModel `bun:"table:webhooks,alias:wh"`

	ID          int64     `bun:"id,pk,autoincrement"`
	RepoID      *int64    `bun:"repo_id"`
	URL         string    `bun:"url"`
	Secret      string    `bun:"secret"` // sealed with the master key; "" = unsigned
	Events      string    `bun:"events"` // JSON list; ["*"] = all
	Active      bool      `bun:"active"`
	InsecureSSL bool      `bun:"insecure_ssl"`
	CreatedByID *int64    `bun:"created_by_id"`
	CreatedAt   time.Time `bun:"created_at"`
	UpdatedAt   time.Time `bun:"updated_at"`
}

// Webhook delivery statuses.
const (
	DeliveryPending = "pending"
	DeliverySuccess = "success"
	DeliveryFailed  = "failed"
)

// WebhookDelivery is one event sent (or to be sent) to a webhook.
type WebhookDelivery struct {
	bun.BaseModel `bun:"table:webhook_deliveries,alias:wd"`

	ID              int64      `bun:"id,pk,autoincrement"`
	WebhookID       int64      `bun:"webhook_id"`
	GUID            string     `bun:"guid"`
	Event           string     `bun:"event"`
	Action          string     `bun:"action"`
	Payload         string     `bun:"payload"`
	Redelivery      bool       `bun:"redelivery"`
	Status          string     `bun:"status"`
	Attempts        int        `bun:"attempts"`
	NextAttemptAt   *time.Time `bun:"next_attempt_at"`
	ClaimedUntil    *time.Time `bun:"claimed_until"`
	RequestURL      string     `bun:"request_url"`
	RequestHeaders  string     `bun:"request_headers"` // JSON
	ResponseStatus  *int       `bun:"response_status"`
	ResponseHeaders string     `bun:"response_headers"` // JSON
	ResponseBody    string     `bun:"response_body"`
	ResponseTrunc   bool       `bun:"response_truncated"`
	DurationMS      *int       `bun:"duration_ms"`
	Error           string     `bun:"error"`
	CreatedAt       time.Time  `bun:"created_at"`
	DeliveredAt     *time.Time `bun:"delivered_at"`
}
