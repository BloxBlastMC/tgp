package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/ci/workflow"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/labeler"
)

// LintFile is the result for one file of tgp workflow lint.
type LintFile struct {
	File   string      `json:"file"`
	Kind   string      `json:"kind"` // workflow or labeler
	Valid  bool        `json:"valid"`
	Errors []LintError `json:"errors"`
}

// LintError is one problem.
type LintError struct {
	Line    int    `json:"line,omitempty"`
	Column  int    `json:"column,omitempty"`
	Message string `json:"message"`
}

// LintResult is tgp workflow lint's output.
type LintResult struct {
	Valid bool       `json:"valid"`
	Files []LintFile `json:"files"`
}

func newWorkflowLintCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lint [FILE...]",
		Short: "Check workflow files (and the labeler config) locally, without a server",
		Long: `Checks workflow files the way the server does: syntax, unknown keys,
expressions and the contexts they use, needs, matrices, host jobs,
service healthchecks and built-in steps.

Without arguments it checks every .tgp/workflows/*.yml|yaml file and the
pull request labeler config (.tgp/labeler.yml or .github/labeler.yml) of
the git repository you're in. Files named labeler.yml are checked as
labeler configs, others as workflows.

Exits with status 6 if any file has errors.`,
		Example: `  tgp workflow lint
  tgp workflow lint .tgp/workflows/build.yml
  tgp workflow lint --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			files := args
			if len(files) == 0 {
				var err error
				if files, err = defaultLintFiles(); err != nil {
					return err
				}
				if len(files) == 0 {
					return usageErr("no workflow files found under %s (run this inside a repository, or name the files)", workflow.Dir)
				}
			}
			res := LintResult{Valid: true, Files: []LintFile{}}
			for _, f := range files {
				lf := lintFile(f)
				if !lf.Valid {
					res.Valid = false
				}
				res.Files = append(res.Files, lf)
			}
			err := a.print(cmd, res, func(w io.Writer) error {
				for _, f := range res.Files {
					if f.Valid {
						fmt.Fprintf(w, "%s %s\n", a.io.SuccessIcon(), f.File)
						continue
					}
					for _, e := range f.Errors {
						pos := f.File
						if e.Line > 0 {
							pos = fmt.Sprintf("%s:%d:%d", f.File, e.Line, e.Column)
						}
						fmt.Fprintf(w, "%s: %s\n", pos, a.io.paint("31", e.Message))
					}
				}
				return nil
			})
			if err != nil {
				return err
			}
			if !res.Valid {
				n := 0
				for _, f := range res.Files {
					n += len(f.Errors)
				}
				return &Error{Code: "invalid_workflow", Exit: ExitConflict, Message: fmt.Sprintf("%d problem(s) found", n), silent: a.out.jsonMode()}
			}
			return nil
		},
	}
	return withOutput(cmd, "LintResult")
}

// defaultLintFiles finds the workflow and labeler files of the current
// repository (or directory).
func defaultLintFiles() ([]string, error) {
	root, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	if out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output(); err == nil {
		root = strings.TrimSpace(string(out))
	}
	var files []string
	for _, pat := range []string{"*.yml", "*.yaml"} {
		m, _ := filepath.Glob(filepath.Join(root, filepath.FromSlash(workflow.Dir), pat))
		files = append(files, m...)
	}
	sort.Strings(files)
	for _, p := range labeler.Paths {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(p))); err == nil {
			files = append(files, filepath.Join(root, filepath.FromSlash(p)))
			break
		}
	}
	// Show paths relative to where the command runs when possible.
	if wd, err := os.Getwd(); err == nil {
		for i, f := range files {
			if rel, err := filepath.Rel(wd, f); err == nil && !strings.HasPrefix(rel, "..") {
				files[i] = rel
			}
		}
	}
	return files, nil
}

func lintFile(path string) LintFile {
	kind := "workflow"
	if b := filepath.Base(path); b == "labeler.yml" || b == "labeler.yaml" {
		kind = "labeler"
	}
	lf := LintFile{File: path, Kind: kind, Valid: true, Errors: []LintError{}}
	data, err := os.ReadFile(path)
	if err != nil {
		lf.Valid = false
		lf.Errors = append(lf.Errors, LintError{Message: err.Error()})
		return lf
	}
	if kind == "labeler" {
		_, err = labeler.Parse(path, data)
	} else {
		_, err = workflow.Parse(path, data)
	}
	if err == nil {
		return lf
	}
	lf.Valid = false
	var wes workflow.Errors
	var les labeler.Errors
	switch {
	case errors.As(err, &wes):
		for _, e := range wes {
			lf.Errors = append(lf.Errors, LintError{Line: e.Line, Column: e.Col, Message: e.Msg})
		}
	case errors.As(err, &les):
		for _, e := range les {
			lf.Errors = append(lf.Errors, LintError{Line: e.Line, Column: e.Col, Message: e.Msg})
		}
	default:
		lf.Errors = append(lf.Errors, LintError{Message: err.Error()})
	}
	return lf
}
