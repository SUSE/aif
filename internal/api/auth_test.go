package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeAuthChecker implements AuthChecker for testing.
//
// allowed/err drive CheckPublisher; resourceAllowed/resourceErr drive
// CheckResource. resourceCalls records each (group, namespace, verb,
// resource) invocation so tests can assert what the middleware asked.
type fakeAuthChecker struct {
	allowed bool
	err     error
	calls   int

	resourceAllowed bool
	resourceErr     error
	resourceCalls   []resourceCall
}

type resourceCall struct {
	user, group, verb, resource, namespace string
	groups                                 []string
}

func (f *fakeAuthChecker) CheckPublisher(_ context.Context, _ string, _ []string) (bool, error) {
	f.calls++
	return f.allowed, f.err
}

func (f *fakeAuthChecker) CheckResource(_ context.Context, user string, groups []string, group, namespace, verb, resource string) (bool, error) {
	f.resourceCalls = append(f.resourceCalls, resourceCall{
		user: user, groups: groups, group: group, namespace: namespace, verb: verb, resource: resource,
	})
	return f.resourceAllowed, f.resourceErr
}

func TestExtractUser_ImpersonateHeader(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Impersonate-User", "alice")
	req.Header.Add("Impersonate-Group", "devs")
	req.Header.Add("Impersonate-Group", "admins")

	user, groups := ExtractUser(req)

	assert.Equal(t, "alice", user)
	assert.Equal(t, []string{"admins", "devs"}, groups)
}

func TestExtractUser_RancherFallback(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Rancher-User", "bob")

	user, groups := ExtractUser(req)

	assert.Equal(t, "bob", user)
	assert.Empty(t, groups)
}

func TestExtractUser_NoHeaders(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	user, groups := ExtractUser(req)

	assert.Equal(t, "", user)
	assert.Empty(t, groups)
}

func TestExtractUser_SortsGroups(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Impersonate-User", "charlie")
	req.Header.Add("Impersonate-Group", "zebra")
	req.Header.Add("Impersonate-Group", "alpha")
	req.Header.Add("Impersonate-Group", "middle")

	user, groups := ExtractUser(req)

	assert.Equal(t, "charlie", user)
	assert.Equal(t, []string{"alpha", "middle", "zebra"}, groups)
}

func TestRequirePublisher_Allowed(t *testing.T) {
	checker := &fakeAuthChecker{allowed: true}
	mw := NewAuthMiddleware(checker)

	handlerCalled := false
	next := func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/bundles/ns/name/submit", nil)
	req.Header.Set("Impersonate-User", "alice")
	w := httptest.NewRecorder()

	mw.RequirePublisher(next)(w, req)

	assert.True(t, handlerCalled)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, 1, checker.calls)
}

func TestRequirePublisher_Denied(t *testing.T) {
	checker := &fakeAuthChecker{allowed: false}
	mw := NewAuthMiddleware(checker)

	handlerCalled := false
	next := func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/bundles/ns/name/submit", nil)
	req.Header.Set("Impersonate-User", "alice")
	w := httptest.NewRecorder()

	mw.RequirePublisher(next)(w, req)

	assert.False(t, handlerCalled)
	assert.Equal(t, http.StatusForbidden, w.Code)

	var apiErr APIError
	require.NoError(t, json.NewDecoder(w.Body).Decode(&apiErr))
	assert.Contains(t, apiErr.Message, "requires aif-blueprint-publisher role")
}

func TestRequirePublisher_NoUser(t *testing.T) {
	checker := &fakeAuthChecker{allowed: true}
	mw := NewAuthMiddleware(checker)

	handlerCalled := false
	next := func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/bundles/ns/name/submit", nil)
	w := httptest.NewRecorder()

	mw.RequirePublisher(next)(w, req)

	assert.False(t, handlerCalled)
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Equal(t, 0, checker.calls, "checker should not be called when no user")

	var apiErr APIError
	require.NoError(t, json.NewDecoder(w.Body).Decode(&apiErr))
	assert.Contains(t, apiErr.Message, "authentication required")
}

func TestRequirePublisher_CheckerError(t *testing.T) {
	checker := &fakeAuthChecker{err: errors.New("k8s unavailable")}
	mw := NewAuthMiddleware(checker)

	handlerCalled := false
	next := func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/bundles/ns/name/submit", nil)
	req.Header.Set("Impersonate-User", "alice")
	w := httptest.NewRecorder()

	mw.RequirePublisher(next)(w, req)

	assert.False(t, handlerCalled)
	assert.Equal(t, http.StatusInternalServerError, w.Code)

	var apiErr APIError
	require.NoError(t, json.NewDecoder(w.Body).Decode(&apiErr))
	assert.Contains(t, apiErr.Message, "authorization check failed")
}

