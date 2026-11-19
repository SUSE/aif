package autovalues

import (
	"strings"
	"testing"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	apixv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

func TestLockedChanges(t *testing.T) {
	comp := aiplatformv1alpha1.BlueprintComponent{ChartName: "cp", Values: &apixv1.JSON{Raw: []byte(
		`{"global":{"domain":"aif:auto-domain:x","replicas":"aif:auto-domain:r"},"fixed":"a"}`)}}
	pins := []aiplatformv1alpha1.ResolvedValue{
		{ComponentName: "cp", Path: "global.domain", Value: "x.1.2.3.4.sslip.io", Source: "auto"},
		{ComponentName: "cp", Path: "global.replicas", Value: "1e+06", Source: "user"},
		{ComponentName: "cp", Path: "fixed", Value: "b", Source: "auto"},
		{ComponentName: "gone", Path: "global.domain", Value: "y", Source: "auto"},
	}
	override := func(raw string) []aiplatformv1alpha1.ComponentValueOverride {
		return []aiplatformv1alpha1.ComponentValueOverride{{ComponentName: "cp", Values: &apixv1.JSON{Raw: []byte(raw)}}}
	}
	for _, tc := range []struct {
		name, raw string
		want      []string
	}{
		{"no override keeps the sentinel", `{}`, nil},
		{"same value", `{"global":{"domain":"x.1.2.3.4.sslip.io"}}`, nil},
		{"legacy fmt.Sprint pin equals JSON number", `{"global":{"replicas":1000000}}`, nil},
		{"path no longer automatic is not locked", `{"fixed":"c"}`, nil},
		{"changed value", `{"global":{"domain":"other"}}`, []string{"global.domain", `"other"`}},
		{"non-string change", `{"global":{"replicas":2}}`, []string{"global.replicas", `"2"`}},
	} {
		got, err := LockedChanges([]aiplatformv1alpha1.BlueprintComponent{comp}, pins, override(tc.raw))
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if len(tc.want) == 0 {
			if len(got) != 0 {
				t.Errorf("%s: unexpected changes %+v", tc.name, got)
			}
			continue
		}
		if len(got) != 1 {
			t.Fatalf("%s: got %+v, want one change", tc.name, got)
		}
		for _, s := range tc.want {
			if !strings.Contains(got[0].String(), s) {
				t.Errorf("%s: %q lacks %s", tc.name, got[0].String(), s)
			}
		}
	}
}

func TestSameValue(t *testing.T) {
	for _, tc := range []struct {
		pinned string
		v      any
		want   bool
	}{
		{"true", true, true},
		{"1000000", float64(1000000), true},
		{"1e+06", float64(1000000), true},
		{"2", float64(3), false},
		{"true", "true", true},
		{"x", nil, false},
	} {
		if got := SameValue(tc.pinned, tc.v); got != tc.want {
			t.Errorf("SameValue(%q, %v) = %v, want %v", tc.pinned, tc.v, got, tc.want)
		}
	}
}
