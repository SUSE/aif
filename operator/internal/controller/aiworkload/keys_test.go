package aiworkload

import (
	"testing"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

func comps(charts ...string) []aiplatformv1alpha1.BlueprintComponent {
	out := make([]aiplatformv1alpha1.BlueprintComponent, 0, len(charts))
	for _, c := range charts {
		out = append(out, aiplatformv1alpha1.BlueprintComponent{ChartRepo: "r", ChartName: c, ChartVersion: "1.0.0"})
	}
	return out
}

func TestDesiredHelmOpKeys_FleetBundleMixed(t *testing.T) {
	keys := desiredHelmOpKeys("wl", []string{"local", "c-a"}, comps("open-webui"), aiplatformv1alpha1.AIWorkloadDeployFleetBundle)
	// One component, mixed targets → two keys (fleet-local + fleet-default).
	if len(keys) != 2 {
		t.Fatalf("want 2 keys, got %d: %+v", len(keys), keys)
	}
	// Sorted by (Namespace, Name): fleet-default before fleet-local.
	if keys[0].Namespace != "fleet-default" || keys[1].Namespace != "fleet-local" {
		t.Fatalf("unexpected order: %+v", keys)
	}
	if keys[0].ExpectedClusters != 1 || keys[1].ExpectedClusters != 1 {
		t.Fatalf("want 1 downstream + 1 local, got %+v", keys)
	}
	name := blueprintBundleName("wl", "open-webui")
	if keys[0].Name != name || keys[1].Name != name {
		t.Fatalf("bundle name mismatch: %+v vs %s", keys, name)
	}
}

func TestDesiredHelmOpKeys_FleetBundleLocalOnly(t *testing.T) {
	keys := desiredHelmOpKeys("wl", []string{"local"}, comps("a"), aiplatformv1alpha1.AIWorkloadDeployFleetBundle)
	if len(keys) != 1 || keys[0].Namespace != "fleet-local" || keys[0].ExpectedClusters != 1 {
		t.Fatalf("want single fleet-local/1, got %+v", keys)
	}
}

func TestDesiredHelmOpKeys_FleetBundleDownstreamMultiCluster(t *testing.T) {
	keys := desiredHelmOpKeys("wl", []string{"c-a", "c-b"}, comps("a"), aiplatformv1alpha1.AIWorkloadDeployFleetBundle)
	if len(keys) != 1 || keys[0].Namespace != "fleet-default" || keys[0].ExpectedClusters != 2 {
		t.Fatalf("want single fleet-default/2, got %+v", keys)
	}
}

func TestDesiredHelmOpKeys_GitOps(t *testing.T) {
	local := desiredHelmOpKeys("wl", []string{"local"}, comps("a"), aiplatformv1alpha1.AIWorkloadDeployGitOps)
	if len(local) != 1 || local[0].Namespace != "fleet-local" || local[0].ExpectedClusters != 1 {
		t.Fatalf("gitops local-only: want fleet-local/1, got %+v", local)
	}
	ds := desiredHelmOpKeys("wl", []string{"c-a", "c-b"}, comps("a"), aiplatformv1alpha1.AIWorkloadDeployGitOps)
	if len(ds) != 1 || ds[0].Namespace != "fleet-default" || ds[0].ExpectedClusters != 2 {
		t.Fatalf("gitops downstream-only: want fleet-default/2, got %+v", ds)
	}
}

func TestDesiredHelmOpKeys_ReleaseName(t *testing.T) {
	// Default: ReleaseName falls back to the (capped) chart name.
	def := desiredHelmOpKeys("wl", []string{"local"}, comps("qdrant"), aiplatformv1alpha1.AIWorkloadDeployFleetBundle)
	if len(def) != 1 || def[0].ReleaseName != "qdrant" {
		t.Fatalf("want ReleaseName=qdrant (chart-name default), got %+v", def)
	}

	// Custom component ReleaseName override propagates onto every key.
	custom := []aiplatformv1alpha1.BlueprintComponent{
		{ChartRepo: "r", ChartName: "qdrant", ChartVersion: "1.0.0", ReleaseName: "saif-qdrant"},
	}
	keys := desiredHelmOpKeys("wl", []string{"local", "c-a"}, custom, aiplatformv1alpha1.AIWorkloadDeployFleetBundle)
	if len(keys) != 2 {
		t.Fatalf("want 2 keys, got %d: %+v", len(keys), keys)
	}
	for _, k := range keys {
		if k.ReleaseName != "saif-qdrant" {
			t.Fatalf("want ReleaseName=saif-qdrant on every key, got %+v", k)
		}
	}
}

func TestDesiredHelmOpKeys_Deterministic(t *testing.T) {
	a := desiredHelmOpKeys("wl", []string{"c-b", "c-a"}, comps("z", "a"), aiplatformv1alpha1.AIWorkloadDeployFleetBundle)
	b := desiredHelmOpKeys("wl", []string{"c-a", "c-b"}, comps("a", "z"), aiplatformv1alpha1.AIWorkloadDeployFleetBundle)
	if len(a) != len(b) {
		t.Fatalf("length mismatch %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("nondeterministic at %d: %+v vs %+v", i, a[i], b[i])
		}
	}
}
