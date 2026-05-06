package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteError_Format(t *testing.T) {
	w := httptest.NewRecorder()
	writeError(w, http.StatusNotFound, fmt.Errorf("resource missing: %w", ErrNotFound))

	resp := w.Result()
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))

	var apiErr APIError
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
	assert.Equal(t, ErrCodeNotFound, apiErr.Code)
	assert.Equal(t, "resource missing: not found", apiErr.Message)
	assert.Nil(t, apiErr.Details)
}

func TestWriteError_SentinelError(t *testing.T) {
	tests := []struct {
		name     string
		sentinel error
		wantCode string
	}{
		{"NotFound", ErrNotFound, ErrCodeNotFound},
		{"InvalidInput", ErrInvalidInput, ErrCodeInvalidInput},
		{"InvalidTransition", ErrInvalidTransition, ErrCodeInvalidTransition},
		{"Immutable", ErrImmutable, ErrCodeImmutable},
		{"Forbidden", ErrForbidden, ErrCodeForbidden},
		{"Conflict", ErrConflict, ErrCodeConflict},
		{"PublishConflict", ErrPublishConflict, ErrCodePublishConflict},
		{"LineageReserved", ErrLineageReserved, ErrCodeLineageReserved},
		{"Internal", ErrInternal, ErrCodeInternalError},
		{"NotImplemented", ErrNotImplemented, ErrCodeNotImplemented},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			wrapped := fmt.Errorf("ctx: %w", tt.sentinel)
			writeError(w, http.StatusInternalServerError, wrapped)

			var apiErr APIError
			require.NoError(t, json.NewDecoder(w.Body).Decode(&apiErr))
			assert.Equal(t, tt.wantCode, apiErr.Code)
		})
	}
}

func TestWriteError_APIError(t *testing.T) {
	w := httptest.NewRecorder()
	apiErr := &APIError{
		Code:    ErrCodeConflict,
		Message: "bundle already submitted",
		Details: map[string]any{"bundle": "test-bundle"},
	}
	writeError(w, http.StatusConflict, apiErr)

	resp := w.Result()
	defer resp.Body.Close()

	assert.Equal(t, http.StatusConflict, resp.StatusCode)

	var got APIError
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	assert.Equal(t, ErrCodeConflict, got.Code)
	assert.Equal(t, "bundle already submitted", got.Message)
	assert.Equal(t, "test-bundle", got.Details["bundle"])
}

func TestWriteError_ContentType(t *testing.T) {
	w := httptest.NewRecorder()
	writeError(w, http.StatusBadRequest, ErrInvalidInput)

	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
}

func TestWriteJSON_Success(t *testing.T) {
	type payload struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}

	w := httptest.NewRecorder()
	writeJSON(w, http.StatusCreated, payload{Name: "test", Count: 42})

	resp := w.Result()
	defer resp.Body.Close()

	assert.Equal(t, http.StatusCreated, resp.StatusCode)
	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))

	var got payload
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	assert.Equal(t, "test", got.Name)
	assert.Equal(t, 42, got.Count)
}

func TestErrorCode_AllSentinels(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantCode string
	}{
		{"NotFound", ErrNotFound, ErrCodeNotFound},
		{"InvalidInput", ErrInvalidInput, ErrCodeInvalidInput},
		{"InvalidTransition", ErrInvalidTransition, ErrCodeInvalidTransition},
		{"Immutable", ErrImmutable, ErrCodeImmutable},
		{"Forbidden", ErrForbidden, ErrCodeForbidden},
		{"Conflict", ErrConflict, ErrCodeConflict},
		{"PublishConflict", ErrPublishConflict, ErrCodePublishConflict},
		{"LineageReserved", ErrLineageReserved, ErrCodeLineageReserved},
		{"Internal", ErrInternal, ErrCodeInternalError},
		{"NotImplemented", ErrNotImplemented, ErrCodeNotImplemented},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantCode, errorCode(tt.err))
		})
	}
}

func TestErrorCode_UnknownError(t *testing.T) {
	assert.Equal(t, ErrCodeInternalError, errorCode(errors.New("something unexpected")))
}

func TestErrorStatus_AllSentinels(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{"NotFound", ErrNotFound, http.StatusNotFound},
		{"InvalidInput", ErrInvalidInput, http.StatusBadRequest},
		{"InvalidTransition", ErrInvalidTransition, http.StatusConflict},
		{"Immutable", ErrImmutable, http.StatusConflict},
		{"Forbidden", ErrForbidden, http.StatusForbidden},
		{"Conflict", ErrConflict, http.StatusConflict},
		{"PublishConflict", ErrPublishConflict, http.StatusConflict},
		{"LineageReserved", ErrLineageReserved, http.StatusConflict},
		{"Internal", ErrInternal, http.StatusInternalServerError},
		{"NotImplemented", ErrNotImplemented, http.StatusNotImplemented},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantStatus, errorStatus(tt.err))
		})
	}
}

func TestErrorStatus_UnknownError(t *testing.T) {
	assert.Equal(t, http.StatusInternalServerError, errorStatus(errors.New("something unexpected")))
}

func TestErrorStatus_WrappedSentinel(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{"WrappedNotFound", fmt.Errorf("user %d: %w", 42, ErrNotFound), http.StatusNotFound},
		{"WrappedForbidden", fmt.Errorf("action denied: %w", ErrForbidden), http.StatusForbidden},
		{"WrappedPublishConflict", fmt.Errorf("bundle xyz: %w", ErrPublishConflict), http.StatusConflict},
		{"DoubleWrapped", fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", ErrImmutable)), http.StatusConflict},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantStatus, errorStatus(tt.err))
		})
	}
}
