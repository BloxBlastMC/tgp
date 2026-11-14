// Package repos manages repository records and their on-disk storage.
package repos

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/apierr"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/git"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/model"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/users"
)

var nameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)

// reserved names would collide with top-level web or API routes.
var reserved = map[string]bool{
	"api": true, "assets": true, "admin": true, "settings": true, "login": true, "logout": true,
	"setup": true, "change-password": true, "pulls": true, "issues": true, "ci": true, "new": true,
	"healthz": true, "readyz": true, "internal": true, "favicon.svg": true, "index.html": true,
	"robots.txt": true, "users": true, "repos": true,
}

// ValidateName checks a repository name.
func ValidateName(name string) error {
	if !nameRe.MatchString(name) {
		return apierr.Validation("repository name %q is invalid: use up to 100 letters, digits, '.', '-' or '_', starting with a letter or digit", name)
	}
	lower := strings.ToLower(name)
	if strings.HasSuffix(lower, ".git") || strings.Contains(name, "..") {
		return apierr.Validation("repository name can't end in .git or contain '..'")
	}
	if reserved[lower] {
		return apierr.Validation("repository name %q is reserved", name)
	}
	return nil
}

// Store manages repositories.
type Store struct {
	DB   bun.IDB
	Git  *git.Git
	Root string
}

// Path returns the on-disk path of a repository.
func (s *Store) Path(r *model.Repo) string {
	return filepath.Join(s.Root, r.LowerName+".git")
}

// Open returns a git handle for r.
func (s *Store) Open(r *model.Repo) *git.Repo { return s.Git.Open(s.Path(r)) }

// Get loads a repository by name (case-insensitive).
func (s *Store) Get(ctx context.Context, db bun.IDB, name string) (*model.Repo, error) {
	r := new(model.Repo)
	err := db.NewSelect().Model(r).Where("lower_name = ?", strings.ToLower(name)).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apierr.NotFound("repository %q not found", name)
	}
	return r, err
}

// CreateParams describes a new repository.
type CreateParams struct {
	Name          string
	Description   string
	DefaultBranch string
	// AutoInit creates an initial commit with a README.
	AutoInit bool
	Creator  *model.User
}

