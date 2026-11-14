// Package scheduler decides which queued CI job a runner gets. It is pure
// logic over a snapshot of the queue and the running jobs, so the rules can
// be tested deterministically; the server takes the snapshot and applies
// the decision atomically.
//
// A job is assigned only when every limit allows it:
//   - the runner is enabled, below its slot capacity and not overloaded;
//   - fewer than MaxConcurrent jobs run across the instance;
//   - its repository runs fewer than MaxPerRepo jobs;
//   - its matrix group runs fewer than the strategy's max-parallel;
//   - the runner has every label the job asks for.
//
// Among eligible jobs, repositories take turns: the repository with the
// fewest running jobs goes first, ties go to the repository that started a
// job least recently, and within a repository the oldest queued job wins.
// One busy repository therefore can't starve the others.
package scheduler

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Limits are the instance-wide scheduling limits. Zero means unlimited.
type Limits struct {
	MaxConcurrent int
	MaxPerRepo    int
	// MaxCPUPercent: runners reporting more load get no new jobs.
	MaxCPUPercent float64
}

// Runner is a runner asking for work.
type Runner struct {
	ID         int64
	Labels     []string
	Capacity   int
	Busy       int
	CPUPercent float64
	Disabled   bool
	Online     bool
}

// Job is a queued job.
type Job struct {
	ID       int64
	RepoID   int64
	Labels   []string
	QueuedAt time.Time
	// Group identifies a matrix job's siblings (run + job key); GroupMax is
	// the strategy's max-parallel (0 = unlimited).
	Group    string
	GroupMax int
}

// State is the current load.
type State struct {
	Running      int
	RepoRunning  map[int64]int
	RepoLastRun  map[int64]time.Time
	GroupRunning map[string]int
}

// RunnerBlock says why a runner gets no job at all, or "".
func RunnerBlock(l Limits, r Runner, st State) string {
	switch {
	case r.Disabled:
		return "runner is disabled"
	case r.Capacity > 0 && r.Busy >= r.Capacity:
		return fmt.Sprintf("runner is full (%d/%d)", r.Busy, r.Capacity)
	case l.MaxCPUPercent > 0 && r.CPUPercent > l.MaxCPUPercent:
		return fmt.Sprintf("runner is overloaded (CPU %.0f%% > %.0f%%)", r.CPUPercent, l.MaxCPUPercent)
	case l.MaxConcurrent > 0 && st.Running >= l.MaxConcurrent:
		return fmt.Sprintf("instance limit reached (%d jobs running)", st.Running)
	}
	return ""
}

// HasLabels reports whether have contains every label in want
// (case-insensitive).
func HasLabels(have, want []string) bool {
	for _, w := range want {
		found := false
		for _, h := range have {
			if strings.EqualFold(h, w) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func eligible(l Limits, st State, j Job) bool {
	if l.MaxPerRepo > 0 && st.RepoRunning[j.RepoID] >= l.MaxPerRepo {
		return false
	}
	if j.GroupMax > 0 && st.GroupRunning[j.Group] >= j.GroupMax {
		return false
	}
	return true
}

// Pick returns the job runner r should run next, or ok=false.
func Pick(l Limits, r Runner, st State, queue []Job) (Job, bool) {
	if RunnerBlock(l, r, st) != "" {
		return Job{}, false
	}
	type repoBest struct {
		job     Job
		running int
		last    time.Time
	}
	best := map[int64]*repoBest{}
	for _, j := range queue {
		if !HasLabels(r.Labels, j.Labels) || !eligible(l, st, j) {
			continue
		}
		b := best[j.RepoID]
		if b == nil {
			best[j.RepoID] = &repoBest{job: j, running: st.RepoRunning[j.RepoID], last: st.RepoLastRun[j.RepoID]}
			continue
		}
		if older(j, b.job) {
			b.job = j
		}
	}
	if len(best) == 0 {
		return Job{}, false
	}
	cands := make([]*repoBest, 0, len(best))
	for _, b := range best {
		cands = append(cands, b)
	}
	sort.Slice(cands, func(i, k int) bool {
		a, b := cands[i], cands[k]
		if a.running != b.running {
			return a.running < b.running
		}
		if !a.last.Equal(b.last) {
			return a.last.Before(b.last)
		}
		return older(a.job, b.job)
	})
	return cands[0].job, true
}

func older(a, b Job) bool {
	if !a.QueuedAt.Equal(b.QueuedAt) {
		return a.QueuedAt.Before(b.QueuedAt)
	}
	return a.ID < b.ID
}

// Apply records that job j started, for simulations that pick repeatedly.
func (st *State) Apply(j Job, now time.Time) {
	st.Running++
	if st.RepoRunning == nil {
		st.RepoRunning = map[int64]int{}
	}
	if st.RepoLastRun == nil {
		st.RepoLastRun = map[int64]time.Time{}
	}
	if st.GroupRunning == nil {
		st.GroupRunning = map[string]int{}
	}
	st.RepoRunning[j.RepoID]++
	st.RepoLastRun[j.RepoID] = now
	if j.Group != "" {
		st.GroupRunning[j.Group]++
	}
}

// WaitReason explains why a queued job hasn't started, for the UI.
func WaitReason(l Limits, runners []Runner, st State, j Job) string {
	if l.MaxPerRepo > 0 && st.RepoRunning[j.RepoID] >= l.MaxPerRepo {
		return fmt.Sprintf("Waiting: this repository is running %d jobs (limit %d)", st.RepoRunning[j.RepoID], l.MaxPerRepo)
	}
	if j.GroupMax > 0 && st.GroupRunning[j.Group] >= j.GroupMax {
		return fmt.Sprintf("Waiting: max-parallel is %d for this matrix", j.GroupMax)
	}
	if l.MaxConcurrent > 0 && st.Running >= l.MaxConcurrent {
		return fmt.Sprintf("Waiting: %d jobs are running across the instance (limit %d)", st.Running, l.MaxConcurrent)
	}
	matching, online, free := 0, 0, 0
	for _, r := range runners {
		if r.Disabled || !HasLabels(r.Labels, j.Labels) {
			continue
		}
		matching++
		if !r.Online {
			continue
		}
		online++
		if RunnerBlock(l, r, st) == "" {
			free++
		}
	}
	labels := "any labels"
	if len(j.Labels) > 0 {
		labels = "labels " + strings.Join(j.Labels, ", ")
	}
	switch {
	case matching == 0 && len(j.Labels) > 0:
		return "Waiting: no runner has " + labels
	case matching == 0:
		return "Waiting: no runner is registered; an admin can add one under Admin → Runners"
	case online == 0:
		return fmt.Sprintf("Waiting: no runner with %s is online (%d offline)", labels, matching)
	case free == 0:
		return fmt.Sprintf("Waiting for a free runner (%d matching, all busy or overloaded)", online)
	}
	return "Waiting for a runner to pick it up"
}
