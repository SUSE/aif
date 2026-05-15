package workload

import (
	"regexp"
	"strings"
)

// maxHelmReleaseNameLen is the Helm 3 release-name limit (per ARCHITECTURE
// §6.6 "Component-to-Helm-release mapping").
const maxHelmReleaseNameLen = 53

// dns1123InvalidChar matches any character NOT allowed in a DNS-1123 label
// (lowercase alphanumeric or '-'). Invalid runs collapse to a single '-'.
var dns1123InvalidChar = regexp.MustCompile(`[^a-z0-9-]+`)

// ComposeReleaseName returns the Helm release name for a workload component:
// `{workloadID}-{componentName}` lowercased, DNS-1123 sanitised, truncated
// to 53 characters, with leading/trailing hyphens stripped.
//
// The function is deterministic and idempotent — the same inputs always
// produce the same output. Truncation losing the suffix is preferred over
// hashing because Helm release upgrades require stable names; the deployer
// catches "two workloads with similar names colliding" via OwnerReference
// at the K8s level (not P4-2's concern).
func ComposeReleaseName(workloadID, componentName string) string {
	raw := strings.ToLower(workloadID + "-" + componentName)
	sanitized := dns1123InvalidChar.ReplaceAllString(raw, "-")
	// Collapse consecutive hyphens to a single hyphen
	for strings.Contains(sanitized, "--") {
		sanitized = strings.ReplaceAll(sanitized, "--", "-")
	}
	if len(sanitized) > maxHelmReleaseNameLen {
		sanitized = sanitized[:maxHelmReleaseNameLen]
	}
	return strings.Trim(sanitized, "-")
}