func TestRequireResource_Allowed(t *testing.T) {
	checker := &fakeAuthChecker{resourceAllowed: true}
	mw := NewAuthMiddleware(checker)

	handlerCalled := false
	next := func(w http.ResponseWriter, r *http.Request) { handlerCalled = true }
	sel := func(r *http.Request) string { return "team-a" }
	h := mw.RequireResource("ai.suse.com", "delete", "workloads", sel, next)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/workloads/team-a/wl", nil)
	req.Header.Set("Impersonate-User", "alice")
	w := httptest.NewRecorder()
	h(w, req)

	assert.True(t, handlerCalled)
	assert.Equal(t, http.StatusOK, w.Code)
	if assert.Len(t, checker.resourceCalls, 1) {
		assert.Equal(t, "team-a", checker.resourceCalls[0].namespace)
		assert.Equal(t, "delete", checker.resourceCalls[0].verb)
		assert.Equal(t, "workloads", checker.resourceCalls[0].resource)
	}
}

func TestRequireResource_Denied(t *testing.T) {
	checker := &fakeAuthChecker{resourceAllowed: false}
	mw := NewAuthMiddleware(checker)

	handlerCalled := false
	next := func(w http.ResponseWriter, r *http.Request) { handlerCalled = true }
	sel := func(r *http.Request) string { return "team-a" }
	h := mw.RequireResource("ai.suse.com", "delete", "workloads", sel, next)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/workloads/team-a/wl", nil)
	req.Header.Set("Impersonate-User", "alice")
	w := httptest.NewRecorder()
	h(w, req)

	assert.False(t, handlerCalled)
	assert.Equal(t, http.StatusForbidden, w.Code)

	// Generic SAR denials MUST NOT leak the publisher-role message — that
	// would tell a workload-RBAC-denied user to chase the wrong binding.
	var apiErr APIError
	require.NoError(t, json.NewDecoder(w.Body).Decode(&apiErr))
	assert.NotContains(t, apiErr.Message, "aif-blueprint-publisher")
	assert.Contains(t, apiErr.Message, "insufficient permissions")
}

func TestRequireResource_NoUser(t *testing.T) {
	checker := &fakeAuthChecker{resourceAllowed: true}
	mw := NewAuthMiddleware(checker)

	handlerCalled := false
	next := func(w http.ResponseWriter, r *http.Request) { handlerCalled = true }
	sel := func(r *http.Request) string { return "team-a" }
	h := mw.RequireResource("ai.suse.com", "delete", "workloads", sel, next)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/workloads/team-a/wl", nil)
	w := httptest.NewRecorder()
	h(w, req)

	assert.False(t, handlerCalled)
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Len(t, checker.resourceCalls, 0)
}

func TestRequireResource_CheckerError(t *testing.T) {
	checker := &fakeAuthChecker{resourceErr: errors.New("k8s down")}
	mw := NewAuthMiddleware(checker)

	handlerCalled := false
	next := func(w http.ResponseWriter, r *http.Request) { handlerCalled = true }
	sel := func(r *http.Request) string { return "team-a" }
	h := mw.RequireResource("ai.suse.com", "delete", "workloads", sel, next)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/workloads/team-a/wl", nil)
	req.Header.Set("Impersonate-User", "alice")
	w := httptest.NewRecorder()
	h(w, req)

	assert.False(t, handlerCalled)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestSARAuthChecker_CacheTTL(t *testing.T) {
	checker := &SARAuthChecker{}

	// Manually store a cache entry that is fresh (within 30s TTL).
	key := cacheKey("alice", []string{"admins", "devs"}, "ai.suse.com", "", "", "")
	checker.cache.Store(key, cacheEntry{
		allowed: true,
		at:      time.Now(),
	})

	// Should hit cache.
	allowed, err := checker.checkCache("alice", []string{"admins", "devs"}, "ai.suse.com", "", "", "")
	require.NoError(t, err)
	assert.True(t, allowed)

	// Store an expired entry (31 seconds ago).
	checker.cache.Store(key, cacheEntry{
		allowed: true,
		at:      time.Now().Add(-31 * time.Second),
	})

	// Should miss cache.
	_, err = checker.checkCache("alice", []string{"admins", "devs"}, "ai.suse.com", "", "", "")
	assert.ErrorIs(t, err, errCacheMiss)
}

func TestSARAuthChecker_CacheKey(t *testing.T) {
	pubKey := cacheKey("alice", []string{"admins", "devs"}, "ai.suse.com", "", "", "")
	assert.Equal(t, "alice|admins,devs|ai.suse.com|||", pubKey)
	// Different verbs on the same resource must yield distinct cache keys so
	// e.g. "get workloads in team-a" cannot leak into "delete workloads in team-a".
	getKey := cacheKey("alice", []string{"admins"}, "ai.suse.com", "get", "workloads", "team-a")
	deleteKey := cacheKey("alice", []string{"admins"}, "ai.suse.com", "delete", "workloads", "team-a")
	assert.NotEqual(t, getKey, deleteKey)
	// Different API groups on the same verb/resource/namespace must also
	// yield distinct keys.
	aifKey := cacheKey("alice", []string{"admins"}, "ai.suse.com", "get", "workloads", "team-a")
	apiextKey := cacheKey("alice", []string{"admins"}, "apiextensions.k8s.io", "get", "workloads", "team-a")
	assert.NotEqual(t, aifKey, apiextKey)
}
