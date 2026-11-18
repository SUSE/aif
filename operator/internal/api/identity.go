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
	"net/http"
	"net/url"
	"slices"
	"strings"

	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/client-go/transport"
)

const userKey contextKey = "user"

// impersonateHeaderPrefix matches every Impersonate-* header (User, Uid,
// Group, Extra-*) in canonical form.
const impersonateHeaderPrefix = "Impersonate-"

// rancherErrorPrincipal is the user and group Rancher's impersonating proxy
// forwards for requests it could not authenticate (observed on Rancher 2.13
// and 2.15).
const rancherErrorPrincipal = "system:cattle:error"

// unauthenticatedUsers and unauthenticatedGroups mark identities that must
// never reach a handler.
var (
	unauthenticatedUsers  = []string{user.Anonymous, user.AllUnauthenticated, rancherErrorPrincipal}
	unauthenticatedGroups = []string{user.AllUnauthenticated, rancherErrorPrincipal}
)

// UserFrom returns the caller identity stored by IdentityFromImpersonation.
func UserFrom(ctx context.Context) (user.Info, bool) {
	u, ok := ctx.Value(userKey).(user.Info)
	return u, ok
}

// IdentityFromImpersonation reads the caller identity Rancher's aggregation
// proxy forwards in the Impersonate-User, Impersonate-Group and
// Impersonate-Extra-<key> headers into a user.Info available through
// UserFrom. It then strips every Impersonate-* header, plus Cookie and
// Authorization (Rancher forwards the user's session and bearer token), so
// no handler can see them.
//
// A request with no Impersonate-User (what Rancher sends for an invalid
// bearer token) or with an unauthenticated user or group (what Rancher sends
// for a request without a session) gets 401.
//
// Trusting these headers is safe only because the API is served solely
// through Rancher's aggregation tunnel, which overwrites any client-supplied
// Impersonate-* headers.
func IdentityFromImpersonation(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		info := identityFromHeaders(r.Header)
		stripCredentialHeaders(r.Header)
		if !isAuthenticated(info) {
			writeError(w, http.StatusUnauthorized, &APIError{Code: ErrCodeUnauthorized, Message: "authentication required"})
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey, user.Info(info))))
	})
}

func identityFromHeaders(h http.Header) *user.DefaultInfo {
	info := &user.DefaultInfo{
		// Impersonate-Uid is deliberately ignored: Rancher never sets it,
		// and nothing consumes a UID. It is still stripped below.
		Name:   h.Get(transport.ImpersonateUserHeader),
		Groups: h.Values(transport.ImpersonateGroupHeader),
	}
	for name, values := range h {
		name = http.CanonicalHeaderKey(name)
		if !strings.HasPrefix(name, transport.ImpersonateUserExtraHeaderPrefix) {
			continue
		}
		// Same decoding as the Kubernetes apiserver: lowercase, then
		// percent-decode, keeping a malformed key verbatim.
		key := unescapeExtraKey(strings.ToLower(name[len(transport.ImpersonateUserExtraHeaderPrefix):]))
		if info.Extra == nil {
			info.Extra = map[string][]string{}
		}
		info.Extra[key] = append(info.Extra[key], values...)
	}
	return info
}

func unescapeExtraKey(encodedKey string) string {
	key, err := url.PathUnescape(encodedKey)
	if err != nil {
		return encodedKey
	}
	return key
}

func isAuthenticated(info user.Info) bool {
	if info.GetName() == "" || slices.Contains(unauthenticatedUsers, info.GetName()) {
		return false
	}
	return !slices.ContainsFunc(info.GetGroups(), func(g string) bool {
		return slices.Contains(unauthenticatedGroups, g)
	})
}

func stripCredentialHeaders(h http.Header) {
	for name := range h {
		if strings.HasPrefix(http.CanonicalHeaderKey(name), impersonateHeaderPrefix) {
			delete(h, name)
		}
	}
	h.Del("Cookie")
	h.Del("Authorization")
}
