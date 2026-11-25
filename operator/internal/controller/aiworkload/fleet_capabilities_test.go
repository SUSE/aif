package aiworkload

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/go-logr/logr/funcr"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	kruntime "k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// staticFleetCapabilities is a fixed FleetCapabilities for render tests.
type staticFleetCapabilities struct{ acceptedStates bool }

func (s staticFleetCapabilities) DependsOnAcceptedStates(context.Context) bool {
	return s.acceptedStates
}

// fleetCRD builds a minimal Fleet CRD whose v1alpha1 schema declares
// spec.dependsOn items, with or without acceptedStates.
func fleetCRD(name string, acceptedStates bool) *unstructured.Unstructured {
	itemProps := map[string]any{
		"name":     map[string]any{"type": "string"},
		"selector": map[string]any{"type": "object"},
	}
	if acceptedStates {
		itemProps["acceptedStates"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
	}
	u := &unstructured.Unstructured{Object: map[string]any{
		"spec": map[string]any{
			"versions": []any{map[string]any{
				"name": "v1alpha1",
				"schema": map[string]any{"openAPIV3Schema": map[string]any{
					"properties": map[string]any{"spec": map[string]any{
						"properties": map[string]any{"dependsOn": map[string]any{
							"items": map[string]any{"properties": itemProps},
						}},
					}},
				}},
			}},
		},
	}}
	u.SetGroupVersionKind(crdGVK)
	u.SetName(name)
	return u
}

func crdClient(t *testing.T, funcs *interceptor.Funcs, objs ...client.Object) client.Client {
	t.Helper()
	s := kruntime.NewScheme()
	s.AddKnownTypeWithName(crdGVK, &unstructured.Unstructured{})
	b := fake.NewClientBuilder().WithScheme(s).WithObjects(objs...)
	if funcs != nil {
		b = b.WithInterceptorFuncs(*funcs)
	}
	return b.Build()
}

func TestFleetCapabilities_DependsOnAcceptedStates(t *testing.T) {
	cases := []struct {
		name string
		objs []client.Object
		want bool
	}{
		{"both CRDs declare acceptedStates", []client.Object{fleetCRD(fleetBundleCRD, true), fleetCRD(fleetHelmOpCRD, true)}, true},
		{"neither declares it (Fleet v0.14)", []client.Object{fleetCRD(fleetBundleCRD, false), fleetCRD(fleetHelmOpCRD, false)}, false},
		{"HelmOp CRD lacks it", []client.Object{fleetCRD(fleetBundleCRD, true), fleetCRD(fleetHelmOpCRD, false)}, false},
		{"Bundle CRD lacks it", []client.Object{fleetCRD(fleetBundleCRD, false), fleetCRD(fleetHelmOpCRD, true)}, false},
		{"HelmOp CRD missing", []client.Object{fleetCRD(fleetBundleCRD, true)}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			caps := newCRDFleetCapabilities(crdClient(t, nil, tc.objs...))
			if got := caps.DependsOnAcceptedStates(context.Background()); got != tc.want {
				t.Fatalf("DependsOnAcceptedStates = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestFleetCapabilities_LookupErrorFallsBackAndLogsOnce(t *testing.T) {
	failing := &interceptor.Funcs{Get: func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
		return errors.New("apiserver unavailable")
	}}
	caps := newCRDFleetCapabilities(crdClient(t, failing))
	now := time.Unix(0, 0)
	caps.now = func() time.Time { return now }

	var logs []string
	ctx := log.IntoContext(context.Background(), funcr.New(func(_, args string) { logs = append(logs, args) }, funcr.Options{}))
	for range 3 {
		if caps.DependsOnAcceptedStates(ctx) {
			t.Fatal("a lookup error must fall back to no Fleet dependsOn")
		}
		now = now.Add(fleetCapabilitiesTTL + time.Second)
	}
	if len(logs) != 1 || !strings.Contains(logs[0], "apiserver unavailable") {
		t.Fatalf("want the lookup error logged once, got %q", logs)
	}
}

func TestFleetCapabilities_LookupErrorKeepsLastKnownResult(t *testing.T) {
	fail := false
	funcs := &interceptor.Funcs{Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
		if fail {
			return errors.New("apiserver unavailable")
		}
		return c.Get(ctx, key, obj, opts...)
	}}
	caps := newCRDFleetCapabilities(crdClient(t, funcs, fleetCRD(fleetBundleCRD, true), fleetCRD(fleetHelmOpCRD, true)))
	now := time.Unix(0, 0)
	caps.now = func() time.Time { return now }
	ctx := log.IntoContext(context.Background(), funcr.New(func(_, _ string) {}, funcr.Options{}))

	if !caps.DependsOnAcceptedStates(ctx) {
		t.Fatal("want supported before the outage")
	}
	fail = true
	now = now.Add(fleetCapabilitiesTTL + time.Second)
	if !caps.DependsOnAcceptedStates(ctx) {
		t.Fatal("a transient lookup error must not flip a known result (that would re-render every dependent component)")
	}
}

func TestFleetCapabilities_TTLRefreshPicksUpFleetUpgrade(t *testing.T) {
	c := crdClient(t, nil, fleetCRD(fleetBundleCRD, false), fleetCRD(fleetHelmOpCRD, false))
	caps := newCRDFleetCapabilities(c)
	now := time.Unix(0, 0)
	caps.now = func() time.Time { return now }
	ctx := context.Background()

	if caps.DependsOnAcceptedStates(ctx) {
		t.Fatal("want unsupported on Fleet v0.14 CRDs")
	}
	for _, name := range []string{fleetBundleCRD, fleetHelmOpCRD} {
		upgraded := fleetCRD(name, true)
		cur := &unstructured.Unstructured{}
		cur.SetGroupVersionKind(crdGVK)
		if err := c.Get(ctx, client.ObjectKey{Name: name}, cur); err != nil {
			t.Fatal(err)
		}
		upgraded.SetResourceVersion(cur.GetResourceVersion())
		if err := c.Update(ctx, upgraded); err != nil {
			t.Fatal(err)
		}
	}
	now = now.Add(fleetCapabilitiesTTL - time.Second)
	if caps.DependsOnAcceptedStates(ctx) {
		t.Fatal("result must be cached within the TTL")
	}
	now = now.Add(2 * time.Second)
	if !caps.DependsOnAcceptedStates(ctx) {
		t.Fatal("want supported after the TTL expires on upgraded CRDs")
	}
}

func TestFleetCapabilities_RetriesSoonUntilFirstSuccess(t *testing.T) {
	fail := true
	calls := 0
	funcs := &interceptor.Funcs{Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
		calls++
		if fail {
			return errors.New("apiserver unavailable")
		}
		return c.Get(ctx, key, obj, opts...)
	}}
	caps := newCRDFleetCapabilities(crdClient(t, funcs, fleetCRD(fleetBundleCRD, true), fleetCRD(fleetHelmOpCRD, true)))
	now := time.Unix(0, 0)
	caps.now = func() time.Time { return now }
	ctx := log.IntoContext(context.Background(), funcr.New(func(_, _ string) {}, funcr.Options{}))

	if caps.DependsOnAcceptedStates(ctx) {
		t.Fatal("want none mode before the first successful lookup")
	}
	now = now.Add(fleetCapabilitiesRetry - time.Second)
	caps.DependsOnAcceptedStates(ctx)
	if calls != 1 {
		t.Fatalf("lookup repeated within the retry interval (%d calls)", calls)
	}
	fail = false
	now = now.Add(2 * time.Second)
	if !caps.DependsOnAcceptedStates(ctx) {
		t.Fatal("want a failed first lookup retried after fleetCapabilitiesRetry, not the full TTL")
	}

	fail = true
	calls = 0
	now = now.Add(fleetCapabilitiesTTL + time.Second)
	caps.DependsOnAcceptedStates(ctx)
	now = now.Add(fleetCapabilitiesRetry + time.Second)
	if !caps.DependsOnAcceptedStates(ctx) || calls != 1 {
		t.Fatalf("after a success, an error must keep the last known value for the full TTL (%d calls)", calls)
	}
}

func TestFleetCapabilities_MissingCRDLoggedOnce(t *testing.T) {
	caps := newCRDFleetCapabilities(crdClient(t, nil, fleetCRD(fleetBundleCRD, true)))
	now := time.Unix(0, 0)
	caps.now = func() time.Time { return now }
	var logs []string
	ctx := log.IntoContext(context.Background(), funcr.New(func(_, args string) { logs = append(logs, args) }, funcr.Options{}))
	for range 3 {
		if caps.DependsOnAcceptedStates(ctx) {
			t.Fatal("a missing Fleet CRD must mean unsupported")
		}
		now = now.Add(fleetCapabilitiesTTL + time.Second)
	}
	if len(logs) != 1 || !strings.Contains(logs[0], fleetHelmOpCRD) {
		t.Fatalf("want the missing CRD logged once, got %q", logs)
	}
}
