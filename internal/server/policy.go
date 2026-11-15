package server

import (
	"context"
	"net/http"
	"strings"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/api/apigen"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/apierr"
)

// access is the minimum requirement to call an operation.
type access int

const (
	// accessUser: any authenticated, active user who has set their own password.
	accessUser access = iota
	// accessPublic: no authentication.
	accessPublic
	// accessPending: authenticated, allowed even while a password change is pending.
	accessPending
	// accessAdmin: admins only (and tokens need the admin scope).
	accessAdmin
)

// operationAccess maps operation IDs (from api/openapi.yaml) to their
// requirement. Operations not listed default to accessUser; admin
// operations must be listed explicitly (TestEveryAdminOperationIsGuarded
// checks this).
var operationAccess = map[string]access{
	"GetMeta":       accessPublic,
	"CompleteSetup": accessPublic,
	"Login":         accessPublic,
	"GetOrgLogo":    accessPublic,

	"GetSession":     accessPending,
	"Logout":         accessPending,
	"ChangePassword": accessPending,

	"UpdateOrg":          accessAdmin,
	"UploadOrgLogo":      accessAdmin,
	"DeleteOrgLogo":      accessAdmin,
	"AdminListUsers":     accessAdmin,
	"AdminCreateUser":    accessAdmin,
	"AdminGetUser":       accessAdmin,
	"AdminUpdateUser":    accessAdmin,
	"AdminResetPassword": accessAdmin,
	"AdminResetTotp":     accessAdmin,
	"ListAuditLog":       accessAdmin,
	"AdminGetUserRepos":  accessAdmin,
	"AdminSetUserRepos":  accessAdmin,
	"CreateRepo":         accessAdmin,
	"DeleteRepo":         accessAdmin,
	"ListOrgSecrets":     accessAdmin,
	"SetOrgSecret":       accessAdmin,
	"DeleteOrgSecret":    accessAdmin,
	"ListRunners":        accessAdmin,
	"GetRunner":          accessAdmin,
	"UpdateRunner":       accessAdmin,
	"DeleteRunner":       accessAdmin,
	"CreateRunnerToken":  accessAdmin,
	"GetCISettings":      accessAdmin,
	"GetServerHealth":    accessAdmin,
	"UpdateCISettings":   accessAdmin,
	"ListOrgWebhooks":    accessAdmin,
	"CreateOrgWebhook":   accessAdmin,
}

// allowedBeforeSetup lists operations that work while setup is pending.
var allowedBeforeSetup = map[string]bool{"GetMeta": true, "CompleteSetup": true, "GetOrgLogo": true}

// policyMiddleware enforces setup state, authentication, forced password
// changes, token scopes and admin-only access for every API operation, and
// makes the raw request available to handlers.
func (s *Server) policyMiddleware(f apigen.StrictHandlerFunc, op string) apigen.StrictHandlerFunc {
	return func(ctx context.Context, w http.ResponseWriter, r *http.Request, req any) (any, error) {
		ctx = context.WithValue(ctx, httpKey, httpPair{w: w, r: r})
		if err := s.checkAccess(ctx, op, r.Method); err != nil {
			return nil, err
		}
		if err := checkJobAccess(ctx, op, r); err != nil {
			return nil, err
		}
		return f(ctx, w, r, req)
	}
}

// jobOperations are the operations outside /repos/{repo} a CI job token
// may call.
var jobOperations = map[string]bool{"GetMeta": true, "GetSession": true}

// checkJobAccess limits CI job tokens to reading their own repository:
// GET requests under /repos/{its repo}, plus the session and server info.
func checkJobAccess(ctx context.Context, op string, r *http.Request) error {
	p := principal(ctx)
	if p == nil || p.Method != MethodJob {
		return nil
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return apierr.Forbidden("CI job tokens are read-only")
	}
	if jobOperations[op] {
		return nil
	}
	prefix := "/api/v1/repos/" + strings.ToLower(p.JobRepo.Name)
	path := strings.ToLower(r.URL.Path)
	if path == prefix || strings.HasPrefix(path, prefix+"/") {
		return nil
	}
	return apierr.Forbidden("CI job tokens can only read their own repository (%s)", p.JobRepo.Name)
}

func (s *Server) checkAccess(ctx context.Context, op, method string) error {
	if !allowedBeforeSetup[op] {
		done, err := s.setupComplete(ctx)
		if err != nil {
			return err
		}
		if !done {
			return apierr.New(http.StatusServiceUnavailable, apierr.CodeSetupRequired, "this server hasn't been set up yet; open the web UI to complete setup")
		}
	}
	level := operationAccess[op]
	if level == accessPublic {
		return nil
	}
	p := principal(ctx)
	if p == nil {
		return apierr.Unauthenticated("authentication required: log in or send a personal access token")
	}
	if p.User.MustChangePassword && level != accessPending {
		return apierr.New(http.StatusForbidden, apierr.CodePasswordChange, "you must change your temporary password before doing anything else")
	}
	if p.Method == MethodToken || p.Method == MethodJob {
		need := "write"
		if method == http.MethodGet || method == http.MethodHead {
			need = "read"
		}
		if !p.HasScope(need) {
			return apierr.Forbidden("this token lacks the %q scope (has: %s)", need, strings.Join(p.Scopes, ", "))
		}
	}
	if level == accessAdmin {
		if !p.User.IsAdmin() {
			return apierr.Forbidden("admins only")
		}
		if !p.HasScope("admin") {
			return apierr.Forbidden("this token lacks the \"admin\" scope")
		}
	}
	return nil
}
