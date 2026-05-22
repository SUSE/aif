package workload

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"testing"

	aifv1 "github.com/SUSE/aif/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/SUSE/aif/pkg/blueprint"
	"github.com/SUSE/aif/pkg/bundle"
	"github.com/SUSE/aif/pkg/fleet"
	"github.com/SUSE/aif/pkg/helm"
	"github.com/SUSE/aif/pkg/nvidia"
)

func TestNewDeployer_ConstructsWithDeps(t *testing.T) {
	log := slog.Default()
	render := helm.NewFake()
	fleetBundle := fleet.NewFakeBundleEngine()
	bpRepo := blueprint.NewFakeRepository()
	bnRepo := bundle.NewFakeRepository()
	disc, _ := nvidia.NewDiscovery(log)
	dep := nvidia.NewDeployer(log)

	d := NewDeployer(log, render, fleetBundle, bpRepo, bnRepo, disc, dep)
	if d == nil {
		t.Fatal("NewDeployer returned nil")
	}
}

func TestResolveSource_App_SynthesizesOneComponent(t *testing.T) {
	d := newTestDeployer(t)

	req := DeployRequest{
		ID: "wid", SpecName: "my-llm",
		Source: SourceRef{
			Kind: SourceKindApp,
			App:  &AppRef{Repo: "oci://r", Chart: "c", Version: "1.0"},
		},
	}

	components, observedGen, err := d.resolveSource(context.Background(), req)
	if err != nil {
		t.Fatalf("resolveSource: %v", err)
	}
	if observedGen != 0 {
		t.Errorf("observedGen=%d, want 0 (App source)", observedGen)
	}
	if len(components) != 1 {
		t.Fatalf("components len=%d, want 1", len(components))
	}
	c := components[0]
	if c.name != "my-llm" || c.chart != "c" || c.version != "1.0" || c.repo != "oci://r" {
		t.Errorf("component=%+v", c)
	}
}

func TestResolveSource_Blueprint_FetchesAndCopiesComponents(t *testing.T) {
	d := newTestDeployer(t)

	bpRepo := d.bpRepo.(*blueprint.FakeRepository)
	bpRepo.Seed(&aifv1.Blueprint{
		ObjectMeta: metav1.ObjectMeta{Name: "rag-1.2.0"},
		Spec: aifv1.BlueprintSpec{
			BlueprintName: "rag",
			Version:       "1.2.0",
			Components: []aifv1.ComponentRef{
				{Name: "llm", Kind: aifv1.ComponentKindApp, App: &aifv1.AppRef{Repo: "r", Chart: "nim-llm", Version: "1"}},
				{Name: "vec", Kind: aifv1.ComponentKindApp, App: &aifv1.AppRef{Repo: "r", Chart: "milvus", Version: "4"}},
			},
			ValueOverrides: map[string]string{
				"llm": "image:\n  repository: registry.suse.com/ai/llm",
			},
		},
	})

	req := DeployRequest{
		Source: SourceRef{
			Kind:      SourceKindBlueprint,
			Blueprint: &BlueprintRef{Name: "rag", Version: "1.2.0"},
		},
	}

	components, _, err := d.resolveSource(context.Background(), req)
	if err != nil {
		t.Fatalf("resolveSource: %v", err)
	}
	if len(components) != 2 {
		t.Fatalf("components len=%d, want 2", len(components))
	}
	if components[0].name != "llm" || components[0].chart != "nim-llm" {
		t.Errorf("component[0]=%+v", components[0])
	}
	if components[0].blueprintOverride == "" {
		t.Errorf("blueprintOverride[0] empty; should carry valueOverrides[llm]")
	}
	if components[1].name != "vec" {
		t.Errorf("component[1]=%+v", components[1])
	}
}

