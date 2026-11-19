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

package settings_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	"github.com/SUSE/aif-operator/internal/blueprintdeps"
	"github.com/SUSE/aif-operator/internal/catalog"
	"github.com/SUSE/aif-operator/internal/credentials"
	"sigs.k8s.io/yaml"
)

// Every chartRepo embedded in a product Blueprint must have a stable identity
// produced by the Settings controller in both connected and mirrored modes.
// This catches catalog additions that would otherwise fail only after a
// customer selects a Blueprint in a disconnected cluster.
func TestBundledBlueprintChartReposAreSettingsManaged(t *testing.T) {
	files := bundledBlueprintFiles(t)

	managed := map[string]bool{
		credentials.ClusterRepoApplicationCollection: true,
		credentials.ClusterRepoSUSERegistry:          true,
		credentials.ClusterRepoNvidia:                true,
		credentials.ClusterRepoNvidiaBlueprint:       true,
	}
	// NGC team repos, public and gated, are provisioned under the same name in
	// connected and mirrored modes (catalogNvidiaTeamRepoNames). Gated ones need
	// NVIDIA credentials, like the nvidia org repo.
	teams := catalog.ClassifyNGCTeamRepos()
	for _, u := range append(teams.Public, teams.Gated...) {
		name, err := catalog.NGCClusterRepoName(u)
		if err != nil {
			t.Fatalf("team repo name for %s: %v", u, err)
		}
		managed[name] = true
	}
	type blueprint struct {
		Kind string `json:"kind"`
		Spec struct {
			Components []struct {
				ChartRepo string `json:"chartRepo"`
			} `json:"components"`
		} `json:"spec"`
	}

	for _, path := range files {
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", filepath.Base(path), err)
		}
		var bp blueprint
		if err := yaml.Unmarshal(contents, &bp); err != nil {
			t.Fatalf("parse %s: %v", filepath.Base(path), err)
		}
		// The colocated bundled BlueprintCatalog file lives in this same folder
		// (Fleet syncs it alongside the blueprints on a git swap) but has no
		// components; skip it.
		if bp.Kind != "Blueprint" {
			continue
		}
		if len(bp.Spec.Components) == 0 {
			t.Errorf("%s has no components", filepath.Base(path))
			continue
		}
		for i, component := range bp.Spec.Components {
			if !managed[component.ChartRepo] {
				t.Errorf("%s component %d references unmanaged chartRepo %q", filepath.Base(path), i, component.ChartRepo)
			}
		}
	}
}

// Bundled Blueprints are applied by Helm, not the API, so nothing else checks
// their dependsOn before an install fails with InvalidDependsOn.
func TestBundledBlueprintDependsOnIsValid(t *testing.T) {
	for _, path := range bundledBlueprintFiles(t) {
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", filepath.Base(path), err)
		}
		var bp aiplatformv1alpha1.Blueprint
		if err := yaml.Unmarshal(contents, &bp); err != nil {
			t.Fatalf("parse %s: %v", filepath.Base(path), err)
		}
		if bp.Kind != "Blueprint" {
			continue
		}
		if err := blueprintdeps.ValidateComponents(bp.Spec.Components); err != nil {
			t.Errorf("%s: %v", filepath.Base(path), err)
		}
	}
}

func bundledBlueprintFiles(t *testing.T) []string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate test source")
	}
	pattern := filepath.Join(filepath.Dir(filename), "../../../../charts/aif-operator/files/blueprints/*.yaml")
	files, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatalf("glob bundled Blueprints: %v", err)
	}
	if len(files) == 0 {
		t.Fatalf("no bundled Blueprints matched %s", pattern)
	}
	return files
}
