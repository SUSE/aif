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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// aggregatedRecorder is a mux with one GET and one POST route that records
// what reached it, wrapped in NewAggregatedHandler.
type aggregatedRecorder struct {
	reached bool
	path    string
	user    string
	header  http.Header
}

func (rec *aggregatedRecorder) handler() http.Handler {
	mux := http.NewServeMux()
	record := func(w http.ResponseWriter, r *http.Request) {
		rec.reached = true
		rec.path = r.URL.Path
		rec.header = r.Header.Clone()
		if u, ok := UserFrom(r.Context()); ok {
			rec.user = u.GetName()
		}
		w.WriteHeader(http.StatusOK)
	}
	mux.HandleFunc("GET /api/v1/version", record)
	mux.HandleFunc("POST /api/v1/settings", record)
	return NewAggregatedHandler(mux)
}

func serveAggregated(req *http.Request) (*httptest.ResponseRecorder, *aggregatedRecorder) {
	rec := &aggregatedRecorder{}
	w := httptest.NewRecorder()
	rec.handler().ServeHTTP(w, req)
	return w, rec
}

func TestAggregatedHandler_RejectsRequestWithoutIdentity(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, AggregationPathPrefix+"/api/v1/version", nil)
	w, rec := serveAggregated(req)

	assertErrorCode(t, w, http.StatusUnauthorized, ErrCodeUnauthorized)
	if rec.reached {
		t.Error("handler ran without an identity")
	}
	if w.Header().Get("X-Request-ID") == "" {
		t.Error("request ID middleware is not outermost: no X-Request-ID on the 401")
	}
}

func TestAggregatedHandler_RejectsRancherUnauthenticatedIdentity(t *testing.T) {
	// What Rancher forwards for a request without a session (observed on
	// Rancher 2.13 and 2.15).
	req := httptest.NewRequest(http.MethodGet, AggregationPathPrefix+"/api/v1/version", nil)
	req.Header.Set("Impersonate-User", "system:cattle:error")
	req.Header.Add("Impersonate-Group", "system:unauthenticated")
	req.Header.Add("Impersonate-Group", "system:cattle:error")
	w, rec := serveAggregated(req)

	assertErrorCode(t, w, http.StatusUnauthorized, ErrCodeUnauthorized)
	if rec.reached {
		t.Error("handler ran for an unauthenticated identity")
	}
}

func TestAggregatedHandler_ServesAuthenticatedRequestWithPrefixStripped(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, AggregationPathPrefix+"/api/v1/version?x=1", nil)
	rancherAdminHeaders(req.Header)
	req.Header.Set("Cookie", "R_SESS=secret; CSRF=abc")
	req.Header.Set("Authorization", "Bearer secret")
	w, rec := serveAggregated(req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", w.Code, w.Body.String())
	}
	if !rec.reached {
		t.Fatal("handler did not run")
	}
	if rec.path != "/api/v1/version" {
		t.Errorf("handler saw path %q, want the prefix stripped", rec.path)
	}
	if rec.user != "user-bgmz8" {
		t.Errorf("handler saw user %q, want user-bgmz8", rec.user)
	}
	for _, h := range []string{"Cookie", "Authorization", "Impersonate-User"} {
		if rec.header.Get(h) != "" {
			t.Errorf("handler saw credential header %s", h)
		}
	}
}

func TestAggregatedHandler_PathWithoutPrefixIsNotFound(t *testing.T) {
	for _, path := range []string{"/api/v1/version", "/v1/aif-operatorx/api/v1/version", "/v1/other/api/v1/version"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rancherAdminHeaders(req.Header)
			w, rec := serveAggregated(req)

			if w.Code != http.StatusNotFound {
				t.Errorf("status = %d, want 404", w.Code)
			}
			if rec.reached {
				t.Error("handler ran for a path outside the aggregation prefix")
			}
		})
	}
}

func TestAggregatedHandler_CookieWriteWithoutCSRFIsForbidden(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, AggregationPathPrefix+"/api/v1/settings", strings.NewReader(`{}`))
	rancherAdminHeaders(req.Header)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", "R_SESS=secret; CSRF=abc")
	w, rec := serveAggregated(req)

	assertErrorCode(t, w, http.StatusForbidden, ErrCodeCSRF)
	if rec.reached {
		t.Error("handler ran for a cookie write without CSRF header")
	}
}

func TestAggregatedHandler_CookieWriteWithCSRFIsServed(t *testing.T) {
	// Proves the CSRF check runs before IdentityFromImpersonation strips Cookie.
	req := httptest.NewRequest(http.MethodPost, AggregationPathPrefix+"/api/v1/settings", strings.NewReader(`{}`))
	rancherAdminHeaders(req.Header)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", "R_SESS=secret; CSRF=abc")
	req.Header.Set("X-Api-Csrf", "abc")
	w, rec := serveAggregated(req)

	if w.Code != http.StatusOK || !rec.reached {
		t.Fatalf("status = %d, reached = %v; want 200 and the handler to run", w.Code, rec.reached)
	}
}

func TestAggregatedHandler_NonJSONWriteIsUnsupported(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, AggregationPathPrefix+"/api/v1/settings", strings.NewReader(`{}`))
	rancherAdminHeaders(req.Header)
	req.Header.Set("Content-Type", "text/plain")
	w, rec := serveAggregated(req)

	assertErrorCode(t, w, http.StatusUnsupportedMediaType, ErrCodeUnsupportedMediaType)
	if rec.reached {
		t.Error("handler ran for a text/plain write")
	}
}
