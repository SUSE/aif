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

package aiworkload

import (
	"strings"
	"testing"

	"github.com/SUSE/aif-operator/internal/naming"
)

func TestTruncateName(t *testing.T) {
	const max = 63

	// Names within the limit are returned verbatim.
	t.Run("passthrough when within limit", func(t *testing.T) {
		const short = "suse-gen-ai-minimal-c-skg6s-opentelemetry-operator"
		if got := naming.TruncateDNS1123Label(short, max); got != short {
			t.Errorf("expected unchanged %q, got %q", short, got)
		}
	})

	// Regression for the reported failure: a naive s[:63] of this kind of name
	// landed mid-segment and left a trailing '-' (e.g. "...-system-c-"), which
	// the API server rejects as an invalid DNS-1123 label.
	t.Run("over-long name is capped to a valid label", func(t *testing.T) {
		long := "suse-ai-opentelemetry-operator-opentelemetry-operator-system-c-skg6s"
		assertValidLabel(t, naming.TruncateDNS1123Label(long, max), max)
	})

	// A cut that lands exactly on a '-' must not produce a trailing '-'.
	t.Run("cut on a dash does not leave a trailing dash", func(t *testing.T) {
		in := strings.Repeat("a", 55) + "-" + strings.Repeat("b", 30)
		assertValidLabel(t, naming.TruncateDNS1123Label(in, max), max)
	})

	// Distinct over-long inputs sharing a long prefix must not collide.
	t.Run("distinct long inputs do not collide", func(t *testing.T) {
		a := strings.Repeat("a", 60) + "-one"
		b := strings.Repeat("a", 60) + "-two"
		if naming.TruncateDNS1123Label(a, max) == naming.TruncateDNS1123Label(b, max) {
			t.Errorf("expected distinct outputs, both -> %q", naming.TruncateDNS1123Label(a, max))
		}
	})

	// Pathological inputs must still yield a valid label.
	t.Run("pathological inputs stay valid", func(t *testing.T) {
		for _, in := range []string{
			strings.Repeat("-", 100),
			strings.Repeat("a", 200),
			strings.Repeat("a", 50) + strings.Repeat("-", 30),
		} {
			assertValidLabel(t, naming.TruncateDNS1123Label(in, max), max)
		}
	})
}

// assertValidLabel checks the invariants every truncated name must satisfy:
// non-empty, within the cap, and a valid DNS-1123 label boundary.
func assertValidLabel(t *testing.T, got string, max int) {
	t.Helper()
	if got == "" {
		t.Errorf("expected non-empty result")
	}
	if len(got) > max {
		t.Errorf("expected <= %d bytes, got %d (%q)", max, len(got), got)
	}
	if strings.HasPrefix(got, "-") || strings.HasSuffix(got, "-") {
		t.Errorf("result %q must not start or end with '-'", got)
	}
}
