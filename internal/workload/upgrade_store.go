package workload

import (
	"context"
	"fmt"

	aifv1 "github.com/SUSE/aif/api/v1alpha1"
	"github.com/SUSE/aif/pkg/blueprint"
	"github.com/SUSE/aif/pkg/workload"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

// UpgradeStore adapts pkg/workload.Reader+Writer to the consumer-defined
// workloadStore port that workload.Upgrader requires. It is the only place
// where aifv1.Workload values cross paths with apierrors translation for the
// upgrade workflow — pkg/workload/upgrader.go stays domain-typed end to end.
//
// All translation rules live here:
//   - apierrors.IsNotFound  → workload.ErrWorkloadNotFound
//   - apierrors.IsConflict  → workload.ErrUpgradeConflict
//   - apiserver RV check    → workload.ErrUpgradeConflict (pre-flight, before patch)
type UpgradeStore struct {
	workloads workload.Repository
}

// NewUpgradeStore wraps a workload.Repository so it satisfies the upgrader's
// narrow workloadStore port. The Repository is reused across reads (Get) and
// writes (Patch); injecting it as a single value keeps the wiring symmetric
// with other handlers.
func NewUpgradeStore(workloads workload.Repository) *UpgradeStore {
	return &UpgradeStore{workloads: workloads}
}

// GetUpgradeView reads the Workload CR and projects it to the read-only
// fields the upgrader needs. The full CR is intentionally not exposed —
// preservation of unrelated spec fields is the adapter's responsibility,
// not the upgrader's, and is exercised by upgrade_store_test.go.
func (s *UpgradeStore) GetUpgradeView(ctx context.Context, namespace, name string) (*workload.UpgradeWorkloadView, error) {
	w, err := s.workloads.Get(ctx, namespace, name)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("%w: %s/%s", workload.ErrWorkloadNotFound, namespace, name)
		}
		return nil, fmt.Errorf("get workload %s/%s: %w", namespace, name, err)
	}
	view := &workload.UpgradeWorkloadView{
		Namespace:       w.Namespace,
		Name:            w.Name,
		ResourceVersion: w.ResourceVersion,
		SourceKind:      workload.SourceKind(w.Spec.Source.Kind),
	}
	if w.Spec.Source.Blueprint != nil {
		view.Blueprint = &workload.BlueprintRef{
			Name:    w.Spec.Source.Blueprint.Name,
			Version: w.Spec.Source.Blueprint.Version,
		}
	}
	return view, nil
}

// PatchBlueprintVersion re-reads the Workload to obtain a complete original
// for the merge-patch base, verifies its ResourceVersion still matches the
// view's RV (pre-flight optimistic check — catches the apiserver-rejection
// case before we issue the patch and gives a clean ErrUpgradeConflict), then
// deep-copies, mutates spec.source.blueprint.version, and patches.
//
// The deep-copy keeps every other field intact (replicas, valueOverrides,
// targetClusters, strategy, deployStrategy …) so the upgrader doesn't need
// to know what those fields are — preservation is the adapter's job.
func (s *UpgradeStore) PatchBlueprintVersion(ctx context.Context, view *workload.UpgradeWorkloadView, newVersion string) error {
	orig, err := s.workloads.Get(ctx, view.Namespace, view.Name)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("%w: %s/%s", workload.ErrWorkloadNotFound, view.Namespace, view.Name)
		}
		return fmt.Errorf("get workload %s/%s: %w", view.Namespace, view.Name, err)
	}
	if orig.ResourceVersion != view.ResourceVersion {
		return fmt.Errorf("%w: %s/%s (view rv=%s, stored rv=%s)",
			workload.ErrUpgradeConflict, view.Namespace, view.Name,
			view.ResourceVersion, orig.ResourceVersion)
	}

	patched := orig.DeepCopy()
	if patched.Spec.Source.Blueprint == nil {
		patched.Spec.Source.Blueprint = &aifv1.BlueprintRef{}
	}
	patched.Spec.Source.Blueprint.Version = newVersion

	if err := s.workloads.Patch(ctx, patched, orig); err != nil {
		if apierrors.IsConflict(err) {
			return fmt.Errorf("%w: %s/%s", workload.ErrUpgradeConflict, view.Namespace, view.Name)
		}
		return fmt.Errorf("patch workload %s/%s: %w", view.Namespace, view.Name, err)
	}
	return nil
}

// BlueprintReader adapts pkg/blueprint.Repository to the narrow
// blueprintReader port the upgrader requires. Get-by-name only — the upgrader
// does not need list/update access.
type BlueprintReader struct {
	blueprints blueprint.Repository
}

// NewBlueprintReader wraps a blueprint.Repository so it satisfies the
// upgrader's blueprintReader port.
func NewBlueprintReader(blueprints blueprint.Repository) *BlueprintReader {
	return &BlueprintReader{blueprints: blueprints}
}

// GetForUpgrade reads the Blueprint CR by name and projects it to the
// read-only fields the upgrader needs. Withdrawn is derived from
// status.phase so the upgrader stays free of the aifv1 phase enum.
func (r *BlueprintReader) GetForUpgrade(ctx context.Context, name string) (*workload.UpgradeBlueprintView, error) {
	bp, err := r.blueprints.Get(ctx, name)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("%w: %s", workload.ErrBlueprintVersionNotFound, name)
		}
		return nil, fmt.Errorf("get blueprint %s: %w", name, err)
	}
	return &workload.UpgradeBlueprintView{
		Name:      bp.Name,
		Lineage:   bp.Spec.BlueprintName,
		Withdrawn: bp.Status.Phase == aifv1.BlueprintPhaseWithdrawn,
	}, nil
}
