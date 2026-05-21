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
	"github.com/SUSE/aif/pkg/helm"
	"github.com/SUSE/aif/pkg/nvidia"
)

func TestNewDeployer_ConstructsWithDeps(t *testing.T) {
	helmEng := helm.NewFake()
	bpRepo := blueprint.NewFakeRepository()
	bnRepo := bundle.NewFakeRepository()
	disc, _ := nvidia.NewDiscovery(slog.Default())
	dep := nvidia.NewDeployer(slog.Default())

	d := NewDeployer(helmEng, bpRepo, bnRepo, disc, dep, slog.Default())
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

	bpRepo := d.blueprintRepo.(*blueprint.FakeRepository)
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
	bpRepo := d.blueprintRepo.(*blueprint.FakeRepository)
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

func TestDetectOrphans_ReturnsRemovedComponents(t *testing.T) {
	previous := []ComponentRelease{
		{Name: "a", ReleaseName: "wid-a"},
		{Name: "b", ReleaseName: "wid-b"},
		{Name: "c", ReleaseName: "wid-c"},
	}
	desired := []desiredComponent{
		{name: "a"}, {name: "c"},
	}
	got := detectOrphans(previous, desired)
	if len(got) != 1 || got[0].Name != "b" {
		t.Errorf("orphans=%+v, want [b]", got)
	}
}

func TestDetectOrphans_EmptyPrevious_ReturnsEmpty(t *testing.T) {
	got := detectOrphans(nil, []desiredComponent{{name: "a"}})
	if len(got) != 0 {
		t.Errorf("orphans=%+v, want empty", got)
	}
}

func TestDetectOrphans_EmptyDesired_ReturnsAllPrevious(t *testing.T) {
	previous := []ComponentRelease{{Name: "a"}, {Name: "b"}}
	got := detectOrphans(previous, nil)
	if len(got) != 2 {
		t.Errorf("orphans=%+v, want all 2", got)
	}
}

func TestDeploy_App_NonNIM_HappyPath(t *testing.T) {
	d := newTestDeployer(t)
	// FakeEngine default returns Status="deployed", Revision=1 — no override needed.

	req := DeployRequest{
		Namespace: "ns", ID: "wid", SpecName: "my-llm",
		Source: SourceRef{Kind: SourceKindApp, App: &AppRef{Repo: "oci://r", Chart: "milvus", Version: "1.0"}},
		Overrides: map[string]string{"my-llm": "replicaCount: 5"},
	}

	result, err := d.Deploy(context.Background(), req)
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}

	if len(result.Components) != 1 {
		t.Fatalf("Components len=%d, want 1", len(result.Components))
	}
	c := result.Components[0]
	if c.Name != "my-llm" {
		t.Errorf("Name=%q, want my-llm", c.Name)
	}
	wantRelease := ComposeReleaseName("wid", "my-llm")
	if c.ReleaseName != wantRelease {
		t.Errorf("ReleaseName=%q, want %q", c.ReleaseName, wantRelease)
	}
	if c.Status != "deployed" {
		t.Errorf("Status=%q, want deployed", c.Status)
	}

	helmEng := d.helm.(*helm.FakeEngine)
	installs := filterInstallCalls(helmEng.Calls)
	if len(installs) != 1 {
		t.Fatalf("install calls=%d, want 1", len(installs))
	}
	call := installs[0]
	if call.Request.ChartRef != "oci://r/milvus:1.0" {
		t.Errorf("ChartRef=%q, want oci://r/milvus:1.0", call.Request.ChartRef)
	}
	rc, ok := call.Request.Overrides.Workload["replicaCount"]
	if !ok {
		t.Errorf("Workload override missing replicaCount: %+v", call.Request.Overrides.Workload)
	}
	// YAML unmarshals integers as float64 OR int depending on the library;
	// sigs.k8s.io/yaml routes through JSON, so it's float64. Accept either.
	switch v := rc.(type) {
	case int, int32, int64:
		if v != 5 && v != int32(5) && v != int64(5) {
			t.Errorf("replicaCount=%v, want 5", v)
		}
	case float64:
		if v != 5 {
			t.Errorf("replicaCount=%v, want 5", v)
		}
	default:
		t.Errorf("replicaCount type=%T value=%v, want numeric 5", rc, rc)
	}
	if call.Request.Overrides.NIMGenerated != nil {
		t.Errorf("NIMGenerated=%+v, want nil (non-NIM)", call.Request.Overrides.NIMGenerated)
	}
}

