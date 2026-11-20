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

package blueprintpolicy

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	"github.com/SUSE/aif-operator/internal/naming"
)

func TestIsRollback(t *testing.T) {
	for _, tc := range []struct {
		from, to string
		want     bool
	}{
		{"2.0.0", "1.0.0", true},
		{"1.10.0", "1.9.0", true},
		{"1.0.0", "1.0.0-rc.1", true},
		{"1.0.0", "2.0.0", false},
		{"1.0.0", "1.0.0", false},
		{"1.0.0", "1.0.0+build.2", false},
		{"", "1.0.0", false},
		{"not-semver", "1.0.0", false},
	} {
		if got := IsRollback(tc.from, tc.to); got != tc.want {
			t.Errorf("IsRollback(%q, %q) = %v, want %v", tc.from, tc.to, got, tc.want)
		}
	}
}

func policyBP(version string, policy aiplatformv1alpha1.RollbackPolicy) *aiplatformv1alpha1.Blueprint {
	return &aiplatformv1alpha1.Blueprint{
		ObjectMeta: metav1.ObjectMeta{Name: naming.BlueprintCRName("rag", version)},
		Spec:       aiplatformv1alpha1.BlueprintSpec{DisplayName: "RAG", Version: version, Rollback: policy},
	}
}

func TestRollbackBlocked(t *testing.T) {
	s := runtime.NewScheme()
	_ = aiplatformv1alpha1.AddToScheme(s)
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(
		policyBP("3.0.0", ""),
		policyBP("2.0.0", aiplatformv1alpha1.RollbackDisabled),
		policyBP("1.0.0", ""),
	).Build()
	ctx := context.Background()
	for _, tc := range []struct {
		name, deployed, to string
		want               bool
	}{
		{"older target from disabled version", "2.0.0", "1.0.0", true},
		{"older target that is disabled", "3.0.0", "2.0.0", true},
		{"older target, neither disabled", "3.0.0", "1.0.0", false},
		{"newer target from disabled version", "2.0.0", "3.0.0", false},
		{"missing Blueprint CRs fail open", "9.0.0", "8.0.0", false},
	} {
		got, err := RollbackBlocked(ctx, c, "rag", tc.deployed, tc.to)
		if err != nil || got != tc.want {
			t.Errorf("%s: RollbackBlocked = %v, %v; want %v", tc.name, got, err, tc.want)
		}
	}
}

func TestHighest(t *testing.T) {
	if got := Highest("", "1.10.0", "not-semver", "1.9.0"); got != "1.10.0" {
		t.Errorf("Highest = %q, want 1.10.0", got)
	}
	if got := Highest("", "x"); got != "" {
		t.Errorf("Highest of no semver = %q, want empty", got)
	}
}

func TestFloorFor(t *testing.T) {
	for _, tc := range []struct{ stored, family, want string }{
		{"", "rag", ""},
		{"rag@2.0.0", "rag", "2.0.0"},
		{"other@2.0.0", "rag", ""},
		{"2.0.0", "rag", "2.0.0"}, // bare version from an earlier build
	} {
		if got := FloorFor(tc.stored, tc.family); got != tc.want {
			t.Errorf("FloorFor(%q, %q) = %q, want %q", tc.stored, tc.family, got, tc.want)
		}
	}
	if got := FloorFor(MinimumSafeVersion("rag", "1.2.3+build.1"), "rag"); got != "1.2.3+build.1" {
		t.Errorf("round trip = %q", got)
	}
}

func TestMoveBlocked(t *testing.T) {
	s := runtime.NewScheme()
	_ = aiplatformv1alpha1.AddToScheme(s)
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(
		policyBP("2.0.0", aiplatformv1alpha1.RollbackDisabled),
		policyBP("1.0.0", ""),
	).Build()
	ctx := context.Background()
	for _, tc := range []struct {
		name, to, minimumSafe, deployed string
		want                            bool
		floor                           string
	}{
		// v1 certified, rollback-disabled v2 started rendering, back to v1.
		{"below minimumSafe", "1.0.0", "rag@2.0.0", "1.0.0", true, "2.0.0"},
		{"below minimumSafe without a Blueprint", "8.0.0", "rag@9.0.0", "8.0.0", true, "9.0.0"},
		{"bare legacy floor", "1.0.0", "2.0.0", "1.0.0", true, "2.0.0"},
		{"floor of another family ignored", "1.0.0", "other@2.0.0", "1.0.0", false, ""},
		{"at minimumSafe", "2.0.0", "rag@2.0.0", "1.0.0", false, ""},
		{"unset minimumSafe uses deployed", "1.0.0", "", "2.0.0", true, "2.0.0"},
		{"newer target", "3.0.0", "rag@2.0.0", "2.0.0", false, ""},
		{"no history", "1.0.0", "", "", false, ""},
	} {
		got, floor, err := MoveBlocked(ctx, c, "rag", tc.to, tc.minimumSafe, tc.deployed)
		if err != nil || got != tc.want || (got && floor != tc.floor) {
			t.Errorf("%s: MoveBlocked = %v, %q, %v; want %v, %q", tc.name, got, floor, err, tc.want, tc.floor)
		}
	}
}
