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
	"reflect"
	"strings"
	"testing"
)

// rancherAdminHeaders sets the headers Rancher's ImpersonatingHandler
// actually forwarded for an authenticated admin (observed on Rancher 2.13).
func rancherAdminHeaders(h http.Header) {
	h.Set("Impersonate-User", "user-bgmz8")
	h.Add("Impersonate-Group", "system:authenticated")
	h.Add("Impersonate-Group", "system:cattle:authenticated")
	h.Set("Impersonate-Extra-Principalid", "local://user-bgmz8")
	h.Set("Impersonate-Extra-Requesthost", "rancher.example")
	h.Set("Impersonate-Extra-Requesttokenid", "token-abcde")
	h.Set("Impersonate-Extra-Username", "admin")
}

func serveIdentity(t *testing.T, req *http.Request) (*httptest.ResponseRecorder, *http.Request) {
	t.Helper()
	var seen *http.Request
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r
		w.WriteHeader(http.StatusOK)
	})
	w := httptest.NewRecorder()
	IdentityFromImpersonation(inner).ServeHTTP(w, req)
	return w, seen
}

func assertNoImpersonateHeaders(t *testing.T, h http.Header) {
	t.Helper()
	for name := range h {
		if strings.HasPrefix(http.CanonicalHeaderKey(name), "Impersonate-") {
			t.Errorf("inner handler saw header %s", name)
		}
	}
}

func TestIdentity_SetsUser(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/settings", nil)
	req.Header.Set("Impersonate-User", "u-abc")
	req.Header.Add("Impersonate-Group", "system:authenticated")
	req.Header.Add("Impersonate-Group", "system:cattle:authenticated")
	req.Header.Set("Impersonate-Extra-Principalid", "local://u-abc")

	w, seen := serveIdentity(t, req)
	if seen == nil {
		t.Fatalf("inner handler not called, status %d: %s", w.Code, w.Body.String())
	}
	u, ok := UserFrom(seen.Context())
	if !ok {
		t.Fatal("UserFrom returned ok=false")
	}
	if got := u.GetName(); got != "u-abc" {
		t.Errorf("name = %q, want u-abc", got)
	}
	wantGroups := []string{"system:authenticated", "system:cattle:authenticated"}
	if got := u.GetGroups(); !reflect.DeepEqual(got, wantGroups) {
		t.Errorf("groups = %v, want %v", got, wantGroups)
	}
	wantExtra := map[string][]string{"principalid": {"local://u-abc"}}
	if got := u.GetExtra(); !reflect.DeepEqual(got, wantExtra) {
		t.Errorf("extra = %v, want %v", got, wantExtra)
	}
	assertNoImpersonateHeaders(t, seen.Header)
}

func TestIdentity_RancherAdminExtras(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/settings", nil)
	rancherAdminHeaders(req.Header)

	_, seen := serveIdentity(t, req)
	if seen == nil {
		t.Fatal("inner handler not called")
	}
	u, _ := UserFrom(seen.Context())
	want := map[string][]string{
		"principalid":    {"local://user-bgmz8"},
		"requesthost":    {"rancher.example"},
		"requesttokenid": {"token-abcde"},
		"username":       {"admin"},
	}
	if got := u.GetExtra(); !reflect.DeepEqual(got, want) {
		t.Errorf("extra = %v, want %v", got, want)
	}
}

func TestIdentity_ExtraKeyDecoding(t *testing.T) {
	// Kubernetes convention: the key is lowercased, then percent-decoded;
	// a malformed escape is kept verbatim. Repeated headers keep all values.
	req := httptest.NewRequest("GET", "/x", nil)
	req.Header.Set("Impersonate-User", "u-abc")
	req.Header["Impersonate-Extra-Example.com%2fScopes"] = []string{"a", "b"}
	req.Header["Impersonate-Extra-Bad%zz"] = []string{"v"}

	_, seen := serveIdentity(t, req)
	if seen == nil {
		t.Fatal("inner handler not called")
	}
	u, _ := UserFrom(seen.Context())
	want := map[string][]string{
		"example.com/scopes": {"a", "b"},
		"bad%zz":             {"v"},
	}
	if got := u.GetExtra(); !reflect.DeepEqual(got, want) {
		t.Errorf("extra = %v, want %v", got, want)
	}
}

