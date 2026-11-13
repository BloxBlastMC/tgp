// Package apierr defines the structured error type returned by every API
// endpoint. Clients (the web UI and the tgp CLI) rely on Code being stable.
package apierr

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// Stable error codes. Add new codes; never rename existing ones.
const (
	CodeBadRequest       = "bad_request"
	CodeValidation       = "validation_failed"
	CodeUnauthenticated  = "unauthenticated"
	CodeForbidden        = "forbidden"
	CodeNotFound         = "not_found"
	CodeConflict         = "conflict"
	CodeRateLimited      = "rate_limited"
	CodeSetupRequired    = "setup_required"
	CodePasswordChange   = "password_change_required"
	CodeTOTPRequired     = "totp_required"
	CodePayloadTooLarge  = "payload_too_large"
	CodeInternal         = "internal_error"
	CodeUnavailable      = "service_unavailable"
	CodeNotImplemented   = "not_implemented"
	CodeMethodNotAllowed = "method_not_allowed"
)

// Error is an API error with an HTTP status and a stable code.
type Error struct {
	Status  int            `json:"-"`
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s", e.Code, e.Message) }

// WithDetails returns a copy of e with details attached.
func (e *Error) WithDetails(d map[string]any) *Error {
	c := *e
	c.Details = d
	return &c
}

// New creates an Error.
func New(status int, code, format string, args ...any) *Error {
	return &Error{Status: status, Code: code, Message: fmt.Sprintf(format, args...)}
}

func BadRequest(format string, args ...any) *Error {
	return New(http.StatusBadRequest, CodeBadRequest, format, args...)
}

func Validation(format string, args ...any) *Error {
	return New(http.StatusUnprocessableEntity, CodeValidation, format, args...)
}

func Unauthenticated(format string, args ...any) *Error {
	return New(http.StatusUnauthorized, CodeUnauthenticated, format, args...)
}

func Forbidden(format string, args ...any) *Error {
	return New(http.StatusForbidden, CodeForbidden, format, args...)
}

func NotFound(format string, args ...any) *Error {
	return New(http.StatusNotFound, CodeNotFound, format, args...)
}

func Conflict(format string, args ...any) *Error {
	return New(http.StatusConflict, CodeConflict, format, args...)
}

// From converts any error into an *Error. Unknown errors become a generic
// internal error so internal details never leak to clients.
func From(err error) *Error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return New(http.StatusInternalServerError, CodeInternal, "internal server error")
}

// Write sends err as a JSON error response.
func Write(w http.ResponseWriter, e *Error) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(e.Status)
	_ = json.NewEncoder(w).Encode(map[string]*Error{"error": e})
}
