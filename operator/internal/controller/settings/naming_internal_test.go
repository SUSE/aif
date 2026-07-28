package settings

import "testing"

func TestTeamClusterRepoName_Slugs(t *testing.T) {
	cases := map[string]string{
		"https://helm.ngc.nvidia.com/nvidia/omniverse":         "nvidia-omniverse",
		"https://helm.ngc.nvidia.com/nim/nvidia":               "nim-nvidia",
		"https://helm.ngc.nvidia.com/nvidia/omniverse-usdcode": "nvidia-omniverse-usdcode",
	}
	for in, want := range cases {
		got, err := teamClusterRepoName(in)
		if err != nil {
			t.Fatalf("teamClusterRepoName(%q): %v", in, err)
		}
		if got != want {
			t.Errorf("teamClusterRepoName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTeamClusterRepoName_RejectsNonNGCAndOrgCollisions(t *testing.T) {
	if _, err := teamClusterRepoName("oci://registry.internal/nvidia"); err == nil {
		t.Error("expected error for non-NGC URL")
	}
	// A path that would slug to an org ClusterRepo name must be rejected.
	if _, err := teamClusterRepoName("https://helm.ngc.nvidia.com/nvidia"); err == nil {
		t.Error("expected error for org-name collision (nvidia)")
	}
}