// Create inserts the record and initialises the bare repository. If the
// git step fails the record is removed again.
func (s *Store) Create(ctx context.Context, p CreateParams) (*model.Repo, error) {
	if err := ValidateName(p.Name); err != nil {
		return nil, err
	}
	branch := strings.TrimSpace(p.DefaultBranch)
	if branch == "" {
		branch = "main"
	}
	if !s.Git.ValidBranchName(ctx, branch) {
		return nil, apierr.Validation("default branch %q is not a valid branch name", branch)
	}
	desc := strings.TrimSpace(p.Description)
	if len(desc) > 500 {
		return nil, apierr.Validation("description is too long (max 500)")
	}
	now := time.Now().UTC()
	r := &model.Repo{
		Name:          p.Name,
		LowerName:     strings.ToLower(p.Name),
		Description:   desc,
		DefaultBranch: branch,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if p.Creator != nil {
		r.CreatedByID = &p.Creator.ID
	}
	if _, err := s.DB.NewInsert().Model(r).Exec(ctx); err != nil {
		if users.IsUniqueViolation(err) {
			return nil, apierr.Conflict("a repository named %q already exists", p.Name)
		}
		return nil, err
	}
	if err := SeedLabels(ctx, s.DB, r.ID); err != nil {
		_, _ = s.DB.NewDelete().Model(r).WherePK().Exec(context.WithoutCancel(ctx))
		return nil, err
	}
	path := s.Path(r)
	cleanup := func() {
		_, _ = s.DB.NewDelete().Model(r).WherePK().Exec(context.WithoutCancel(ctx))
		_ = os.RemoveAll(path)
	}
	if _, err := os.Stat(path); err == nil {
		cleanup()
		return nil, apierr.Conflict("storage for %q already exists on disk; remove %s or pick another name", p.Name, path)
	}
	if err := s.Git.InitBare(ctx, path, branch); err != nil {
		cleanup()
		return nil, fmt.Errorf("init repository: %w", err)
	}
	if p.AutoInit {
		author := git.Signature{Name: "tgp", Email: "tgp@localhost", Date: now}
		if p.Creator != nil {
			author.Name = p.Creator.DisplayName
			author.Email = p.Creator.Email
			if author.Email == "" {
				author.Email = p.Creator.Username + "@users.tgp"
			}
		}
		readme := "# " + p.Name + "\n"
		if desc != "" {
			readme += "\n" + desc + "\n"
		}
		if _, err := s.Open(r).CommitFiles(ctx, git.CommitOptions{
			Branch: branch, Message: "Initial commit", Author: author,
			Files: []git.FileChange{{Path: "README.md", Content: []byte(readme)}},
		}); err != nil {
			cleanup()
			return nil, fmt.Errorf("initial commit: %w", err)
		}
		r.PushedAt = &now
		_, _ = s.DB.NewUpdate().Model(r).Column("pushed_at").WherePK().Exec(ctx)
	}
	return r, nil
}

// DefaultLabels are created in every new repository.
var DefaultLabels = []model.Label{
	{Name: "bug", Color: "d73a4a", Description: "Something isn't working"},
	{Name: "enhancement", Color: "a2eeef", Description: "New feature or request"},
	{Name: "documentation", Color: "0075ca", Description: "Improvements or additions to documentation"},
	{Name: "question", Color: "d876e3", Description: "Further information is requested"},
	{Name: "needs testing", Color: "fbca04", Description: "Ready for a tester to verify"},
	{Name: "duplicate", Color: "cfd3d7", Description: "This issue or pull request already exists"},
	{Name: "wontfix", Color: "ffffff", Description: "This will not be worked on"},
}

// SeedLabels creates the default labels in a repository.
func SeedLabels(ctx context.Context, db bun.IDB, repoID int64) error {
	now := time.Now().UTC()
	for _, d := range DefaultLabels {
		l := d
		l.RepoID, l.LowerName, l.CreatedAt = repoID, strings.ToLower(l.Name), now
		if _, err := db.NewInsert().Model(&l).Exec(ctx); err != nil {
			return err
		}
	}
	return nil
}

// Rename changes a repository's name and moves its storage.
func (s *Store) Rename(ctx context.Context, r *model.Repo, newName string) error {
	if err := ValidateName(newName); err != nil {
		return err
	}
	if strings.EqualFold(newName, r.Name) {
		r.Name = newName
		_, err := s.DB.NewUpdate().Model(r).Column("name").WherePK().Exec(ctx)
		return err
	}
	exists, err := s.DB.NewSelect().Model((*model.Repo)(nil)).Where("lower_name = ?", strings.ToLower(newName)).Exists(ctx)
	if err != nil {
		return err
	}
	if exists {
		return apierr.Conflict("a repository named %q already exists", newName)
	}
	oldPath := s.Path(r)
	oldName, oldLower := r.Name, r.LowerName
	r.Name, r.LowerName = newName, strings.ToLower(newName)
	if err := git.RenameDir(oldPath, s.Path(r)); err != nil {
		r.Name, r.LowerName = oldName, oldLower
		return fmt.Errorf("move repository storage: %w", err)
	}
	r.UpdatedAt = time.Now().UTC()
	if _, err := s.DB.NewUpdate().Model(r).Column("name", "lower_name", "updated_at").WherePK().Exec(ctx); err != nil {
		_ = git.RenameDir(s.Path(r), oldPath)
		r.Name, r.LowerName = oldName, oldLower
		return err
	}
	return nil
}

// Delete removes the record (grants cascade) and the storage.
func (s *Store) Delete(ctx context.Context, r *model.Repo) error {
	if _, err := s.DB.NewDelete().Model(r).WherePK().Exec(ctx); err != nil {
		return err
	}
	return os.RemoveAll(s.Path(r))
}
