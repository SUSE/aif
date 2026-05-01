package bundle

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"

	aifv1 "github.com/SUSE/aif/api/v1alpha1"
)

func TestManager_Upsert_InvalidUseCase(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	mgr := New(logger)

	bundle := Bundle{
		Namespace:       "test-ns",
		Name:            "test-bundle",
		TargetBlueprint: "test-blueprint",
		UseCase:         "invalid-usecase", // Invalid
		Components:      []aifv1.ComponentRef{{Name: "test"}},
	}

	err := mgr.Upsert(context.Background(), bundle)
	if err == nil {
		t.Fatal("expected error for invalid useCase, got nil")
	}
	if err.Error() != "invalid useCase: invalid-usecase" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestManager_Upsert_InvalidTargetBlueprint(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	mgr := New(logger)

	testCases := []string{
		"",                  // Empty string
		"Test-Blueprint",    // Uppercase not allowed
		"-invalid",          // Cannot start with hyphen
		"invalid-",          // Cannot end with hyphen
		"invalid_.name",     // Underscore not allowed
		"invalid name",      // Space not allowed
		"INVALID",           // All uppercase
	}

	for _, invalidName := range testCases {
		bundle := Bundle{
			Namespace:       "test-ns",
			Name:            "test-bundle",
			TargetBlueprint: invalidName,
			UseCase:         "rag",
			Components:      []aifv1.ComponentRef{{Name: "test"}},
		}

		err := mgr.Upsert(context.Background(), bundle)
		if err == nil {
			t.Fatalf("expected error for invalid targetBlueprint %q, got nil", invalidName)
		}
		if err.Error() != "targetBlueprint must be DNS-1123 format" {
			t.Errorf("unexpected error message for %q: %v", invalidName, err)
		}
	}
}

func TestManager_Upsert_InvalidTargetBlueprint_ExceedsMaxLength(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	mgr := New(logger)

	bundle := Bundle{
		Namespace:       "test-ns",
		Name:            "test-bundle",
		TargetBlueprint: strings.Repeat("a", 254), // Exceeds 253 character limit
		UseCase:         "rag",
		Components:      []aifv1.ComponentRef{{Name: "test"}},
	}

	err := mgr.Upsert(context.Background(), bundle)
	if err == nil {
		t.Fatal("expected error for targetBlueprint exceeding max length, got nil")
	}
	if err.Error() != "targetBlueprint exceeds maximum length of 253 characters" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestManager_Upsert_EmptyComponents(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	mgr := New(logger)

	bundle := Bundle{
		Namespace:       "test-ns",
		Name:            "test-bundle",
		TargetBlueprint: "test-blueprint",
		UseCase:         "rag",
		Components:      []aifv1.ComponentRef{}, // Empty
	}

	err := mgr.Upsert(context.Background(), bundle)
	if err == nil {
		t.Fatal("expected error for empty components, got nil")
	}
	if err.Error() != "components must not be empty" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestManager_Upsert_ValidBundle(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	mgr := New(logger)

	bundle := Bundle{
		Namespace:       "test-ns",
		Name:            "test-bundle",
		TargetBlueprint: "test-blueprint",
		UseCase:         "rag",
		Components:      []aifv1.ComponentRef{{Name: "test"}},
	}

	err := mgr.Upsert(context.Background(), bundle)
	if err != nil {
		t.Fatalf("expected no error for valid bundle, got: %v", err)
	}
}

func TestManager_Get(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	mgr := New(logger)

	bundle := Bundle{
		Namespace:       "test-ns",
		Name:            "test-bundle",
		TargetBlueprint: "test-blueprint",
		UseCase:         "rag",
		Components:      []aifv1.ComponentRef{{Name: "test"}},
	}

	// Upsert the bundle
	err := mgr.Upsert(context.Background(), bundle)
	if err != nil {
		t.Fatalf("failed to upsert bundle: %v", err)
	}

	// Retrieve it
	retrieved, ok := mgr.Get(context.Background(), "test-ns", "test-bundle")
	if !ok {
		t.Fatal("expected to retrieve bundle from cache, got not found")
	}

	if retrieved.Name != bundle.Name {
		t.Errorf("expected name %q, got %q", bundle.Name, retrieved.Name)
	}
	if retrieved.Namespace != bundle.Namespace {
		t.Errorf("expected namespace %q, got %q", bundle.Namespace, retrieved.Namespace)
	}

	// Try to retrieve non-existent bundle
	_, ok = mgr.Get(context.Background(), "non-existent-ns", "non-existent-bundle")
	if ok {
		t.Fatal("expected not to find non-existent bundle, but found it")
	}
}

func TestManager_Concurrent(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	mgr := New(logger)

	// Create multiple bundles concurrently
	errs := make(chan error, 10)
	for i := 0; i < 10; i++ {
		go func(index int) {
			bundle := Bundle{
				Namespace:       "test-ns",
				Name:            fmt.Sprintf("bundle-%d", index),
				TargetBlueprint: "test-blueprint",
				UseCase:         "rag",
				Components:      []aifv1.ComponentRef{{Name: "test"}},
			}
			errs <- mgr.Upsert(context.Background(), bundle)
		}(i)
	}

	// Collect results
	for i := 0; i < 10; i++ {
		err := <-errs
		if err != nil {
			t.Fatalf("concurrent upsert failed: %v", err)
		}
	}

	// Verify all bundles were stored
	for i := 0; i < 10; i++ {
		_, ok := mgr.Get(context.Background(), "test-ns", fmt.Sprintf("bundle-%d", i))
		if !ok {
			t.Errorf("bundle-%d not found in cache", i)
		}
	}
}