func TestDeploy_Blueprint_3Components_InstallsInOrder(t *testing.T) {
	d := newTestDeployer(t)

	bpRepo := d.blueprintRepo.(*blueprint.FakeRepository)
	bpRepo.Seed(&aifv1.Blueprint{
		ObjectMeta: metav1.ObjectMeta{Name: "rag-1.0"},
		Spec: aifv1.BlueprintSpec{
			Components: []aifv1.ComponentRef{
				{Name: "llm", Kind: aifv1.ComponentKindApp, App: &aifv1.AppRef{Repo: "r", Chart: "milvus", Version: "1"}},
				{Name: "vec", Kind: aifv1.ComponentKindApp, App: &aifv1.AppRef{Repo: "r", Chart: "vec", Version: "1"}},
				{Name: "ret", Kind: aifv1.ComponentKindApp, App: &aifv1.AppRef{Repo: "r", Chart: "ret", Version: "1"}},
			},
		},
	})

	req := DeployRequest{
		Namespace: "ns", ID: "wid",
		Source: SourceRef{Kind: SourceKindBlueprint, Blueprint: &BlueprintRef{Name: "rag", Version: "1.0"}},
	}

	result, err := d.Deploy(context.Background(), req)
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}

	if len(result.Components) != 3 {
		t.Fatalf("Components len=%d, want 3", len(result.Components))
	}
	wantOrder := []string{"llm", "vec", "ret"}
	for i, name := range wantOrder {
		if result.Components[i].Name != name {
			t.Errorf("Components[%d].Name=%q, want %q", i, result.Components[i].Name, name)
		}
	}
}

func TestDeploy_NIM_GeneratesValuesAndPassesAsLayer4(t *testing.T) {
	d := newTestDeployer(t)

	disc := d.nvidiaDisc.(*stubDiscovery)
	disc.SetEntry("nim-llm:1.0", nvidia.NIMEntry{
		ID: "nim-llm:1.0", Chart: "nim-llm", Version: "1.0", Type: nvidia.TypeLLM, DefaultGPUs: 2,
	})

	nimDep := d.nvidiaDeployer.(*stubNvidiaDeployer)
	nimDep.GenerateResult = map[string]any{
		"resources": map[string]any{"limits": map[string]any{"nvidia.com/gpu": "2"}},
	}

	req := DeployRequest{
		Namespace: "ns", ID: "wid", SpecName: "my-llm", Replicas: 3,
		Source: SourceRef{Kind: SourceKindApp, App: &AppRef{Repo: "oci://r", Chart: "nim-llm", Version: "1.0"}},
	}

	_, err := d.Deploy(context.Background(), req)
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}

	if len(nimDep.Calls) != 1 {
		t.Fatalf("nvidia.Deployer.GenerateValues called %d times, want 1", len(nimDep.Calls))
	}
	gr := nimDep.Calls[0]
	if gr.Entry.ID != "nim-llm:1.0" {
		t.Errorf("Entry.ID=%q, want nim-llm:1.0", gr.Entry.ID)
	}
	if gr.Replicas != 3 {
		t.Errorf("Replicas=%d, want 3 (from req.Replicas)", gr.Replicas)
	}
	if gr.GPUs != nil {
		t.Errorf("GPUs=%v, want nil (no override → fall back to Entry.DefaultGPUs)", gr.GPUs)
	}

	helmEng := d.helm.(*helm.FakeEngine)
	installs := filterInstallCalls(helmEng.Calls)
	if len(installs) != 1 {
		t.Fatalf("helm installs=%d", len(installs))
	}
	if installs[0].Request.Overrides.NIMGenerated == nil {
		t.Errorf("NIMGenerated=nil, want layer-4 block")
	}
}

