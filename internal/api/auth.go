package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	authorizationv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// ExtractUser extracts the calling user and groups from the request headers.
// It checks Impersonate-User first, falling back to X-Rancher-User.
// Groups are sorted for deterministic ordering (e.g., for cache keys).
func ExtractUser(r *http.Request) (user string, groups []string) {
	user = r.Header.Get("Impersonate-User")
	if user == "" {
		user = r.Header.Get("X-Rancher-User")
	}
	groups = r.Header.Values("Impersonate-Group")
	sort.Strings(groups)
	return
}

// AuthChecker abstracts authorization checks so that controllers and tests
// can swap in different implementations.
//
// CheckPublisher is the legacy bundle-publish path (kept for compatibility
// with publish.go). CheckResource is the general verb+resource check used
// by the workload CRUD endpoints; downstream handlers call it directly when
// they need per-namespace authorization tailored to the request (e.g.
// create's namespace comes from the request body, not the URL path). The
// API group is a parameter so the checker can serve resources outside
// ai.suse.com (e.g. apiextensions.k8s.io) without further interface churn.
type AuthChecker interface {
	CheckPublisher(ctx context.Context, user string, groups []string) (bool, error)
	CheckResource(ctx context.Context, user string, groups []string, group, namespace, verb, resource string) (bool, error)
}

// AuthMiddleware provides HTTP middleware methods for authorization.
type AuthMiddleware struct {
	checker AuthChecker
}

// NewAuthMiddleware creates an AuthMiddleware backed by the given checker.
func NewAuthMiddleware(checker AuthChecker) *AuthMiddleware {
	return &AuthMiddleware{checker: checker}
}

// errInsufficientPermissions is returned when a user lacks the publisher role.
// Reserved for RequirePublisher — it names the role to bind. Generic CRUD
// denials go through errResourceAccessDenied instead.
var errInsufficientPermissions = &APIError{
	Code:    ErrCodeForbidden,
	Message: "requires aif-blueprint-publisher role; ask your cluster admin to bind you to the role",
}

// errResourceAccessDenied is the generic SAR-deny envelope used by
// RequireResource and inline CheckResource call sites. Keeping it separate
// from errInsufficientPermissions prevents a workload-RBAC denial from
// telling the user to chase the bundle-publisher role.
var errResourceAccessDenied = &APIError{
	Code:    ErrCodeForbidden,
	Message: "forbidden: insufficient permissions for the requested action",
}

// RequirePublisher returns middleware that checks whether the calling user has
// the aif-blueprint-publisher role before invoking the next handler.
func (m *AuthMiddleware) RequirePublisher(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, groups := ExtractUser(r)
		if user == "" {
			writeError(w, http.StatusForbidden, errors.New("authentication required"))
			return
		}

		allowed, err := m.checker.CheckPublisher(r.Context(), user, groups)
		if err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Errorf("authorization check failed: %w", err))
			return
		}

		if !allowed {
			writeError(w, http.StatusForbidden, errInsufficientPermissions)
			return
		}

		next(w, r)
	}
}

// ResourceSelector extracts the namespace for a SAR check from the incoming
// request. The same middleware works for path-based routes ({namespace}/{name})
// and query-based routes (?namespace=...). Routes whose namespace lives in the
// request body (e.g. create) should call checker.CheckResource directly inside
// the handler after decoding the body.
type ResourceSelector func(r *http.Request) (namespace string)

// RequireResource returns middleware that performs a SAR
// (group, verb, resource, namespace) before invoking next. Namespace is
// computed per request via selector.
func (m *AuthMiddleware) RequireResource(group, verb, resource string, selector ResourceSelector, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, groups := ExtractUser(r)
		if user == "" {
			writeError(w, http.StatusForbidden, errors.New("authentication required"))
			return
		}
		ns := ""
		if selector != nil {
			ns = selector(r)
		}
		allowed, err := m.checker.CheckResource(r.Context(), user, groups, group, ns, verb, resource)
		if err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Errorf("authorization check failed: %w", err))
			return
		}
		if !allowed {
			writeError(w, http.StatusForbidden, errResourceAccessDenied)
			return
		}
		next(w, r)
	}
}

// cacheEntry holds a cached authorization result with its timestamp.
type cacheEntry struct {
	allowed bool
	at      time.Time
}

// cacheTTL is the duration for which cached authorization results are valid.
const cacheTTL = 30 * time.Second

// errCacheMiss is returned when no valid cache entry exists for the user.
var errCacheMiss = errors.New("cache miss")

