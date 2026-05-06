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
type AuthChecker interface {
	CheckPublisher(ctx context.Context, user string, groups []string) (bool, error)
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
var errInsufficientPermissions = &APIError{
	Code:    ErrCodeForbidden,
	Message: "requires aif-blueprint-publisher role; ask your cluster admin to bind you to the role",
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

// cacheKey builds a deterministic cache key from user and sorted groups.
func cacheKey(user string, groups []string) string {
	return user + "|" + strings.Join(groups, ",")
}

// checkCache returns the cached result if within TTL, or errCacheMiss otherwise.
func (s *SARAuthChecker) checkCache(user string, groups []string) (bool, error) {
	key := cacheKey(user, groups)
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
	allowed, err := s.checkCache(user, groups)
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
	key := cacheKey(user, groups)
	s.cache.Store(key, cacheEntry{
		allowed: result.Status.Allowed,
		at:      time.Now(),
	})

	return result.Status.Allowed, nil
}
