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

package v1alpha1

import (
	"encoding/json"
	"strings"
	"testing"
)

// Guard the FleetSettings→GitRepoSource embed refactor: existing JSON tags must be
// byte-stable so stored Settings round-trip unchanged after upgrade.
func TestFleetSettingsJSONTagsStable(t *testing.T) {
	in := FleetSettings{GitRepoSource: GitRepoSource{
		RepoURL:  "https://git.example/repo",
		Branch:   "main",
		Username: "u",
	}}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	for _, want := range []string{`"repoURL":"https://git.example/repo"`, `"branch":"main"`, `"username":"u"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("marshaled %s missing %s", got, want)
		}
	}
}

func TestBlueprintCatalogMarshals(t *testing.T) {
	c := BlueprintCatalog{
		Name:      "partner-acme",
		Paths:     []string{"catalog/blueprints"},
		GitRepoSource: GitRepoSource{RepoURL: "https://git.example/acme", Branch: "main"},
	}
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"name":"partner-acme"`, `"paths":["catalog/blueprints"]`, `"repoURL":"https://git.example/acme"`} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("marshaled %s missing %s", b, want)
		}
	}
}
