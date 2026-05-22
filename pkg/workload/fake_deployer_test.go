package workload

import (
	"context"
	"errors"
	"testing"
)

func TestFakeDeployer_RecordsDeployCalls(t *testing.T) {
	f := &FakeDeployer{}
	req := DeployRequest{Namespace: "ns", ID: "wid", SpecName: "name"}

	_, err := f.Deploy(context.Background(), req)
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if len(f.DeployCalls) != 1 {
		t.Fatalf("DeployCalls=%d, want 1", len(f.DeployCalls))
	}
	if f.DeployCalls[0].Namespace != "ns" || f.DeployCalls[0].ID != "wid" {
		t.Errorf("recorded request mismatch: %+v", f.DeployCalls[0])
	}
}

func TestFakeDeployer_ReturnsConfiguredResult(t *testing.T) {
	want := DeployResult{
		Components: []ComponentRelease{{Name: "c1", ReleaseName: "wid-c1", Status: "deployed"}},
	}
	f := &FakeDeployer{DeployResult: want}

	got, err := f.Deploy(context.Background(), DeployRequest{})
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if len(got.Components) != 1 || got.Components[0].Status != "deployed" {
		t.Errorf("Components=%+v, want one with Status=deployed", got.Components)
	}
}

func TestFakeDeployer_ReturnsConfiguredError(t *testing.T) {
	want := errors.New("boom")
	f := &FakeDeployer{DeployErr: want}

	_, err := f.Deploy(context.Background(), DeployRequest{})
	if !errors.Is(err, want) {
		t.Errorf("err=%v, want %v", err, want)
	}
}

func TestFakeDeployer_TeardownRecordsAndReturnsErr(t *testing.T) {
	want := errors.New("teardown boom")
	f := &FakeDeployer{TeardownErr: want}

	releases := []ComponentRelease{{Name: "c1", ReleaseName: "wid-c1"}}
	err := f.Teardown(context.Background(), "ns", "wid", releases)
	if !errors.Is(err, want) {
		t.Errorf("err=%v, want %v", err, want)
	}
	if len(f.TeardownCalls) != 1 || f.TeardownCalls[0].Namespace != "ns" || f.TeardownCalls[0].WorkloadID != "wid" {
		t.Errorf("TeardownCalls=%+v", f.TeardownCalls)
	}
}

func TestFakeDeployer_Reset_ClearsCallLog(t *testing.T) {
	f := &FakeDeployer{
		DeployResult: DeployResult{
			Components: []ComponentRelease{{Name: "c1", Status: "deployed"}},
		},
		DeployErr:   errors.New("configured deploy err"),
		TeardownErr: errors.New("configured teardown err"),
	}
	_, _ = f.Deploy(context.Background(), DeployRequest{})
	_ = f.Teardown(context.Background(), "ns", "wid", nil)

	f.Reset()

	if len(f.DeployCalls) != 0 || len(f.TeardownCalls) != 0 {
		t.Errorf("Reset did not clear call log: deploy=%d teardown=%d",
			len(f.DeployCalls), len(f.TeardownCalls))
	}
	if len(f.DeployResult.Components) != 0 {
		t.Errorf("Reset did not clear DeployResult: %+v", f.DeployResult)
	}
	if f.DeployErr != nil {
		t.Errorf("Reset did not clear DeployErr: %v", f.DeployErr)
	}
	if f.TeardownErr != nil {
		t.Errorf("Reset did not clear TeardownErr: %v", f.TeardownErr)
	}
}
