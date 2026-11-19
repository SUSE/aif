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
