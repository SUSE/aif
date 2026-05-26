package fleet_test

import (
	"context"
	"errors"
	"testing"

	"github.com/SUSE/aif/pkg/fleet"
)

func TestFakeGitRepoEngine_Records(t *testing.T) {
	f := &fleet.FakeGitRepoEngine{}
	spec := fleet.GitRepoDeploymentSpec{WorkloadID: "wl", WorkloadNS: "ns"}
	if _, err := f.Apply(context.Background(), spec); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if err := f.Teardown(context.Background(), "ns", "wl"); err != nil {
		t.Fatalf("Teardown: %v", err)
	}
	if len(f.Applied) != 1 || f.Applied[0].WorkloadID != "wl" {
		t.Fatalf("Applied not recorded: %+v", f.Applied)
	}
	if len(f.TornDown) != 1 || f.TornDown[0] != "ns/wl" {
		t.Fatalf("TornDown not recorded: %+v", f.TornDown)
	}
}

func TestFakeGitRepoEngine_PropagatesError(t *testing.T) {
	sentinel := errors.New("boom")
	f := &fleet.FakeGitRepoEngine{ApplyErr: sentinel}
	_, err := f.Apply(context.Background(), fleet.GitRepoDeploymentSpec{})
	if !errors.Is(err, sentinel) {
		t.Fatalf("want sentinel, got %v", err)
	}
}
