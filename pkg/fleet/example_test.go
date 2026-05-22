package fleet_test

import (
	"fmt"

	"github.com/SUSE/aif/pkg/fleet"
	"github.com/SUSE/aif/pkg/workload"
)

func Example_buildBundleCR_singleComponent() {
	spec := fleet.BundleDeploymentSpec{
		WorkloadID:     "demo",
		WorkloadNS:     "team-a",
		TargetClusters: []string{"prod-east", "prod-west"},
		Components: []fleet.ComponentBundle{{
			Name:     "llama",
			ChartRef: "oci://registry.example.test/ai/charts/nim-llm:1.2.3",
			Values:   map[string]any{"replicas": 2},
		}},
		PullSecretData: []byte(`{"auths":{}}`),
		Owner: fleet.OwnerRef{
			APIVersion: "ai.suse.com/v1alpha1",
			Kind:       "Workload",
			Name:       "demo",
			UID:        "u-1",
			Controller: true,
		},
	}
	b, err := fleet.BuildBundleCRForTest(spec)
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println("name:", b.Name)
	fmt.Println("targets:", len(b.Spec.Targets))
	fmt.Println("chart:", b.Spec.Helm.Chart)
	fmt.Println("resources:", len(b.Spec.Resources))
	// Output:
	// name: team-a-demo
	// targets: 2
	// chart: oci://registry.example.test/ai/charts/nim-llm:1.2.3
	// resources: 1
}

func Example_workloadMapFleetStateToPhase() {
	for _, s := range []string{"Ready", "Modified", "ErrApplied", "Pending", ""} {
		fmt.Printf("%-10s -> %s\n", s, workload.MapFleetStateToPhase(s))
	}
	// Output:
	// Ready      -> Running
	// Modified   -> Running
	// ErrApplied -> Failed
	// Pending    -> Deploying
	//            -> Deploying
}
