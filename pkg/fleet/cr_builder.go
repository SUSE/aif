package fleet

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
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
