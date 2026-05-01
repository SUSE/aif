package bundle

import "context"

// Manager handles Bundle business logic and in-memory caching
type Manager interface {
	// Upsert validates and stores a Bundle in the in-memory cache
	// Returns error if validation fails
	Upsert(ctx context.Context, b Bundle) error

	// Get retrieves a Bundle from cache (for self-healing logic)
	Get(ctx context.Context, namespace, name string) (Bundle, bool)
}

// Note: P3-1 will expand this interface with Create, List, Update, Delete, ListPendingReview
