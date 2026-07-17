package aiworkload

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// sortedCopy returns a sorted copy of a string slice (leaves input untouched).
func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

// intentDigest hashes only the user-owned, recovery-relevant intent fields — never
// operator-derived (fleetBundleNames) or display-only (displayName) fields — so operator
// writes to spec do not change operation identity.
func intentDigest(spec aiplatformv1alpha1.AIWorkloadSpec) string {
	type intent struct {
		BlueprintName    string   `json:"blueprintName"`
		BlueprintVersion string   `json:"blueprintVersion"`
		TargetNamespace  string   `json:"targetNamespace"`
		DeployStrategy   string   `json:"deployStrategy"`
		TargetClusters   []string `json:"targetClusters"`
	}
	i := intent{
		TargetNamespace: spec.TargetNamespace,
		DeployStrategy:  string(spec.DeployStrategy),
		TargetClusters:  sortedCopy(spec.TargetClusters),
	}
	if spec.Source.Blueprint != nil {
		i.BlueprintName = spec.Source.Blueprint.Name
		i.BlueprintVersion = spec.Source.Blueprint.Version
	}
	b, _ := json.Marshal(i)
	return sha256Hex(b)
}

// ComponentRenderInputs is the exact desired, operator-owned HelmOp render for one component.
// It intentionally OMITS volatile fields (forceSyncGeneration) and the digest label itself.
type ComponentRenderInputs struct {
	ChartRepo    string         `json:"chartRepo"`
	ChartName    string         `json:"chartName"`
	ChartVersion string         `json:"chartVersion"`
	Namespace    string         `json:"namespace"`
	Vendor       string         `json:"vendor"`
	RepoURL      string         `json:"repoURL"`
	Targets      []string       `json:"targets"`
	Values       map[string]any `json:"values"`
}

// perHelmOpRenderDigest hashes the canonical form of one component's desired HelmOp render.
// Targets (a set) are sorted; encoding/json sorts all map keys, so Values ordering is normalized.
func perHelmOpRenderDigest(in ComponentRenderInputs) string {
	in.Targets = sortedCopy(in.Targets)
	b, _ := json.Marshal(in)
	return sha256Hex(b)
}

// AggregateEntry is one (namespace, name) → per-HelmOp digest pair.
type AggregateEntry struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Digest    string `json:"digest"`
}

// aggregateRenderDigest combines per-HelmOp digests into the workload-level render digest,
// sorted by (namespace, name) so entry order is irrelevant.
func aggregateRenderDigest(entries []AggregateEntry) string {
	sorted := append([]AggregateEntry(nil), entries...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Namespace != sorted[j].Namespace {
			return sorted[i].Namespace < sorted[j].Namespace
		}
		return sorted[i].Name < sorted[j].Name
	})
	b, _ := json.Marshal(sorted)
	return sha256Hex(b)
}
