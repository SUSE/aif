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

package autovalues

import (
	"encoding/json"
	"fmt"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

// LockedChange is a pinned value the spec would change.
type LockedChange struct {
	Component string
	Path      string
	Pinned    string
	Requested string
}

func (c LockedChange) String() string {
	return fmt.Sprintf("value %s of component %s can't be changed after install (pinned: %q, requested: %q)",
		c.Path, c.Component, c.Pinned, c.Requested)
}

// LockedChanges returns every pin whose effective value (the component's
// Blueprint values merged with overrides) differs from it. Values pinned at
// install are locked: leaving the sentinel in place renders the pin, any other
// value must equal it. Only paths the Blueprint still marks automatic are
// checked, so a newer Blueprint that drops a sentinel doesn't block upgrades.
func LockedChanges(components []aiplatformv1alpha1.BlueprintComponent, pins []aiplatformv1alpha1.ResolvedValue, overrides []aiplatformv1alpha1.ComponentValueOverride) ([]LockedChange, error) {
	var out []LockedChange
	for _, c := range components {
		base, merged := map[string]any(nil), map[string]any(nil)
		for _, pin := range pins {
			if pin.ComponentName != c.ChartName {
				continue
			}
			if base == nil {
				var err error
				if base, err = MergedValues(c, nil); err != nil {
					return nil, err
				}
				if merged, err = MergedValues(c, overrides); err != nil {
					return nil, err
				}
			}
			if v, ok := Get(base, pin.Path); !ok || !IsSentinel(v) {
				continue
			}
			v, ok := Get(merged, pin.Path)
			if !ok || IsSentinel(v) || SameValue(pin.Value, v) {
				continue
			}
			out = append(out, LockedChange{Component: c.ChartName, Path: pin.Path, Pinned: pin.Value, Requested: ScalarString(v)})
		}
	}
	return out, nil
}

// ScalarString is the pin form of a Helm value: strings as-is, anything else
// as JSON (so 1e6 is "1000000" and true is "true").
func ScalarString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

// SameValue reports whether v equals the pinned string, also accepting the
// fmt.Sprint form older operator versions pinned non-string values with.
func SameValue(pinned string, v any) bool {
	if s, ok := v.(string); ok {
		return s == pinned
	}
	return ScalarString(v) == pinned || fmt.Sprint(v) == pinned
}
