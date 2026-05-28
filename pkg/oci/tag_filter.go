package oci

import "regexp"

// sigstoreTagPattern matches cosign-shaped tags: a manifest digest
// referenced by tag, suffixed with the artifact kind. Cosign and the
// related sigstore tooling push signatures (.sig), attestations (.att),
// and SBOMs (.sbom) as additional manifests on the same repository as
// the signed chart, using these tag shapes as a poor-man's referrers
// API. They are never deployment artifacts, so every Walker consumer
// wants them filtered out.
//
// The pattern is strict: lowercase hex only (OCI digests are lowercase
// per the spec) and exactly 64 chars after `sha256-`.
var sigstoreTagPattern = regexp.MustCompile(`^sha256-[a-f0-9]{64}\.(sig|att|sbom)$`)

// isSigstoreTag reports whether tag is a cosign signature, attestation,
// or SBOM manifest tag rather than a real chart version.
func isSigstoreTag(tag string) bool {
	return sigstoreTagPattern.MatchString(tag)
}
