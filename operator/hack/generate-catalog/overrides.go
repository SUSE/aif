package main

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/SUSE/aif-operator/internal/catalog"
)

// overrides maps a slug_name to a partial catalog entry: the JSON object of the
// fields a human has pinned. Only the keys present in each object overwrite the
// NGC-derived values; every other field stays fresh from the search API. This is
// the sole place manual curation lives, so default-catalog.json can be fully
// machine-owned (see README.md).
type overrides map[string]json.RawMessage

// loadOverrides parses the overrides document. An empty or whitespace-only input
// is valid and yields no overrides, so the tool runs before the file has any
// content. Keys are slug_names; values are partial catalog entries (any subset of
// catalog.Item's JSON fields).
func loadOverrides(data []byte) (overrides, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return overrides{}, nil
	}
	var ov overrides
	if err := json.Unmarshal(data, &ov); err != nil {
		return nil, fmt.Errorf("parse overrides: %w", err)
	}
	return ov, nil
}

// apply overlays the pinned fields for item.SlugName onto item, if any exist.
// Unmarshalling the override object onto the already-populated struct writes only
// the keys the object contains, leaving all other fields at their derived values.
func (ov overrides) apply(item *catalog.Item) error {
	raw, ok := ov[item.SlugName]
	if !ok {
		return nil
	}
	if err := json.Unmarshal(raw, item); err != nil {
		return fmt.Errorf("apply override for %q: %w", item.SlugName, err)
	}
	return nil
}
