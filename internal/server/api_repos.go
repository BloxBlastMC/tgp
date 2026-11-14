package server

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/api/apigen"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/apierr"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/audit"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/authz"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/git"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/markdown"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/model"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/repos"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/users"
)

const maxBlobBytes = 1 << 20

// repoFor loads a repository and checks the caller has at least need.
// Repositories the caller can't read look like they don't exist.
func (s *Server) repoFor(ctx context.Context, name string, need authz.Perm) (*model.Repo, authz.Perm, error) {
	u := mustPrincipal(ctx).User
	repo, err := s.repos.Get(ctx, s.db, name)
	if err != nil {
		return nil, authz.None, err
	}
	perm, err := authz.RepoPerm(ctx, s.db, u, repo)
	if err != nil {
		return nil, authz.None, err
	}
	if perm < authz.Read {
		return nil, authz.None, apierr.NotFound("repository %q not found", name)
	}
	if perm < need {
		return nil, perm, apierr.Forbidden("this needs %s access to %s; you have %s", need, repo.Name, perm)
	}
	return repo, perm, nil
}

func (s *Server) cloneURL(ctx context.Context, repo *model.Repo) string {
	base := s.cfg.Server.BaseURL
	if base == "" {
		if org, err := s.loadOrg(ctx); err == nil {
			base = org.BaseURL
		}
	}
	if base == "" {
		_, r := httpFrom(ctx)
		scheme := "http"
		if r.TLS != nil || (s.cfg.Server.TrustProxy && r.Header.Get("X-Forwarded-Proto") == "https") {
			scheme = "https"
		}
		base = scheme + "://" + r.Host
	}
	return strings.TrimRight(base, "/") + "/" + repo.Name + ".git"
}

func (s *Server) toAPIRepo(ctx context.Context, repo *model.Repo, perm authz.Perm) (apigen.Repo, error) {
	empty, err := s.repos.Open(repo).IsEmpty(ctx)
	if err != nil {
		return apigen.Repo{}, err
	}
	return apigen.Repo{
		Id:            repo.ID,
		Name:          repo.Name,
		Description:   repo.Description,
		DefaultBranch: repo.DefaultBranch,
		Empty:         empty,
		Archived:      repo.Archived,
		CloneUrl:      s.cloneURL(ctx, repo),
		Permission:    apigen.Permission(perm.String()),
		CreatedAt:     repo.CreatedAt,
		UpdatedAt:     repo.UpdatedAt,
		PushedAt:      repo.PushedAt,
	}, nil
}

