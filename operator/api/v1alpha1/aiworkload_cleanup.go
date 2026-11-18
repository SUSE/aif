package v1alpha1

import "strings"

// ExternalHelmCleanupAnnotation records the caller's confirmation that a direct
// Helm release on a downstream cluster was removed using their Rancher session.
// Fleet workloads are cleaned up by Fleet; local Helm uses the operator identity.
const ExternalHelmCleanupAnnotation = "ai-factory.suse.com/external-helm-cleanup"

// ExternalHelmCleanupID binds a confirmation to this CR and its exact release
// target. It is an acknowledgement, not a credential or proof of authorization.
// A changed target or recreated workload cannot reuse an old acknowledgement.
func (w *AIWorkload) ExternalHelmCleanupID() string {
	if w.UID == "" || (w.Spec.DeployStrategy != "" && w.Spec.DeployStrategy != AIWorkloadDeployHelm) ||
		w.Spec.Source.App == nil || len(w.Spec.TargetClusters) != 1 ||
		w.Spec.TargetClusters[0] == "" || w.Spec.TargetClusters[0] == "local" {
		return ""
	}
	return strings.Join([]string{string(w.UID), w.Spec.TargetClusters[0], w.Spec.TargetNamespace, w.Spec.Source.App.Release}, "/")
}
