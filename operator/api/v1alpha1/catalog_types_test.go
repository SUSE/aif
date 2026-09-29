package v1alpha1

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCatalogMarshals(t *testing.T) {
	c := BlueprintCatalog{
		Spec: BlueprintCatalogSpec{
			DisplayName: "Catalog A",
			Description: "starter set",
			Icon:        "https://x/logo.svg",
			Maintainer:  &BlueprintCatalogMaintainer{Name: "SUSE", URL: "https://suse.com"},
			Categories:  []string{"rag", "agents"},
			Blueprints: []BlueprintCatalogMember{
				{Name: "blueprint-1", Featured: true, Category: "rag"},
				{Name: "blueprint-2"},
			},
		},
	}
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"displayName":"Catalog A"`,
		`"blueprints":[`,
		`"name":"blueprint-1"`,
		`"featured":true`,
		`"maintainer":{"name":"SUSE"`,
	} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("marshaled %s missing %s", b, want)
		}
	}
}
