// Package labs holds the embedded catalog of available AIF labs and the helpers
// used to fetch, parse and validate external lab catalogs.
package labs

import (
	_ "embed"
	"encoding/json"

	v1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

//go:embed catalog.json
var catalogJSON []byte

// Entry is a catalog item: Lab.spec minus enabled, plus name.
type Entry = v1alpha1.LabCatalogEntry

// Catalog returns the embedded catalog entries.
func Catalog() []Entry {
	var out []Entry
	if err := json.Unmarshal(catalogJSON, &out); err != nil {
		panic("labs: invalid embedded catalog.json: " + err.Error())
	}
	return out
}
