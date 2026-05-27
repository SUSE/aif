package fleet

import (
	"context"
	"fmt"
	"log/slog"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fleetv1 "github.com/rancher/fleet/pkg/apis/fleet.cattle.io/v1alpha1"
)

// fieldManager identifies this controller's server-side-apply ownership.
// Keep stable across releases — changing it would trigger spurious conflicts
// on existing Bundles.
const fieldManager = "aif-workload-controller"

type bundleEngine struct {
	log    *slog.Logger
	client client.Client
}

// NewBundleEngine returns a production FleetBundleEngine that operates
// on the supplied controller-runtime client. The client's scheme must
// have fleetv1 registered (done in cmd/operator/main.go via
// fleetv1.AddToScheme).
func NewBundleEngine(log *slog.Logger, c client.Client) FleetBundleEngine {
	return &bundleEngine{log: log, client: c}
}

func (e *bundleEngine) Apply(ctx context.Context, spec BundleDeploymentSpec) (BundleObservedStatus, error) {
	if err := validateSpec(spec); err != nil {
		return BundleObservedStatus{}, fmt.Errorf("%w: %v", ErrBundleInvalidSpec, err)
	}

	desired, err := buildBundleCR(spec)
	if err != nil {
		return BundleObservedStatus{}, fmt.Errorf("%w: %v", ErrBundleInvalidSpec, err)
	}

	// Server-side-apply: idempotent on identical spec, surfaces conflicts
	// cleanly. ForceOwnership lets us reclaim fields if a previous run was
	// interrupted with a different field manager.
	//
	// TODO: migrate to client.Client.Apply() once an ApplyConfiguration is
	// available for fleetv1.Bundle (controller-runtime v0.23.3 deprecates the
	// client.Apply Patch constant in favour of the typed API). Mirrors the
	// outstanding migration in internal/api/settings.go.
	if err := e.client.Patch(ctx, desired, client.Apply, //nolint:staticcheck // SA1019: see TODO above
		client.FieldOwner(fieldManager),
		client.ForceOwnership,
	); err != nil {
		if apierrors.IsConflict(err) {
			return BundleObservedStatus{}, fmt.Errorf("%w: %v", ErrBundleConflict, err)
		}
		return BundleObservedStatus{}, fmt.Errorf("%w: %v", ErrBundleApplyFailed, err)
	}

	var observed fleetv1.Bundle
	if err := e.client.Get(ctx, client.ObjectKeyFromObject(desired), &observed); err != nil {
		return BundleObservedStatus{}, fmt.Errorf("%w: %v", ErrBundleApplyFailed, err)
	}

	e.log.Debug("fleet bundle applied",
		slog.String("component", "fleet.bundleEngine"),
		slog.String("bundle", desired.Name),
		slog.String("namespace", desired.Namespace),
		slog.Int("targets", len(spec.TargetClusters)),
	)

	return mirrorStatus(observed.Status, spec.TargetClusters), nil
}

func (e *bundleEngine) Teardown(ctx context.Context, ns, workloadID string) error {
	name := fleetBundleName(ns, workloadID)
	err := e.client.Delete(ctx, &fleetv1.Bundle{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name},
	})
	if err == nil || apierrors.IsNotFound(err) {
		return nil
	}
	return fmt.Errorf("%w: delete bundle %s/%s: %v", ErrBundleApplyFailed, ns, name, err)
}

func (e *bundleEngine) UpdateSettings(_ FleetSettings) {
	// No-op — the bundle path writes Bundle CRs to the local apiserver
	// and doesn't need git creds. Implemented for symmetry with the
	// SettingsApplier contract so engine_bus can call UpdateSettings
	// on every settings-aware engine uniformly.
}
