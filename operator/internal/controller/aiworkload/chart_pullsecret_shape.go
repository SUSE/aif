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

package aiworkload

import "path"

// flatStringPullSecretPaths lists charts whose own templates read the pull
// secrets from a flat list of secret-name strings instead of the standard
// []LocalObjectReference ({name: ...}) shape the injectors write at the top
// level. Each entry is the value path that the chart reads with precedence over
// the top-level imagePullSecrets key.
//
// network-operator (NVIDIA/Mellanox): the network-operator.operator.imagePullSecrets
// helper wraps each entry of operator.imagePullSecrets (or, when empty, the
// top-level imagePullSecrets) as (dict "name" .). An object entry therefore
// renders as {name: {name: ...}}, and the API server rejects the operator
// Deployment and the keep-ncp / upgrade-crd hook Jobs. The flat-string shape is
// the documented chart contract (Mellanox/network-operator#1707). Its subcharts
// (node-feature-discovery, maintenance-operator) use the standard object shape
// through global.imagePullSecrets, so the top-level and global entries stay as
// the injector wrote them.
var flatStringPullSecretPaths = map[string][]string{
	"network-operator": {"operator", "imagePullSecrets"},
}

// adaptPullSecretShapeForChart copies the pull secrets that the injector wrote
// at the top-level imagePullSecrets key into the flat-string path of the charts
// listed in flatStringPullSecretPaths. It runs after every injector. It does
// nothing for other charts, when the injector wrote no secrets, or when a
// parent value has an unexpected shape (author intent). Author-supplied entries
// at the target path are kept; injected names are prepended when missing.
func adaptPullSecretShapeForChart(chartName string, vals map[string]any) {
	keys, ok := flatStringPullSecretPaths[path.Base(chartName)]
	if !ok || len(keys) == 0 {
		return
	}
	injected, _ := vals["imagePullSecrets"].([]any)
	var names []string
	for _, e := range injected {
		if m, ok := e.(map[string]any); ok {
			if n, ok := m["name"].(string); ok && n != "" {
				names = append(names, n)
			}
		}
	}
	if len(names) == 0 {
		return
	}

	parent := vals
	for _, k := range keys[:len(keys)-1] {
		switch child := parent[k].(type) {
		case nil:
			m := map[string]any{}
			parent[k] = m
			parent = m
		case map[string]any:
			parent = child
		default:
			return
		}
	}

	leaf := keys[len(keys)-1]
	var list []any
	switch existing := parent[leaf].(type) {
	case nil:
	case []any:
		list = existing
	default:
		return
	}
	var add []any
	for _, n := range names {
		if !containsString(list, n) {
			add = append(add, n)
		}
	}
	parent[leaf] = append(add, list...)
}
