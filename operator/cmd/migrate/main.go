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

// Command migrate relocates existing AIWorkload CRs into the single control-cluster
// workload namespace. It is intended to be run once, out-of-band (for example as a
// Kubernetes Job using the operator image), after upgrading to a build that stores
// new AIWorkloads in the workload namespace.
//
// Usage:
//
//	migrate [-workload-namespace aif-workloads] [-dry-run]
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	"github.com/SUSE/aif-operator/internal/config"
	"github.com/SUSE/aif-operator/internal/migrate"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func main() {
	var (
		workloadNamespace string
		dryRun            bool
	)
	flag.StringVar(&workloadNamespace, "workload-namespace", config.GetWorkloadNamespace(),
		"destination namespace for AIWorkload CRs (defaults to $WORKLOAD_NAMESPACE or aif-workloads)")
	flag.BoolVar(&dryRun, "dry-run", false, "report what would change without mutating any resource")
	flag.Parse()

	if err := run(workloadNamespace, dryRun); err != nil {
		fmt.Fprintln(os.Stderr, "migration failed:", err)
		os.Exit(1)
	}
}

func run(workloadNamespace string, dryRun bool) error {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		return err
	}
	if err := aiplatformv1alpha1.AddToScheme(scheme); err != nil {
		return err
	}

	c, err := client.New(ctrl.GetConfigOrDie(), client.Options{Scheme: scheme})
	if err != nil {
		return fmt.Errorf("build client: %w", err)
	}

	report, err := migrate.Run(context.Background(), c, workloadNamespace, migrate.Options{DryRun: dryRun})
	if err != nil {
		return err
	}
	printReport(report)
	if len(report.Skipped) > 0 {
		// Name collisions need a human decision; surface as a non-zero exit so a
		// Job is marked failed and the operator notices.
		return fmt.Errorf("%d workload(s) skipped due to name collisions; resolve and re-run", len(report.Skipped))
	}
	return nil
}

func printReport(r *migrate.Report) {
	mode := "applied"
	if r.DryRun {
		mode = "dry-run (no changes made)"
	}
	fmt.Printf("AIWorkload migration to namespace %q — %s\n", r.WorkloadNamespace, mode)
	fmt.Printf("  already in place: %d\n", len(r.AlreadyInPlace))
	fmt.Printf("  migrated:         %d\n", len(r.Migrated))
	for _, m := range r.Migrated {
		fmt.Printf("    - %s\n", m)
	}
	if len(r.Skipped) > 0 {
		fmt.Printf("  skipped (collisions): %d\n", len(r.Skipped))
		for _, s := range r.Skipped {
			fmt.Printf("    - %s (conflicts with %s): %s\n", s.Source, s.Conflict, s.Reason)
		}
	}
	fmt.Printf("  orphan pull-secret bundles deleted: %d\n", r.OrphanBundlesDeleted)
	if len(r.Warnings) > 0 {
		fmt.Printf("  warnings: %d\n", len(r.Warnings))
		for _, w := range r.Warnings {
			fmt.Printf("    - %s\n", w)
		}
	}
}
