package bundle

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"sync"
)

// Precompiled DNS-1123 regex to avoid repeated compilation on each validation call
var dns1123Regex = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`)

type manager struct {
	logger *slog.Logger
	mu     sync.RWMutex
	cache  map[string]Bundle // key: "namespace/name"
}

// New creates a new Bundle manager
func New(logger *slog.Logger) Manager {
	return &manager{
		logger: logger,
		cache:  make(map[string]Bundle),
	}
}

// Upsert validates and stores a Bundle in the cache
func (m *manager) Upsert(ctx context.Context, b Bundle) error {
	// Validate spec
	if err := m.validateSpec(b); err != nil {
		return err
	}

	// Store in cache
	m.mu.Lock()
	defer m.mu.Unlock()
	key := fmt.Sprintf("%s/%s", b.Namespace, b.Name)
	m.cache[key] = b

	m.logger.Info("bundle upserted", "namespace", b.Namespace, "name", b.Name)
	return nil
}

// Get retrieves a Bundle from cache
func (m *manager) Get(ctx context.Context, namespace, name string) (Bundle, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	key := fmt.Sprintf("%s/%s", namespace, name)
	b, ok := m.cache[key]
	return b, ok
}

// validateSpec validates Bundle spec fields
func (m *manager) validateSpec(b Bundle) error {
	// UseCase enum validation
	validUseCases := map[string]bool{
		"rag":         true,
		"vision":      true,
		"fine-tuning": true,
		"inference":   true,
		"other":       true,
	}
	if !validUseCases[b.UseCase] {
		return fmt.Errorf("invalid useCase: %s", b.UseCase)
	}

	// TargetBlueprint DNS-1123 validation
	if !dns1123Regex.MatchString(b.TargetBlueprint) {
		return fmt.Errorf("targetBlueprint must be DNS-1123 format")
	}
	if len(b.TargetBlueprint) > 253 {
		return fmt.Errorf("targetBlueprint exceeds maximum length of 253 characters")
	}

	// Components non-empty validation
	if len(b.Components) == 0 {
		return fmt.Errorf("components must not be empty")
	}

	return nil
}
