package aiworkload

import (
	"context"
	"sync"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// FleetCapabilities reports optional features of the management cluster's Fleet.
type FleetCapabilities interface {
	// DependsOnAcceptedStates reports whether every Fleet resource the operator
	// renders dependsOn into accepts BundleRef.acceptedStates (Fleet >= v0.15).
	DependsOnAcceptedStates(ctx context.Context) bool
}

const (
	fleetBundleCRD = "bundles.fleet.cattle.io"
	fleetHelmOpCRD = "helmops.fleet.cattle.io"
	// fleetCapabilitiesTTL bounds how long a Fleet upgrade goes unnoticed.
	fleetCapabilitiesTTL = 10 * time.Minute
	// fleetCapabilitiesRetry re-tries a failed lookup sooner until the first success.
	fleetCapabilitiesRetry = 30 * time.Second
)

var crdGVK = schema.GroupVersionKind{Group: "apiextensions.k8s.io", Version: "v1", Kind: "CustomResourceDefinition"}

// crdFleetCapabilities detects Fleet features from its CRD schemas, caching the
// answer for fleetCapabilitiesTTL. A lookup error keeps the last known answer
// (false before the first success) so a transient outage doesn't re-render.
type crdFleetCapabilities struct {
	reader client.Reader
	now    func() time.Time

	mu             sync.Mutex
	nextCheck      time.Time
	detected       bool
	acceptedStates bool
	errLogged      bool
	notFoundLogged bool
}

func newCRDFleetCapabilities(reader client.Reader) *crdFleetCapabilities {
	return &crdFleetCapabilities{reader: reader, now: time.Now}
}

func (f *crdFleetCapabilities) DependsOnAcceptedStates(ctx context.Context) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := f.now()
	if now.Before(f.nextCheck) {
		return f.acceptedStates
	}
	supported, err := f.detectAcceptedStates(ctx)
	if err != nil {
		if f.detected {
			f.nextCheck = now.Add(fleetCapabilitiesTTL)
		} else {
			f.nextCheck = now.Add(fleetCapabilitiesRetry)
		}
		if !f.errLogged {
			f.errLogged = true
			log.FromContext(ctx).Error(err, "cannot read Fleet CRDs; keeping the last known Fleet dependsOn mode (none until detected, ordering relies on the operator's dependency hold)",
				"acceptedStates", f.acceptedStates)
		}
		return f.acceptedStates
	}
	f.nextCheck = now.Add(fleetCapabilitiesTTL)
	f.detected, f.errLogged = true, false
	f.acceptedStates = supported
	return supported
}

func (f *crdFleetCapabilities) detectAcceptedStates(ctx context.Context) (bool, error) {
	for _, name := range []string{fleetBundleCRD, fleetHelmOpCRD} {
		crd := &unstructured.Unstructured{}
		crd.SetGroupVersionKind(crdGVK)
		if err := f.reader.Get(ctx, client.ObjectKey{Name: name}, crd); err != nil {
			if apierrors.IsNotFound(err) {
				if !f.notFoundLogged {
					f.notFoundLogged = true
					log.FromContext(ctx).Info("Fleet CRD not found; rendering no Fleet dependsOn (ordering relies on the operator's dependency hold)", "crd", name)
				}
				return false, nil
			}
			return false, err
		}
		if !crdDependsOnHasAcceptedStates(crd) {
			return false, nil
		}
	}
	return true, nil
}

// crdDependsOnHasAcceptedStates checks the v1alpha1 schema (the version the
// operator writes) for spec.dependsOn[].acceptedStates.
func crdDependsOnHasAcceptedStates(crd *unstructured.Unstructured) bool {
	versions, _, _ := unstructured.NestedSlice(crd.Object, "spec", "versions")
	for _, v := range versions {
		vm, ok := v.(map[string]any)
		if !ok || vm["name"] != bundleGVK.Version {
			continue
		}
		_, found, _ := unstructured.NestedMap(vm, "schema", "openAPIV3Schema", "properties", "spec",
			"properties", "dependsOn", "items", "properties", "acceptedStates")
		return found
	}
	return false
}

// fleetDependsOnMode is how Blueprint dependencies render into Fleet spec.dependsOn.
type fleetDependsOnMode int

const (
	// fleetDependsOnNone renders no Fleet dependsOn: Fleet < v0.15 accepts only
	// Ready dependencies, so a Modified one would block forever. The operator's
	// dependency hold orders components on its own.
	fleetDependsOnNone fleetDependsOnMode = iota
	// fleetDependsOnReadyOrModified accepts the same states as the hold.
	fleetDependsOnReadyOrModified
)

// fleetDependsOnAcceptedStates are Fleet BundleState values (bundle_types.go).
var fleetDependsOnAcceptedStates = []any{"Ready", "Modified"}

func (r *AIWorkloadReconciler) fleetCapabilities() FleetCapabilities {
	if r.FleetCapabilities != nil {
		return r.FleetCapabilities
	}
	r.defaultFleetCapsOnce.Do(func() {
		r.defaultFleetCaps = newCRDFleetCapabilities(r.controllerReader())
	})
	return r.defaultFleetCaps
}

func (r *AIWorkloadReconciler) fleetDependsOnMode(ctx context.Context) fleetDependsOnMode {
	if r.fleetCapabilities().DependsOnAcceptedStates(ctx) {
		return fleetDependsOnReadyOrModified
	}
	return fleetDependsOnNone
}