func TestIdentity_RejectsMissingUser(t *testing.T) {
	// An invalid bearer token reaches the backend with no Impersonate-*
	// headers at all, only the forwarded Authorization header (observed on
	// Rancher 2.13 and 2.15).
	for name, setup := range map[string]func(http.Header){
		"no headers":         func(http.Header) {},
		"authorization only": func(h http.Header) { h.Set("Authorization", "Bearer bogus") },
		"empty user":         func(h http.Header) { h.Set("Impersonate-User", "") },
		"groups without user": func(h http.Header) {
			h.Add("Impersonate-Group", "system:authenticated")
		},
	} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/v1/settings", nil)
			setup(req.Header)
			w, seen := serveIdentity(t, req)
			if seen != nil {
				t.Fatal("inner handler must not be called")
			}
			assertUnauthorized(t, w)
		})
	}
}

func TestIdentity_RejectsUnauthenticated(t *testing.T) {
	cases := map[string]func(http.Header){
		"user system:unauthenticated": func(h http.Header) {
			h.Set("Impersonate-User", "system:unauthenticated")
		},
		"user system:anonymous": func(h http.Header) {
			h.Set("Impersonate-User", "system:anonymous")
		},
		// What Rancher actually forwards for an unauthenticated request
		// (observed on Rancher 2.13 and 2.15).
		"rancher unauthenticated": func(h http.Header) {
			h.Set("Impersonate-User", "system:cattle:error")
			h.Add("Impersonate-Group", "system:unauthenticated")
			h.Add("Impersonate-Group", "system:cattle:error")
		},
		"user system:cattle:error": func(h http.Header) {
			h.Set("Impersonate-User", "system:cattle:error")
		},
		"group system:unauthenticated": func(h http.Header) {
			h.Set("Impersonate-User", "u-abc")
			h.Add("Impersonate-Group", "system:authenticated")
			h.Add("Impersonate-Group", "system:unauthenticated")
		},
		"group system:cattle:error": func(h http.Header) {
			h.Set("Impersonate-User", "u-abc")
			h.Add("Impersonate-Group", "system:cattle:error")
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/v1/settings", nil)
			setup(req.Header)
			w, seen := serveIdentity(t, req)
			if seen != nil {
				t.Fatal("inner handler must not be called")
			}
			assertUnauthorized(t, w)
		})
	}
}

func TestIdentity_StripsCredentials(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/v1/settings", nil)
	rancherAdminHeaders(req.Header)
	req.Header.Set("Impersonate-Uid", "uid-1")
	req.Header.Set("Cookie", "R_SESS=token-abcde:secret; CSRF=abc")
	req.Header.Set("Authorization", "Bearer token-abcde:secret")
	req.Header.Set("X-Request-ID", "keep-me")

	_, seen := serveIdentity(t, req)
	if seen == nil {
		t.Fatal("inner handler not called")
	}
	assertNoImpersonateHeaders(t, seen.Header)
	for _, h := range []string{"Cookie", "Authorization"} {
		if v := seen.Header.Values(h); len(v) != 0 {
			t.Errorf("inner handler saw %s header", h)
		}
	}
	if len(seen.Cookies()) != 0 {
		t.Error("inner handler can still read cookies")
	}
	if got := seen.Header.Get("X-Request-ID"); got != "keep-me" {
		t.Errorf("unrelated header dropped: X-Request-ID = %q", got)
	}
}

func TestIdentity_IgnoresImpersonateUID(t *testing.T) {
	// Rancher never sets Impersonate-Uid; a client-supplied value must
	// not become the caller's UID, and the header must still be stripped.
	req := httptest.NewRequest("GET", "/api/v1/settings", nil)
	req.Header.Set("Impersonate-User", "u-abc")
	req.Header.Set("Impersonate-Uid", "x")

	_, seen := serveIdentity(t, req)
	if seen == nil {
		t.Fatal("inner handler not called")
	}
	u, _ := UserFrom(seen.Context())
	if got := u.GetUID(); got != "" {
		t.Errorf("uid = %q, want empty", got)
	}
	if v := seen.Header.Values("Impersonate-Uid"); len(v) != 0 {
		t.Error("inner handler saw Impersonate-Uid")
	}
}

func TestIdentity_UserFromWithoutMiddleware(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	if u, ok := UserFrom(req.Context()); ok || u != nil {
		t.Errorf("UserFrom on a bare context = (%v, %v), want (nil, false)", u, ok)
	}
}

func assertUnauthorized(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	var body APIError
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v (%s)", err, w.Body.String())
	}
	if body.Code != ErrCodeUnauthorized {
		t.Errorf("error = %q, want %q", body.Code, ErrCodeUnauthorized)
	}
}
