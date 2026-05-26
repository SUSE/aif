package git

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
)

// dnsInvalid matches any character outside DNS-1123 subdomain alphabet.
var dnsInvalid = regexp.MustCompile(`[^a-z0-9-]+`)

// SanitizeComponentName lower-cases s, replaces invalid characters with
// "-", collapses consecutive dashes, and trims leading/trailing dashes.
// Returns "" when the result is empty.
func SanitizeComponentName(s string) string {
	clean := dnsInvalid.ReplaceAllString(strings.ToLower(s), "-")
	for strings.Contains(clean, "--") {
		clean = strings.ReplaceAll(clean, "--", "-")
	}
	return strings.Trim(clean, "-")
}

// SanitizeComponentNameUnique returns the sanitized name, appending a
// 4-char SHA-256 prefix of the original when the sanitized name
// already exists in `seen`.
func SanitizeComponentNameUnique(original string, seen map[string]struct{}) string {
	base := SanitizeComponentName(original)
	if _, ok := seen[base]; !ok {
		return base
	}
	sum := sha256.Sum256([]byte(original))
	return base + "-" + hex.EncodeToString(sum[:])[:4]
}

// MaxComponentIndex is the largest index ManifestFilename will accept
// before returning an empty string. The "%02d-..." scheme below
// reserves the 00..09 prefix range for engine-owned files (currently
// just 00-namespace.yaml) and the 10..99 range for components, so
// indices 0..89 map to filenames "10-..." through "99-...".
const MaxComponentIndex = 89

// ManifestFilename returns the per-component filename inside manifests/.
// All indices use the "%02d-..." form with a (+10) offset, so the
// lexicographic sort matches the numeric sort across the whole range:
//
//	00-09 → reserved for engine-owned files (e.g. 00-namespace.yaml)
//	10-99 → component files (indices 0..89)
//
// Returns "" if index exceeds MaxComponentIndex; callers must enforce
// the limit upstream (validateGitRepoSpec) rather than silently emitting
// a file that would sort before lower indices.
func ManifestFilename(index int, sanitizedName string) string {
	if index < 0 || index > MaxComponentIndex {
		return ""
	}
	return fmt.Sprintf("%02d-%s.yaml", index+10, sanitizedName)
}
