package aiworkload

import (
	"reflect"
	"testing"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func wl(name string) *aiplatformv1alpha1.AIWorkload {
	return &aiplatformv1alpha1.AIWorkload{ObjectMeta: metav1.ObjectMeta{Name: name}}
}

func TestResolveDependsOnMapsToBundleNamesSorted(t *testing.T) {
	got := resolveDependsOn(wl("wl"), []string{"beta", "alpha"})
	want := []any{
		map[string]any{"name": blueprintBundleName("wl", "alpha")},
		map[string]any{"name": blueprintBundleName("wl", "beta")},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestResolveDependsOnEmptyIsNil(t *testing.T) {
	if resolveDependsOn(wl("wl"), nil) != nil {
		t.Fatalf("empty deps must map to nil so the HelmOp omits spec.dependsOn")
	}
}

func TestResolveDependsOnDropsExcluded(t *testing.T) {
	w := &aiplatformv1alpha1.AIWorkload{
		ObjectMeta: metav1.ObjectMeta{Name: "wl"},
		Spec: aiplatformv1alpha1.AIWorkloadSpec{
			ComponentValues: []aiplatformv1alpha1.ComponentValueOverride{{ComponentName: "beta", Enabled: ptrBool(false)}},
		},
	}
	gotNames := resolveDependsOnNames(w, []string{"beta", "alpha"})
	wantNames := []string{blueprintBundleName("wl", "alpha")}
	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Fatalf("resolveDependsOnNames: got %v want %v", gotNames, wantNames)
	}
	got := resolveDependsOn(w, []string{"beta", "alpha"})
	want := []any{map[string]any{"name": blueprintBundleName("wl", "alpha")}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("resolveDependsOn: got %v want %v", got, want)
	}
}

func TestDigestIncludesDependsOn(t *testing.T) {
	base := ComponentRenderInputs{ChartName: "n"}
	with := base
	with.DependsOn = []string{"x"}
	if perHelmOpRenderDigest(base) == perHelmOpRenderDigest(with) {
		t.Fatalf("dependsOn must change the render digest")
	}
}

func TestDigestDeterministicAndNilFieldsOmitted(t *testing.T) {
	// Frictionless-upgrade guard: with both new fields nil/empty, the digest is
	// deterministic and the `omitempty` JSON tags keep them out of the hashed
	// form, so it equals the pre-change encoding. The authoritative byte-identity
	// check is the existing render/digest golden tests in this package, which
	// must still pass unchanged after this task (run the full package in Step 7).
	in := ComponentRenderInputs{ChartRepo: "r", ChartName: "n", ChartVersion: "1.0.0", Namespace: "ns"}
	if got := perHelmOpRenderDigest(in); got == "" || got != perHelmOpRenderDigest(in) {
		t.Fatalf("digest must be deterministic and non-empty, got %q", got)
	}
}
