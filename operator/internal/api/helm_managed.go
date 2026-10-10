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

package api

import (
	"encoding/json"
	"fmt"
	"strings"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/equality"
)

// helmManaged is the decoded SettingsHelmManagedAnnotation. Unknown keys are
// ignored so a newer chart can declare more without breaking this operator.
type helmManaged struct {
	BlueprintCatalogs []string `json:"blueprintCatalogs,omitempty"`
}

// parseHelmManaged decodes the annotation. A missing or blank annotation means
// nothing is Helm-managed. An undecodable one is an error: the caller must not
// guess, because applying a Helm value would make the API a co-owner of it.
func parseHelmManaged(annotations map[string]string) (helmManaged, error) {
	raw := strings.TrimSpace(annotations[aiplatformv1alpha1.SettingsHelmManagedAnnotation])
	if raw == "" {
		return helmManaged{}, nil
	}
	var m helmManaged
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return helmManaged{}, fmt.Errorf("annotation %s on Settings is not valid JSON; restore it with helm upgrade --reuse-values --force-conflicts: %w",
			aiplatformv1alpha1.SettingsHelmManagedAnnotation, err)
	}
	return m, nil
}

// filterHelmManagedCatalogs removes Helm-declared catalogs from a Settings-page
// request. Server-side apply keeps a list entry while any manager still applies
// it, so re-sending a Helm entry would make aif-operator-api a co-owner and the
// entry would survive its removal from the chart values. An entry identical to
// the live one (what any Settings page sends back) is dropped; a changed entry,
// or a new one under a Helm-declared name, is rejected. A Helm entry missing
// from the request is not a delete: the API simply does not apply it.
func filterHelmManagedCatalogs(req, live []aiplatformv1alpha1.BlueprintCatalogSource, managed helmManaged) ([]aiplatformv1alpha1.BlueprintCatalogSource, error) {
	if len(managed.BlueprintCatalogs) == 0 {
		return req, nil
	}
	isManaged := make(map[string]bool, len(managed.BlueprintCatalogs))
	for _, n := range managed.BlueprintCatalogs {
		isManaged[n] = true
	}
	liveByName := make(map[string]aiplatformv1alpha1.BlueprintCatalogSource, len(live))
	for _, c := range live {
		liveByName[c.Name] = c
	}

	out := make([]aiplatformv1alpha1.BlueprintCatalogSource, 0, len(req))
	for i, c := range req {
		if !isManaged[c.Name] {
			out = append(out, c)
			continue
		}
		if l, ok := liveByName[c.Name]; ok && sameCatalog(c, l) {
			continue
		}
		return nil, fmt.Errorf("%w: blueprintCatalogs[%d]: %q is managed by Helm values; change it with helm upgrade",
			ErrInvalidInput, i, c.Name)
	}
	return out, nil
}

// sameCatalog compares two catalogs the way the operator interprets them: an
// empty branch reconciles as "main" (see applyGitRepo), and the Settings page
// fills in "main" when it round-trips an entry. Nil and empty slices are equal
// under equality.Semantic.
func sameCatalog(a, b aiplatformv1alpha1.BlueprintCatalogSource) bool {
	if a.Branch == "" {
		a.Branch = "main"
	}
	if b.Branch == "" {
		b.Branch = "main"
	}
	return equality.Semantic.DeepEqual(a, b)
}
