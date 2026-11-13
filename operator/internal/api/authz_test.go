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
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"

	authorizationv1 "k8s.io/api/authorization/v1"
	"k8s.io/apiserver/pkg/authentication/user"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

// testUser is the identity the handler tests run as.
var testUser = &user.DefaultInfo{
	Name:   "u-abc",
	Groups: []string{"system:authenticated", "system:cattle:authenticated"},
	Extra:  map[string][]string{"principalid": {"local://u-abc"}, "username": {"alice"}},
}

// sarClient returns a fake client whose SubjectAccessReview creates are
// answered by decide. Each reviewed SAR is appended to *seen when seen is
// non-nil.
func sarClient(seen *[]*authorizationv1.SubjectAccessReview, decide func(*authorizationv1.SubjectAccessReview) (bool, error)) client.Client {
	return fake.NewClientBuilder().WithInterceptorFuncs(interceptor.Funcs{
		Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			sar, ok := obj.(*authorizationv1.SubjectAccessReview)
			if !ok {
				return c.Create(ctx, obj, opts...)
			}
			if seen != nil {
				*seen = append(*seen, sar.DeepCopy())
			}
			allowed, err := decide(sar)
			if err != nil {
				return err
			}
			sar.Status.Allowed = allowed
			return nil
		},
	}).Build()
}

// newTestMux registers handlers through a real Authorizer with DefaultRules
// whose SubjectAccessReviews are always allowed, and runs every request as
// testUser. It lets handler tests exercise routes without exercising authz.
func newTestMux(handlers ...Handler) http.Handler {
	a := NewAuthorizer(sarClient(nil, func(*authorizationv1.SubjectAccessReview) (bool, error) { return true, nil }), DefaultRules(testNS))
	mux := http.NewServeMux()
	for _, h := range handlers {
		h.Register(a, mux)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mux.ServeHTTP(w, withUser(r, testUser))
	})
}

func withUser(r *http.Request, u user.Info) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), userKey, u))
}

// concretePath turns a route pattern into a method and request path.
func concretePath(pattern string) (string, string) {
	method, path, _ := strings.Cut(pattern, " ")
	path = strings.ReplaceAll(path, "{namespace}", "team-a")
	path = strings.ReplaceAll(path, "{name}", "obj-1")
	return method, path
}

