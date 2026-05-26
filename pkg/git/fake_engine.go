package git

import (
	"context"
	"sync"
)

// FakeEngine is an in-memory Engine for unit tests. Records every Push
// request; configurable error / result.
//
// Thread-safe (the consuming engine may call Push from multiple goroutines).
type FakeEngine struct {
	mu sync.Mutex

	// Pushes records every PushRequest seen, in arrival order.
	Pushes []PushRequest

	// Settings records the last EngineSettings pushed via UpdateSettings.
	Settings EngineSettings

	// PushErr, if non-nil, is returned from Push. PushResult is returned
	// when PushErr is nil.
	PushErr    error
	PushResult PushResult
}

func (f *FakeEngine) Push(_ context.Context, req PushRequest) (PushResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Pushes = append(f.Pushes, req)
	if f.PushErr != nil {
		return PushResult{}, f.PushErr
	}
	return f.PushResult, nil
}

func (f *FakeEngine) UpdateSettings(s EngineSettings) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Settings = s
}
