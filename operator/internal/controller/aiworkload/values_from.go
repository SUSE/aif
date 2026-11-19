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

import (
	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

const reasonInvalidValuesFrom = "InvalidValuesFrom"

// valuesFromSpec renders a component's ValuesFrom as Fleet's spec.helm.valuesFrom,
// or nil when it has none.
func valuesFromSpec(c aiplatformv1alpha1.BlueprintComponent) []any {
	if len(c.ValuesFrom) == 0 {
		return nil
	}
	out := make([]any, 0, len(c.ValuesFrom))
	for _, vf := range c.ValuesFrom {
		entry := map[string]any{}
		if vf.ConfigMapKeyRef != nil {
			entry["configMapKeyRef"] = keyRefSpec(vf.ConfigMapKeyRef)
		}
		if vf.SecretKeyRef != nil {
			entry["secretKeyRef"] = keyRefSpec(vf.SecretKeyRef)
		}
		out = append(out, entry)
	}
	return out
}

func keyRefSpec(r *aiplatformv1alpha1.ValuesFromKeyRef) map[string]any {
	m := map[string]any{"name": r.Name}
	if r.Namespace != "" {
		m["namespace"] = r.Namespace
	}
	if r.Key != "" {
		m["key"] = r.Key
	}
	return m
}
