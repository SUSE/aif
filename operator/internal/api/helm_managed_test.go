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
	"errors"
	"strings"
	"testing"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

func catalogSource(name, repo, branch string, paths ...string) aiplatformv1alpha1.BlueprintCatalogSource {
	return aiplatformv1alpha1.BlueprintCatalogSource{
		Name:          name,
		Paths:         paths,
		GitRepoSource: aiplatformv1alpha1.GitRepoSource{RepoURL: repo, Branch: branch},
	}
}

func TestParseHelmManaged(t *testing.T) {
	cases := []struct {
		name        string
		annotations map[string]string
		want        []string
		wantErr     bool
	}{
		{name: "nil annotations", annotations: nil, want: nil},
		{name: "annotation absent", annotations: map[string]string{"other": "x"}, want: nil},
		{name: "blank value", annotations: map[string]string{aiplatformv1alpha1.SettingsHelmManagedAnnotation: "  "}, want: nil},
		{name: "empty object", annotations: map[string]string{aiplatformv1alpha1.SettingsHelmManagedAnnotation: "{}"}, want: nil},
		{
			name:        "catalog names",
			annotations: map[string]string{aiplatformv1alpha1.SettingsHelmManagedAnnotation: `{"blueprintCatalogs":["partner-blueprints","team-a"]}`},
			want:        []string{"partner-blueprints", "team-a"},
		},
		{
			// Scope 2 adds "fields"; a Scope 1 operator must ignore it.
			name:        "unknown keys are ignored",
			annotations: map[string]string{aiplatformv1alpha1.SettingsHelmManagedAnnotation: `{"blueprintCatalogs":["a"],"fields":["rancherCatalog.url"]}`},
			want:        []string{"a"},
		},
		{name: "invalid JSON", annotations: map[string]string{aiplatformv1alpha1.SettingsHelmManagedAnnotation: `{"blueprintCatalogs":`}, wantErr: true},
		{name: "wrong type", annotations: map[string]string{aiplatformv1alpha1.SettingsHelmManagedAnnotation: `{"blueprintCatalogs":"a"}`}, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseHelmManaged(tc.annotations)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want error, got %+v", got)
				}
				if !strings.Contains(err.Error(), "helm upgrade --reuse-values --force-conflicts") {
					t.Errorf("error %q should tell the admin how to recover", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if strings.Join(got.BlueprintCatalogs, ",") != strings.Join(tc.want, ",") {
				t.Errorf("BlueprintCatalogs=%v want %v", got.BlueprintCatalogs, tc.want)
			}
		})
	}
}

func TestFilterHelmManagedCatalogs(t *testing.T) {
	partner := catalogSource("partner-blueprints", "https://github.com/suse/partner-blueprints.git", "main", "partners")
	teamA := catalogSource("team-a", "https://git.example.com/a.git", "main")
	managed := helmManaged{BlueprintCatalogs: []string{"partner-blueprints"}}

	t.Run("no managed names passes the request through unchanged", func(t *testing.T) {
		got, err := filterHelmManagedCatalogs([]aiplatformv1alpha1.BlueprintCatalogSource{partner, teamA}, nil, helmManaged{})
		if err != nil || len(got) != 2 {
			t.Fatalf("got %v, %v; want both entries, nil", got, err)
		}
	})

	t.Run("an identical Helm entry is dropped and UI entries are kept", func(t *testing.T) {
		got, err := filterHelmManagedCatalogs(
			[]aiplatformv1alpha1.BlueprintCatalogSource{partner, teamA},
			[]aiplatformv1alpha1.BlueprintCatalogSource{partner},
			managed)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 1 || got[0].Name != teamA.Name {
			t.Fatalf("got %v; want only team-a", got)
		}
	})

	t.Run("a missing Helm entry is not an error", func(t *testing.T) {
		got, err := filterHelmManagedCatalogs(
			[]aiplatformv1alpha1.BlueprintCatalogSource{teamA},
			[]aiplatformv1alpha1.BlueprintCatalogSource{partner},
			managed)
		if err != nil || len(got) != 1 || got[0].Name != teamA.Name {
			t.Fatalf("got %v, %v; want [team-a], nil", got, err)
		}
	})

	// Review Focus 1: the 2.3.0 Settings page sends branch "main" for an entry
	// whose branch is empty. The operator treats empty as "main", so it is the
	// same catalog and must not be rejected.
	t.Run("empty live branch equals main", func(t *testing.T) {
		liveNoBranch := catalogSource("partner-blueprints", "https://github.com/suse/partner-blueprints.git", "", "partners")
		got, err := filterHelmManagedCatalogs(
			[]aiplatformv1alpha1.BlueprintCatalogSource{partner},
			[]aiplatformv1alpha1.BlueprintCatalogSource{liveNoBranch},
			managed)
		if err != nil || len(got) != 0 {
			t.Fatalf("got %v, %v; want the entry dropped, nil", got, err)
		}
	})

	t.Run("nil and empty paths are equal", func(t *testing.T) {
		live := catalogSource("partner-blueprints", "https://x.git", "main")
		req := live
		req.Paths = []string{}
		got, err := filterHelmManagedCatalogs([]aiplatformv1alpha1.BlueprintCatalogSource{req}, []aiplatformv1alpha1.BlueprintCatalogSource{live}, managed)
		if err != nil || len(got) != 0 {
			t.Fatalf("got %v, %v; want the entry dropped, nil", got, err)
		}
	})

	t.Run("a changed Helm entry is rejected", func(t *testing.T) {
		changed := partner
		changed.Branch = "dev"
		_, err := filterHelmManagedCatalogs(
			[]aiplatformv1alpha1.BlueprintCatalogSource{teamA, changed},
			[]aiplatformv1alpha1.BlueprintCatalogSource{partner},
			managed)
		if !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("err=%v; want ErrInvalidInput", err)
		}
		want := `blueprintCatalogs[1]: "partner-blueprints" is managed by Helm values; change it with helm upgrade`
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err=%q; want it to contain %q", err, want)
		}
	})

	// Review Focus 5: a UI-created entry that reuses a Helm-declared name.
	t.Run("a Helm-declared name with no live entry is rejected", func(t *testing.T) {
		_, err := filterHelmManagedCatalogs([]aiplatformv1alpha1.BlueprintCatalogSource{partner}, nil, managed)
		if !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("err=%v; want ErrInvalidInput", err)
		}
	})
}