func TestDeploy_NIM_ExtractsGPUCountFromWorkloadOverrides(t *testing.T) {
	d := newTestDeployer(t)
	disc := d.nvidiaDisc.(*stubDiscovery)
	disc.SetEntry("nim-llm:1.0", nvidia.NIMEntry{ID: "nim-llm:1.0", Chart: "nim-llm", Version: "1.0", DefaultGPUs: 1})
	nimDep := d.nvidiaDeployer.(*stubNvidiaDeployer)
	nimDep.GenerateResult = map[string]any{}

	req := DeployRequest{
		Namespace: "ns", ID: "wid", SpecName: "my-llm", Replicas: 1,
		Source:    SourceRef{Kind: SourceKindApp, App: &AppRef{Repo: "r", Chart: "nim-llm", Version: "1.0"}},
		Overrides: map[string]string{"my-llm": "gpuCount: 4\nreplicaCount: 5"},
	}

	if _, err := d.Deploy(context.Background(), req); err != nil {
		t.Fatalf("Deploy: %v", err)
	}

	gr := nimDep.Calls[0]
	if gr.GPUs == nil || *gr.GPUs != 4 {
		t.Errorf("GPUs=%v, want &4", gr.GPUs)
	}
}

func TestDeploy_NIM_InvalidGPUCount_WrapsErrComponentInstallFailed(t *testing.T) {
	d := newTestDeployer(t)
	disc := d.nvidiaDisc.(*stubDiscovery)
	disc.SetEntry("nim-llm:1.0", nvidia.NIMEntry{ID: "nim-llm:1.0", Chart: "nim-llm", Version: "1.0"})
	nimDep := d.nvidiaDeployer.(*stubNvidiaDeployer)
	nimDep.GenerateErr = nvidia.ErrInvalidGPUCount

	req := DeployRequest{
		Namespace: "ns", ID: "wid", SpecName: "my-llm", Replicas: 1,
		Source:    SourceRef{Kind: SourceKindApp, App: &AppRef{Repo: "r", Chart: "nim-llm", Version: "1.0"}},
		Overrides: map[string]string{"my-llm": "gpuCount: 0"},
	}

	_, err := d.Deploy(context.Background(), req)
	if !errors.Is(err, ErrComponentInstallFailed) {
		t.Errorf("err=%v, want ErrComponentInstallFailed", err)
	}
	if !errors.Is(err, nvidia.ErrInvalidGPUCount) {
		t.Errorf("err=%v, want chain to nvidia.ErrInvalidGPUCount", err)
	}
}

