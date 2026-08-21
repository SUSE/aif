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

// Package migrate relocates existing AIWorkload CRs into the single control-cluster
// workload namespace (see config.GetWorkloadNamespace).
//
// The downstream deployment identity of a workload (Fleet Bundle / HelmOp / Helm
// release name, target namespace, target cluster) is derived from the CR's Name,
// chart, Spec.TargetNamespace and target cluster — never from the CR's own
// namespace. A re-created CR with the same Name therefore recomputes identical
// downstream names and the operator re-adopts the already-running workload with no
// redeploy.
//
// The one hazard is the finalizer: deleting an AIWorkload with the cleanup
// finalizer attached tears down the live downstream workload. Migration therefore
// strips the finalizer from the source CR *before* deleting it, so no teardown runs.
package migrate

import (
	"context"
	"fmt"
	"sort"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	aiWorkloadFinalizer    = "ai-factory.suse.com/cleanup"
	migratedFromAnnotation = "ai-factory.suse.com/migrated-from"
	ownerNameLabel         = "ai-factory.suse.com/owner-name"
	ownerNamespaceLabel    = "ai-factory.suse.com/owner-namespace"
)

// fleetNamespaces are the Fleet workspaces where pull-secret Bundles may live.
var fleetNamespaces = []string{"fleet-local", "fleet-default"}

var bundleListGVK = schema.GroupVersionKind{
	Group: "fleet.cattle.io", Version: "v1alpha1", Kind: "BundleList",
}

// Options controls a migration run.
type Options struct {
	// DryRun reports what would happen without mutating any resource.
	DryRun bool
}

// Skip records a source CR that was not migrated because a different CR already
// occupies its name in the workload namespace.
type Skip struct {
	Source   string // "<namespace>/<name>"
	Conflict string // "<workloadNamespace>/<name>"
	Reason   string
}

// Report summarizes a migration run.
type Report struct {
	// WorkloadNamespace is the destination namespace.
	WorkloadNamespace string
	// AlreadyInPlace lists CRs already residing in the workload namespace.
	AlreadyInPlace []string
	// Migrated lists relocations as "<oldNamespace>/<name> -> <workloadNamespace>/<name>".
	Migrated []string
	// Skipped lists name collisions that require manual resolution.
	Skipped []Skip
	// OrphanBundlesDeleted counts pull-secret Bundles swept after relocation.
	OrphanBundlesDeleted int
	// Warnings holds non-fatal problems (e.g. best-effort bundle cleanup failures).
	Warnings []string
	// DryRun echoes whether this run mutated anything.
	DryRun bool
}

// Run relocates every AIWorkload CR that lives outside workloadNamespace into it.
// It is idempotent and safe to re-run: CRs already in place are skipped, and a
// partially-migrated CR (destination copy exists, source not yet deleted) is
// resumed rather than reported as a collision.
func Run(ctx context.Context, c client.Client, workloadNamespace string, opts Options) (*Report, error) {
	if workloadNamespace == "" {
		return nil, fmt.Errorf("workloadNamespace must not be empty")
	}
	report := &Report{WorkloadNamespace: workloadNamespace, DryRun: opts.DryRun}

	var list aiplatformv1alpha1.AIWorkloadList
	if err := c.List(ctx, &list); err != nil {
		return nil, fmt.Errorf("list AIWorkloads: %w", err)
	}

	// Names already occupying the destination namespace, mapped to whether the
	// occupant is one of our own migrated copies (so a resumed run is not treated
	// as a collision).
	destOccupant := map[string]*aiplatformv1alpha1.AIWorkload{}
	var sources []aiplatformv1alpha1.AIWorkload
	for i := range list.Items {
		w := list.Items[i]
		if w.Namespace == workloadNamespace {
			destOccupant[w.Name] = &list.Items[i]
			report.AlreadyInPlace = append(report.AlreadyInPlace, w.Namespace+"/"+w.Name)
			continue
		}
		sources = append(sources, w)
	}

	// Deterministic order keeps reports and collision resolution stable.
	sort.Slice(sources, func(i, j int) bool {
		if sources[i].Namespace != sources[j].Namespace {
			return sources[i].Namespace < sources[j].Namespace
		}
		return sources[i].Name < sources[j].Name
	})

	for i := range sources {
		src := &sources[i]
		migratedFrom := src.Namespace + "/" + src.Name

		if occ, ok := destOccupant[src.Name]; ok {
			// A destination CR with this name already exists. If it is our own
			// migrated copy of this exact source, resume; otherwise it is a real
			// collision that a human must resolve (renaming changes downstream
			// identity, so we never auto-rename).
			if occ.Annotations[migratedFromAnnotation] != migratedFrom {
				report.Skipped = append(report.Skipped, Skip{
					Source:   migratedFrom,
					Conflict: workloadNamespace + "/" + src.Name,
					Reason:   "name already used by a different workload in the destination namespace",
				})
				continue
			}
			// Resume: destination copy exists; finish by removing the source.
			if err := finalizeSource(ctx, c, src, opts); err != nil {
				report.Warnings = append(report.Warnings, err.Error())
				continue
			}
			report.Migrated = append(report.Migrated, migratedFrom+" -> "+workloadNamespace+"/"+src.Name)
			sweepOrphans(ctx, c, src.Name, src.Namespace, opts, report)
			continue
		}

		if opts.DryRun {
			report.Migrated = append(report.Migrated, migratedFrom+" -> "+workloadNamespace+"/"+src.Name)
			destOccupant[src.Name] = src // reserve the name for collision detection
			continue
		}

		if err := createDestination(ctx, c, src, workloadNamespace, migratedFrom); err != nil {
			report.Warnings = append(report.Warnings, fmt.Sprintf("create %s/%s: %v", workloadNamespace, src.Name, err))
			continue
		}
		if err := finalizeSource(ctx, c, src, opts); err != nil {
			report.Warnings = append(report.Warnings, err.Error())
			continue
		}
		report.Migrated = append(report.Migrated, migratedFrom+" -> "+workloadNamespace+"/"+src.Name)
		// Reserve the destination name so a same-named source later in the run is
		// correctly reported as a collision.
		reserved := src.DeepCopy()
		if reserved.Annotations == nil {
			reserved.Annotations = map[string]string{}
		}
		reserved.Annotations[migratedFromAnnotation] = migratedFrom
		destOccupant[src.Name] = reserved

		sweepOrphans(ctx, c, src.Name, src.Namespace, opts, report)
	}

	return report, nil
}

