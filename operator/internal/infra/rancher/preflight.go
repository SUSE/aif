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

package rancher

import (
	"context"

	logging "github.com/SUSE/aif-operator/internal/logging"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
)

// CheckCRDs reads through m.client — the same cached client every other
// Manager method already uses — rather than dialling a fresh in-cluster
// config and a separate typed clientset the way this used to. That old path
// only ever worked inside a real pod (rest.InClusterConfig hard-fails
// anywhere else), which made this the one Manager method that could never be
// exercised under go test, envtest included: every existing test that needs
// CheckCRDs to pass stubs out the whole rancherManager interface instead of
// running the real thing.
//
// Reading through the cache also means this now shares the exact informer
// InstallAIExtensionReconciler's CustomResourceDefinition watch already
// maintains (operator/internal/controller/installaiextension), instead of
// making a live API call every reconcile — cheaper, and the two mechanisms
// can no longer observe different answers from each other. The trade is that
// a CRD's presence is now only as fresh as that informer: if it ever silently
// stopped receiving events without controller-runtime treating that as fatal,
// this would keep returning its last known answer rather than re-verifying
// against the API server on every call the way the old implementation did.
func (m *Manager) CheckCRDs(ctx context.Context, crds []string) error {
	log := logging.FromContext(ctx, "rancher.preflight")

	for _, crd := range crds {
		var obj apiextensionsv1.CustomResourceDefinition
		err := m.client.Get(ctx, types.NamespacedName{Name: crd}, &obj)

		if err != nil {
			if apierrors.IsNotFound(err) {
				logging.Debug(log).Info(
					"Required CRD not found yet",
					"logicalDependency", crd,
				)

				return &DependencyNotReadyError{
					Dependency: crd,
				}
			}
			return err
		}
	}

	logging.Debug(log).Info("All required CRDs are present")
	return nil
}
