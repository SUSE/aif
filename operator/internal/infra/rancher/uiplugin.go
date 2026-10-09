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

	logging "github.com/SUSE/aif-operator/internal/logging"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// labelClusterRepoName records which ClusterRepo an extension was installed
// from. When Rancher installs an extension chart from its Extensions page,
// helmop puts this label on the Helm release (`--labels`), and Rancher's App
// controller copies it onto the release's App. Since Rancher 2.15, the
// Extensions page reads it back off the App to pair the installed UIPlugin with
// its catalog chart, which is where the card's logo, description and
// certification come from. An extension installed without it is shown as a
// "Third-Party" card with no logo, next to a second, uninstalled copy of itself
// under Available.
const labelClusterRepoName = "catalog.cattle.io/cluster-repo-name"

// ExtensionChartLabels returns the release labels that tie an extension chart
// to the ClusterRepo it is installed from, the same ones an install from
// Rancher's Extensions page writes.
func ExtensionChartLabels(clusterRepo string) map[string]string {
	return map[string]string{labelClusterRepoName: clusterRepo}
}

// ExtensionChartValues points the UIPlugin that an extension chart renders at
// the extension server that serves it.
//
// Required because the chart cannot work this out for itself. The
// `plugin.endpoint` and `plugin.compressedEndpoint` keys come from the UI-plugin
// chart that @rancher/shell's publish script generates. Their defaults assume
// the server's Service is called `<image>-svc`, the name Rancher's "Import
// Extension Catalog" dialog gives it, and ours is not. The paths follow the
// layout of the catalog image that same script builds.
func ExtensionChartValues(svcURL, name, version string) map[string]interface{} {
	endpoint := fmt.Sprintf("%s/plugin/%s-%s", svcURL, name, version)
	return map[string]interface{}{
		"plugin": map[string]interface{}{
			"endpoint":           endpoint,
			"compressedEndpoint": endpoint + ".tgz",
		},
	}
}

// DeleteUIPlugin removes a UIPlugin the extension's Helm release does not own:
// one written directly by an operator release that predates installing the
// extension chart, and never adopted because the CR went away first.
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
