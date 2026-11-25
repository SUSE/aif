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

package blueprintpolicy

import (
	"fmt"
	"sort"
	"strings"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

// CheckValuesFromNamespaces confines valuesFrom to the Blueprint's own
// namespaces, since Fleet reads the referenced Secret or ConfigMap into Helm
// values on the downstream cluster. A reference must leave namespace empty
// (the component's release namespace) or name a namespace some component of
// the same Blueprint deploys to. defaultNamespace is where components without
// a targetNamespace deploy; pass "" when it isn't known yet (saving a Blueprint).
func CheckValuesFromNamespaces(components []aiplatformv1alpha1.BlueprintComponent, defaultNamespace string) error {
	allowed := map[string]bool{}
	for _, c := range components {
		ns := c.TargetNamespace
		if ns == "" {
			ns = defaultNamespace
		}
		if ns != "" {
			allowed[ns] = true
		}
	}
	for _, c := range components {
		for i, vf := range c.ValuesFrom {
			ref := vf.SecretKeyRef
			if ref == nil {
				ref = vf.ConfigMapKeyRef
			}
			if ref == nil || ref.Namespace == "" || allowed[ref.Namespace] {
				continue
			}
			names := make([]string, 0, len(allowed))
			for ns := range allowed {
				names = append(names, ns)
			}
			sort.Strings(names)
			hint := "leave it empty to read from the release namespace"
			if len(names) > 0 {
				hint = "use one of " + strings.Join(names, ", ") + ", or " + hint
			}
			return fmt.Errorf("components[%s].valuesFrom[%d]: namespace %q is not a namespace this Blueprint deploys to; %s",
				c.ChartName, i, ref.Namespace, hint)
		}
	}
	return nil
}
