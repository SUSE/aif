package fleet

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	fleetv1 "github.com/rancher/fleet/pkg/apis/fleet.cattle.io/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/yaml"
)

// maxFleetBundleNameLen is the DNS-1123 subdomain limit (Fleet Bundle is
// namespaced, name maps to a subdomain).
const maxFleetBundleNameLen = 63

// suffixLen is the stable SHA-8 suffix length used when the name needs
// truncation. 8 hex chars = 32 bits ≈ 1-in-4-billion collision per
// (ns, id) pair, well under the count of workloads any cluster carries.
const suffixLen = 8

var dnsInvalid = regexp.MustCompile(`[^a-z0-9-]+`)

// fleetBundleName returns the Fleet Bundle name for a workload:
//
//	"{ns}-{workloadID}"   lowercased + DNS-1123-sanitized
//
// When the result exceeds 63 chars, the tail is replaced with
// "-{sha256(ns+'/'+id)[0:8]}" so that two long workload IDs that share a
// prefix don't collide post-truncation. Deterministic and idempotent.
func fleetBundleName(ns, workloadID string) string {
	raw := strings.ToLower(ns + "-" + workloadID)
	clean := dnsInvalid.ReplaceAllString(raw, "-")
	for strings.Contains(clean, "--") {
		clean = strings.ReplaceAll(clean, "--", "-")
	}
	clean = strings.Trim(clean, "-")

	if len(clean) <= maxFleetBundleNameLen {
		return clean
	}

	sum := sha256.Sum256([]byte(ns + "/" + workloadID))
	suffix := "-" + hex.EncodeToString(sum[:])[:suffixLen]
	head := clean[:maxFleetBundleNameLen-len(suffix)]
	head = strings.TrimRight(head, "-")
	return head + suffix
}

// labelManagedBy and labelWorkload tag Fleet Bundles created by this
// controller so they're cheap to list/audit.
const (
	labelManagedBy = "ai.suse.com/managed-by"
	labelWorkload  = "ai.suse.com/workload"
	managedByValue = "aif-workload-controller"
)

// pullSecretName is the Secret name embedded into the Fleet Bundle's
// resources/ for downstream image pulls. Matches the upstream
// suse-registry-creds convention (single pull-secret per workload ns).
const pullSecretName = "suse-registry-creds"

// validateSpec enforces required fields. Returns nil on success.
func validateSpec(s BundleDeploymentSpec) error {
	if s.WorkloadID == "" {
		return fmt.Errorf("WorkloadID is required")
	}
	if s.WorkloadNS == "" {
		return fmt.Errorf("WorkloadNS is required")
	}
	if len(s.Components) == 0 {
		return fmt.Errorf("at least one Component is required")
	}
	if len(s.TargetClusters) == 0 {
		return fmt.Errorf("at least one TargetClusters entry is required")
	}
	for i, c := range s.Components {
		if c.Name == "" {
			return fmt.Errorf("Components[%d].Name is required", i)
		}
		if c.ChartRef == "" {
			return fmt.Errorf("Components[%d].ChartRef is required", i)
		}
	}
	return nil
}

// buildBundleCR translates a BundleDeploymentSpec into a fully-formed
// fleet.cattle.io/v1alpha1 Bundle CR. Pure function — no I/O.
func buildBundleCR(spec BundleDeploymentSpec) (*fleetv1.Bundle, error) {
	if err := validateSpec(spec); err != nil {
		return nil, err
	}

	first := spec.Components[0]

	resources := make([]fleetv1.BundleResource, 0, len(spec.Components))
	valuesFiles := make([]string, 0, len(spec.Components)-1)

	for _, c := range spec.Components[1:] {
		yml, err := yaml.Marshal(c.Values)
		if err != nil {
			return nil, fmt.Errorf("marshal values for %q: %w", c.Name, err)
		}
		path := "values/" + c.Name + ".yaml"
		resources = append(resources, fleetv1.BundleResource{
			Name:    path,
			Content: string(yml),
		})
		valuesFiles = append(valuesFiles, path)
	}

	if len(spec.PullSecretData) > 0 {
		secretManifest, err := renderPullSecret(spec.WorkloadNS, spec.PullSecretData)
		if err != nil {
			return nil, fmt.Errorf("render pull-secret manifest: %w", err)
		}
		resources = append(resources, fleetv1.BundleResource{
			Name:    "manifests/suse-registry-creds.yaml",
			Content: secretManifest,
		})
	}

	// JSON round-trip normalizes nested maps to map[string]interface{} and
	// []interface{} so Fleet's GenericMap.DeepCopyInto can recurse correctly
	// — its deepCopyMap only handles those two stdlib types.
	firstValuesJSON, err := json.Marshal(first.Values)
	if err != nil {
		return nil, fmt.Errorf("marshal first-component values: %w", err)
	}
	var firstValuesData map[string]any
	if err := json.Unmarshal(firstValuesJSON, &firstValuesData); err != nil {
		return nil, fmt.Errorf("re-unmarshal first values: %w", err)
	}

	targets := make([]fleetv1.BundleTarget, 0, len(spec.TargetClusters))
	for _, c := range spec.TargetClusters {
		targets = append(targets, fleetv1.BundleTarget{
			Name:        c,
			ClusterName: c,
		})
	}

	return &fleetv1.Bundle{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fleetBundleName(spec.WorkloadNS, spec.WorkloadID),
			Namespace: spec.WorkloadNS,
			Labels: map[string]string{
				labelManagedBy: managedByValue,
				labelWorkload:  spec.WorkloadID,
			},
			OwnerReferences: []metav1.OwnerReference{toOwnerReference(spec.Owner)},
		},
		Spec: fleetv1.BundleSpec{
			BundleDeploymentOptions: fleetv1.BundleDeploymentOptions{
				Helm: &fleetv1.HelmOptions{
					Chart:       first.ChartRef,
					Values:      &fleetv1.GenericMap{Data: firstValuesData},
					ValuesFiles: valuesFiles,
				},
			},
			Resources: resources,
			Targets:   targets,
		},
	}, nil
}

func toOwnerReference(o OwnerRef) metav1.OwnerReference {
	controller := o.Controller
	blockOwnerDeletion := true
	return metav1.OwnerReference{
		APIVersion:         o.APIVersion,
		Kind:               o.Kind,
		Name:               o.Name,
		UID:                types.UID(o.UID),
		Controller:         &controller,
		BlockOwnerDeletion: &blockOwnerDeletion,
	}
}

// renderPullSecret produces a YAML Secret manifest carrying the supplied
// .dockerconfigjson payload. Lives here (not as a Go template) because
// Fleet ships the file content verbatim to the downstream cluster.
func renderPullSecret(ns string, dockerConfigJSON []byte) (string, error) {
	secret := map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"type":       "kubernetes.io/dockerconfigjson",
		"metadata": map[string]any{
			"name":      pullSecretName,
			"namespace": ns,
		},
		"data": map[string]any{
			".dockerconfigjson": base64.StdEncoding.EncodeToString(dockerConfigJSON),
		},
	}
	out, err := yaml.Marshal(secret)
	if err != nil {
		return "", err
	}
	return string(out), nil
}