func TestAuthz_DefaultRules(t *testing.T) {
	const opNS = "aif-operator"
	// Expected SAR attributes per route, written out independently of
	// DefaultRules, so a change to a rule must be made in both places.
	want := map[string]authorizationv1.ResourceAttributes{
		"GET /api/v1/version":                                             {Verb: verbGet, Resource: resourceSettings, Namespace: opNS},
		"GET /api/v1/settings":                                            {Verb: verbGet, Resource: resourceSettings, Namespace: opNS},
		"PUT /api/v1/settings":                                            {Verb: verbUpdate, Resource: resourceSettings, Namespace: opNS},
		"GET /api/v1/settings/registry-status":                            {Verb: verbGet, Resource: resourceSettings, Namespace: opNS},
		"POST /api/v1/settings/validate-credentials":                      {Verb: verbUpdate, Resource: resourceSettings, Namespace: opNS},
		"POST /api/v1/settings/validate-chart-access":                     {Verb: verbUpdate, Resource: resourceSettings, Namespace: opNS},
		"POST /api/v1/git/publish":                                        {Verb: verbUpdate, Resource: resourceSettings, Namespace: opNS},
		"GET /api/v1/catalog":                                             {Verb: verbGet, Resource: resourceSettings, Namespace: opNS},
		"GET /api/v1/blueprint-catalogs":                                  {Verb: verbList, Resource: resourceBlueprintCatalogs},
		"GET /api/v1/blueprints":                                          {Verb: verbList, Resource: resourceBlueprints},
		"GET /api/v1/blueprints/{name}":                                   {Verb: verbGet, Resource: resourceBlueprints, Name: "obj-1"},
		"POST /api/v1/blueprints":                                         {Verb: verbCreate, Resource: resourceBlueprints},
		"PUT /api/v1/blueprints/{name}":                                   {Verb: verbUpdate, Resource: resourceBlueprints, Name: "obj-1"},
		"DELETE /api/v1/blueprints/{name}":                                {Verb: verbDelete, Resource: resourceBlueprints, Name: "obj-1"},
		"GET /api/v1/aiworkloads":                                         {Verb: verbList, Resource: resourceAIWorkloads},
		"POST /api/v1/namespaces/{namespace}/aiworkloads":                 {Verb: verbCreate, Resource: resourceAIWorkloads, Namespace: "team-a"},
		"PATCH /api/v1/namespaces/{namespace}/aiworkloads/{name}":         {Verb: verbUpdate, Resource: resourceAIWorkloads, Namespace: "team-a", Name: "obj-1"},
		"DELETE /api/v1/namespaces/{namespace}/aiworkloads/{name}":        {Verb: verbDelete, Resource: resourceAIWorkloads, Namespace: "team-a", Name: "obj-1"},
		"POST /api/v1/namespaces/{namespace}/aiworkloads/{name}/upgrade":  {Verb: verbPatch, Resource: resourceAIWorkloads, Namespace: "team-a", Name: "obj-1"},
		"POST /api/v1/namespaces/{namespace}/aiworkloads/{name}/rollback": {Verb: verbPatch, Resource: resourceAIWorkloads, Namespace: "team-a", Name: "obj-1"},
		"POST /api/v1/namespaces/{namespace}/aiworkloads/{name}/retry":    {Verb: verbPatch, Resource: resourceAIWorkloads, Namespace: "team-a", Name: "obj-1"},
	}
	rules := DefaultRules(opNS)
	if len(rules) != len(want) {
		t.Fatalf("DefaultRules has %d rules, want %d", len(rules), len(want))
	}

	for pattern, attrs := range want {
		for _, allowed := range []bool{true, false} {
			t.Run(pattern, func(t *testing.T) {
				var seen []*authorizationv1.SubjectAccessReview
				a := NewAuthorizer(sarClient(&seen, func(*authorizationv1.SubjectAccessReview) (bool, error) { return allowed, nil }), rules)
				mux := http.NewServeMux()
				called := false
				a.HandleFunc(mux, pattern, func(w http.ResponseWriter, _ *http.Request) {
					called = true
					w.WriteHeader(http.StatusTeapot)
				})

				method, path := concretePath(pattern)
				w := httptest.NewRecorder()
				mux.ServeHTTP(w, withUser(httptest.NewRequest(method, path, nil), testUser))

				if len(seen) != 1 {
					t.Fatalf("expected 1 SAR, got %d", len(seen))
				}
				spec := seen[0].Spec
				wantAttrs := attrs
				wantAttrs.Group = "ai-factory.suse.com"
				wantAttrs.Version = "v1alpha1"
				if spec.ResourceAttributes == nil || *spec.ResourceAttributes != wantAttrs {
					t.Errorf("resource attributes = %+v, want %+v", spec.ResourceAttributes, wantAttrs)
				}
				if spec.User != "u-abc" || !reflect.DeepEqual(spec.Groups, testUser.Groups) {
					t.Errorf("user/groups = %q %v", spec.User, spec.Groups)
				}
				wantExtra := map[string]authorizationv1.ExtraValue{
					"principalid": {"local://u-abc"}, "username": {"alice"},
				}
				if !reflect.DeepEqual(spec.Extra, wantExtra) {
					t.Errorf("extra = %v, want %v", spec.Extra, wantExtra)
				}
				if spec.UID != "" || spec.NonResourceAttributes != nil {
					t.Errorf("UID or non-resource attributes set: %+v", spec)
				}

				if allowed {
					if !called || w.Code != http.StatusTeapot {
						t.Errorf("allowed: called=%v code=%d", called, w.Code)
					}
				} else if called || w.Code != http.StatusForbidden {
					t.Errorf("denied: called=%v code=%d", called, w.Code)
				}
			})
		}
	}
}

