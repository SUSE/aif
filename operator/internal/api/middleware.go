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
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"mime"
	"net/http"
	"strings"
)

type contextKey string

const requestIDKey contextKey = "request-id"

// RequestIDFromContext retrieves the request ID stamped by RequestIDMiddleware.
func RequestIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey).(string); ok {
		return id
	}
	return ""
}

// RequestIDMiddleware stamps X-Request-ID on every request/response.
func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" || len(id) > 64 || strings.ContainsFunc(id, func(c rune) bool { return c < 0x20 || c > 0x7e }) {
			b := make([]byte, 8)
			_, _ = rand.Read(b)
			id = hex.EncodeToString(b)
		}
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey, id)))
	})
}

const (
	// rancherSessionCookie is Rancher's session cookie. Its presence marks a
	// browser (cookie-authenticated) request.
	rancherSessionCookie = "R_SESS"
	// rancherCSRFCookie and rancherCSRFHeader are Rancher's double-submit
	// CSRF pair: the UI echoes the cookie value in the header on writes.
	rancherCSRFCookie = "CSRF"
	rancherCSRFHeader = "X-Api-Csrf"
)

// isWriteMethod reports whether method may change state. It is deny by
// default: anything other than GET, HEAD and OPTIONS counts as a write.
// Methods are case-sensitive (RFC 9110), so "patch" or "PROPFIND" are writes.
func isWriteMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	}
	return true
}

// RequireCSRFForCookieWrites enforces Rancher's double-submit CSRF check on
// cookie-authenticated writes, which Rancher itself does not enforce on
// aggregated paths. A write (any method other than GET, HEAD and OPTIONS,
// see isWriteMethod) carrying an R_SESS cookie
// must send X-Api-Csrf equal to the (non-empty) CSRF cookie, or it gets 403.
// Requests without a session cookie (bearer-token automation) and safe
// methods pass through. It must run before IdentityFromImpersonation, which
// strips the Cookie header. Cookie and header values are never logged or
// echoed.
func RequireCSRFForCookieWrites(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isWriteMethod(r.Method) {
			if _, err := r.Cookie(rancherSessionCookie); err == nil && !csrfTokenMatches(r) {
				writeError(w, http.StatusForbidden, &APIError{Code: ErrCodeCSRF, Message: "missing or invalid CSRF token"})
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// csrfTokenMatches reports whether the X-Api-Csrf header is non-empty and
// equal to the CSRF cookie, compared in constant time.
func csrfTokenMatches(r *http.Request) bool {
	header := r.Header.Get(rancherCSRFHeader)
	if header == "" {
		return false
	}
	cookie, err := r.Cookie(rancherCSRFCookie)
	if err != nil || cookie.Value == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(header), []byte(cookie.Value)) == 1
}

// RequireJSONForWrites rejects writes (any method other than GET, HEAD and
// OPTIONS, see isWriteMethod) whose
// Content-Type media type is not application/json (parameters such as
// charset are allowed) with 415. Browsers cannot send that content type
// cross-origin without a CORS preflight, and the API sends no CORS headers,
// so this blocks cross-site writes riding on the user's session. Safe
// methods pass through. A DELETE without a body must still send the header.
func RequireJSONForWrites(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isWriteMethod(r.Method) {
			mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if err != nil || mediaType != "application/json" {
				writeError(w, http.StatusUnsupportedMediaType, &APIError{
					Code:    ErrCodeUnsupportedMediaType,
					Message: "write requests must use Content-Type: application/json",
				})
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// Chain wraps handler with request-ID middleware. The UI calls the API
// same-origin through the Rancher proxy, so no CORS headers are needed.
func Chain(handler http.Handler) http.Handler {
	return RequestIDMiddleware(handler)
}

// AggregationPathPrefix is the path Rancher's APIService routes to the
// operator. Rancher delivers the full path (/v1/aif-operator/api/v1/...),
// so the operator strips it before routing.
const AggregationPathPrefix = "/v1/aif-operator"

// NewAggregatedHandler wraps mux for serving over Rancher's aggregation
// tunnel. Outermost first: request ID, strip AggregationPathPrefix (paths
// outside AggregationPathPrefix + "/" get 404), the cookie CSRF check (which
// reads Cookie, so it must run before the identity middleware strips it), the
// JSON content-type check for writes, then the caller identity from Rancher's
// Impersonate-* headers (401 without one).
func NewAggregatedHandler(mux http.Handler) http.Handler {
	return Chain(requireAggregationPrefix(http.StripPrefix(AggregationPathPrefix,
		RequireCSRFForCookieWrites(RequireJSONForWrites(IdentityFromImpersonation(mux))))))
}

// requireAggregationPrefix answers 404 unless the path is below
// AggregationPathPrefix on a segment boundary, so /v1/aif-operatorx never
// reaches StripPrefix (which would hand the mux a relative path it
// redirects).
func requireAggregationPrefix(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, AggregationPathPrefix+"/") {
			writeError(w, http.StatusNotFound, &APIError{Code: ErrCodeNotFound, Message: "not found"})
			return
		}
		next.ServeHTTP(w, r)
	})
}
