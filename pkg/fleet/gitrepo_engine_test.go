package fleet_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	fleetv1 "github.com/rancher/fleet/pkg/apis/fleet.cattle.io/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/SUSE/aif/pkg/fleet"
	"github.com/SUSE/aif/pkg/git"
)

func newScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(s); err != nil {
		t.Fatalf("clientgo AddToScheme: %v", err)
	}
	if err := fleetv1.AddToScheme(s); err != nil {
		t.Fatalf("fleetv1 AddToScheme: %v", err)
	}
	return s
}

func newSilentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func validSpec() fleet.GitRepoDeploymentSpec {
	return fleet.GitRepoDeploymentSpec{
		WorkloadID:     "wl-1",
		WorkloadNS:     "ns-a",
		TargetClusters: []string{"cluster-a", "cluster-b"},
		Components: []fleet.ComponentBundle{{
			Name:     "nim-llm",
			ChartRef: "oci://example/nim-llm:1.0.0",
			Values:   map[string]any{"replicaCount": 1},
		}},
		Owner: fleet.OwnerRef{
			APIVersion: "ai.suse.com/v1alpha1",
			Kind:       "Workload",
			Name:       "wl-1",
			UID:        "uid-1",
			Controller: true,
		},
	}
}

func TestGitRepoEngine_ErrNotConfiguredWhenSettingsEmpty(t *testing.T) {
	s := newScheme(t)
	c := fake.NewClientBuilder().WithScheme(s).Build()
	g := &git.FakeEngine{}
	e := fleet.NewGitRepoEngine(newSilentLogger(), c, g)

	// Do NOT call UpdateSettings → engine state empty → ErrNotConfigured.
	_, err := e.Apply(context.Background(), validSpec())
	if !errors.Is(err, git.ErrNotConfigured) {
		t.Fatalf("got %v, want git.ErrNotConfigured", err)
	}
}

func TestGitRepoEngine_AppliesGitRepoPerCluster(t *testing.T) {
	s := newScheme(t)
	c := fake.NewClientBuilder().WithScheme(s).Build()
	g := &git.FakeEngine{PushResult: git.PushResult{CommitSHA: "abc123"}}
	e := fleet.NewGitRepoEngine(newSilentLogger(), c, g)
	e.UpdateSettings(fleet.FleetSettings{
		GitRepoURL: "https://example.test/r.git",
		GitBranch:  "main",
	})

	obs, err := e.Apply(context.Background(), validSpec())
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(obs.PerCluster) != 2 {
		t.Fatalf("expected 2 per-cluster entries, got %d", len(obs.PerCluster))
	}

	var list fleetv1.GitRepoList
	if err := c.List(context.Background(), &list, client.InNamespace("ns-a")); err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list.Items) != 2 {
		t.Fatalf("expected 2 GitRepo CRs, got %d", len(list.Items))
	}

	if len(g.Pushes) != 1 {
		t.Fatalf("expected 1 Push, got %d", len(g.Pushes))
	}
	if len(g.Pushes[0].Subtrees) != 2 {
		t.Fatalf("expected 2 subtrees, got %d", len(g.Pushes[0].Subtrees))
	}
}

func TestGitRepoEngine_RejectsTooManyComponents(t *testing.T) {
	s := newScheme(t)
	c := fake.NewClientBuilder().WithScheme(s).Build()
	g := &git.FakeEngine{}
	e := fleet.NewGitRepoEngine(newSilentLogger(), c, g)
	e.UpdateSettings(fleet.FleetSettings{
		GitRepoURL: "https://example.test/r.git",
		GitBranch:  "main",
	})

	spec := validSpec()
	// MaxComponentIndex+1 components is the limit; one more must fail
	// validation so we never emit a filename outside the 10..99 prefix
	// range that would sort wrong against the 00-09 engine-owned files.
	spec.Components = make([]fleet.ComponentBundle, git.MaxComponentIndex+2)
	for i := range spec.Components {
		spec.Components[i] = fleet.ComponentBundle{Name: "c", ChartRef: "oci://x:1"}
	}

	_, err := e.Apply(context.Background(), spec)
	if !errors.Is(err, fleet.ErrGitRepoInvalidSpec) {
		t.Fatalf("got %v, want ErrGitRepoInvalidSpec", err)
	}
}

func TestGitRepoEngine_TeardownDeletesByLabel(t *testing.T) {
	s := newScheme(t)
	existing := &fleetv1.GitRepo{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ns-a-wl-1-cluster-a",
			Namespace: "ns-a",
			Labels: map[string]string{
				"ai.suse.com/managed-by": "aif-workload-controller",
				"ai.suse.com/workload":   "wl-1",
			},
		},
	}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(existing).Build()
	g := &git.FakeEngine{}
	e := fleet.NewGitRepoEngine(newSilentLogger(), c, g)

	if err := e.Teardown(context.Background(), "ns-a", "wl-1"); err != nil {
		t.Fatalf("Teardown: %v", err)
	}
	var list fleetv1.GitRepoList
	if err := c.List(context.Background(), &list, client.InNamespace("ns-a")); err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list.Items) != 0 {
		t.Fatalf("expected 0 GitRepo CRs after teardown, got %d", len(list.Items))
	}
}

func TestGitRepoEngine_TeardownIdempotent(t *testing.T) {
	s := newScheme(t)
	c := fake.NewClientBuilder().WithScheme(s).Build()
	e := fleet.NewGitRepoEngine(newSilentLogger(), c, &git.FakeEngine{})
	if err := e.Teardown(context.Background(), "ns-a", "wl-1"); err != nil {
		t.Fatalf("first Teardown: %v", err)
	}
	if err := e.Teardown(context.Background(), "ns-a", "wl-1"); err != nil {
		t.Fatalf("second Teardown: %v", err)
	}
}
