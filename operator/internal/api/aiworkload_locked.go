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
	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	"github.com/SUSE/aif-operator/internal/autovalues"
)

// lockedValueViolations reports the install-time values newSpec would change,
// using the same rule the controller enforces at reconcile.
func lockedValueViolations(bp aiplatformv1alpha1.Blueprint, pins []aiplatformv1alpha1.ResolvedValue, newSpec aiplatformv1alpha1.AIWorkloadSpec) ([]string, error) {
	var enabled []aiplatformv1alpha1.BlueprintComponent
	for _, c := range bp.Spec.Components {
		if effectiveComponentEnabled(newSpec.ComponentValues, c.ChartName) {
			enabled = append(enabled, c)
		}
	}
	changes, err := autovalues.LockedChanges(enabled, pins, newSpec.ComponentValues)
	if err != nil {
		return nil, err
	}
	violations := make([]string, 0, len(changes))
	for _, c := range changes {
		violations = append(violations, c.String())
	}
	return violations, nil
}