func TestDeploy_NonNIM_DoesNotCallGenerateValues(t *testing.T) {
	d := newTestDeployer(t)
	// stubDiscovery default returns ErrNIMNotFound for anything not seeded.
	nimDep := d.nvidiaDeployer.(*stubNvidiaDeployer)

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

// filterInstallCalls returns only the InstallChartFromRepo entries from the
// FakeEngine call log — there's no per-method slice; the fake records all
// methods in one Calls slice.
func filterInstallCalls(calls []helm.FakeCall) []helm.FakeCall {
	out := make([]helm.FakeCall, 0, len(calls))
	for _, c := range calls {
		if c.Method == "InstallChartFromRepo" {
			out = append(out, c)
		}
	}
	return out
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

func TestDeploy_DriftCleanup_UninstallsOrphans(t *testing.T) {
	d := newTestDeployer(t)
	// FakeEngine default install returns deployed; default uninstall returns nil.

	req := DeployRequest{
		Namespace: "ns", ID: "wid", SpecName: "n",
		Source: SourceRef{Kind: SourceKindApp, App: &AppRef{Repo: "r", Chart: "milvus", Version: "1"}},
		Previous: []ComponentRelease{
			{Name: "n", ReleaseName: ComposeReleaseName("wid", "n"), Status: "deployed"},
			{Name: "old", ReleaseName: "wid-old", Status: "deployed"},
		},
	}

	result, err := d.Deploy(context.Background(), req)
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}

	helmEng := d.helm.(*helm.FakeEngine)
	uninstalls := filterUninstallCalls(helmEng.Calls)
	if len(uninstalls) != 1 || uninstalls[0].Name != "wid-old" {
		t.Errorf("uninstalls=%+v, want [wid-old]", uninstalls)
	}
	for _, c := range result.Components {
		if c.Name == "old" {
			t.Errorf("orphan 'old' present in result: %+v", c)
		}
	}
}

func TestDeploy_OrphanUninstallFails_RecordsStatus(t *testing.T) {
	d := newTestDeployer(t)
	helmEng := d.helm.(*helm.FakeEngine)
	helmEng.UninstallResult = func(ns, release string) error {
		if release == "wid-old" {
			return errors.New("transient failure")
		}
		return nil
	}

	req := DeployRequest{
		Namespace: "ns", ID: "wid", SpecName: "n",
		Source:   SourceRef{Kind: SourceKindApp, App: &AppRef{Repo: "r", Chart: "milvus", Version: "1"}},
		Previous: []ComponentRelease{
			{Name: "n", ReleaseName: ComposeReleaseName("wid", "n"), Status: "deployed"},
			{Name: "old", ReleaseName: "wid-old", Status: "deployed"},
		},
	}

	result, err := d.Deploy(context.Background(), req)
	if !errors.Is(err, ErrComponentUninstallFailed) {
		t.Errorf("err=%v, want ErrComponentUninstallFailed", err)
	}

	var foundOld *ComponentRelease
	for i := range result.Components {
		if result.Components[i].Name == "old" {
			foundOld = &result.Components[i]
		}
	}
	if foundOld == nil || foundOld.Status != ComponentStatusOrphanUninstallFailed {
		t.Errorf("expected 'old' with Status=%s, got %+v", ComponentStatusOrphanUninstallFailed, foundOld)
	}
}

// filterUninstallCalls — companion to filterInstallCalls (added in Task 19)
// for filtering the FakeEngine.Calls log by Method.
func filterUninstallCalls(calls []helm.FakeCall) []helm.FakeCall {
	out := make([]helm.FakeCall, 0, len(calls))
	for _, c := range calls {
		if c.Method == "Uninstall" {
			out = append(out, c)
		}
	}
	return out
}

func TestDeploy_PartialFailure_ContinuesAndJoins(t *testing.T) {
	d := newTestDeployer(t)

	bpRepo := d.blueprintRepo.(*blueprint.FakeRepository)
	bpRepo.Seed(&aifv1.Blueprint{
		ObjectMeta: metav1.ObjectMeta{Name: "x-1.0"},
		Spec: aifv1.BlueprintSpec{
			Components: []aifv1.ComponentRef{
				{Name: "a", Kind: aifv1.ComponentKindApp, App: &aifv1.AppRef{Repo: "r", Chart: "ca", Version: "1"}},
				{Name: "b", Kind: aifv1.ComponentKindApp, App: &aifv1.AppRef{Repo: "r", Chart: "cb", Version: "1"}},
				{Name: "c", Kind: aifv1.ComponentKindApp, App: &aifv1.AppRef{Repo: "r", Chart: "cc", Version: "1"}},
			},
		},
	})

	helmEng := d.helm.(*helm.FakeEngine)
	helmEng.InstallByRelease = map[string]helm.InstallOutcome{
		ComposeReleaseName("wid", "a"): {Status: helm.ReleaseStatus{Status: "deployed", Revision: 1}},
		ComposeReleaseName("wid", "b"): {Err: helm.ErrPullFailed},
		ComposeReleaseName("wid", "c"): {Status: helm.ReleaseStatus{Status: "deployed", Revision: 1}},
	}

	req := DeployRequest{
		Namespace: "ns", ID: "wid",
		Source: SourceRef{Kind: SourceKindBlueprint, Blueprint: &BlueprintRef{Name: "x", Version: "1.0"}},
	}

	result, err := d.Deploy(context.Background(), req)
	if !errors.Is(err, ErrComponentInstallFailed) {
		t.Errorf("err=%v, want ErrComponentInstallFailed", err)
	}
	if !errors.Is(err, helm.ErrPullFailed) {
		t.Errorf("err=%v, want chain to helm.ErrPullFailed", err)
	}
	if len(result.Components) != 3 {
		t.Fatalf("Components len=%d, want 3 (all attempted, even after b failed)", len(result.Components))
	}
	// Phase is no longer set on DeployResult (P5-1 moved phase ownership
	// to the controller); the per-component statuses below drive the
	// controller's RecomputePhase call into PhaseFailed.
	var failedSeen bool
	for _, c := range result.Components {
		if c.Status == "failed" {
			failedSeen = true
			break
		}
	}
	if !failedSeen {
		t.Errorf("expected at least one component with Status=failed in %+v", result.Components)
	}
}

// newTestDeployer is a helper used by all deployer_test.go tests.
// Builds a real *deployer with fakes for every dependency.
//
// Uses the codebase's actual fake constructors (verified in Task 14):
//   helm.NewFake() → *helm.FakeEngine
//   blueprint.NewFakeRepository() → *blueprint.FakeRepository
//   bundle.NewFakeRepository() → *bundle.FakeRepository
//   nvidia.NewDiscovery(logger) → (Discovery, AnnotationReader) — take first
//   nvidia.NewDeployer(logger) → Deployer
func newTestDeployer(t *testing.T) *deployer {
	t.Helper()
	logger := slog.Default()
	return &deployer{
		helm:           helm.NewFake(),
		blueprintRepo:  blueprint.NewFakeRepository(),
		bundleRepo:     bundle.NewFakeRepository(),
		nvidiaDisc:     newStubDiscovery(),
		nvidiaDeployer: &stubNvidiaDeployer{},
		logger:         logger,
	}
}

func TestTeardown_HappyPath(t *testing.T) {
	d := newTestDeployer(t)
	helmEng := d.helm.(*helm.FakeEngine)

	releases := []ComponentRelease{
		{Name: "a", ReleaseName: "wid-a"},
		{Name: "b", ReleaseName: "wid-b"},
	}

	if err := d.Teardown(context.Background(), "ns", releases); err != nil {
		t.Fatalf("Teardown: %v", err)
	}
	uninstalls := filterUninstallCalls(helmEng.Calls)
	if len(uninstalls) != 2 {
		t.Errorf("UninstallCalls=%d, want 2", len(uninstalls))
	}
}

func TestTeardown_PartialFailure_ReturnsJoined(t *testing.T) {
	d := newTestDeployer(t)
	helmEng := d.helm.(*helm.FakeEngine)
	var teardownBoom = errors.New("can't uninstall b")
	helmEng.UninstallResult = func(ns, release string) error {
		if release == "wid-b" {
			return teardownBoom
		}
		return nil
	}

	releases := []ComponentRelease{
		{Name: "a", ReleaseName: "wid-a"},
		{Name: "b", ReleaseName: "wid-b"},
		{Name: "c", ReleaseName: "wid-c"},
	}

	err := d.Teardown(context.Background(), "ns", releases)
	if err == nil {
		t.Fatal("Teardown returned nil, want error")
	}
	if !errors.Is(err, teardownBoom) {
		t.Errorf("err=%v, want chain to teardownBoom", err)
	}
	// All three should have been attempted (no early exit).
	uninstalls := filterUninstallCalls(helmEng.Calls)
	if len(uninstalls) != 3 {
		t.Errorf("UninstallCalls=%d, want 3 (all attempted)", len(uninstalls))
	}
}

func TestTeardown_EmptyReleases_ReturnsNil(t *testing.T) {
	d := newTestDeployer(t)
	if err := d.Teardown(context.Background(), "ns", nil); err != nil {
		t.Errorf("Teardown(nil)=%v, want nil", err)
	}
}

func TestDeploy_Idempotent_SameInputProducesSameHelmCalls(t *testing.T) {
	d := newTestDeployer(t)
	// FakeEngine default returns Status="deployed", Revision=1 — no override needed.

	req := DeployRequest{
		Namespace: "ns", ID: "wid", SpecName: "n",
		Source:    SourceRef{Kind: SourceKindApp, App: &AppRef{Repo: "r", Chart: "milvus", Version: "1"}},
		Overrides: map[string]string{"n": "replicaCount: 5"},
	}

	helmEng := d.helm.(*helm.FakeEngine)

	r1, err := d.Deploy(context.Background(), req)
	if err != nil {
		t.Fatalf("first Deploy: %v", err)
	}
	firstCount := len(filterInstallCalls(helmEng.Calls))
	first := append([]helm.FakeCall(nil), filterInstallCalls(helmEng.Calls)...)

	r2, err := d.Deploy(context.Background(), req)
	if err != nil {
		t.Fatalf("second Deploy: %v", err)
	}
	allInstalls := filterInstallCalls(helmEng.Calls)
	if len(allInstalls) != firstCount*2 {
		t.Fatalf("after 2nd call, install count=%d, want %d", len(allInstalls), firstCount*2)
	}
	second := allInstalls[firstCount:]

	if len(second) != len(first) {
		t.Fatalf("second call count=%d, want %d", len(second), len(first))
	}
	for i := range first {
		if first[i].Request.ReleaseName != second[i].Request.ReleaseName ||
			first[i].Request.ChartRef != second[i].Request.ChartRef {
			t.Errorf("call[%d] differs: first=%+v second=%+v", i, first[i].Request, second[i].Request)
		}
	}
	if !reflect.DeepEqual(r1.Components, r2.Components) {
		t.Errorf("results differ:\nfirst:  %+v\nsecond: %+v", r1.Components, r2.Components)
	}
}
