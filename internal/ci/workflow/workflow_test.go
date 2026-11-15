package workflow

import (
	"strings"
	"testing"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/ci/expr"
)

const full = `
name: CI
on:
  push:
    branches: [main, 'release/**', '!release/old']
    paths-ignore: ['docs/**']
  pull_request:
    branches: [main]
  workflow_dispatch:
    inputs:
      level:
        type: choice
        options: [debug, info]
        default: info
      dry:
        type: boolean
env:
  GLOBAL: "1"
concurrency:
  group: ci-${{ github.ref }}
  cancel-in-progress: true
defaults:
  run:
    shell: bash
jobs:
  test:
    runs-on: [linux]
    container:
      image: golang:${{ matrix.go }}
      env: {CGO_ENABLED: "0"}
    services:
      db:
        image: postgres:17
        env: {POSTGRES_PASSWORD: pw}
    strategy:
      fail-fast: false
      matrix:
        go: ["1.26", "1.27"]
        os: [linux, darwin]
        exclude:
          - os: darwin
            go: "1.26"
        include:
          - os: linux
            race: true
    timeout-minutes: 30
    resources: {cpus: 2, memory: 4g, pids: 512}
    outputs:
      version: ${{ steps.v.outputs.version }}
    steps:
      - uses: actions/checkout@v4
        with: {fetch-depth: 0}
      - id: v
        run: echo "version=1.0" >> "$TGP_OUTPUT"
      - name: Test
        run: go test ./...
        env: {DB: "${{ secrets.DB_URL }}"}
        if: matrix.os == 'linux'
  deploy:
    needs: test
    if: github.ref == 'refs/heads/main'
    steps:
      - run: echo ${{ needs.test.outputs.version }}
`

func TestParseFull(t *testing.T) {
	w, err := Parse("ci.yml", []byte(full))
	if err != nil {
		t.Fatal(err)
	}
	if w.Name != "CI" || w.On.Push == nil || w.On.PullRequest == nil || w.On.Dispatch == nil {
		t.Fatalf("triggers: %+v", w.On)
	}
	if strings.Join(w.JobOrder, ",") != "test,deploy" {
		t.Fatalf("order %v", w.JobOrder)
	}
	j := w.Jobs["test"]
	if j.Resources.CPUs != 2 || j.Resources.MemoryBytes != 4<<30 || j.Resources.Pids != 512 {
		t.Fatalf("resources %+v", j.Resources)
	}
	if j.Steps[0].Uses != "checkout" || j.Steps[0].With["fetch-depth"] != "0" {
		t.Fatalf("checkout %+v", j.Steps[0])
	}
	if !w.Concurrency.CancelInProgress || w.Defaults.Shell != "bash" {
		t.Fatal("concurrency/defaults")
	}
	in := w.On.Dispatch.Inputs["level"]
	if in.Type != "choice" || in.Default != "info" || w.On.Dispatch.Inputs["dry"].Type != "boolean" {
		t.Fatalf("inputs %+v", w.On.Dispatch.Inputs)
	}
	insts, err := j.Instances(&expr.Context{})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, i := range insts {
		names = append(names, i.Name)
	}
	// 2x2 minus 1 excluded; include merges race:true into the linux combos.
	want := "test (1.26, linux, true),test (1.27, linux, true),test (1.27, darwin)"
	if strings.Join(names, ",") != want {
		t.Fatalf("instances:\n%s\nwant\n%s", strings.Join(names, ","), want)
	}
}

func TestParseErrors(t *testing.T) {
	cases := map[string]string{
		"jobs:\n  a:\n    steps: [{run: x}]\n":                                                                    `missing "on"`,
		"on: push\n":                                                                                              `missing "jobs"`,
		"on: push\njobs:\n  a:\n    stepz: []\n":                                                                  `unknown key "stepz"`,
		"on: schedule\njobs:\n  a:\n    steps: [{run: x}]\n":                                                      `"schedule" event isn't supported`,
		"on: push\njobs:\n  a:\n    steps: [{uses: someone/thing@v1}]\n":                                          `isn't a built-in step`,
		"on: push\njobs:\n  a:\n    steps: [{run: x, uses: checkout}]\n":                                          "either run or uses",
		"on: push\njobs:\n  a:\n    needs: b\n    steps: [{run: x}]\n":                                            `needs "b"`,
		"on: push\njobs:\n  a:\n    needs: b\n    steps: [{run: x}]\n  b:\n    needs: a\n    steps: [{run: x}]\n": "dependency cycle",
		"on: push\njobs:\n  a:\n    resources: {memory: 4}\n    steps: [{run: x}]\n":                              "invalid size",
		"on: push\njobs:\n  a:\n    steps: [{run: 'echo ${{ secrets.X '}]\n":                                      "missing closing",
		"on: push\njobs:\n  a:\n    if: secrets.X == 'y'\n    steps: [{run: x}]\n":                                `"secrets" context isn't available`,
		"on: push\njobs:\n  a:\n    steps: [{id: s, run: x}, {id: s, run: y}]\n":                                  "duplicate step id",
		"on: push\njobs:\n  a:\n    strategy: {matrix: {x: [1,2], exclude: [{y: 1}]}}\n    steps: [{run: x}]\n":   `"y" isn't a matrix dimension`,
		"on:\n  pull_request:\n    types: [closed]\njobs:\n  a:\n    steps: [{run: x}]\n":                         `unknown type "closed"`,
		"on: [push\n": "invalid YAML",
	}
	for src, want := range cases {
		_, err := Parse("w.yml", []byte(src))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q:\n got %v\nwant %q", src, err, want)
		}
	}
	// Errors carry positions.
	_, err := Parse("w.yml", []byte("on: push\njobs:\n  a:\n    steps:\n      - run: x\n        bogus: 1\n"))
	if es, ok := err.(Errors); !ok || es[0].Line != 6 {
		t.Fatalf("position: %#v", err)
	}
}

