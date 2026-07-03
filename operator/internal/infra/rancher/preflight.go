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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/client-go/rest"
)

func (m *Manager) CheckCRDs(ctx context.Context, crds []string) error {
	log := logging.FromContext(ctx, "rancher.preflight")

	cfg, err := rest.InClusterConfig()
	if err != nil {
		return err
	}

	clientset, err := apiextensionsclient.NewForConfig(cfg)
	if err != nil {
		return err
	}

	for _, crd := range crds {
		_, err := clientset.
			ApiextensionsV1().
			CustomResourceDefinitions().
			Get(ctx, crd, metav1.GetOptions{})

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
