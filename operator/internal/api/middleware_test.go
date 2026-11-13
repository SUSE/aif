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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestChain_NoCORSHeaders(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/api/v1/settings", nil)
	req.Header.Set("Origin", "https://evil.example")
	w := httptest.NewRecorder()
	Chain(inner).ServeHTTP(w, req)

	for _, h := range []string{
		"Access-Control-Allow-Origin",
		"Access-Control-Allow-Methods",
		"Access-Control-Allow-Headers",
	} {
		if v := w.Header().Get(h); v != "" {
			t.Errorf("expected no %s header, got %q", h, v)
		}
	}
}

func TestChain_OptionsNotShortCircuited(t *testing.T) {
	reached := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusMethodNotAllowed)
	})

	req := httptest.NewRequest("OPTIONS", "/x", nil)
	w := httptest.NewRecorder()
	Chain(inner).ServeHTTP(w, req)

	if !reached {
		t.Fatalf("expected OPTIONS to reach the inner handler, got status %d", w.Code)
	}
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected inner handler status 405, got %d", w.Code)
	}
}

func TestChain_SetsRequestID(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/api/v1/version", nil)
	w := httptest.NewRecorder()
	Chain(inner).ServeHTTP(w, req)

	if id := w.Header().Get("X-Request-ID"); id == "" {
		t.Error("expected non-empty X-Request-ID header")
	}
}

// writeMethods includes a lowercase "patch" and an unusual method: anything
// other than GET, HEAD and OPTIONS is a write (deny by default).
const methodPropfind = "PROPFIND"

var writeMethods = []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, "patch", methodPropfind}

// serveGuard runs mw around a recording inner handler and reports whether
// the inner handler was reached.
func serveGuard(mw func(http.Handler) http.Handler, req *http.Request) (*httptest.ResponseRecorder, bool) {
	reached := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	})
	w := httptest.NewRecorder()
	mw(inner).ServeHTTP(w, req)
	return w, reached
}

func assertErrorCode(t *testing.T, w *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if w.Code != status {
		t.Fatalf("status = %d, want %d", w.Code, status)
	}
	var body APIError
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v (%s)", err, w.Body.String())
	}
	if body.Code != code {
		t.Errorf("error = %q, want %q", body.Code, code)
	}
}

func TestRequireJSON_RejectsSimpleContentTypes(t *testing.T) {
	for _, method := range writeMethods {
		for _, ct := range []string{
			"",
			"text/plain",
			"text/plain; charset=utf-8",
			"application/x-www-form-urlencoded",
			"multipart/form-data; boundary=x",
			"application/jsonx",
			"application/json-patch+json",
			"not a media type;;",
		} {
			t.Run(method+" "+ct, func(t *testing.T) {
				req := httptest.NewRequest(method, "/api/v1/settings", strings.NewReader(`{}`))
				if ct != "" {
					req.Header.Set("Content-Type", ct)
				}
				w, reached := serveGuard(RequireJSONForWrites, req)
				if reached {
					t.Fatal("inner handler must not be called")
				}
				assertErrorCode(t, w, http.StatusUnsupportedMediaType, ErrCodeUnsupportedMediaType)
			})
		}
	}
}

func TestRequireJSON_AllowsJSON(t *testing.T) {
	for _, method := range writeMethods {
		for _, ct := range []string{"application/json", "application/json; charset=utf-8", "Application/JSON"} {
			t.Run(method+" "+ct, func(t *testing.T) {
				req := httptest.NewRequest(method, "/api/v1/settings", strings.NewReader(`{}`))
				req.Header.Set("Content-Type", ct)
				if w, reached := serveGuard(RequireJSONForWrites, req); !reached {
					t.Fatalf("inner handler not called, status %d", w.Code)
				}
			})
		}
	}
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		t.Run(method+" without content type", func(t *testing.T) {
			req := httptest.NewRequest(method, "/api/v1/settings", nil)
			if w, reached := serveGuard(RequireJSONForWrites, req); !reached {
				t.Fatalf("inner handler not called, status %d", w.Code)
			}
		})
	}
}

func TestCSRF_CookieWriteRequiresMatchingHeader(t *testing.T) {
	const csrfValue = "abc"
	const sessionCookie = "R_SESS=x; CSRF=" + csrfValue
	cases := []struct {
		name   string
		method string
		cookie string
		header string
		pass   bool
	}{
		{"POST cookie no header", http.MethodPost, sessionCookie, "", false},
		{"POST cookie matching header", http.MethodPost, sessionCookie, csrfValue, true},
		{"POST cookie wrong header", http.MethodPost, sessionCookie, "zzz", false},
		{"POST cookie header prefix", http.MethodPost, sessionCookie, "ab", false},
		{"PUT cookie no header", http.MethodPut, sessionCookie, "", false},
		{"PATCH cookie wrong header", http.MethodPatch, sessionCookie, "zzz", false},
		{"DELETE cookie no header", http.MethodDelete, sessionCookie, "", false},
		{"DELETE cookie matching header", http.MethodDelete, sessionCookie, csrfValue, true},
		{"lowercase patch cookie no header", "patch", sessionCookie, "", false},
		{"PROPFIND cookie no header", methodPropfind, sessionCookie, "", false},
		{"PROPFIND cookie matching header", methodPropfind, sessionCookie, csrfValue, true},
		{"POST session without CSRF cookie", http.MethodPost, "R_SESS=x", "", false},
		{"POST session with empty CSRF cookie and empty header", http.MethodPost, "R_SESS=x; CSRF=", "", false},
		{"GET cookie no header", http.MethodGet, sessionCookie, "", true},
		{"HEAD cookie no header", http.MethodHead, sessionCookie, "", true},
		{"OPTIONS cookie no header", http.MethodOptions, sessionCookie, "", true},
		{"POST bearer no cookie no header", http.MethodPost, "", "", true},
		{"POST other cookies only", http.MethodPost, "CSRF=abc; R_LOCALE=en", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, "/api/v1/settings", nil)
			if tc.cookie != "" {
				req.Header.Set("Cookie", tc.cookie)
			}
			if tc.header != "" {
				req.Header.Set("X-Api-Csrf", tc.header)
			}
			w, reached := serveGuard(RequireCSRFForCookieWrites, req)
			if reached != tc.pass {
				t.Fatalf("reached = %v, want %v (status %d)", reached, tc.pass, w.Code)
			}
			if !tc.pass {
				assertErrorCode(t, w, http.StatusForbidden, ErrCodeCSRF)
				if strings.Contains(w.Body.String(), csrfValue) {
					t.Error("response body echoes the CSRF value")
				}
			}
		})
	}
}
