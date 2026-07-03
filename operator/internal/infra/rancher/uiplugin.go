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
	"fmt"

	v1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	logging "github.com/SUSE/aif-operator/internal/logging"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (m *Manager) EnsureUIPlugin(
	ctx context.Context,
	ext *v1alpha1.InstallAIExtension,
	svcURL string,
	namespace string,
) error {
	log := logging.FromContext(ctx, "rancher.uiplugin").
		WithValues(
			logging.KeyExtension, ext.Spec.Extension.Name,
			logging.KeyVersion, ext.Spec.Extension.Version,
		)

	ui := &unstructured.Unstructured{}
	ui.SetAPIVersion("catalog.cattle.io/v1")
	ui.SetKind("UIPlugin")
	ui.SetName(ext.Spec.Extension.Name)
	ui.SetNamespace(namespace)

	log.Info("Ensuring UIPlugin", "namespace", namespace)

	_, err := ctrl.CreateOrUpdate(ctx, m.client, ui, func() error {
		if err := unstructured.SetNestedField(ui.Object, ext.Spec.Extension.Name, "spec", "plugin", "name"); err != nil {
			return err
		}
		if err := unstructured.SetNestedField(ui.Object, ext.Spec.Extension.Version, "spec", "plugin", "version"); err != nil {
			return err
		}

		pluginEndpoint := fmt.Sprintf("%s/plugin/%s-%s", svcURL, ext.Spec.Extension.Name, ext.Spec.Extension.Version)
		if err := unstructured.SetNestedField(ui.Object, pluginEndpoint, "spec", "plugin", "endpoint"); err != nil {
			return err
		}

		logging.Trace(log).Info("Configuring UIPlugin spec", "endpoint", pluginEndpoint)

		metadata, err := buildExtensionMetadata(
			ctx,
			m.indexCache,
			svcURL,
			ext.Spec.Extension.Name,
			ext.Spec.Extension.Version,
			nil,
		)
		if err != nil {
			return err
		}

		return unstructured.SetNestedStringMap(ui.Object, metadata, "spec", "plugin", "metadata")
	})
	if err != nil {
		return err
	}

	logging.Debug(log).Info("UIPlugin ensured")
	return nil
}

func (m *Manager) DeleteUIPlugin(ctx context.Context, name string, namespace string) error {
	log := logging.FromContext(ctx, "rancher.uiplugin").
		WithValues(logging.KeyExtension, name)

	log.Info("Deleting UIPlugin", logging.KeyNamespace, namespace)

	ui := &unstructured.Unstructured{}
	ui.SetAPIVersion("catalog.cattle.io/v1")
	ui.SetKind("UIPlugin")
	ui.SetName(name)
	ui.SetNamespace(namespace)

	if err := m.client.Delete(ctx, ui); client.IgnoreNotFound(err) != nil {
		log.Error(err, "Failed to delete UIPlugin")
		return err
	}

	log.Info("UIPlugin deleted")
	return nil
}
