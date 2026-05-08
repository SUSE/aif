package apps

// Compile-time assertions that the canonical types and ports exist with
// the expected shape. These will fail to compile until interface.go and
// types.go are populated; that compile failure IS the failing test
// (TDD red) for Layer 1 of the P2-3 plan.

import (
	"context"
	"testing"
)

// Catalog port shape (4 methods — within ISP target).
var _ Catalog = catalogShapeCheck{}

type catalogShapeCheck struct{}

func (catalogShapeCheck) List(_ context.Context, _ ListOpts) ([]App, error) { return nil, nil }
func (catalogShapeCheck) Get(_ context.Context, _ string) (App, error)      { return App{}, nil }
func (catalogShapeCheck) Refresh(_ context.Context) error                   { return nil }
func (catalogShapeCheck) UpdateSettings(_ EngineSettings)                   {}

// Source port shape (4 methods — within ISP target).
var _ Source = sourceShapeCheck{}

type sourceShapeCheck struct{}

func (sourceShapeCheck) Name() string                          { return "" }
func (sourceShapeCheck) List(_ context.Context) ([]App, error) { return nil, nil }
func (sourceShapeCheck) Refresh(_ context.Context) error       { return nil }
func (sourceShapeCheck) UpdateSettings(_ EngineSettings)       {}

// TestPortsAreSatisfiedBySanityImpls is a no-op test whose presence
// causes the compile-time assertions above to be evaluated by the test
// binary. If the ports drift, this file stops compiling.
func TestPortsAreSatisfiedBySanityImpls(t *testing.T) {
	t.Helper()
}