func TestAuthz_SARErrorFailsClosed(t *testing.T) {
	a := NewAuthorizer(sarClient(nil, func(*authorizationv1.SubjectAccessReview) (bool, error) {
		return false, errors.New("apiserver unavailable")
	}), DefaultRules(testNS))
	mux := http.NewServeMux()
	called := false
	a.HandleFunc(mux, "GET /api/v1/settings", func(http.ResponseWriter, *http.Request) { called = true })

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, withUser(httptest.NewRequest("GET", "/api/v1/settings", nil), testUser))

	if w.Code != http.StatusInternalServerError || called {
		t.Fatalf("code=%d called=%v, want 500 and no handler call", w.Code, called)
	}
	if strings.Contains(w.Body.String(), "apiserver unavailable") {
		t.Errorf("internal error leaked: %s", w.Body.String())
	}
}

func TestAuthz_NoUserIsUnauthorized(t *testing.T) {
	var seen []*authorizationv1.SubjectAccessReview
	a := NewAuthorizer(sarClient(&seen, func(*authorizationv1.SubjectAccessReview) (bool, error) { return true, nil }), DefaultRules(testNS))
	mux := http.NewServeMux()
	called := false
	a.HandleFunc(mux, "GET /api/v1/settings", func(http.ResponseWriter, *http.Request) { called = true })

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/settings", nil))

	if w.Code != http.StatusUnauthorized || called || len(seen) != 0 {
		t.Fatalf("code=%d called=%v sars=%d, want 401, no handler, no SAR", w.Code, called, len(seen))
	}
}

func TestAuthz_PanicsOnUnknownPattern(t *testing.T) {
	a := NewAuthorizer(sarClient(nil, nil), DefaultRules(testNS))
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for a pattern without a rule")
		}
	}()
	a.HandleFunc(http.NewServeMux(), "GET /api/v1/nope", func(http.ResponseWriter, *http.Request) {})
}

func TestAuthz_AllRoutesCovered(t *testing.T) {
	rules := DefaultRules(testNS)
	a := NewAuthorizer(sarClient(nil, nil), rules)
	mux := http.NewServeMux()
	c := fake.NewClientBuilder().Build()
	handlers := []Handler{
		NewSettingsHandler(c, testNS),
		NewBlueprintHandler(c),
		NewAIWorkloadHandler(c),
		NewVersionHandler("v", "c", "cv"),
		NewCatalogHandler(c, testNS),
		NewBlueprintCatalogHandler(c),
	}
	for _, h := range handlers {
		h.Register(a, mux) // panics on any route without a rule
	}

	registered := a.registered
	for pattern := range rules {
		if !slices.Contains(registered, pattern) {
			t.Errorf("stale rule: %q is not registered by any handler", pattern)
		}
	}
	if len(registered) != len(rules) {
		t.Errorf("registered %d routes, have %d rules", len(registered), len(rules))
	}
}

func TestAuthz_ForbiddenBodyHasNoData(t *testing.T) {
	c := sarClient(nil, func(*authorizationv1.SubjectAccessReview) (bool, error) { return false, nil })
	a := NewAuthorizer(c, DefaultRules(testNS))
	mux := http.NewServeMux()
	NewSettingsHandler(c, testNS).Register(a, mux)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, withUser(httptest.NewRequest("GET", "/api/v1/settings", nil), testUser))

	if w.Code != http.StatusForbidden {
		t.Fatalf("code=%d, want 403", w.Code)
	}
	if got, want := strings.TrimSpace(w.Body.String()), `{"error":"FORBIDDEN","message":"forbidden"}`; got != want {
		t.Errorf("body = %s, want %s", got, want)
	}
}
