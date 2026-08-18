/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package kubernetes

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type DeploymentStatus struct {
	Ready   bool
	Message string
}

// IsDeploymentReady reports whether every Deployment belonging to a release has
// finished rolling out the revision currently in its spec.
//
// The reader should be an uncached one when the caller has just applied a
// change: a cached read can be served an entry from before the apply, which
// describes the previous rollout completing and is indistinguishable from this
// one completing. See rolloutIncomplete.
func IsDeploymentReady(
	ctx context.Context,
	c client.Reader,
	namespace, releaseName string,
	log logr.Logger,
) (DeploymentStatus, error) {

	var list appsv1.DeploymentList
	if err := c.List(
		ctx,
		&list,
		client.InNamespace(namespace),
		client.MatchingLabels{
			"app.kubernetes.io/instance": releaseName,
		},
	); err != nil {
		return DeploymentStatus{}, err
	}

	if len(list.Items) == 0 {
		return DeploymentStatus{
			Ready:   false,
			Message: "No deployments found for release " + releaseName,
		}, nil
	}

	for _, d := range list.Items {
		if reason := rolloutIncomplete(&d); reason != "" {
			log.Info("Deployment not ready",
				"deployment", d.Name,
				"reason", reason,
				"generation", d.Generation,
				"observedGeneration", d.Status.ObservedGeneration,
				"replicas", d.Status.Replicas,
				"updatedReplicas", d.Status.UpdatedReplicas,
				"availableReplicas", d.Status.AvailableReplicas,
			)
			return DeploymentStatus{
				Ready:   false,
				Message: "Deployment " + d.Name + " not ready: " + reason,
			}, nil
		}
	}

	return DeploymentStatus{
		Ready:   true,
		Message: "All deployments ready",
	}, nil
}

// rolloutIncomplete names why a Deployment has not finished rolling out the
// revision currently in its spec, or returns "" once it has.
//
// This answers "is the version I just applied serving?", not "are some pods
// up?" — the distinction is the whole point. The Helm upgrade no longer waits
// for readiness itself (see helm.newUpgradeAction), so this is the only check
// between applying a manifest and the CR reporting Installed. Counting ready
// pods alone cannot make that call: the extension chart rolls at one replica
// with maxUnavailable 25%, which rounds down to zero, so the *previous*
// revision's pod stays Ready for the entire rollout and keeps the count at its
// desired value from the first moment to the last.
//
// The four conditions are `kubectl rollout status`' own, in its order
// (kubectl/pkg/polymorphichelpers/rollout_status.go), because that is the
// definition every operator already reasons about — and reaching for a
// different one here would mean this function and `kubectl rollout status`
// could disagree about the same Deployment.
func rolloutIncomplete(d *appsv1.Deployment) string {
	// Until the deployment controller acts on the new spec, every count below
	// still describes the revision being replaced. Checked first: without it the
	// others read stale numbers and agree the rollout is done before it started.
	if d.Status.ObservedGeneration < d.Generation {
		return "waiting for the deployment spec update to be observed"
	}

	desired := int32(1)
	if d.Spec.Replicas != nil {
		desired = *d.Spec.Replicas
	}

	if d.Status.UpdatedReplicas < desired {
		return fmt.Sprintf("%d of %d replicas have been updated to the new revision",
			d.Status.UpdatedReplicas, desired)
	}
	if d.Status.Replicas > d.Status.UpdatedReplicas {
		return fmt.Sprintf("%d replicas from the previous revision are pending termination",
			d.Status.Replicas-d.Status.UpdatedReplicas)
	}
	// Available rather than Ready: it is Ready plus minReadySeconds, so it is the
	// stricter of the two and never disagrees when minReadySeconds is unset.
	if d.Status.AvailableReplicas < d.Status.UpdatedReplicas {
		return fmt.Sprintf("%d of %d updated replicas are available",
			d.Status.AvailableReplicas, d.Status.UpdatedReplicas)
	}
	return ""
}
