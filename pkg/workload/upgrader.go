package workload

import (
	"context"
	"fmt"
	"log/slog"

	aifv1 "github.com/SUSE/aif/api/v1alpha1"
	"github.com/SUSE/aif/pkg/blueprint"
	"golang.org/x/mod/semver"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

// upgrader is the production Upgrader. It runs the 5 validation rules from
// PROJECT_PLAN.md §P5-3 AC in the order the AC specifies, emits a
// UpgradeStarted event BEFORE the spec patch (audit-before-patch), then
// patches Workload.spec.source.blueprint.version via the merge-patch path
// (optimistic concurrency).
//
// This file imports aifv1 — same exception pkg/publish/workflow.go takes.
// The CR-mutation logic is inherently CR-coupled. CLAUDE.md only forbids
// aifv1 in interface.go.
type upgrader struct {
	workloads  Repository
	blueprints blueprint.Repository
	events     UpgradeEventRecorder
	logger     *slog.Logger
}

// NewUpgrader returns an Upgrader bound to the given ports. The logger is
// used for structured server-side debugging; user-facing audit goes through
// the UpgradeEventRecorder.
func NewUpgrader(workloads Repository, blueprints blueprint.Repository, events UpgradeEventRecorder, logger *slog.Logger) Upgrader {
	return &upgrader{
		workloads:  workloads,
		blueprints: blueprints,
		events:     events,
		logger:     logger,
	}
}

// Upgrade implements the P5-3 workflow. Validation order matches AC §P5-3.
//
//nolint:cyclop // Sequential validation gates; splitting hurts readability.
func (u *upgrader) Upgrade(ctx context.Context, namespace, name, toVersion, user string) (UpgradeResult, error) {
	// (1) Get the Workload.
	w, err := u.workloads.Get(ctx, namespace, name)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return UpgradeResult{}, fmt.Errorf("%w: %s/%s", ErrWorkloadNotFound, namespace, name)
		}
		return UpgradeResult{}, fmt.Errorf("get workload %s/%s: %w", namespace, name, err)
	}

	// (2) Validate source.kind == Blueprint.
	if w.Spec.Source.Kind != aifv1.WorkloadSourceKindBlueprint || w.Spec.Source.Blueprint == nil {
		return UpgradeResult{}, fmt.Errorf("%w (got kind=%s)", ErrSourceNotBlueprint, w.Spec.Source.Kind)
	}

	lineage := w.Spec.Source.Blueprint.Name
	currentVersion := w.Spec.Source.Blueprint.Version

	// (3) Lookup the target Blueprint CR by constructed name.
	targetBlueprintName := fmt.Sprintf("%s.%s", lineage, toVersion)
	bp, err := u.blueprints.Get(ctx, targetBlueprintName)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return UpgradeResult{}, fmt.Errorf("%w: %s", ErrBlueprintVersionNotFound, targetBlueprintName)
		}
		return UpgradeResult{}, fmt.Errorf("get blueprint %s: %w", targetBlueprintName, err)
	}

	// (4) Validate same lineage (spec.blueprintName).
	if bp.Spec.BlueprintName != lineage {
		return UpgradeResult{}, fmt.Errorf("%w: Cross-lineage upgrade not allowed (workload lineage=%s, target lineage=%s)",
			ErrCrossLineageUpgrade, lineage, bp.Spec.BlueprintName)
	}

	// (5) Validate phase != Withdrawn.
	if bp.Status.Phase == aifv1.BlueprintPhaseWithdrawn {
		return UpgradeResult{}, fmt.Errorf("%w: Cannot upgrade to a Withdrawn Blueprint version (%s)",
			ErrTargetWithdrawn, targetBlueprintName)
	}

	// (6) Validate new version is strictly greater per semver.
	if !isStrictlyGreater(toVersion, currentVersion) {
		return UpgradeResult{}, fmt.Errorf("%w: Upgrade must target a higher version (downgrade is not supported in v1) — current=%s target=%s",
			ErrDowngradeNotSupported, currentVersion, toVersion)
	}

	// (7) Emit event BEFORE patch (audit-before-patch per AC line 2004).
	u.events.UpgradeStarted(ctx, namespace, name, currentVersion, toVersion)

	// (8) Patch via merge-patch (optimistic concurrency).
	orig := w.DeepCopy()
	w.Spec.Source.Blueprint.Version = toVersion
	if err := u.workloads.Patch(ctx, w, orig); err != nil {
		if apierrors.IsConflict(err) {
			return UpgradeResult{}, fmt.Errorf("%w: %s/%s", ErrUpgradeConflict, namespace, name)
		}
		return UpgradeResult{}, fmt.Errorf("patch workload %s/%s: %w", namespace, name, err)
	}

	u.logger.Info("workload upgraded",
		"namespace", namespace,
		"name", name,
		"lineage", lineage,
		"oldVersion", currentVersion,
		"newVersion", toVersion,
		"user", user,
	)

	return UpgradeResult{
		Namespace:     namespace,
		Name:          name,
		BlueprintName: lineage,
		OldVersion:    currentVersion,
		NewVersion:    toVersion,
	}, nil
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
