package suse_registry

import "golang.org/x/mod/semver"

// pickLatestSemver returns the highest-precedence semver tag from tags
// (using golang.org/x/mod/semver, which requires a leading "v" and is
// pre-release aware). Non-semver tags are skipped. The original tag
// string (with or without "v" prefix) is returned so callers can use it
// as a registry-addressable reference. Returns ("", false) if no input
// tag parses as semver — caller should drop the chart entirely.
//
// Sort key for ties (e.g. "1.0.0" vs "v1.0.0", which have equal semver
// precedence): the un-prefixed form wins, so the returned tag matches
// how charts are conventionally pushed to the SUSE Registry today.
func pickLatestSemver(tags []string) (string, bool) {
	best := ""
	bestNorm := ""
	for _, tag := range tags {
		norm := tag
		if len(norm) == 0 || norm[0] != 'v' {
			norm = "v" + norm
		}
		if !semver.IsValid(norm) {
			continue
		}
		if best == "" {
			best, bestNorm = tag, norm
			continue
		}
		cmp := semver.Compare(norm, bestNorm)
		if cmp > 0 || (cmp == 0 && len(tag) < len(best)) {
			best, bestNorm = tag, norm
		}
	}
	return best, best != ""
}
