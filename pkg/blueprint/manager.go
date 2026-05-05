package blueprint

import (
	"fmt"
	"log/slog"
	"regexp"

	aifv1 "github.com/SUSE/aif/api/v1alpha1"
	"golang.org/x/mod/semver"
)

type manager struct {
	logger *slog.Logger
}

// New creates a new blueprint manager.
func New(logger *slog.Logger) Manager {
	return &manager{
		logger: logger,
	}
}

// strictVersionPattern enforces exactly three version parts (major.minor.patch),
// with optional prerelease (-xxx) and metadata (+xxx). Compiled once.
var strictVersionPattern = regexp.MustCompile(`^\d+\.\d+\.\d+(?:-[a-zA-Z0-9.-]+)?(?:\+[a-zA-Z0-9.-]+)?$`)

// Validate is the canonical Blueprint spec validator. It is a free function
// over the pure-Go domain Blueprint — no Manager state, no aifv1 import. New
// callers should construct a Blueprint (typically via FromCR) and call this
// directly; Manager.ValidateSpec is preserved as a backward-compat shim.
func Validate(bp Blueprint) error {
	if bp.Version == "" {
		return fmt.Errorf("version is required")
	}
	if !strictVersionPattern.MatchString(bp.Version) {
		return fmt.Errorf("invalid semver version: %s", bp.Version)
	}
	if !semver.IsValid("v" + bp.Version) {
		return fmt.Errorf("invalid semver version: %s", bp.Version)
	}
	if bp.Source.Type != SourceTypePublished && bp.Source.Type != SourceTypeWrapsVendorChart {
		return fmt.Errorf("invalid source.type: %s (must be Published or WrapsVendorChart)", bp.Source.Type)
	}
	return nil
}

// ValidateSpec is a backward-compat shim for the Manager interface; it converts
// the CR to the domain type and delegates to Validate. New callers should call
// Validate directly with a domain Blueprint.
func (m *manager) ValidateSpec(bp *aifv1.Blueprint) error {
	return Validate(FromCR(bp))
}

// ComputeDeploymentCount counts Workloads sourced from this Blueprint
// Implements ARCHITECTURE.md §8.2 snippet exactly
func (m *manager) ComputeDeploymentCount(bp *aifv1.Blueprint, workloads []aifv1.Workload) int32 {
	count := 0
	for _, w := range workloads {
		if w.Spec.Source.Kind == aifv1.WorkloadSourceKindBlueprint &&
			w.Spec.Source.Blueprint != nil &&
			w.Spec.Source.Blueprint.Name == bp.Spec.BlueprintName &&
			w.Spec.Source.Blueprint.Version == bp.Spec.Version {
			count++
		}
	}
	return int32(count)
}
