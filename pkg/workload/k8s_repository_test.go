package workload

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	aifv1 "github.com/SUSE/aif/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

// TestK8sRepository_Patch_IncludesResourceVersion is the production-path
// counterpart to FakeRepository_Patch_Conflict. The fake hand-rolls a
// ResourceVersion comparison, so it cannot catch a Patch implementation that
// silently drops the optimistic-lock option. This test intercepts the real
// controller-runtime Patch call, extracts the JSON payload via Patch.Data,
// and asserts metadata.resourceVersion is present — without it, the apiserver
// would silently overwrite concurrent writes and no 409 would ever surface.
func TestK8sRepository_Patch_IncludesResourceVersion(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := aifv1.AddToScheme(scheme); err != nil {
		t.Fatalf("add scheme: %v", err)
	}

	const rv = "42"
	orig := &aifv1.Workload{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "ns", Name: "wl", ResourceVersion: rv,
		},
		Spec: aifv1.WorkloadSpec{
			Name: "wl",
			Source: aifv1.WorkloadSource{
				Kind:      aifv1.WorkloadSourceKindBlueprint,
				Blueprint: &aifv1.BlueprintRef{Name: "rag", Version: "1.0.0"},
			},
		},
	}

	var capturedBody []byte
	interceptedClient := interceptor.NewClient(
		fake.NewClientBuilder().WithScheme(scheme).WithObjects(orig).Build(),
		interceptor.Funcs{
			Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
				body, err := patch.Data(obj)
				if err != nil {
					return err
				}
				capturedBody = body
				return c.Patch(ctx, obj, patch, opts...)
			},
		},
	)

	repo := NewK8sRepository(interceptedClient)
	mutated := orig.DeepCopy()
	mutated.Spec.Source.Blueprint.Version = "1.1.0"

	if err := repo.Patch(context.Background(), mutated, orig); err != nil {
		t.Fatalf("Patch: %v", err)
	}

	if capturedBody == nil {
		t.Fatal("interceptor did not capture a Patch call")
	}
	if !strings.Contains(string(capturedBody), `"resourceVersion":"`+rv+`"`) {
		t.Fatalf("patch body missing resourceVersion %q; got: %s", rv, string(capturedBody))
	}

	// Sanity-check the body is valid JSON describing the version change.
	var parsed map[string]any
	if err := json.Unmarshal(capturedBody, &parsed); err != nil {
		t.Fatalf("patch body is not valid JSON: %v", err)
	}
}
