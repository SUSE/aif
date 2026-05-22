package workload

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"golang.org/x/mod/semver"
)

// workloadStore is the consumer-defined narrow port the Upgrader needs.
// Per CLAUDE.md, "ports live with their consumers — the consuming package
// defines the interface narrowly tailored to what it needs."
//
// All methods speak in domain types and return pkg/workload sentinels; the
// production adapter (internal/workload/UpgradeStore) translates aifv1 +
// apierrors at the wiring layer. This is what keeps this file aifv1-free
// per the package's layering rule.
type workloadStore interface {
	// GetUpgradeView returns the read-only projection Upgrader needs.
	// Returns ErrWorkloadNotFound when the apiserver returns 404.
	GetUpgradeView(ctx context.Context, namespace, name string) (*UpgradeWorkloadView, error)

	// PatchBlueprintVersion bumps spec.source.blueprint.version to
	// newVersion using the resourceVersion captured in view for optimistic
	// concurrency. Returns ErrUpgradeConflict when the apiserver returns
	// 409 (concurrent mutation between GetUpgradeView and patch apply).
	PatchBlueprintVersion(ctx context.Context, view *UpgradeWorkloadView, newVersion string) error
}

// blueprintReader is the consumer-defined narrow port for the upgrade-target
// Blueprint lookup. Returns ErrBlueprintVersionNotFound on 404.
type blueprintReader interface {
	GetForUpgrade(ctx context.Context, name string) (*UpgradeBlueprintView, error)
}

// upgrader is the production Upgrader. It runs the 5 validation rules from
// PROJECT_PLAN.md §P5-3 AC in the order the AC specifies, emits a
// UpgradeStarted event BEFORE the spec patch (audit-before-patch), then
// patches Workload.spec.source.blueprint.version via the merge-patch path
// (optimistic concurrency).
type upgrader struct {
	workloads  workloadStore
	blueprints blueprintReader
	events     UpgradeEventRecorder
	logger     *slog.Logger
}

// NewUpgrader returns an Upgrader bound to the given ports. workloads and
// blueprints are accepted as the narrowest possible ports; the K8s-typed
// adapters live in internal/workload.
func NewUpgrader(workloads workloadStore, blueprints blueprintReader, events UpgradeEventRecorder, logger *slog.Logger) Upgrader {
	return &upgrader{
		workloads:  workloads,
		blueprints: blueprints,
		events:     events,
		logger:     logger,
	}
}

// Upgrade implements the P5-3 workflow. Validation order matches AC §P5-3.
func (u *upgrader) Upgrade(ctx context.Context, namespace, name, toVersion, user string) (UpgradeResult, error) {
	view, err := u.workloads.GetUpgradeView(ctx, namespace, name)
	if err != nil {
		if errors.Is(err, ErrWorkloadNotFound) {
			return UpgradeResult{}, err
		}
		return UpgradeResult{}, fmt.Errorf("get workload %s/%s: %w", namespace, name, err)
	}

	if err := u.validate(ctx, view, toVersion); err != nil {
		return UpgradeResult{}, err
	}

	currentVersion := view.Blueprint.Version
	lineage := view.Blueprint.Name

	// Audit-before-patch (AC line 2004): record the intent before the spec
	// mutation so the audit trail survives a 409 conflict OR any other
	// downstream failure (webhook rejection, apiserver blip, RBAC denial).
	u.events.UpgradeStarted(ctx, namespace, name, currentVersion, toVersion)

	if err := u.workloads.PatchBlueprintVersion(ctx, view, toVersion); err != nil {
		if errors.Is(err, ErrUpgradeConflict) {
			return UpgradeResult{}, err
		}
		// Event already recorded; surface the non-conflict failure to the
		// server log so operators can correlate the orphaned audit entry
		// with the underlying cause.
		u.logger.Error("upgrade patch failed after UpgradeStarted event recorded",
			"namespace", namespace,
			"name", name,
			"lineage", lineage,
			"oldVersion", currentVersion,
			"newVersion", toVersion,
			"user", user,
			"error", err,
		)
		return UpgradeResult{}, fmt.Errorf("patch workload %s/%s: %w", namespace, name, err)
	}

	return UpgradeResult{
		Namespace:     namespace,
		Name:          name,
		BlueprintName: lineage,
		OldVersion:    currentVersion,
		NewVersion:    toVersion,
	}, nil
}

// validate runs AC validations 2–6 against the workload view and the target
// blueprint. validation 1 (workload exists) is performed before validate is
// called. On success the view's Blueprint pointer is guaranteed non-nil.
func (u *upgrader) validate(ctx context.Context, view *UpgradeWorkloadView, toVersion string) error {
	if view.SourceKind != SourceKindBlueprint {
		return fmt.Errorf("%w: got kind=%s", ErrSourceNotBlueprint, view.SourceKind)
	}
	if view.Blueprint == nil {
		return fmt.Errorf("%w: source.blueprint is missing despite kind=Blueprint", ErrSourceNotBlueprint)
	}

	lineage := view.Blueprint.Name
	currentVersion := view.Blueprint.Version
	targetCRName := lineage + "." + toVersion

	bp, err := u.blueprints.GetForUpgrade(ctx, targetCRName)
	if err != nil {
		if errors.Is(err, ErrBlueprintVersionNotFound) {
			return err
		}
		return fmt.Errorf("get blueprint %s: %w", targetCRName, err)
	}

	if bp.Lineage != lineage {
		return fmt.Errorf("%w: Cross-lineage upgrade not allowed (workload lineage=%s, target lineage=%s)",
			ErrCrossLineageUpgrade, lineage, bp.Lineage)
	}
	if bp.Withdrawn {
		return fmt.Errorf("%w: Cannot upgrade to a Withdrawn Blueprint version (%s)",
			ErrTargetWithdrawn, targetCRName)
	}
	if !isStrictlyGreater(toVersion, currentVersion) {
		return fmt.Errorf("%w: Upgrade must target a higher version (downgrade is not supported in v1) -- current=%s target=%s",
			ErrDowngradeNotSupported, currentVersion, toVersion)
	}

	return nil
}

// isStrictlyGreater returns true iff newV > oldV under semver ordering.
// golang.org/x/mod/semver requires the "v" prefix; the CRD pattern is bare
// (^\d+\.\d+\.\d+$). semver.Compare returns 0 for invalid input, so an
// unparseable newV falls through as "not greater" → ErrDowngradeNotSupported.
// The handler validates shape (regex) up-front so malformed input gets a
// clearer 400 instead.
func isStrictlyGreater(newV, oldV string) bool {
	return semver.Compare("v"+newV, "v"+oldV) > 0
}

var _ Upgrader = (*upgrader)(nil)
