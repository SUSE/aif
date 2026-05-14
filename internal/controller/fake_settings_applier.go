package controller

import (
	"context"
	"sync"
)

// FakeSettingsApplier is a recording SettingsApplier for tests. Append-only;
// safe under -race. Used by reconciler tests in settings_controller_test.go
// to assert that Apply was called with the right SettingsSnapshot, without
// pulling in real engine refs.
type FakeSettingsApplier struct {
	mu    sync.Mutex
	Calls []SettingsSnapshot

	// ApplyErr, if set, is returned from Apply (overrides the default nil).
	// Lets tests exercise the reconciler's Apply-error branch.
	ApplyErr error
}

func (f *FakeSettingsApplier) Apply(_ context.Context, s SettingsSnapshot) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, s)
	return f.ApplyErr
}

// Reset clears the recorded calls. Used by Ginkgo BeforeEach to isolate
// tests when multiple Describe blocks share one suite-level applier.
func (f *FakeSettingsApplier) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = nil
	f.ApplyErr = nil
}

// Snapshot returns a copy of Calls for safe reads by test assertions.
// Protects against data races when the reconciler is appending concurrently.
func (f *FakeSettingsApplier) Snapshot() []SettingsSnapshot {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]SettingsSnapshot, len(f.Calls))
	copy(out, f.Calls)
	return out
}