func TestMatrixTooLarge(t *testing.T) {
	src := "on: push\njobs:\n  a:\n    strategy:\n      matrix:\n        x: [1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17]\n        y: [1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17]\n    steps: [{run: x}]\n"
	if _, err := Parse("w.yml", []byte(src)); err == nil || !strings.Contains(err.Error(), "limit is 256") {
		t.Fatalf("got %v", err)
	}
}

func TestMatrixIncludeOnly(t *testing.T) {
	st := &Strategy{Include: []map[string]any{{"a": 1.0}, {"a": 2.0, "b": "x"}}}
	if got := ExpandMatrix(st); len(got) != 2 || got[1]["b"] != "x" {
		t.Fatalf("%v", got)
	}
	// An include that would overwrite an original value becomes a new combo.
	st = &Strategy{Matrix: map[string][]any{"os": {"linux"}}, MatrixKeys: []string{"os"}, Include: []map[string]any{{"os": "windows", "x": 1.0}}}
	if got := ExpandMatrix(st); len(got) != 2 || got[0]["x"] != nil {
		t.Fatalf("%v", got)
	}
}

func TestMatches(t *testing.T) {
	w, err := Parse("ci.yml", []byte(full))
	if err != nil {
		t.Fatal(err)
	}
	changed := func(files ...string) func() ([]string, error) {
		return func() ([]string, error) { return files, nil }
	}
	cases := []struct {
		ev   Event
		want bool
	}{
		{Event{Name: "push", Ref: "refs/heads/main", Changed: changed("main.go")}, true},
		{Event{Name: "push", Ref: "refs/heads/main", Changed: changed("docs/a.md", "docs/b/c.md")}, false},
		{Event{Name: "push", Ref: "refs/heads/main", Changed: changed("docs/a.md", "x.go")}, true},
		{Event{Name: "push", Ref: "refs/heads/release/1.0"}, true},
		{Event{Name: "push", Ref: "refs/heads/release/old"}, false},
		{Event{Name: "push", Ref: "refs/heads/feature"}, false},
		{Event{Name: "push", Ref: "refs/tags/v1"}, false}, // branch filter only: tags don't run
		{Event{Name: "pull_request", BaseBranch: "main", Action: "opened"}, true},
		{Event{Name: "pull_request", BaseBranch: "main", Action: "ready_for_review"}, false},
		{Event{Name: "pull_request", BaseBranch: "dev", Action: "opened"}, false},
		{Event{Name: "workflow_dispatch"}, true},
	}
	for _, c := range cases {
		got, err := w.Matches(&c.ev)
		if err != nil || got != c.want {
			t.Errorf("%+v: got %v %v want %v", c.ev, got, err, c.want)
		}
	}
	tags, _ := Parse("t.yml", []byte("on:\n  push:\n    tags: ['v*']\njobs:\n  a:\n    steps: [{run: x}]\n"))
	for ref, want := range map[string]bool{"refs/tags/v1.2": true, "refs/tags/x": false, "refs/heads/main": false} {
		if got, _ := tags.Matches(&Event{Name: "push", Ref: ref}); got != want {
			t.Errorf("tags %s: %v", ref, got)
		}
	}
}

func TestGlob(t *testing.T) {
	cases := []struct {
		p, s string
		want bool
	}{
		{"*", "main", true},
		{"*", "a/b", false},
		{"**", "a/b", true},
		{"feature/*", "feature/x", true},
		{"feature/*", "feature/x/y", false},
		{"**.go", "cmd/a/main.go", true},
		{"docs/**", "docs/a/b.md", true},
		{"v[12].*", "v1.0", true},
		{"v[12].*", "v3.0", false},
		{"ab?c", "ac", true},
		{"ab+c", "abbbc", true},
		{"a\\*b", "a*b", true},
	}
	for _, c := range cases {
		if got := MatchGlob(c.p, c.s); got != c.want {
			t.Errorf("%s ~ %s = %v", c.p, c.s, got)
		}
	}
}

