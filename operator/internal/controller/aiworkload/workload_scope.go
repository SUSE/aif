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
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

// workloadScope is what an AIWorkload is entitled to, derived from its spec
// (and Blueprint) rather than from its status: the chart repositories whose
// pull secrets may be delivered for it, and the Helm releases, per namespace,
// whose pods may be recreated on its behalf.
type workloadScope struct {
	repos    map[string]bool
	releases map[string]map[string]bool
	// resolved is false when the scope could not be determined (e.g. the
	// Blueprint is gone); callers then leave existing deliveries alone.
	resolved bool
}

func (s workloadScope) addRelease(namespace, release string) {
	if s.releases[namespace] == nil {
		s.releases[namespace] = map[string]bool{}
	}
	s.releases[namespace][release] = true
}

// resolveWorkloadScope derives the scope of w. App workloads use their chart
// repository and release; Blueprint workloads use each enabled component's
// repository, namespace and release. A Blueprint that cannot be found yields
// an unresolved scope; existing deliveries are then left unchanged.
func (r *AIWorkloadReconciler) resolveWorkloadScope(ctx context.Context, w *aiplatformv1alpha1.AIWorkload) (workloadScope, error) {
	scope := workloadScope{repos: map[string]bool{}, releases: map[string]map[string]bool{}, resolved: true}
	switch {
	case w.Spec.Source.App != nil:
		src := w.Spec.Source.App
		scope.repos[src.ChartRepo] = true
		scope.addRelease(w.Spec.TargetNamespace, capReleaseName(src.Release))
	case w.Spec.Source.Blueprint != nil:
		src := w.Spec.Source.Blueprint
		var bp aiplatformv1alpha1.Blueprint
		if err := r.Get(ctx, types.NamespacedName{Name: bpCRName(src.Name, src.Version)}, &bp); err != nil {
			if errors.IsNotFound(err) {
				return workloadScope{repos: map[string]bool{}, releases: map[string]map[string]bool{}}, nil
			}
			return scope, fmt.Errorf("get Blueprint for workload %s/%s: %w", w.Namespace, w.Name, err)
		}
		for _, c := range filterEnabledComponents(w, bp.Spec.Components) {
			scope.repos[c.ChartRepo] = true
			scope.addRelease(componentNamespace(w, c), capReleaseName(componentReleaseName(c)))
		}
	}
	return scope, nil
}
