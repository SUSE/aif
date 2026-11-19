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

// Package blueprintpolicy holds the Blueprint policy rules the API (early
// rejection) and the AIWorkload controller (authoritative enforcement, since
// kubectl and GitOps bypass the API) must agree on.
package blueprintpolicy

import (
	"context"

	"github.com/Masterminds/semver/v3"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	"github.com/SUSE/aif-operator/internal/naming"
)

// IsRollback reports whether to is an older semantic version than from.
// Versions that don't parse as semver are never a rollback.
func IsRollback(from, to string) bool {
	f, err := semver.NewVersion(from)
	if err != nil {
		return false
	}
	t, err := semver.NewVersion(to)
	if err != nil {
		return false
	}
	return t.LessThan(f)
}

// RollbackDisabled reports whether any listed version of the Blueprint family
// sets Rollback=Disabled. A missing Blueprint CR counts as allowed (fail open):
// a deleted version can't state a policy, and blocking on it would strand the
// workload on a version that may no longer render.
func RollbackDisabled(ctx context.Context, c client.Reader, family string, versions ...string) (bool, error) {
	seen := map[string]bool{}
	for _, v := range versions {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		var bp aiplatformv1alpha1.Blueprint
		if err := c.Get(ctx, types.NamespacedName{Name: naming.BlueprintCRName(family, v)}, &bp); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return false, err
		}
		if bp.Spec.Rollback == aiplatformv1alpha1.RollbackDisabled {
			return true, nil
		}
	}
	return false, nil
}

// RollbackBlocked reports whether moving a workload from the deployed version
// to version to is a rollback that either version's Blueprint disables.
func RollbackBlocked(ctx context.Context, c client.Reader, family, deployed, to string) (bool, error) {
	if !IsRollback(deployed, to) {
		return false, nil
	}
	return RollbackDisabled(ctx, c, family, deployed, to)
}
