package server

import (
	"context"
	"errors"
	"strings"

	"github.com/uptrace/bun"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/git"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/labeler"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/model"
)

// labelerConfig reads the labeler configuration at commit. It returns nil
// when the repository has none.
func (s *Server) labelerConfig(ctx context.Context, g *git.Repo, commit string) (*labeler.Config, string, error) {
	for _, p := range labeler.Paths {
		b, err := g.ReadBlob(ctx, commit, p, labeler.MaxFileBytes+1)
		if errors.Is(err, git.ErrNotFound) || errors.Is(err, git.ErrNotFile) {
			continue
		}
		if err != nil {
			return nil, p, err
		}
		c, err := labeler.Parse(p, b.Content)
		return c, p, err
	}
	return nil, "", nil
}

// labelPull applies the repository's labeler rules (.tgp/labeler.yml or
// .github/labeler.yml) to a pull request. The rules come from the base
// branch, so a pull request can't change how it is labeled. Labels are
// added by tgp (no actor); labels the repository doesn't have are
// skipped. Errors are logged: labeling never fails the caller.
func (s *Server) labelPull(ctx context.Context, repo *model.Repo, pr *model.PullRequest, number int64) {
	log := s.log.With("repo", repo.Name, "pull", number)
	g := s.repos.Open(repo)
	base, err := g.ResolveCommit(ctx, pr.BaseBranch)
	if err != nil {
		return
	}
	cfg, path, err := s.labelerConfig(ctx, g, base)
	if err != nil {
		log.Warn("labeler: invalid configuration; no labels applied", "file", path, "err", err)
		return
	}
	if cfg == nil || len(cfg.Labels) == 0 {
		return
	}
	mb, err := g.MergeBase(ctx, base, pr.HeadSHA)
	if err != nil {
		mb = base
	}
	files, err := g.ChangedFiles(ctx, mb, pr.HeadSHA)
	if err != nil {
		log.Warn("labeler: changed files", "err", err)
		return
	}
	match := cfg.Match(labeler.PR{Files: files, HeadBranch: pr.HeadBranch, BaseBranch: pr.BaseBranch})
	var repoLabels []model.Label
	if err := s.db.NewSelect().Model(&repoLabels).Where("repo_id = ?", repo.ID).Scan(ctx); err != nil {
		log.Warn("labeler: labels", "err", err)
		return
	}
	exists := map[string]bool{}
	for _, l := range repoLabels {
		exists[l.LowerName] = true
	}
	var cur []model.Label
	if err := s.db.NewSelect().Model(&cur).Where("l.id IN (SELECT label_id FROM issue_labels WHERE issue_id = ?)", pr.IssueID).Scan(ctx); err != nil {
		log.Warn("labeler: current labels", "err", err)
		return
	}
	has := map[string]bool{}
	for _, l := range cur {
		has[l.LowerName] = true
	}
	matched := map[string]bool{}
	var add []string
	for _, name := range match {
		lower := strings.ToLower(name)
		matched[lower] = true
		switch {
		case !exists[lower]:
			log.Info("labeler: label doesn't exist in the repository; skipped", "label", name, "file", path)
		case !has[lower]:
			add = append(add, name)
		}
	}
	var remove []string
	if cfg.SyncLabels {
		for _, name := range cfg.Names() {
			if lower := strings.ToLower(name); has[lower] && !matched[lower] {
				remove = append(remove, name)
			}
		}
	}
	if len(add) == 0 && len(remove) == 0 {
		return
	}
	issue := new(model.Issue)
	if err := s.db.NewSelect().Model(issue).Where("id = ?", pr.IssueID).Scan(ctx); err != nil {
		return
	}
	c := metaChange{}
	if len(add) > 0 {
		c.addLabels = &add
	}
	if len(remove) > 0 {
		c.removeLabels = &remove
	}
	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		return s.applyMeta(ctx, tx, repo, issue, nil, c)
	})
	if err != nil {
		log.Warn("labeler: applying labels failed", "err", err)
		return
	}
	log.Info("labeler: labels updated", "added", add, "removed", remove)
}
