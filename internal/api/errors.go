package api

import (
	"encoding/json"
	"errors"
	"net/http"
)

// Error code constants used in API error responses.
const (
	ErrCodeNotFound          = "NOT_FOUND"
	ErrCodeInvalidInput      = "INVALID_INPUT"
	ErrCodeInvalidTransition = "INVALID_TRANSITION"
	ErrCodeImmutable         = "IMMUTABLE"
	ErrCodeForbidden         = "FORBIDDEN"
	ErrCodeConflict          = "CONFLICT"
	ErrCodePublishConflict   = "PUBLISH_CONFLICT"
	ErrCodeLineageReserved   = "LINEAGE_RESERVED"
	ErrCodeUnavailable       = "UNAVAILABLE"
	ErrCodeInternalError     = "INTERNAL_ERROR"
	ErrCodeNotImplemented    = "NOT_IMPLEMENTED"
)

// Sentinel errors — each maps to one error code.
var (
	ErrNotFound          = errors.New("not found")
	ErrInvalidInput      = errors.New("invalid input")
	ErrInvalidTransition = errors.New("invalid transition")
	ErrImmutable         = errors.New("immutable")
	ErrForbidden         = errors.New("forbidden")
	ErrConflict          = errors.New("conflict")
	ErrPublishConflict   = errors.New("publish conflict")
	ErrLineageReserved   = errors.New("lineage reserved")
	ErrUnavailable       = errors.New("unavailable")
	ErrInternal          = errors.New("internal error")
	ErrNotImplemented    = errors.New("not implemented")
)

// APIError is the structured JSON error envelope returned by all API endpoints.
// The Code field serializes as "error" in JSON to match the API contract.
type APIError struct {
	Code    string         `json:"error"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

// Error implements the error interface, returning the message.
func (e *APIError) Error() string {
	return e.Message
}

// writeError writes a structured JSON error response. If err is an *APIError,
// its Error, Message, and Details fields are used directly. Otherwise, errorCode
// is called to map the error to a code string.
func writeError(w http.ResponseWriter, status int, err error) {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		writeJSON(w, status, apiErr)
		return
	}

	writeJSON(w, status, &APIError{
		Code:    errorCode(err),
		Message: err.Error(),
	})
}

// writeJSON sets Content-Type to application/json, writes the HTTP status code,
// and encodes v as JSON into the response body. Encode errors are ignored.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// errorCode maps an error to its corresponding error code string using errors.Is.
// More specific sentinel errors (ErrPublishConflict, ErrLineageReserved) are checked
// before the general ErrConflict. Unknown errors default to ErrCodeInternalError.
func errorCode(err error) string {
	switch {
	case errors.Is(err, ErrNotFound):
		return ErrCodeNotFound
	case errors.Is(err, ErrInvalidInput):
		return ErrCodeInvalidInput
	case errors.Is(err, ErrInvalidTransition):
		return ErrCodeInvalidTransition
	case errors.Is(err, ErrImmutable):
		return ErrCodeImmutable
	case errors.Is(err, ErrForbidden):
		return ErrCodeForbidden
	case errors.Is(err, ErrPublishConflict):
		return ErrCodePublishConflict
	case errors.Is(err, ErrLineageReserved):
		return ErrCodeLineageReserved
	case errors.Is(err, ErrConflict):
		return ErrCodeConflict
	case errors.Is(err, ErrUnavailable):
		return ErrCodeUnavailable
	case errors.Is(err, ErrInternal):
		return ErrCodeInternalError
	case errors.Is(err, ErrNotImplemented):
		return ErrCodeNotImplemented
	default:
		return ErrCodeInternalError
	}
}

// errorStatus maps an error to its corresponding HTTP status code using errors.Is.
// More specific sentinel errors (ErrPublishConflict, ErrLineageReserved) are checked
// before the general ErrConflict. Unknown errors default to 500 Internal Server Error.
func errorStatus(err error) int {
	switch {
	case errors.Is(err, ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, ErrInvalidInput):
		return http.StatusBadRequest
	case errors.Is(err, ErrForbidden):
		return http.StatusForbidden
	case errors.Is(err, ErrPublishConflict):
		return http.StatusConflict
	case errors.Is(err, ErrLineageReserved):
		return http.StatusConflict
	case errors.Is(err, ErrInvalidTransition):
		return http.StatusConflict
	case errors.Is(err, ErrImmutable):
		return http.StatusConflict
	case errors.Is(err, ErrConflict):
		return http.StatusConflict
	case errors.Is(err, ErrUnavailable):
		return http.StatusServiceUnavailable
	case errors.Is(err, ErrNotImplemented):
		return http.StatusNotImplemented
	case errors.Is(err, ErrInternal):
		return http.StatusInternalServerError
	default:
		return http.StatusInternalServerError
	}
}

// WriteError is the exported version of writeError. It writes a structured JSON
// error response and is intended for use by packages outside internal/api.
func WriteError(w http.ResponseWriter, status int, err error) {
	writeError(w, status, err)
}

// WriteJSON is the exported version of writeJSON. It sets Content-Type to
// application/json, writes the HTTP status code, and encodes v as JSON.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	writeJSON(w, status, v)
}

// ErrorCode is the exported version of errorCode. It maps an error to its
// corresponding error code string.
func ErrorCode(err error) string {
	return errorCode(err)
}

// ErrorStatus is the exported version of errorStatus. It maps an error to its
// corresponding HTTP status code.
func ErrorStatus(err error) int {
	return errorStatus(err)
}