func (a *api) ListRepos(ctx context.Context, req apigen.ListReposRequestObject) (apigen.ListReposResponseObject, error) {
	s := a.s
	u := mustPrincipal(ctx).User
	var rs []model.Repo
	q := s.db.NewSelect().Model(&rs)
	q = authz.VisibleRepos(q, u)
	if f := strings.TrimSpace(deref(req.Params.Q)); f != "" {
		q = q.Where("r.lower_name LIKE ?", "%"+escapeLike(strings.ToLower(f))+"%")
	}
	// Recently pushed first; never-pushed repos last, by name.
	if err := q.OrderExpr("CASE WHEN r.pushed_at IS NULL THEN 1 ELSE 0 END, r.pushed_at DESC, r.lower_name ASC").Scan(ctx); err != nil {
		return nil, err
	}
	ceiling := authz.RoleCeiling(u.Role)
	out := make(apigen.ListRepos200JSONResponse, 0, len(rs))
	for i := range rs {
		r, err := s.toAPIRepo(ctx, &rs[i], ceiling)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

func (a *api) CreateRepo(ctx context.Context, req apigen.CreateRepoRequestObject) (apigen.CreateRepoResponseObject, error) {
	s := a.s
	_, r := httpFrom(ctx)
	u := mustPrincipal(ctx).User
	b := req.Body
	if b == nil {
		return nil, apierr.BadRequest("missing request body")
	}
	repo, err := s.repos.Create(ctx, repos.CreateParams{
		Name:          strings.TrimSpace(b.Name),
		Description:   deref(b.Description),
		DefaultBranch: deref(b.DefaultBranch),
		AutoInit:      deref(b.AutoInit),
		Creator:       u,
	})
	if err != nil {
		return nil, err
	}
	_ = audit.Record(ctx, s.db, audit.Entry{Actor: u, Action: audit.RepoCreate, TargetType: "repo", Target: repo.Name, IP: clientIP(r)})
	out, err := s.toAPIRepo(ctx, repo, authz.Admin)
	if err != nil {
		return nil, err
	}
	return apigen.CreateRepo201JSONResponse(out), nil
}

func (a *api) GetRepo(ctx context.Context, req apigen.GetRepoRequestObject) (apigen.GetRepoResponseObject, error) {
	repo, perm, err := a.s.repoFor(ctx, req.Repo, authz.Read)
	if err != nil {
		return nil, err
	}
	out, err := a.s.toAPIRepo(ctx, repo, perm)
	if err != nil {
		return nil, err
	}
	return apigen.GetRepo200JSONResponse(out), nil
}

func (a *api) UpdateRepo(ctx context.Context, req apigen.UpdateRepoRequestObject) (apigen.UpdateRepoResponseObject, error) {
	s := a.s
	_, r := httpFrom(ctx)
	u := mustPrincipal(ctx).User
	b := req.Body
	if b == nil {
		return nil, apierr.BadRequest("missing request body")
	}
	repo, perm, err := s.repoFor(ctx, req.Repo, authz.Write)
	if err != nil {
		return nil, err
	}
	if (b.Name != nil || b.Archived != nil) && perm < authz.Admin {
		return nil, apierr.Forbidden("only admins can rename or archive repositories")
	}
	changes := map[string]any{}
	if b.Description != nil {
		d := strings.TrimSpace(*b.Description)
		if len(d) > 500 {
			return nil, apierr.Validation("description is too long (max 500)")
		}
		repo.Description, changes["description"] = d, d
	}
	if b.DefaultBranch != nil && *b.DefaultBranch != repo.DefaultBranch {
		br := *b.DefaultBranch
		g := s.repos.Open(repo)
		if !git.ValidRefName(br) {
			return nil, apierr.Validation("invalid branch name %q", br)
		}
		if _, err := g.ResolveCommit(ctx, "refs/heads/"+br); err != nil {
			return nil, apierr.Validation("branch %q doesn't exist", br)
		}
		if err := g.SetHead(ctx, br); err != nil {
			return nil, err
		}
		repo.DefaultBranch, changes["default_branch"] = br, br
	}
	if b.Archived != nil {
		repo.Archived, changes["archived"] = *b.Archived, *b.Archived
	}
	if b.Name != nil && *b.Name != repo.Name {
		old := repo.Name
		if err := s.repos.Rename(ctx, repo, strings.TrimSpace(*b.Name)); err != nil {
			return nil, err
		}
		changes["name"] = map[string]string{"from": old, "to": repo.Name}
	}
	repo.UpdatedAt = time.Now().UTC()
	if _, err := s.db.NewUpdate().Model(repo).Column("description", "default_branch", "archived", "updated_at").WherePK().Exec(ctx); err != nil {
		return nil, err
	}
	if len(changes) > 0 {
		_ = audit.Record(ctx, s.db, audit.Entry{Actor: u, Action: audit.RepoUpdate, TargetType: "repo", Target: repo.Name, IP: clientIP(r), Details: changes})
	}
	out, err := s.toAPIRepo(ctx, repo, perm)
	if err != nil {
		return nil, err
	}
	return apigen.UpdateRepo200JSONResponse(out), nil
}

func (a *api) DeleteRepo(ctx context.Context, req apigen.DeleteRepoRequestObject) (apigen.DeleteRepoResponseObject, error) {
	s := a.s
	_, r := httpFrom(ctx)
	repo, _, err := s.repoFor(ctx, req.Repo, authz.Admin)
	if err != nil {
		return nil, err
	}
	var jobIDs []int64
	if err := s.db.NewSelect().Model((*model.Job)(nil)).Column("id").Where("repo_id = ?", repo.ID).Scan(ctx, &jobIDs); err != nil {
		return nil, err
	}
	if err := s.repos.Delete(ctx, repo); err != nil {
		return nil, err
	}
	for _, id := range jobIDs { // CI logs live outside the database
		if err := s.store.DeletePrefix(ctx, logPrefix(id)); err != nil {
			s.log.Warn("delete CI logs", "job", id, "err", err)
		}
	}
	// Artifacts, caches and release assets are stored under the repo's ID.
	for _, prefix := range []string{"artifacts", "caches", "releases"} {
		if err := s.store.DeletePrefix(ctx, prefix+"/"+strconv.FormatInt(repo.ID, 10)); err != nil {
			s.log.Warn("delete repository blobs", "prefix", prefix, "repo", repo.ID, "err", err)
		}
	}
	_ = audit.Record(ctx, s.db, audit.Entry{Actor: mustPrincipal(ctx).User, Action: audit.RepoDelete, TargetType: "repo", Target: repo.Name, IP: clientIP(r)})
	return apigen.DeleteRepo204Response{}, nil
}

func (a *api) ListRepoAccess(ctx context.Context, req apigen.ListRepoAccessRequestObject) (apigen.ListRepoAccessResponseObject, error) {
	s := a.s
	repo, _, err := s.repoFor(ctx, req.Repo, authz.Read)
	if err != nil {
		return nil, err
	}
	var us []model.User
	err = s.db.NewSelect().Model(&us).
		Where("disabled = ?", false).
		WhereGroup(" AND ", func(q *bun.SelectQuery) *bun.SelectQuery {
			return q.Where("role = ?", model.RoleAdmin).WhereOr("all_repos = ?", true).
				WhereOr("id IN (SELECT user_id FROM repo_grants WHERE repo_id = ?)", repo.ID)
		}).Order("username ASC").Scan(ctx)
	if err != nil {
		return nil, err
	}
	out := make(apigen.ListRepoAccess200JSONResponse, 0, len(us))
	for _, u := range us {
		src := apigen.RepoAccessSourceGrant
		switch {
		case u.Role == model.RoleAdmin:
			src = apigen.RepoAccessSourceAdmin
		case u.AllRepos:
			src = apigen.RepoAccessSourceAllRepos
		}
		out = append(out, apigen.RepoAccess{
			User:       apigen.UserSummary{Id: u.ID, Username: u.Username, DisplayName: u.DisplayName, Role: apigen.Role(u.Role)},
			Permission: apigen.Permission(authz.RoleCeiling(u.Role).String()),
			Source:     src,
		})
	}
	return out, nil
}

// --- code browsing ---

// resolve returns the ref name and commit for an optional ref parameter.
func (s *Server) resolve(ctx context.Context, repo *model.Repo, ref *string) (string, string, *git.Repo, error) {
	g := s.repos.Open(repo)
	name := deref(ref)
	if name == "" {
		name = repo.DefaultBranch
	}
	commit, err := g.ResolveCommit(ctx, name)
	if err != nil {
		if empty, _ := g.IsEmpty(ctx); empty {
			return "", "", nil, apierr.NotFound("%s is empty: push a commit first", repo.Name)
		}
		return "", "", nil, apierr.NotFound("ref %q not found in %s", name, repo.Name)
	}
	return name, commit, g, nil
}

func cleanPath(p *string) (string, error) {
	clean, ok := git.CleanPath(deref(p))
	if !ok {
		return "", apierr.Validation("invalid path %q", deref(p))
	}
	return clean, nil
}

func gitErr(err error, what, p string) error {
	switch {
	case errors.Is(err, git.ErrNotFound):
		return apierr.NotFound("%s %q not found", what, p)
	case errors.Is(err, git.ErrNotDir):
		return apierr.Validation("%q is a file, not a directory", p)
	case errors.Is(err, git.ErrNotFile):
		return apierr.Validation("%q is not a file", p)
	}
	return err
}

func toAPIRefs(refs []git.Ref, defaultBranch string) []apigen.Ref {
	out := make([]apigen.Ref, 0, len(refs))
	for _, r := range refs {
		isDef := r.Name == defaultBranch
		out = append(out, apigen.Ref{Name: r.Name, Commit: r.Commit, Subject: r.Subject, Author: r.Author, Date: r.Date, IsDefault: &isDef})
	}
	return out
}

func (a *api) ListBranches(ctx context.Context, req apigen.ListBranchesRequestObject) (apigen.ListBranchesResponseObject, error) {
	repo, _, err := a.s.repoFor(ctx, req.Repo, authz.Read)
	if err != nil {
		return nil, err
	}
	refs, err := a.s.repos.Open(repo).Branches(ctx)
	if err != nil {
		return nil, err
	}
	out := toAPIRefs(refs, repo.DefaultBranch)
	rules, err := a.s.protectionRules(ctx, a.s.db, repo.ID)
	if err != nil {
		return nil, err
	}
	for i := range out {
		p := matchRule(rules, out[i].Name) != nil
		out[i].Protected = &p
	}
	// Default branch first.
	sort.SliceStable(out, func(i, j int) bool { return deref(out[i].IsDefault) && !deref(out[j].IsDefault) })
	return apigen.ListBranches200JSONResponse(out), nil
}

func (a *api) ListTags(ctx context.Context, req apigen.ListTagsRequestObject) (apigen.ListTagsResponseObject, error) {
	repo, _, err := a.s.repoFor(ctx, req.Repo, authz.Read)
	if err != nil {
		return nil, err
	}
	refs, err := a.s.repos.Open(repo).Tags(ctx)
	if err != nil {
		return nil, err
	}
	return apigen.ListTags200JSONResponse(toAPIRefs(refs, "")), nil
}

func (a *api) GetTree(ctx context.Context, req apigen.GetTreeRequestObject) (apigen.GetTreeResponseObject, error) {
	repo, _, err := a.s.repoFor(ctx, req.Repo, authz.Read)
	if err != nil {
		return nil, err
	}
	p, err := cleanPath(req.Params.Path)
	if err != nil {
		return nil, err
	}
	ref, commit, g, err := a.s.resolve(ctx, repo, req.Params.Ref)
	if err != nil {
		return nil, err
	}
	entries, err := g.Tree(ctx, commit, p)
	if err != nil {
		return nil, gitErr(err, "directory", p)
	}
	out := apigen.Tree{Ref: ref, Commit: commit, Path: p, Entries: make([]apigen.TreeEntry, 0, len(entries))}
	for _, e := range entries {
		out.Entries = append(out.Entries, apigen.TreeEntry{Name: e.Name, Path: e.Path, Type: apigen.TreeEntryType(e.Type), Mode: e.Mode, Sha: e.SHA, Size: e.Size})
	}
	return apigen.GetTree200JSONResponse(out), nil
}

func (a *api) GetBlob(ctx context.Context, req apigen.GetBlobRequestObject) (apigen.GetBlobResponseObject, error) {
	repo, _, err := a.s.repoFor(ctx, req.Repo, authz.Read)
	if err != nil {
		return nil, err
	}
	p, err := cleanPath(&req.Params.Path)
	if err != nil {
		return nil, err
	}
	ref, commit, g, err := a.s.resolve(ctx, repo, req.Params.Ref)
	if err != nil {
		return nil, err
	}
	b, err := g.ReadBlob(ctx, commit, p, maxBlobBytes)
	if err != nil {
		return nil, gitErr(err, "file", p)
	}
	out := apigen.Blob{Ref: ref, Commit: commit, Path: p, Sha: b.SHA, Size: b.Size, Binary: b.Binary, Truncated: b.Truncated}
	if b.Binary {
		out.Encoding, out.Content = apigen.Base64, base64.StdEncoding.EncodeToString(b.Content)
	} else {
		out.Encoding, out.Content = apigen.Utf8, string(b.Content)
	}
	return apigen.GetBlob200JSONResponse(out), nil
}

// rawResponse streams a blob with a safe content type: never HTML, so a
// committed file can't run script on this origin.
type rawResponse struct {
	rc          io.ReadCloser
	contentType string
	size        int64
	name        string
}

func (r rawResponse) VisitGetRawResponse(w http.ResponseWriter) error {
	defer r.rc.Close()
	h := w.Header()
	h.Set("Content-Type", r.contentType)
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "private, max-age=60")
	if r.contentType == "application/octet-stream" {
		h.Set("Content-Disposition", `attachment; filename="`+strings.ReplaceAll(r.name, `"`, "")+`"`)
	}
	w.WriteHeader(http.StatusOK)
	_, err := io.Copy(w, r.rc)
	return err
}

var inlineImageTypes = map[string]bool{"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true}

func (a *api) GetRaw(ctx context.Context, req apigen.GetRawRequestObject) (apigen.GetRawResponseObject, error) {
	repo, _, err := a.s.repoFor(ctx, req.Repo, authz.Read)
	if err != nil {
		return nil, err
	}
	p, err := cleanPath(&req.Params.Path)
	if err != nil {
		return nil, err
	}
	_, commit, g, err := a.s.resolve(ctx, repo, req.Params.Ref)
	if err != nil {
		return nil, err
	}
	head, err := g.ReadBlob(ctx, commit, p, 8000)
	if err != nil {
		return nil, gitErr(err, "file", p)
	}
	ct := "application/octet-stream"
	switch sniff := http.DetectContentType(head.Content); {
	case inlineImageTypes[sniff]:
		ct = sniff
	case strings.HasSuffix(strings.ToLower(p), ".svg") && !head.Binary:
		ct = "image/svg+xml" // the CSP sandbox stops embedded script
	case !head.Binary:
		ct = "text/plain; charset=utf-8"
	}
	rc, err := g.StreamBlob(ctx, head.SHA)
	if err != nil {
		return nil, err
	}
	return rawResponse{rc: rc, contentType: ct, size: head.Size, name: path.Base(p)}, nil
}

func (a *api) GetReadme(ctx context.Context, req apigen.GetReadmeRequestObject) (apigen.GetReadmeResponseObject, error) {
	repo, _, err := a.s.repoFor(ctx, req.Repo, authz.Read)
	if err != nil {
		return nil, err
	}
	dir, err := cleanPath(req.Params.Path)
	if err != nil {
		return nil, err
	}
	ref, commit, g, err := a.s.resolve(ctx, repo, req.Params.Ref)
	if err != nil {
		return nil, err
	}
	p, err := g.FindReadme(ctx, commit, dir)
	if err != nil {
		return nil, gitErr(err, "directory", dir)
	}
	if p == "" {
		return nil, apierr.NotFound("no README in %q", dir)
	}
	b, err := g.ReadBlob(ctx, commit, p, maxBlobBytes)
	if err != nil {
		return nil, err
	}
	var html string
	lower := strings.ToLower(p)
	if strings.HasSuffix(lower, ".md") || strings.HasSuffix(lower, ".markdown") {
		html, err = markdown.Render(b.Content, markdown.RepoContext{Repo: repo.Name, Ref: ref, Dir: dir})
		if err != nil {
			return nil, err
		}
	} else {
		html = "<pre>" + htmlEscape(string(b.Content)) + "</pre>"
	}
	return apigen.GetReadme200JSONResponse{Path: p, Html: html}, nil
}

func htmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(s)
}

func toAPICommit(c *git.Commit) apigen.Commit {
	return apigen.Commit{
		Sha:       c.SHA,
		Parents:   c.Parents,
		Author:    apigen.Signature{Name: c.Author.Name, Email: c.Author.Email, Date: c.Author.Date},
		Committer: apigen.Signature{Name: c.Committer.Name, Email: c.Committer.Email, Date: c.Committer.Date},
		Subject:   c.Subject,
		Body:      c.Body,
	}
}

func (a *api) ListCommits(ctx context.Context, req apigen.ListCommitsRequestObject) (apigen.ListCommitsResponseObject, error) {
	repo, _, err := a.s.repoFor(ctx, req.Repo, authz.Read)
	if err != nil {
		return nil, err
	}
	p, err := cleanPath(req.Params.Path)
	if err != nil {
		return nil, err
	}
	_, commit, g, err := a.s.resolve(ctx, repo, req.Params.Ref)
	if err != nil {
		return nil, err
	}
	page, per := max(1, deref(req.Params.Page)), 30
	if req.Params.PerPage != nil {
		per = max(1, min(*req.Params.PerPage, 100))
	}
	cs, err := g.Log(ctx, commit, p, (page-1)*per, per+1)
	if err != nil {
		return nil, err
	}
	out := apigen.CommitPage{Commits: []apigen.Commit{}, HasMore: len(cs) > per}
	if len(cs) > per {
		cs = cs[:per]
	}
	for i := range cs {
		out.Commits = append(out.Commits, toAPICommit(&cs[i]))
	}
	return apigen.ListCommits200JSONResponse(out), nil
}

func toAPIDiff(d *git.Diff) apigen.Diff {
	out := apigen.Diff{Files: make([]apigen.DiffFile, 0, len(d.Files)), Additions: d.Additions, Deletions: d.Deletions, Truncated: d.Truncated}
	for _, f := range d.Files {
		af := apigen.DiffFile{
			OldPath: f.OldPath, NewPath: f.NewPath, Status: apigen.DiffFileStatus(f.Status), Binary: f.Binary,
			Additions: f.Additions, Deletions: f.Deletions, Truncated: f.Truncated, Hunks: make([]apigen.DiffHunk, 0, len(f.Hunks)),
		}
		if f.OldMode != "" {
			af.OldMode = &f.OldMode
		}
		if f.NewMode != "" {
			af.NewMode = &f.NewMode
		}
		for _, h := range f.Hunks {
			ah := apigen.DiffHunk{Header: h.Header, OldStart: h.OldStart, OldLines: h.OldLines, NewStart: h.NewStart, NewLines: h.NewLines, Lines: make([]apigen.DiffLine, 0, len(h.Lines))}
			if h.Section != "" {
				ah.Section = &h.Section
			}
			for _, l := range h.Lines {
				al := apigen.DiffLine{Type: apigen.DiffLineType(l.Type), Content: l.Content}
				if l.OldLine > 0 {
					v := l.OldLine
					al.OldLine = &v
				}
				if l.NewLine > 0 {
					v := l.NewLine
					al.NewLine = &v
				}
				if l.NoNewline {
					t := true
					al.NoNewline = &t
				}
				ah.Lines = append(ah.Lines, al)
			}
			af.Hunks = append(af.Hunks, ah)
		}
		out.Files = append(out.Files, af)
	}
	return out
}

func (a *api) GetCommit(ctx context.Context, req apigen.GetCommitRequestObject) (apigen.GetCommitResponseObject, error) {
	repo, _, err := a.s.repoFor(ctx, req.Repo, authz.Read)
	if err != nil {
		return nil, err
	}
	_, sha, g, err := a.s.resolve(ctx, repo, &req.Sha)
	if err != nil {
		return nil, err
	}
	c, err := g.GetCommit(ctx, sha)
	if err != nil {
		return nil, gitErr(err, "commit", req.Sha)
	}
	from := git.EmptyTreeID
	if len(c.Parents) > 0 {
		from = c.Parents[0]
	}
	d, err := g.Diff(ctx, from, sha, git.DefaultDiffLimits)
	if err != nil {
		return nil, err
	}
	return apigen.GetCommit200JSONResponse{Commit: toAPICommit(c), Diff: toAPIDiff(d)}, nil
}

func (a *api) GetBlame(ctx context.Context, req apigen.GetBlameRequestObject) (apigen.GetBlameResponseObject, error) {
	repo, _, err := a.s.repoFor(ctx, req.Repo, authz.Read)
	if err != nil {
		return nil, err
	}
	p, err := cleanPath(&req.Params.Path)
	if err != nil {
		return nil, err
	}
	ref, commit, g, err := a.s.resolve(ctx, repo, req.Params.Ref)
	if err != nil {
		return nil, err
	}
	ranges, err := g.Blame(ctx, commit, p)
	if err != nil {
		return nil, gitErr(err, "file", p)
	}
	out := apigen.Blame{Ref: ref, Commit: commit, Path: p, Ranges: make([]apigen.BlameRange, 0, len(ranges))}
	for _, r := range ranges {
		out.Ranges = append(out.Ranges, apigen.BlameRange{Commit: r.Commit, Author: r.Author, Email: r.Email, Date: r.Date, Summary: r.Summary, StartLine: r.StartLine, Lines: r.Lines})
	}
	return apigen.GetBlame200JSONResponse(out), nil
}

func (a *api) RenderMarkdown(ctx context.Context, req apigen.RenderMarkdownRequestObject) (apigen.RenderMarkdownResponseObject, error) {
	if req.Body == nil {
		return nil, apierr.BadRequest("missing request body")
	}
	if len(req.Body.Text) > 1<<20 {
		return nil, apierr.Validation("text is too long (max 1 MiB)")
	}
	rc := markdown.RepoContext{}
	if name := deref(req.Body.Repo); name != "" {
		repo, _, err := a.s.repoFor(ctx, name, authz.Read)
		if err != nil {
			return nil, err
		}
		rc.Repo, rc.Ref = repo.Name, deref(req.Body.Ref)
		if rc.Ref == "" {
			rc.Ref = repo.DefaultBranch
		}
	}
	html, err := markdown.Render([]byte(req.Body.Text), rc)
	if err != nil {
		return nil, err
	}
	return apigen.RenderMarkdown200JSONResponse{Html: html}, nil
}

// --- admin: per-user repository grants ---

func (s *Server) userRepoAccess(ctx context.Context, db bun.IDB, u *model.User) (apigen.UserRepoAccess, error) {
	var names []string
	err := db.NewSelect().Model((*model.Repo)(nil)).Column("name").
		Where("id IN (SELECT repo_id FROM repo_grants WHERE user_id = ?)", u.ID).Order("lower_name ASC").Scan(ctx, &names)
	if err != nil {
		return apigen.UserRepoAccess{}, err
	}
	if names == nil {
		names = []string{}
	}
	return apigen.UserRepoAccess{AllRepos: u.AllRepos, Repos: names}, nil
}

func (a *api) AdminGetUserRepos(ctx context.Context, req apigen.AdminGetUserReposRequestObject) (apigen.AdminGetUserReposResponseObject, error) {
	u, err := users.GetByUsername(ctx, a.s.db, req.Username)
	if err != nil {
		return nil, err
	}
	out, err := a.s.userRepoAccess(ctx, a.s.db, u)
	if err != nil {
		return nil, err
	}
	return apigen.AdminGetUserRepos200JSONResponse(out), nil
}

func (a *api) AdminSetUserRepos(ctx context.Context, req apigen.AdminSetUserReposRequestObject) (apigen.AdminSetUserReposResponseObject, error) {
	s := a.s
	_, r := httpFrom(ctx)
	actor := mustPrincipal(ctx).User
	b := req.Body
	if b == nil {
		return nil, apierr.BadRequest("missing request body")
	}
	var out apigen.UserRepoAccess
	err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		u, err := users.GetByUsername(ctx, tx, req.Username)
		if err != nil {
			return err
		}
		ids := make([]int64, 0, len(b.Repos))
		seen := map[int64]bool{}
		for _, name := range b.Repos {
			repo, err := s.repos.Get(ctx, tx, name)
			if err != nil {
				return err
			}
			if !seen[repo.ID] {
				seen[repo.ID] = true
				ids = append(ids, repo.ID)
			}
		}
		if _, err := tx.NewDelete().Model((*model.RepoGrant)(nil)).Where("user_id = ?", u.ID).Exec(ctx); err != nil {
			return err
		}
		now := time.Now().UTC()
		if len(ids) > 0 {
			grants := make([]model.RepoGrant, len(ids))
			for i, id := range ids {
				grants[i] = model.RepoGrant{RepoID: id, UserID: u.ID, CreatedAt: now}
			}
			if _, err := tx.NewInsert().Model(&grants).Exec(ctx); err != nil {
				return err
			}
		}
		u.AllRepos = b.AllRepos || u.Role == model.RoleAdmin
		u.UpdatedAt = now
		if _, err := tx.NewUpdate().Model(u).Column("all_repos", "updated_at").WherePK().Exec(ctx); err != nil {
			return err
		}
		if out, err = s.userRepoAccess(ctx, tx, u); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{Actor: actor, Action: audit.UserRepoAccess, TargetType: "user", Target: u.Username, IP: clientIP(r),
			Details: map[string]any{"all_repos": out.AllRepos, "repos": out.Repos}})
	})
	if err != nil {
		return nil, err
	}
	return apigen.AdminSetUserRepos200JSONResponse(out), nil
}
