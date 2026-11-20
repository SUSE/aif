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
	"strings"

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

// Highest returns the highest semantic version among versions, ignoring empty
// and non-semver ones; "" when none parses.
func Highest(versions ...string) string {
	var best *semver.Version
	out := ""
	for _, v := range versions {
		sv, err := semver.NewVersion(v)
		if err != nil {
			continue
		}
		if best == nil || sv.GreaterThan(best) {
			best, out = sv, v
		}
	}
	return out
}

// MinimumSafeVersion is the AIWorkload status.minimumSafeVersion value
// recording that version of family started rendering.
func MinimumSafeVersion(family, version string) string {
	return family + "@" + version
}

// FloorFor returns the version recorded in a status.minimumSafeVersion value
// for family: "" when it was recorded for another family. A bare version (no
// "family@") applies to any family.
func FloorFor(stored, family string) string {
	i := strings.LastIndex(stored, "@")
	if i < 0 {
		return stored
	}
	if stored[:i] != family {
		return ""
	}
	return stored[i+1:]
}

// MoveBlocked reports whether moving a workload of family to version to is
// refused, and the version to point the user at instead. Only versions that
// were deployed or started rendering count: a move below the recorded
// minimumSafeVersion (see FloorFor) is always refused, otherwise it is
// RollbackBlocked from the deployed version. The spec version is not used, as
// it may never have rendered (a typo, or a render refused before any write).
func MoveBlocked(ctx context.Context, c client.Reader, family, to, minimumSafe, deployed string) (bool, string, error) {
	floor := FloorFor(minimumSafe, family)
	use := Highest(floor, deployed)
	if IsRollback(floor, to) {
		return true, use, nil
	}
	blocked, err := RollbackBlocked(ctx, c, family, deployed, to)
	return blocked, use, err
}
