package scheduler

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)

func queue(repo int64, n int, start time.Duration, labels ...string) []Job {
	var out []Job
	for i := 0; i < n; i++ {
		out = append(out, Job{ID: repo*1000 + int64(i), RepoID: repo, Labels: labels, QueuedAt: t0.Add(start + time.Duration(i)*time.Second)})
	}
	return out
}

// simulate assigns jobs to runners, one free slot at a time, until nothing
// more can start, and returns the order of repo IDs started.
func simulate(l Limits, runners []Runner, q []Job) ([]int64, State) {
	st := State{}
	var order []int64
	now := t0.Add(time.Hour)
	for progress := true; progress; {
		progress = false
		for i := range runners {
			j, ok := Pick(l, runners[i], st, q)
			if !ok {
				continue
			}
			for k := range q {
				if q[k].ID == j.ID {
					q = append(q[:k], q[k+1:]...)
					break
				}
			}
			now = now.Add(time.Second)
			st.Apply(j, now)
			runners[i].Busy++
			order = append(order, j.RepoID)
			progress = true
		}
	}
	return order, st
}

func TestFairnessAcrossRepos(t *testing.T) {
	// Repo 1 queued 20 jobs first; repo 2 and 3 queued later. Repos still alternate.
	q := append(append(queue(1, 20, 0), queue(2, 3, time.Minute)...), queue(3, 1, 2*time.Minute)...)
	order, _ := simulate(Limits{}, []Runner{{ID: 1, Capacity: 6}}, q)
	got := fmt.Sprint(order)
	if got != "[1 2 3 1 2 1]" {
		t.Fatalf("order %s", got)
	}
}

func TestGlobalAndRepoCaps(t *testing.T) {
	q := append(queue(1, 10, 0), queue(2, 10, 0)...)
	runners := []Runner{{ID: 1, Capacity: 10}, {ID: 2, Capacity: 10}}
	order, st := simulate(Limits{MaxConcurrent: 5, MaxPerRepo: 3}, runners, q)
	if len(order) != 5 || st.Running != 5 {
		t.Fatalf("global cap: %v", order)
	}
	if st.RepoRunning[1] > 3 || st.RepoRunning[2] > 3 {
		t.Fatalf("repo cap: %v", st.RepoRunning)
	}
	order, st = simulate(Limits{MaxPerRepo: 2}, []Runner{{ID: 1, Capacity: 10}}, queue(1, 10, 0))
	if len(order) != 2 {
		t.Fatalf("repo cap alone: %v", order)
	}
	if r := WaitReason(Limits{MaxPerRepo: 2}, nil, st, Job{RepoID: 1}); !strings.Contains(r, "limit 2") {
		t.Fatalf("reason %q", r)
	}
}

func TestRunnerCapacityLoadLabels(t *testing.T) {
	q := append(queue(1, 3, 0, "gpu"), queue(2, 3, time.Minute)...)
	runners := []Runner{
		{ID: 1, Capacity: 2, Labels: []string{"linux"}},
		{ID: 2, Capacity: 2, Labels: []string{"linux", "GPU"}, CPUPercent: 95},
	}
	l := Limits{MaxCPUPercent: 90}
	order, _ := simulate(l, runners, q)
	// The GPU runner is overloaded, so only runner 1 works, and it lacks gpu.
	if fmt.Sprint(order) != "[2 2]" {
		t.Fatalf("order %v", order)
	}
	if b := RunnerBlock(l, runners[1], State{}); !strings.Contains(b, "overloaded") {
		t.Fatalf("block %q", b)
	}
	runners[1].CPUPercent = 10
	runners[0].Busy = 0
	order, _ = simulate(l, runners, append(queue(1, 3, 0, "gpu"), queue(2, 3, time.Minute)...))
	// Runner 2 takes gpu jobs (and others), runner 1 only non-gpu ones.
	ones := 0
	for _, r := range order {
		if r == 1 {
			ones++
		}
	}
	if len(order) != 4 || ones != 2 {
		t.Fatalf("order %v", order)
	}
	if (Runner{Disabled: true}).Disabled && RunnerBlock(l, Runner{Disabled: true}, State{}) == "" {
		t.Fatal("disabled runner not blocked")
	}
}

func TestMaxParallel(t *testing.T) {
	var q []Job
	for i := 0; i < 6; i++ {
		q = append(q, Job{ID: int64(i + 1), RepoID: 1, Group: "run1/test", GroupMax: 2, QueuedAt: t0})
	}
	order, _ := simulate(Limits{}, []Runner{{ID: 1, Capacity: 10}}, q)
	if len(order) != 2 {
		t.Fatalf("max-parallel: %v", order)
	}
}

func TestWaitReasons(t *testing.T) {
	j := Job{RepoID: 1, Labels: []string{"gpu"}}
	cases := []struct {
		runners []Runner
		want    string
	}{
		{nil, "no runner has labels gpu"},
		{[]Runner{{Labels: []string{"gpu"}}}, "no runner with labels gpu is online"},
		{[]Runner{{Labels: []string{"gpu"}, Online: true, Capacity: 1, Busy: 1}}, "all busy"},
		{[]Runner{{Labels: []string{"gpu"}, Online: true, Capacity: 1}}, "pick it up"},
	}
	for _, c := range cases {
		if got := WaitReason(Limits{}, c.runners, State{}, j); !strings.Contains(got, c.want) {
			t.Errorf("got %q want %q", got, c.want)
		}
	}
	if got := WaitReason(Limits{}, nil, State{}, Job{}); !strings.Contains(got, "no runner is registered") {
		t.Errorf("got %q", got)
	}
	if got := WaitReason(Limits{MaxConcurrent: 1}, nil, State{Running: 1}, Job{}); !strings.Contains(got, "across the instance") {
		t.Errorf("got %q", got)
	}
}
