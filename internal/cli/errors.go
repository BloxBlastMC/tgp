package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/apierr"
)

// Exit codes. They are part of the CLI's contract (docs/cli.md); never
// change the meaning of an existing code.
const (
	ExitOK          = 0
	ExitError       = 1 // anything not covered below
	ExitUsage       = 2 // bad arguments or flags, missing required input
	ExitAuth        = 3 // not logged in, bad/expired token, password change or 2FA needed
	ExitForbidden   = 4 // logged in but not allowed
	ExitNotFound    = 5
	ExitConflict    = 6 // conflict or validation failure: the request can't succeed as-is
	ExitUnavailable = 7 // network error, server busy or rate limited: retrying may help
)

// CLI-specific error codes, in addition to the API's codes (apierr).
const (
	CodeUsage         = "usage"
	CodeInputRequired = "input_required"
	CodeNotLoggedIn   = "not_logged_in"
	CodeNoRepo        = "repo_required"
	CodeNetwork       = "network_error"
	CodeCancelled     = "cancelled"
	CodeBadResponse   = "invalid_response"
	CodeNotTgp        = "not_a_tgp_server"
	CodeGit           = "git_failed"
)

// Error is a CLI error with a stable code and exit status.
type Error struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Hint    string         `json:"hint,omitempty"`
	Status  int            `json:"status,omitempty"` // HTTP status when the error came from the API
	Details map[string]any `json:"details,omitempty"`
	Exit    int            `json:"-"`

	silent bool // already reported (e.g. in the command's output); just exit
}

func (e *Error) Error() string { return e.Message }

func usageErr(format string, args ...any) *Error {
	return &Error{Code: CodeUsage, Exit: ExitUsage, Message: fmt.Sprintf(format, args...)}
}

func errorf(code string, exit int, format string, args ...any) *Error {
	return &Error{Code: code, Exit: exit, Message: fmt.Sprintf(format, args...)}
}

// exitForAPICode maps an API error code (and HTTP status) to an exit code.
func exitForAPICode(code string, status int) int {
	switch code {
	case apierr.CodeUnauthenticated, apierr.CodePasswordChange, apierr.CodeTOTPRequired:
		return ExitAuth
	case apierr.CodeForbidden:
		return ExitForbidden
	case apierr.CodeNotFound:
		return ExitNotFound
	case apierr.CodeConflict, apierr.CodeValidation, apierr.CodeSetupRequired, apierr.CodePayloadTooLarge:
		return ExitConflict
	case apierr.CodeBadRequest, apierr.CodeMethodNotAllowed:
		return ExitUsage
	case apierr.CodeRateLimited, apierr.CodeUnavailable:
		return ExitUnavailable
	}
	switch {
	case status == 401:
		return ExitAuth
	case status == 403:
		return ExitForbidden
	case status == 404:
		return ExitNotFound
	case status == 409 || status == 422:
		return ExitConflict
	case status == 429 || status == 502 || status == 503 || status == 504:
		return ExitUnavailable
	}
	return ExitError
}

// apiError builds an Error from an API error response body.
func apiError(status int, body []byte) *Error {
	var env struct {
		Error struct {
			Code    string         `json:"code"`
			Message string         `json:"message"`
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &env); err != nil || env.Error.Code == "" {
		msg := strings.TrimSpace(string(body))
		if len(msg) > 200 {
			msg = msg[:200] + "…"
		}
		if msg == "" {
			msg = fmt.Sprintf("HTTP %d", status)
		}
		code := apierr.CodeInternal
		if status == 404 {
			code = apierr.CodeNotFound
		}
		return &Error{Code: code, Status: status, Exit: exitForAPICode(code, status),
			Message: fmt.Sprintf("server returned HTTP %d: %s", status, msg)}
	}
	e := &Error{Code: env.Error.Code, Message: env.Error.Message, Details: env.Error.Details,
		Status: status, Exit: exitForAPICode(env.Error.Code, status)}
	switch e.Code {
	case apierr.CodeUnauthenticated:
		e.Hint = "run `tgp auth login` (or set TGP_TOKEN) to authenticate"
	case apierr.CodePasswordChange:
		e.Hint = "run `tgp auth login` in a terminal, or log in to the web UI, to set your password"
	}
	return e
}

// toError converts any error into a CLI Error.
func toError(err error) *Error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	var ue *url.Error
	var ne net.Error
	if errors.As(err, &ue) || errors.As(err, &ne) {
		return &Error{Code: CodeNetwork, Exit: ExitUnavailable, Message: err.Error()}
	}
	msg := err.Error()
	// Cobra's argument and flag errors.
	for _, p := range []string{"unknown flag", "unknown shorthand flag", "unknown command", "accepts ", "requires at least", "requires at most", "invalid argument", "flag needs an argument", "required flag", "if any flags in the group", "none of the others can be"} {
		if strings.Contains(msg, p) {
			return &Error{Code: CodeUsage, Exit: ExitUsage, Message: msg}
		}
	}
	return &Error{Code: "error", Exit: ExitError, Message: msg}
}

// writeError prints err to w, as JSON when asJSON is set.
func writeError(w io.Writer, e *Error, asJSON bool) {
	if e.silent {
		return
	}
	if asJSON {
		b, _ := json.Marshal(map[string]any{"error": e})
		fmt.Fprintln(w, string(b))
		return
	}
	fmt.Fprintln(w, "error:", e.Message)
	if e.Hint != "" {
		fmt.Fprintln(w, "hint:", e.Hint)
	}
}
