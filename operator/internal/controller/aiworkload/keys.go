package aiworkload

import (
	"sort"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	"github.com/SUSE/aif-operator/internal/naming"
)

// HelmOpKey identifies one desired HelmOp/Bundle and how many clusters it targets.
type HelmOpKey struct {
	Namespace        string // fleet-local | fleet-default
	Name             string // deterministic bundle name
	ExpectedClusters int    // clusters this HelmOp is responsible for
}

// blueprintBundleName is the deterministic bundle/HelmOp name for a component, a pure
// function of (workload, chartName). Mirrors the naming used by ensureBlueprintHelmOp.
func blueprintBundleName(workloadName, chartName string) string {
	return naming.TruncateDNS1123Label(workloadName+"-"+naming.Slugify(chartName), 63)
}

// desiredHelmOpKeys returns the sorted set of HelmOps a blueprint workload should have.
// FleetBundle: up to two per component (fleet-local for the local target, fleet-default for
// downstream). GitOps: exactly one per component (fleet-local if local-only, else fleet-default;
// mixed local+downstream GitOps is rejected at admission — Plan 1). Non-blueprint strategies
// (Helm) return nil.
func desiredHelmOpKeys(
	workloadName string,
	targetClusters []string,
	components []aiplatformv1alpha1.BlueprintComponent,
	strategy aiplatformv1alpha1.AIWorkloadDeployStrategy,
) []HelmOpKey {
	localCount, downstreamCount := 0, 0
	for _, c := range targetClusters {
		if c == "local" {
			localCount = 1
		} else {
			downstreamCount++
		}
	}
	hasLocal := localCount > 0
	hasDownstream := downstreamCount > 0

	keys := make([]HelmOpKey, 0, len(components)*2)
	for _, c := range components {
		name := blueprintBundleName(workloadName, c.ChartName)
		switch strategy {
		case aiplatformv1alpha1.AIWorkloadDeployFleetBundle:
			if hasLocal {
				keys = append(keys, HelmOpKey{Namespace: "fleet-local", Name: name, ExpectedClusters: localCount})
			}
			if hasDownstream {
				keys = append(keys, HelmOpKey{Namespace: "fleet-default", Name: name, ExpectedClusters: downstreamCount})
			}
		case aiplatformv1alpha1.AIWorkloadDeployGitOps:
			if hasLocal && !hasDownstream {
				keys = append(keys, HelmOpKey{Namespace: "fleet-local", Name: name, ExpectedClusters: localCount})
			} else {
				keys = append(keys, HelmOpKey{Namespace: "fleet-default", Name: name, ExpectedClusters: downstreamCount})
			}
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Namespace != keys[j].Namespace {
			return keys[i].Namespace < keys[j].Namespace
		}
		return keys[i].Name < keys[j].Name
	})
	return keys
}