// createDestination creates a copy of src in workloadNamespace carrying the same
// name, spec, labels and annotations (plus a migrated-from marker) and preserving
// status. It is a no-op if the destination already exists.
func createDestination(ctx context.Context, c client.Client, src *aiplatformv1alpha1.AIWorkload, workloadNamespace, migratedFrom string) error {
	dst := &aiplatformv1alpha1.AIWorkload{}
	dst.Name = src.Name
	dst.Namespace = workloadNamespace
	dst.Labels = copyStringMap(src.Labels)
	dst.Annotations = copyStringMap(src.Annotations)
	if dst.Annotations == nil {
		dst.Annotations = map[string]string{}
	}
	dst.Annotations[migratedFromAnnotation] = migratedFrom
	src.Spec.DeepCopyInto(&dst.Spec)

	if err := c.Create(ctx, dst); err != nil {
		if client.IgnoreAlreadyExists(err) == nil {
			return nil
		}
		return err
	}

	// Best-effort: preserve status (phase, per-cluster statuses) so the UI shows
	// continuity. A failure here does not affect the running workload.
	dst.Status = *src.Status.DeepCopy()
	if err := c.Status().Update(ctx, dst); err != nil {
		return fmt.Errorf("preserve status for %s/%s: %w", workloadNamespace, dst.Name, err)
	}
	return nil
}

// finalizeSource strips the cleanup finalizer from the source CR and deletes it.
// Removing the finalizer first is what prevents the deletion from tearing down the
// live downstream workload.
func finalizeSource(ctx context.Context, c client.Client, src *aiplatformv1alpha1.AIWorkload, opts Options) error {
	if opts.DryRun {
		return nil
	}
	if controllerutil.ContainsFinalizer(src, aiWorkloadFinalizer) {
		controllerutil.RemoveFinalizer(src, aiWorkloadFinalizer)
		if err := c.Update(ctx, src); err != nil {
			return fmt.Errorf("strip finalizer from %s/%s: %w", src.Namespace, src.Name, err)
		}
	}
	if err := c.Delete(ctx, src); err != nil && client.IgnoreNotFound(err) != nil {
		return fmt.Errorf("delete source %s/%s: %w", src.Namespace, src.Name, err)
	}
	return nil
}

// sweepOrphans deletes pull-secret Bundles still labelled with the source CR's old
// namespace, which the destination CR's reconcile has re-created under the new
// namespace. Best-effort: failures are recorded as warnings, not fatal.
func sweepOrphans(ctx context.Context, c client.Client, name, oldNamespace string, opts Options, report *Report) {
	if opts.DryRun {
		return
	}
	for _, ns := range fleetNamespaces {
		var bundles unstructured.UnstructuredList
		bundles.SetGroupVersionKind(bundleListGVK)
		err := c.List(ctx, &bundles,
			client.InNamespace(ns),
			client.MatchingLabels{
				ownerNameLabel:      name,
				ownerNamespaceLabel: oldNamespace,
			},
		)
		if err != nil {
			report.Warnings = append(report.Warnings,
				fmt.Sprintf("list orphan bundles in %s for %s/%s: %v", ns, oldNamespace, name, err))
			continue
		}
		for i := range bundles.Items {
			b := &bundles.Items[i]
			if err := c.Delete(ctx, b); err != nil && client.IgnoreNotFound(err) != nil {
				report.Warnings = append(report.Warnings,
					fmt.Sprintf("delete orphan bundle %s/%s: %v", b.GetNamespace(), b.GetName(), err))
				continue
			}
			report.OrphanBundlesDeleted++
		}
	}
}

func copyStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
