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
	"context"
	"net/http"

	"k8s.io/apimachinery/pkg/api/equality"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

// Install-time Kustomize overlays (applied by the Fleet agent with its own
// rights) and deploy-order overrides are reserved to users who may author
// them in a Blueprint. This is the single place to change that permission.
const (
	advancedCustomizationVerb     = verbUpdate
	advancedCustomizationResource = resourceBlueprints

	advancedCustomizationForbidden = "Changing deploy order or adding a Kustomize overlay requires permission to update Blueprints"
)

// CanCustomizeAdvanced reports whether the caller in ctx may add or change
// componentValues[].kustomize and componentValues[].dependsOn.
func (a *Authorizer) CanCustomizeAdvanced(ctx context.Context) (bool, error) {
	return a.Can(ctx, advancedCustomizationVerb, advancedCustomizationResource)
}

// advancedChanges reports whether next adds or changes a kustomize overlay or
// a dependsOn override relative to existing (nil on create). Removing one, or
// keeping it unchanged, is not a change; nor is a stored dependsOn with
// self-references and names absent from the Blueprint (known, nil when
// unknown) pruned. Like the controller, only the first stored entry per
// componentName counts.
func advancedChanges(existing *aiplatformv1alpha1.AIWorkloadSpec, next aiplatformv1alpha1.AIWorkloadSpec, known map[string]bool) bool {
	stored := map[string]aiplatformv1alpha1.ComponentValueOverride{}
	if existing != nil {
		for _, ov := range existing.ComponentValues {
			if _, seen := stored[ov.ComponentName]; !seen {
				stored[ov.ComponentName] = ov
			}
		}
	}
	for _, ov := range next.ComponentValues {
		s := stored[ov.ComponentName]
		if ov.Kustomize != nil && !equality.Semantic.DeepEqual(s.Kustomize, ov.Kustomize) {
			return true
		}
		if ov.DependsOn != nil && !dependsOnKept(ov.ComponentName, s.DependsOn, *ov.DependsOn, known) {
			return true
		}
	}
	return false
}

func dependsOnKept(component string, stored *[]string, next []string, known map[string]bool) bool {
	if stored == nil {
		return false
	}
	if sameStringSet(*stored, next) {
		return true
	}
	if known == nil {
		return false
	}
	pruned := make([]string, 0, len(*stored))
	for _, d := range *stored {
		if known[d] && d != component {
			pruned = append(pruned, d)
		}
	}
	return sameStringSet(pruned, next)
}

// requireAdvancedCustomization writes a 403 (or a 500 when the check cannot be
// made) and returns false when spec makes an advanced change the caller may
// not make. Without an Authorizer it fails closed.
func (h *AIWorkloadHandler) requireAdvancedCustomization(w http.ResponseWriter, r *http.Request, existing *aiplatformv1alpha1.AIWorkloadSpec, spec aiplatformv1alpha1.AIWorkloadSpec, known map[string]bool) bool {
	if !advancedChanges(existing, spec, known) {
		return true
	}
	allowed := false
	if h.authz != nil {
		var err error
		if allowed, err = h.authz.CanCustomizeAdvanced(r.Context()); err != nil {
			writeError(w, http.StatusInternalServerError, errAuthorizationCheckFailed)
			return false
		}
	}
	if !allowed {
		writeError(w, http.StatusForbidden, &APIError{Code: ErrCodeForbidden, Message: advancedCustomizationForbidden})
		return false
	}
	return true
}