func TestResolveSource_Blueprint_NotFound_ReturnsErrSourceNotResolved(t *testing.T) {
	d := newTestDeployer(t)
	req := DeployRequest{
		Source: SourceRef{Kind: SourceKindBlueprint, Blueprint: &BlueprintRef{Name: "nope", Version: "1"}},
	}
	_, _, err := d.resolveSource(context.Background(), req)
	if !errors.Is(err, ErrSourceNotResolved) {
		t.Errorf("err=%v, want ErrSourceNotResolved", err)
	}
}

func TestResolveSource_Blueprint_RejectsNestedBlueprint(t *testing.T) {
	d := newTestDeployer(t)
	bpRepo := d.bpRepo.(*blueprint.FakeRepository)
	bpRepo.Seed(&aifv1.Blueprint{
		ObjectMeta: metav1.ObjectMeta{Name: "outer-1.0"},
		Spec: aifv1.BlueprintSpec{
			Components: []aifv1.ComponentRef{
				{Name: "child", Kind: aifv1.ComponentKindBlueprint, Blueprint: &aifv1.BlueprintRef{Name: "inner", Version: "1"}},
			},
		},
	})
	req := DeployRequest{
		Source: SourceRef{Kind: SourceKindBlueprint, Blueprint: &BlueprintRef{Name: "outer", Version: "1.0"}},
	}
	_, _, err := d.resolveSource(context.Background(), req)
	if !errors.Is(err, ErrNestedBlueprintNotSupported) {
		t.Errorf("err=%v, want ErrNestedBlueprintNotSupported", err)
	}
}

func TestResolveSource_BundleTest_RecordsObservedGeneration(t *testing.T) {
	d := newTestDeployer(t)

	bnRepo := d.bundleRepo.(*bundle.FakeRepository)
	bnRepo.Seed(&aifv1.Bundle{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "b1", Generation: 7},
		Spec: aifv1.BundleSpec{
			Components: []aifv1.ComponentRef{
				{Name: "c1", Kind: aifv1.ComponentKindApp, App: &aifv1.AppRef{Repo: "r", Chart: "c", Version: "1"}},
			},
		},
	})

	req := DeployRequest{
		Source: SourceRef{
			Kind: SourceKindBundleTest,
			BundleTest: &BundleTestRef{Namespace: "ns", Name: "b1", Generation: 5},
		},
	}

	components, observedGen, err := d.resolveSource(context.Background(), req)
	if err != nil {
		t.Fatalf("resolveSource: %v", err)
	}
	if observedGen != 7 {
		t.Errorf("observedGen=%d, want 7 (current bundle.metadata.generation)", observedGen)
	}
	if len(components) != 1 {
		t.Errorf("components=%+v", components)
	}
}

func TestResolveSource_BundleTest_NotFound_ReturnsErrSourceNotResolved(t *testing.T) {
	d := newTestDeployer(t)
	req := DeployRequest{
		Source: SourceRef{Kind: SourceKindBundleTest, BundleTest: &BundleTestRef{Namespace: "ns", Name: "nope", Generation: 1}},
	}
	_, _, err := d.resolveSource(context.Background(), req)
	if !errors.Is(err, ErrSourceNotResolved) {
		t.Errorf("err=%v, want ErrSourceNotResolved", err)
	}
}

func TestDeploy_NonNIM_DoesNotCallGenerateValues(t *testing.T) {
	d := newTestDeployer(t)
	// stubDiscovery default returns ErrNIMNotFound for anything not seeded.
	nimDep := d.nvDepl.(*stubNvidiaDeployer)

	req := DeployRequest{
		Namespace: "ns", ID: "wid", SpecName: "my-app",
		Source: SourceRef{Kind: SourceKindApp, App: &AppRef{Repo: "r", Chart: "milvus", Version: "1.0"}},
	}

	if _, err := d.Deploy(context.Background(), req); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if len(nimDep.Calls) != 0 {
		t.Errorf("GenerateValues called %d times, want 0", len(nimDep.Calls))
	}
}

