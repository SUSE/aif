package fleet

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	fleetv1 "github.com/rancher/fleet/pkg/apis/fleet.cattle.io/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/SUSE/aif/pkg/git"
)

const (
	// labelCluster pins a per-cluster GitRepo CR to one downstream
	// cluster. Used by Teardown to delete by selector when the workload
	// had no per-cluster bookkeeping at delete time.
	labelCluster = "ai.suse.com/cluster"
)

// gitRepoName returns the per-cluster GitRepo CR name:
//
//	{ns}-{workloadID}-{cluster}, lowercased + DNS-1123 sanitized.
//
// When the result exceeds 63 chars, replace the tail with
// "-{sha256(ns+'/'+id+'/'+cluster)[0:8]}" so two long inputs that share
// a prefix don't collide post-truncation. Same algorithm as
// fleetBundleName but with an extra component.
func gitRepoName(ns, workloadID, cluster string) string {
	raw := strings.ToLower(ns + "-" + workloadID + "-" + cluster)
	clean := dnsInvalid.ReplaceAllString(raw, "-")
	for strings.Contains(clean, "--") {
		clean = strings.ReplaceAll(clean, "--", "-")
	}
	clean = strings.Trim(clean, "-")
	if len(clean) <= maxFleetBundleNameLen {
		return clean
	}
	sum := sha256.Sum256([]byte(ns + "/" + workloadID + "/" + cluster))
	suffix := "-" + hex.EncodeToString(sum[:])[:suffixLen]
	head := clean[:maxFleetBundleNameLen-len(suffix)]
	head = strings.TrimRight(head, "-")
	return head + suffix
}

// gitRepoPath returns the GitRepo.Spec.Paths[0] entry:
// "gitops/{cluster}/{workload}".
func gitRepoPath(workloadID, cluster string) string {
	return fmt.Sprintf("gitops/%s/%s", cluster, workloadID)
}

// validateGitRepoSpec mirrors validateSpec (Bundle path) for the GitRepo path.
func validateGitRepoSpec(s GitRepoDeploymentSpec) error {
	if s.WorkloadID == "" {
		return fmt.Errorf("WorkloadID is required")
	}
	if s.WorkloadNS == "" {
		return fmt.Errorf("WorkloadNS is required")
	}
	if len(s.Components) == 0 {
		return fmt.Errorf("at least one Component is required")
	}
	if len(s.Components) > git.MaxComponentIndex+1 {
		return fmt.Errorf("too many Components: %d (limit %d, see git.MaxComponentIndex)",
			len(s.Components), git.MaxComponentIndex+1)
	}
	if len(s.TargetClusters) == 0 {
		return fmt.Errorf("at least one TargetClusters entry is required")
	}
	return nil
}

// buildGitRepoCR translates a GitRepoDeploymentSpec into one
// fleet.cattle.io/v1alpha1 GitRepo CR, scoped to the supplied cluster.
// Pure function — no I/O.
//
// Two intentional gaps in P4-3, both addressed in P5-4b:
//
//   - ClientSecretName is NOT set. Until P5-4b extends FleetSettings with
//     a GitClientSecretName field and wires it here, Fleet itself can't
//     clone the remote — but the operator-side reconciliation works
//     end-to-end (which is what P4-3 verifies).
//
//   - spec.PullSecretData is ignored. The original design called for
//     embedding the suse-registry-creds manifest into
//     GitRepo.Spec.Resources, but fleet.cattle.io/v1alpha1.GitRepoSpec
//     has no Resources field (verified against v0.10.14 and v0.15.2 —
//     Resources lives only on GitRepoStatus). P5-4b will introduce an
//     out-of-band downstream-cluster pull-secret reconciler; until then
//     the field is plumbed through GitRepoDeploymentSpec for upstream
//     compatibility but unused here.
func buildGitRepoCR(
	spec GitRepoDeploymentSpec,
	cluster string,
	settings FleetSettings,
) (*fleetv1.GitRepo, error) {
	if err := validateGitRepoSpec(spec); err != nil {
		return nil, err
	}

	gr := &fleetv1.GitRepo{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gitRepoName(spec.WorkloadNS, spec.WorkloadID, cluster),
			Namespace: spec.WorkloadNS,
			Labels: map[string]string{
				labelManagedBy: managedByValue,
				labelWorkload:  spec.WorkloadID,
				labelCluster:   cluster,
			},
			OwnerReferences: []metav1.OwnerReference{toOwnerReference(spec.Owner)},
		},
		Spec: fleetv1.GitRepoSpec{
			Repo:   settings.GitRepoURL,
			Branch: settings.GitBranch,
			Paths:  []string{gitRepoPath(spec.WorkloadID, cluster)},
			Targets: []fleetv1.GitTarget{
				{ClusterName: cluster},
			},
		},
	}
	gr.SetGroupVersionKind(fleetv1.SchemeGroupVersion.WithKind("GitRepo"))
	return gr, nil
}
