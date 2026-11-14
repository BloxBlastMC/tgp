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
	NextNumber    int64      `bun:"next_number,scanonly"` // advanced only by nextNumber
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
	CreatedAt             time.Time `bun:"created_at"`
	UpdatedAt             time.Time `bun:"updated_at"`
}
