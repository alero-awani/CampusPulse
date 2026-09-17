// Package apperr defines errors that map directly to API error responses.
package apperr

import (
	"fmt"
	"net/http"
)

// Error is an error the client should see, with its HTTP status and API error code.
type Error struct {
	Status  int
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Message }

func Invalid(format string, args ...any) error {
	return &Error{Status: http.StatusBadRequest, Code: "validation_error", Message: fmt.Sprintf(format, args...)}
}

func NotFound(what string) error {
	return &Error{Status: http.StatusNotFound, Code: "not_found", Message: what + " not found"}
}

func Unauthorized(message string) error {
	return &Error{Status: http.StatusUnauthorized, Code: "unauthorized", Message: message}
}

func InvalidCredentials() error {
	return &Error{Status: http.StatusUnauthorized, Code: "invalid_credentials", Message: "Incorrect email or password"}
}

func Forbidden() error {
	return &Error{Status: http.StatusForbidden, Code: "forbidden", Message: "You don't have access to this"}
}

func Conflict(message string) error {
	return &Error{Status: http.StatusConflict, Code: "conflict", Message: message}
}

func RateLimited() error {
	return &Error{Status: http.StatusTooManyRequests, Code: "rate_limited", Message: "Too many attempts. Try again in a few minutes."}
}
