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
// because validation depends only on the input — no Manager state is used.
// Prefer this over Manager.ValidateSpec in new code.
func Validate(bp *aifv1.Blueprint) error {
	version := bp.Spec.Version
	if version == "" {
		return fmt.Errorf("version is required")
	}
	if !strictVersionPattern.MatchString(version) {
		return fmt.Errorf("invalid semver version: %s", version)
	}
	if !semver.IsValid("v" + version) {
		return fmt.Errorf("invalid semver version: %s", version)
	}
	if bp.Spec.Source.Type != aifv1.BlueprintSourcePublished &&
		bp.Spec.Source.Type != aifv1.BlueprintSourceWrapsVendorChart {
		return fmt.Errorf("invalid source.type: %s (must be Published or WrapsVendorChart)", bp.Spec.Source.Type)
	}
	return nil
}

// ValidateSpec is a thin shim preserved for the Manager interface; the receiver
// is unused. New callers should use the free function Validate.
func (m *manager) ValidateSpec(bp *aifv1.Blueprint) error {
	return Validate(bp)
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
