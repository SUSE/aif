/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
)

const (
	ErrCodeNotFound     = "NOT_FOUND"
	ErrCodeInvalidInput = "INVALID_INPUT"
	ErrCodeInternal     = "INTERNAL_ERROR"
)

var (
	ErrNotFound     = errors.New("not found")
	ErrInvalidInput = errors.New("invalid input")
)

// APIError is the structured JSON error envelope returned by all endpoints.
// Code serialises as "error" to match the API contract.
type APIError struct {
	Code    string `json:"error"`
	Message string `json:"message"`
}

func (e *APIError) Error() string { return e.Message }

// writeJSON sets Content-Type, writes the status code, and encodes v as JSON.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("api: failed to encode response: %v", err)
	}
}

// writeError writes a structured JSON error response.
func writeError(w http.ResponseWriter, status int, err error) {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		writeJSON(w, status, apiErr)
		return
	}
	code := ErrCodeInternal
	switch {
	case errors.Is(err, ErrNotFound):
		code = ErrCodeNotFound
	case errors.Is(err, ErrInvalidInput):
		code = ErrCodeInvalidInput
	}
	writeJSON(w, status, &APIError{Code: code, Message: err.Error()})
}
