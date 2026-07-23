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

package naming_test

import (
	"strings"
	"testing"

	"github.com/SUSE/aif-operator/internal/naming"
)

func TestSlugify(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"spaces", "Hello World", "hello-world"},
		{"slashes", "nvidia/omniverse", "nvidia-omniverse"},
		{"mixed case", "NVIDIA-BluePrint", "nvidia-blueprint"},
		{"leading/trailing separators", "  /Foo Bar/  ", "foo-bar"},
		{"collapse runs", "a___b...c", "a-b-c"},
		{"already clean", "nvidia-cuopt", "nvidia-cuopt"},
		{"all separators", "///", ""},
		{"empty", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := naming.Slugify(tc.in); got != tc.want {
				t.Errorf("Slugify(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestTruncateDNS1123Label_ShortUnchanged(t *testing.T) {
	in := "already-short"
	if got := naming.TruncateDNS1123Label(in, 63); got != in {
		t.Errorf("expected short string unchanged, got %q", got)
	}
	// Exactly at the limit is also unchanged.
	if got := naming.TruncateDNS1123Label(in, len(in)); got != in {
		t.Errorf("expected string at limit unchanged, got %q", got)
	}
}

func TestTruncateDNS1123Label_OverLimitIsValid(t *testing.T) {
	in := strings.Repeat("abcdefghij", 8) // 80 chars
	const max = 63
	got := naming.TruncateDNS1123Label(in, max)
	if len(got) > max {
		t.Errorf("result %q longer than max %d", got, max)
	}
	if strings.HasSuffix(got, "-") {
		t.Errorf("result %q has trailing '-'", got)
	}
	if strings.HasPrefix(got, "-") {
		t.Errorf("result %q has leading '-'", got)
	}
}

func TestTruncateDNS1123Label_DistinctInputsSharedPrefix(t *testing.T) {
	prefix := strings.Repeat("x", 70)
	a := prefix + "-alpha"
	b := prefix + "-beta"
	const max = 63
	gotA := naming.TruncateDNS1123Label(a, max)
	gotB := naming.TruncateDNS1123Label(b, max)
	if gotA == gotB {
		t.Errorf("distinct long inputs sharing a prefix collided: both -> %q", gotA)
	}
}

func TestTruncateDNS1123Label_PathologicalAllSeparatorHead(t *testing.T) {
	// A head that is entirely '-' should trim to empty, so the function returns
	// just the hash suffix (a valid, non-empty, no-dash label).
	in := strings.Repeat("-", 70)
	const max = 10
	got := naming.TruncateDNS1123Label(in, max)
	if got == "" {
		t.Fatal("expected non-empty suffix for all-separator head")
	}
	if strings.Contains(got, "-") {
		t.Errorf("expected suffix-only result with no '-', got %q", got)
	}
	if len(got) > max {
		t.Errorf("result %q longer than max %d", got, max)
	}
}
