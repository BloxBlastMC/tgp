// Package runnerapi defines the protocol between tgp-server and tgp-runner.
//
// All endpoints live under /api/runner/v1 and take and return JSON.
// Registration uses a one-time token from an admin; every other call uses
// the runner token returned by registration as a Bearer token.
//
//	POST /register            RegisterRequest  → RegisterResponse
//	POST /fetch               FetchRequest     → 200 FetchResponse, or 204 when no job is ready (long poll, ~25s)
//	POST /heartbeat           HeartbeatRequest → HeartbeatResponse (every 10s)
//	POST /jobs/{id}/logs      LogRequest       → LogResponse
//	POST /jobs/{id}/update    UpdateRequest    → UpdateResponse
//	POST /jobs/{id}/complete  CompleteRequest  → 204
//
// Errors use the API's error shape: {"error": {"code", "message"}}. A 401
// means the runner was removed; a 404 or 409 on a job endpoint means the
// server no longer considers the job running on this runner, and the runner
// should abort it.
package runnerapi

import (
	"time"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/ci/workflow"
)

// Prefix is the URL path prefix of the runner API.
const Prefix = "/api/runner/v1"

// Token prefixes.
const (
	RegistrationTokenPrefix = "tgp_reg_"
	RunnerTokenPrefix       = "tgp_runner_"
	JobTokenPrefix          = "tgp_job_"
	// JobTokenUser is the git username paired with a job token.
	JobTokenUser = "tgp-job"
)

// HeartbeatInterval is how often runners report in.
const HeartbeatInterval = 10 * time.Second

// Load is the runner host's resource usage.
type Load struct {
	CPUPercent    float64 `json:"cpu_percent"`
	MemoryPercent float64 `json:"memory_percent"`
}

type RegisterRequest struct {
	Token    string   `json:"token"`
	Name     string   `json:"name"`
	Labels   []string `json:"labels"`
	Capacity int      `json:"capacity"`
	Version  string   `json:"version"`
	OS       string   `json:"os"`
	Arch     string   `json:"arch"`
}

type RegisterResponse struct {
	ID     int64    `json:"id"`
	Name   string   `json:"name"`
	Token  string   `json:"token"`
	Labels []string `json:"labels"`
}

// FetchRequest asks for a job. Running lists the jobs the runner is
// running now; the server only hands out a job if it has a free slot.
type FetchRequest struct {
	Capacity int      `json:"capacity"`
	Running  []int64  `json:"running"`
	Labels   []string `json:"labels"`
	Load     Load     `json:"load"`
	Version  string   `json:"version"`
}

type FetchResponse struct {
	Job *JobSpec `json:"job"`
}

type HeartbeatRequest struct {
	Capacity int     `json:"capacity"`
	Running  []int64 `json:"running"`
	Load     Load    `json:"load"`
}

type HeartbeatResponse struct {
	// Cancel lists jobs to stop (cancelled by a user, timed out, superseded).
	Cancel []int64 `json:"cancel"`
	// Unknown lists running jobs the server doesn't consider assigned to
	// this runner any more (e.g. failed as lost); abort them.
	Unknown []int64 `json:"unknown"`
}

// Service is a service container started next to the job container on the
// job's network, reachable by its name.
type Service struct {
	Name  string            `json:"name"`
	Image string            `json:"image"`
	Env   map[string]string `json:"env,omitempty"`
}

// Step is a workflow step with its number (1-based).
type Step struct {
	Number int `json:"number"`
	workflow.Step
}

// Resources are the container limits for the job.
type Resources struct {
	CPUs        float64 `json:"cpus"`
	MemoryBytes int64   `json:"memory_bytes"`
	Pids        int     `json:"pids"`
}

// GitSource says how to check out the repository.
type GitSource struct {
	URL      string `json:"url"`
	Username string `json:"username"`
	Token    string `json:"token"`
	Ref      string `json:"ref"`
	SHA      string `json:"sha"`
}

// JobSpec is everything a runner needs to run a job. String fields from the
// workflow (env, run, with, if, outputs) are unevaluated; the runner
// evaluates them with Contexts, secrets and step outputs as the job runs.
type JobSpec struct {
	ID        int64  `json:"id"`
	RunID     int64  `json:"run_id"`
	RunNumber int64  `json:"run_number"`
	Attempt   int    `json:"attempt"`
	Repo      string `json:"repo"`
	Workflow  string `json:"workflow"`
	JobKey    string `json:"job_key"`
	Name      string `json:"name"`

	Image        string            `json:"image"`
	ContainerEnv map[string]string `json:"container_env,omitempty"`
	Services     []Service         `json:"services,omitempty"`
	Steps        []Step            `json:"steps"`
	WorkflowEnv  map[string]string `json:"workflow_env,omitempty"`
	JobEnv       map[string]string `json:"job_env,omitempty"`
	Defaults     workflow.Defaults `json:"defaults"`
	Outputs      map[string]string `json:"outputs,omitempty"`

	TimeoutSeconds int       `json:"timeout_seconds"`
	Resources      Resources `json:"resources"`

	// Contexts holds github, matrix, needs, inputs, strategy and vars.
	Contexts map[string]any    `json:"contexts"`
	Secrets  map[string]string `json:"secrets,omitempty"`
	// Env are extra environment variables for every step (GITHUB_*, TGP_*, CI).
	Env map[string]string `json:"env"`

	Git         GitSource `json:"git"`
	MaxLogBytes int64     `json:"max_log_bytes"`
}

// Step numbers used in logs for lines outside workflow steps.
const (
	StepSetup    = 0
	StepComplete = -1
)

// LogLine is one line of job output.
type LogLine struct {
	Step int       `json:"step"`
	T    time.Time `json:"t"`
	Text string    `json:"text"`
}

// LogRequest appends lines. First is the number of the first line (from
// 1); the server ignores lines it already has, so retries are safe.
type LogRequest struct {
	First int64     `json:"first"`
	Lines []LogLine `json:"lines"`
}

type LogResponse struct {
	// Next is the line number the server expects next.
	Next int64 `json:"next"`
	// Truncated: the log hit the size limit; further lines are dropped.
	Truncated bool `json:"truncated"`
	// Cancel: stop the job.
	Cancel bool `json:"cancel"`
}

// Step statuses and conclusions.
const (
	StatusPending    = "pending"
	StatusInProgress = "in_progress"
	StatusCompleted  = "completed"

	ConclusionSuccess   = "success"
	ConclusionFailure   = "failure"
	ConclusionCancelled = "cancelled"
	ConclusionSkipped   = "skipped"
	ConclusionTimedOut  = "timed_out"
)

type StepUpdate struct {
	Number      int        `json:"number"`
	Status      string     `json:"status"`
	Conclusion  string     `json:"conclusion,omitempty"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

type UpdateRequest struct {
	Steps []StepUpdate `json:"steps"`
}

type UpdateResponse struct {
	Cancel bool `json:"cancel"`
}

// CompleteRequest finishes a job. Conclusion is success, failure, cancelled
// or timed_out.
type CompleteRequest struct {
	Conclusion    string            `json:"conclusion"`
	FailureReason string            `json:"failure_reason,omitempty"`
	Outputs       map[string]string `json:"outputs,omitempty"`
	Steps         []StepUpdate      `json:"steps,omitempty"`
}
