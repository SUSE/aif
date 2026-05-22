//go:build live

// Package fleet live tests exercise FleetBundleEngine against a real
// Fleet manager. Excluded from the default build by the live tag; run
// with `go test -tags=live` (or `make verify-fleet-live`).
//
// Required env vars:
//   AIF_FLEET_LIVE_KUBECONFIG     — kubeconfig pointing at the Fleet manager.
//   AIF_FLEET_LIVE_NAMESPACE      — existing namespace where Bundles may be created.
//   AIF_FLEET_LIVE_TARGET_CLUSTER — Fleet Cluster.metadata.name (not a kube context);
//                                   commonly "local" for self-management.
//
// Optional:
//   AIF_FLEET_LIVE_KEEP=1         — skip Teardown so you can inspect the Bundle.
package fleet

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	fleetv1 "github.com/rancher/fleet/pkg/apis/fleet.cattle.io/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestLive_FleetBundle_RoundTrip(t *testing.T) {
	kc := os.Getenv("AIF_FLEET_LIVE_KUBECONFIG")
	ns := os.Getenv("AIF_FLEET_LIVE_NAMESPACE")
	target := os.Getenv("AIF_FLEET_LIVE_TARGET_CLUSTER")
	if kc == "" || ns == "" || target == "" {
		t.Skip("set AIF_FLEET_LIVE_{KUBECONFIG,NAMESPACE,TARGET_CLUSTER} to enable")
	}

	cfg, err := clientcmd.BuildConfigFromFlags("", kc)
	if err != nil {
		t.Fatalf("kubeconfig: %v", err)
	}

	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := fleetv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	c, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatalf("client: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine := NewBundleEngine(logger, c)

	spec := BundleDeploymentSpec{
		WorkloadID:     "aif-live-" + time.Now().Format("20060102-150405"),
		WorkloadNS:     ns,
		TargetClusters: []string{target},
		Components: []ComponentBundle{{
			Name:     "noop",
			ChartRef: "oci://registry.example.test/aif/noop:0.0.1",
			Values:   map[string]any{},
		}},
		Owner: OwnerRef{
			APIVersion: "v1",
			Kind:       "ConfigMap", // unused real owner — just exercises the wire shape
			Name:       "live-test-owner",
			UID:        "00000000-0000-0000-0000-000000000000",
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	obs, err := engine.Apply(ctx, spec)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	t.Logf("Apply returned %d cluster observations", len(obs.PerCluster))

	var got fleetv1.Bundle
	if err := c.Get(ctx, client.ObjectKey{
		Namespace: spec.WorkloadNS,
		Name:      fleetBundleName(spec.WorkloadNS, spec.WorkloadID),
	}, &got); err != nil {
		t.Fatalf("Get: %v", err)
	}
	t.Logf("Bundle resourceVersion: %s, targets: %d", got.ResourceVersion, len(got.Spec.Targets))

	if os.Getenv("AIF_FLEET_LIVE_KEEP") == "1" {
		t.Log("AIF_FLEET_LIVE_KEEP=1 set; skipping Teardown")
		return
	}
	if err := engine.Teardown(ctx, spec.WorkloadNS, spec.WorkloadID); err != nil {
		t.Fatalf("Teardown: %v", err)
	}
}
