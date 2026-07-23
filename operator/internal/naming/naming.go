/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package naming is the single home for DNS-1123 slug/truncate helpers used to
// derive Kubernetes object names. This logic was previously duplicated verbatim
// across three packages — internal/cluster (pullSecretBundleName), internal/
// controller/aiworkload (slugifyBP/truncateName), and internal/controller/
// settings (slugifyRepoPath/truncateRepoName). It now lives here so a single
// change keeps every derived name consistent.
package naming

import (
	"hash/fnv"
	"regexp"
	"strconv"
	"strings"
)

// nonAlphanumRE matches runs of characters that are not lowercase letters or
// digits. Slugify lowercases first, so the class only needs to cover [a-z0-9].
var nonAlphanumRE = regexp.MustCompile(`[^a-z0-9]+`)

// Slugify lowercases s, replaces each run of non-alphanumeric characters with a
// single '-', and trims leading/trailing '-'. The result is a DNS-1123-friendly
// slug fragment (it may still be empty for all-separator input, and is not
// length-capped — pass it through TruncateDNS1123Label if a bound is needed).
func Slugify(s string) string {
	s = strings.ToLower(s)
	s = nonAlphanumRE.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

// TruncateDNS1123Label caps s to at most max characters as a VALID DNS-1123
// label. A naive s[:max] can cut mid-segment and leave a trailing '-' (rejected
// by the API server, e.g. "...-system-c-") or collapse two distinct long names
// onto the same prefix. When truncation is needed we append a deterministic
// FNV-1a/base36 suffix (capped at 6 chars) and trim any trailing '-' from the
// head so the result is always valid and distinct inputs stay distinct. Inputs
// already within the limit are returned unchanged.
func TruncateDNS1123Label(s string, max int) string {
	if len(s) <= max {
		return s
	}
	const hashLen = 6
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	suffix := strconv.FormatUint(uint64(h.Sum32()), 36)
	if len(suffix) > hashLen {
		suffix = suffix[:hashLen]
	}
	head := strings.TrimRight(s[:max-len(suffix)-1], "-")
	if head == "" {
		return suffix
	}
	return head + "-" + suffix
}