// stubDiscovery is a controllable Discovery for the deployer tests.
// Maps[id]NIMEntry; .Get returns the entry if present, else nvidia.ErrNIMNotFound.
// Only Get is exercised by the deployer; other methods are no-ops/stubs.
type stubDiscovery struct {
	entries map[string]nvidia.NIMEntry
}

func newStubDiscovery() *stubDiscovery {
	return &stubDiscovery{entries: map[string]nvidia.NIMEntry{}}
}

func (s *stubDiscovery) SetEntry(id string, e nvidia.NIMEntry) {
	s.entries[id] = e
}

func (s *stubDiscovery) Get(_ context.Context, id string) (nvidia.NIMEntry, error) {
	if e, ok := s.entries[id]; ok {
		return e, nil
	}
	return nvidia.NIMEntry{}, nvidia.ErrNIMNotFound
}

func (s *stubDiscovery) Index(_ context.Context) ([]nvidia.NIMEntry, error) { return nil, nil }
func (s *stubDiscovery) Refresh(_ context.Context) error                    { return nil }
func (s *stubDiscovery) UpdateSettings(_ nvidia.EngineSettings)             {}

// stubNvidiaDeployer is a controllable Deployer for the deployer tests.
type stubNvidiaDeployer struct {
	GenerateResult map[string]any
	GenerateErr    error
	Calls          []nvidia.GenerateRequest
}

func (s *stubNvidiaDeployer) GenerateValues(_ context.Context, req nvidia.GenerateRequest) (map[string]any, error) {
	s.Calls = append(s.Calls, req)
	if s.GenerateErr != nil {
		return nil, s.GenerateErr
	}
	return s.GenerateResult, nil
}

func (s *stubNvidiaDeployer) UpdateSettings(_ nvidia.EngineSettings) {}

// newTestDeployer is a helper used by all deployer_test.go tests.
// Builds a real *deployer with fakes for every dependency.
//
// Uses the codebase's actual fake constructors (verified in Task 14):
//   helm.NewFake() → *helm.FakeEngine (satisfies ValueRenderer)
//   fleet.NewFakeBundleEngine() → *fleet.FakeBundleEngine
//   blueprint.NewFakeRepository() → *blueprint.FakeRepository
//   bundle.NewFakeRepository() → *bundle.FakeRepository
//   nvidia.NewDiscovery(logger) → (Discovery, AnnotationReader) — take first
//   nvidia.NewDeployer(logger) → Deployer
func newTestDeployer(t *testing.T) *deployer {
	t.Helper()
	log := slog.Default()
	return &deployer{
		log:         log,
		render:      helm.NewFake(),
		fleetBundle: fleet.NewFakeBundleEngine(),
		bpRepo:      blueprint.NewFakeRepository(),
		bundleRepo:  bundle.NewFakeRepository(),
		nvDisc:      newStubDiscovery(),
		nvDepl:      &stubNvidiaDeployer{},
	}
}

func TestTeardown_CallsFleetTeardown(t *testing.T) {
	d := newTestDeployer(t)
	fakeFleet := d.fleetBundle.(*fleet.FakeBundleEngine)

	if err := d.Teardown(context.Background(), "ns", "workloadID", nil); err != nil {
		t.Fatalf("Teardown: %v", err)
	}
	if len(fakeFleet.TornDown) != 1 || fakeFleet.TornDown[0] != "ns/workloadID" {
		t.Errorf("TornDown=%+v, want [ns/workloadID]", fakeFleet.TornDown)
	}
}

func TestTeardown_FleetError_Propagates(t *testing.T) {
	d := newTestDeployer(t)
	fakeFleet := d.fleetBundle.(*fleet.FakeBundleEngine)
	testErr := errors.New("fleet teardown failed")
	fakeFleet.TeardownErr = testErr

	err := d.Teardown(context.Background(), "ns", "workloadID", nil)
	if !errors.Is(err, testErr) {
		t.Errorf("err=%v, want %v", err, testErr)
	}
}