func TestParseMemory(t *testing.T) {
	for in, want := range map[string]int64{"512m": 512 << 20, "4g": 4 << 30, "1.5GiB": 3 << 29, "100k": 100 << 10, "64MB": 64 << 20} {
		if got, err := ParseMemory(in); err != nil || got != want {
			t.Errorf("%s: %d %v", in, got, err)
		}
	}
	for _, in := range []string{"4", "lots", "4x"} {
		if _, err := ParseMemory(in); err == nil {
			t.Errorf("%s: expected error", in)
		}
	}
}

func TestHostJobsHealthchecksPermissions(t *testing.T) {
	src := `on: push
permissions:
  contents: read
  packages: write
jobs:
  image:
    container: none
    steps: [{uses: checkout}, {uses: registry-login}, {run: 'docker build -t "${{ tgp.image }}/issues" .'}]
  labeled:
    runs-on: [host, big]
    permissions: {packages: read}
    steps: [{run: x}]
  test:
    services:
      db:
        image: mysql:8.4
        healthcheck:
          cmd: [mysqladmin, ping, -h, 127.0.0.1]
          interval: 2s
          timeout: 1m
          retries: 20
          start-period: 30
      cache:
        image: redis:7
        healthcheck: redis-cli ping
    permissions: read-all
    steps: [{run: x}]
`
	w, err := Parse("w.yml", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	img := w.Jobs["image"]
	if !img.Host || img.Container != nil || strings.Join(img.RunsOn, ",") != "host" || img.Packages != PackagesWrite {
		t.Fatalf("image job: %+v", img)
	}
	if l := w.Jobs["labeled"]; !l.Host || l.Packages != PackagesRead {
		t.Fatalf("runs-on host job: %+v", l)
	}
	test := w.Jobs["test"]
	if test.Host || test.Packages != PackagesRead {
		t.Fatalf("test job: %+v", test)
	}
	h := test.Services["db"].Healthcheck
	if h == nil || h.Cmd != "mysqladmin ping -h 127.0.0.1" || h.Interval != 2 || h.Timeout != 60 || h.Retries != 20 || h.StartPeriod != 30 {
		t.Fatalf("healthcheck: %+v", h)
	}
	if c := test.Services["cache"].Healthcheck; c == nil || c.Cmd != "redis-cli ping" {
		t.Fatalf("scalar healthcheck: %+v", c)
	}
	// A permissions mapping without packages means packages: none.
	w, err = Parse("w.yml", []byte("on: push\npermissions: {contents: read}\njobs:\n  a:\n    steps: [{run: x}]\n"))
	if err != nil || w.Jobs["a"].Packages != PackagesNone {
		t.Fatalf("packages none: %v %+v", err, w)
	}
	// container: with an image is still a container job, even with runs-on: host.
	w, err = Parse("w.yml", []byte("on: push\njobs:\n  a:\n    runs-on: host\n    container: alpine:3\n    steps: [{run: x}]\n"))
	if err != nil || w.Jobs["a"].Host {
		t.Fatalf("container job on host runner: %v %+v", err, w.Jobs["a"])
	}

	bad := map[string]string{
		"on: push\njobs:\n  a:\n    container: none\n    services: {db: {image: mysql}}\n    steps: [{run: x}]\n":                     "can't have services",
		"on: push\njobs:\n  a:\n    services: {db: {image: mysql, healthcheck: {interval: 2s}}}\n    steps: [{run: x}]\n":             "cmd is required",
		"on: push\njobs:\n  a:\n    services: {db: {image: mysql, healthcheck: {cmd: x, interval: 2 days}}}\n    steps: [{run: x}]\n": "must be a duration",
		"on: push\njobs:\n  a:\n    container: {image: x, healthcheck: y}\n    steps: [{run: x}]\n":                                   `unknown key "healthcheck"`,
		"on: push\npermissions: {packages: admin}\njobs:\n  a:\n    steps: [{run: x}]\n":                                              "must be read, write or none",
		"on: push\npermissions: everything\njobs:\n  a:\n    steps: [{run: x}]\n":                                                     "read-all, write-all",
		"on: push\njobs:\n  a:\n    steps: [{uses: registry-login, with: {registry: x}}]\n":                                           `doesn't take "registry"`,
	}
	for src, want := range bad {
		if _, err := Parse("w.yml", []byte(src)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q:\n got %v\nwant %q", src, err, want)
		}
	}
}