// SARAuthChecker checks publisher authorization by creating a SubjectAccessReview
// against the Kubernetes API. Results are cached for 30 seconds.
type SARAuthChecker struct {
	client kubernetes.Interface
	cache  sync.Map
}

// NewSARAuthChecker creates a SARAuthChecker backed by the given Kubernetes client.
func NewSARAuthChecker(client kubernetes.Interface) *SARAuthChecker {
	return &SARAuthChecker{client: client}
}

// cacheKey builds a deterministic cache key from user, sorted groups, API
// group, and (for CheckResource) verb/resource/namespace. CheckPublisher
// passes "ai.suse.com" for group and empty strings for verb/resource/
// namespace.
//
// The "|" separator is collision-free in practice: K8s username/group/
// namespace validation rejects pipes (RFC 1123 / DNS-1123 subdomains), and
// verb, resource, and API group come from closed enumerations. If a custom
// auth proxy is ever wired in that accepts pipes in usernames, the join
// scheme here MUST move to a length-prefixed or escaped encoding.
func cacheKey(user string, groups []string, group, verb, resource, namespace string) string {
	return user + "|" + strings.Join(groups, ",") + "|" + group + "|" + verb + "|" + resource + "|" + namespace
}

// checkCache returns the cached result if within TTL, or errCacheMiss otherwise.
func (s *SARAuthChecker) checkCache(user string, groups []string, group, verb, resource, namespace string) (bool, error) {
	key := cacheKey(user, groups, group, verb, resource, namespace)
	val, ok := s.cache.Load(key)
	if !ok {
		return false, errCacheMiss
	}

	entry, ok := val.(cacheEntry)
	if !ok {
		return false, errCacheMiss
	}

	if time.Since(entry.at) > cacheTTL {
		return false, errCacheMiss
	}

	return entry.allowed, nil
}

// CheckPublisher checks whether the user is allowed to perform publisher actions.
// It creates a SubjectAccessReview for verb "update" on resource "bundles"
// subresource "approve" in group "ai.suse.com". Results are cached; errors are not.
func (s *SARAuthChecker) CheckPublisher(ctx context.Context, user string, groups []string) (bool, error) {
	// Check cache first.
	allowed, err := s.checkCache(user, groups, "ai.suse.com", "", "", "")
	if err == nil {
		return allowed, nil
	}

	// Cache miss — perform SAR.
	sar := &authorizationv1.SubjectAccessReview{
		Spec: authorizationv1.SubjectAccessReviewSpec{
			User:   user,
			Groups: groups,
			ResourceAttributes: &authorizationv1.ResourceAttributes{
				Verb:        "update",
				Group:       "ai.suse.com",
				Resource:    "bundles",
				Subresource: "approve",
			},
		},
	}

	result, err := s.client.AuthorizationV1().SubjectAccessReviews().Create(ctx, sar, metav1.CreateOptions{})
	if err != nil {
		// Do not cache errors.
		return false, fmt.Errorf("SubjectAccessReview: %w", err)
	}

	// Cache the result.
	key := cacheKey(user, groups, "ai.suse.com", "", "", "")
	s.cache.Store(key, cacheEntry{
		allowed: result.Status.Allowed,
		at:      time.Now(),
	})

	return result.Status.Allowed, nil
}

// CheckResource checks whether the user may perform verb on resource (in
// the given API group) within namespace. Empty namespace means
// cluster-scoped (e.g. for list across all namespaces). Results are cached
// for cacheTTL keyed by user+groups+group+verb+resource+namespace so
// different verbs / groups / namespaces on the same resource don't collide.
func (s *SARAuthChecker) CheckResource(ctx context.Context, user string, groups []string, group, namespace, verb, resource string) (bool, error) {
	allowed, err := s.checkCache(user, groups, group, verb, resource, namespace)
	if err == nil {
		return allowed, nil
	}

	sar := &authorizationv1.SubjectAccessReview{
		Spec: authorizationv1.SubjectAccessReviewSpec{
			User:   user,
			Groups: groups,
			ResourceAttributes: &authorizationv1.ResourceAttributes{
				Verb:      verb,
				Group:     group,
				Resource:  resource,
				Namespace: namespace,
			},
		},
	}

	result, err := s.client.AuthorizationV1().SubjectAccessReviews().Create(ctx, sar, metav1.CreateOptions{})
	if err != nil {
		return false, fmt.Errorf("SubjectAccessReview: %w", err)
	}

	key := cacheKey(user, groups, group, verb, resource, namespace)
	s.cache.Store(key, cacheEntry{
		allowed: result.Status.Allowed,
		at:      time.Now(),
	})

	return result.Status.Allowed, nil
}