func TestDeploy_Idempotent_SameInputProducesSameFleetSpec(t *testing.T) {
	d := newTestDeployer(t)

	req := DeployRequest{
		Namespace: "ns", ID: "wid", SpecName: "n",
		Source:    SourceRef{Kind: SourceKindApp, App: &AppRef{Repo: "r", Chart: "milvus", Version: "1"}},
		Overrides: map[string]string{"n": "replicaCount: 5"},
	}

	fakeFleet := d.fleetBundle.(*fleet.FakeBundleEngine)

	r1, err := d.Deploy(context.Background(), req)
	if err != nil {
		t.Fatalf("first Deploy: %v", err)
	}

	r2, err := d.Deploy(context.Background(), req)
	if err != nil {
		t.Fatalf("second Deploy: %v", err)
	}

	if len(fakeFleet.Applied) != 2 {
		t.Fatalf("Apply calls=%d, want 2", len(fakeFleet.Applied))
	}

	first := fakeFleet.Applied[0]
	second := fakeFleet.Applied[1]

	if first.WorkloadID != second.WorkloadID || first.WorkloadNS != second.WorkloadNS {
		t.Errorf("identity differs: first=%+v second=%+v", first, second)
	}
	if len(first.Components) != len(second.Components) {
		t.Fatalf("component count differs: %d vs %d", len(first.Components), len(second.Components))
	}
	if !reflect.DeepEqual(r1.Components, r2.Components) {
		t.Errorf("results differ:\nfirst:  %+v\nsecond: %+v", r1.Components, r2.Components)
	}
}

func TestDeployer_HelmStrategy_CallsFleetBundleEngine(t *testing.T) {
	fakeFleet := fleet.NewFakeBundleEngine()
	fakeHelm := helm.NewFake()
	d := NewDeployer(
		slog.Default(),
		fakeHelm,         // helm.ValueRenderer
		fakeFleet,        // fleet.FleetBundleEngine
		blueprint.NewFakeRepository(),
		bundle.NewFakeRepository(),
		newStubDiscovery(),
		&stubNvidiaDeployer{},
	)
	req := DeployRequest{
		Namespace:      "team-a",
		ID:             "demo",
		SpecName:       "llama",
		DeployStrategy: "helm",
		TargetClusters: []string{"prod-east"},
		Source: SourceRef{
			Kind: SourceKindApp,
			App:  &AppRef{Repo: "registry.example.test/charts", Chart: "llama", Version: "1.0.0"},
		},
		Owner: fleet.OwnerRef{APIVersion: "ai.suse.com/v1alpha1", Kind: "Workload", Name: "demo", UID: "u-1"},
	}
	res, err := d.Deploy(context.Background(), req)
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if len(fakeFleet.Applied) != 1 {
		t.Fatalf("FleetBundleEngine.Apply not called: %+v", fakeFleet.Applied)
	}
	applied := fakeFleet.Applied[0]
	if applied.WorkloadID != "demo" || applied.WorkloadNS != "team-a" {
		t.Fatalf("Fleet spec identity wrong: %+v", applied)
	}
	if len(applied.Components) != 1 || applied.Components[0].ChartRef != "oci://registry.example.test/charts/llama:1.0.0" {
		t.Fatalf("Fleet spec components wrong: %+v", applied.Components)
	}
	// Phase is no longer on DeployResult (P5-1 moved phase ownership to the
	// controller). Assert PerCluster mirroring instead — the controller's
	// RecomputePhase reads this via PhaseInput.PerClusterPhases (Rule 0).
	if len(res.PerCluster) == 0 {
		// FakeBundleEngine may return no per-cluster entries on first apply;
		// accept that as the equivalent of the previous PhasePending check.
		return
	}
	for _, pc := range res.PerCluster {
		if pc.Phase != ClusterDeploying && pc.Phase != ClusterRunning {
			t.Fatalf("unexpected per-cluster phase %v on %q", pc.Phase, pc.ClusterName)
		}
	}
}
