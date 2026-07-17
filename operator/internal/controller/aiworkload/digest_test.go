package aiworkload

import (
	"strings"
	"testing"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

func intentSpec(version, ns, strategy string, clusters ...string) aiplatformv1alpha1.AIWorkloadSpec {
	return aiplatformv1alpha1.AIWorkloadSpec{
		DisplayName:     "display",
		TargetNamespace: ns,
		DeployStrategy:  aiplatformv1alpha1.AIWorkloadDeployStrategy(strategy),
		TargetClusters:  clusters,
		Source: aiplatformv1alpha1.AIWorkloadSource{
			SourceType: aiplatformv1alpha1.AIWorkloadSourceBlueprint,
			Blueprint:  &aiplatformv1alpha1.BlueprintSource{Name: "rag", Version: version},
		},
	}
}

func TestIntentDigest_StableAcrossIrrelevantChanges(t *testing.T) {
	a := intentSpec("1.0.0", "ai", "FleetBundle", "c-a", "c-b")
	b := intentSpec("1.0.0", "ai", "FleetBundle", "c-b", "c-a") // reordered set
	b.DisplayName = "different-name"                            // display-only
	b.FleetBundleNames = []string{"x", "y"}                     // operator-derived
	if intentDigest(a) != intentDigest(b) {
		t.Fatalf("digest changed on irrelevant/reordered fields: %s vs %s", intentDigest(a), intentDigest(b))
	}
	if !strings.HasPrefix(intentDigest(a), "sha256:") {
		t.Fatalf("digest not prefixed: %s", intentDigest(a))
	}
}

func TestIntentDigest_ChangesOnIntentFields(t *testing.T) {
	base := intentSpec("1.0.0", "ai", "FleetBundle", "c-a")
	changes := []aiplatformv1alpha1.AIWorkloadSpec{
		intentSpec("2.0.0", "ai", "FleetBundle", "c-a"),        // version
		intentSpec("1.0.0", "other", "FleetBundle", "c-a"),     // namespace
		intentSpec("1.0.0", "ai", "GitOps", "c-a"),             // strategy
		intentSpec("1.0.0", "ai", "FleetBundle", "c-a", "c-b"), // targets
	}
	for i, c := range changes {
		if intentDigest(base) == intentDigest(c) {
			t.Errorf("case %d: digest should differ on intent change", i)
		}
	}
}
