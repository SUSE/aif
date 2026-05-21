package workload

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	aifv1 "github.com/SUSE/aif/api/v1alpha1"
	"github.com/SUSE/aif/pkg/blueprint"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// workloadFixture returns a Blueprint-sourced Workload at the given version.
func workloadFixture(version string) *aifv1.Workload {
	return &aifv1.Workload{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       "team-a",
			Name:            "rag-prod",
			ResourceVersion: "100",
		},
		Spec: aifv1.WorkloadSpec{
			Name: "rag-prod",
			Source: aifv1.WorkloadSource{
				Kind:      aifv1.WorkloadSourceKindBlueprint,
				Blueprint: &aifv1.BlueprintRef{Name: "rag", Version: version},
			},
		},
	}
}

// blueprintFixture returns a Blueprint CR with name = "{lineage}.{version}",
// spec.blueprintName = lineage, spec.version = version, status.phase = phase.
func blueprintFixture(lineage, version string, phase aifv1.BlueprintPhase) *aifv1.Blueprint {
	return &aifv1.Blueprint{
		ObjectMeta: metav1.ObjectMeta{Name: lineage + "." + version},
		Spec: aifv1.BlueprintSpec{
			BlueprintName: lineage,
			Version:       version,
		},
		Status: aifv1.BlueprintStatus{Phase: phase},
	}
}

type testWriter struct{ t *testing.T }

func (w testWriter) Write(p []byte) (int, error) { w.t.Log(string(p)); return len(p), nil }

func newTestUpgrader(t *testing.T, workloads []*aifv1.Workload, blueprints []*aifv1.Blueprint) (Upgrader, *FakeRepository, *blueprint.FakeRepository, *FakeUpgradeEventRecorder) {
	t.Helper()
	wRepo := NewFakeRepository()
	wRepo.Seed(workloads...)
	bpRepo := blueprint.NewFakeRepository()
	bpRepo.Seed(blueprints...)
	rec := &FakeUpgradeEventRecorder{}
	logger := slog.New(slog.NewTextHandler(testWriter{t}, nil))
	return NewUpgrader(wRepo, bpRepo, rec, logger), wRepo, bpRepo, rec
}

func TestUpgrader_WorkloadNotFound(t *testing.T) {
	u, _, _, rec := newTestUpgrader(t, nil, nil)
	_, err := u.Upgrade(context.Background(), "team-a", "missing", "1.1.0", "alice")
	if !errors.Is(err, ErrWorkloadNotFound) {
		t.Errorf("expected ErrWorkloadNotFound, got %v", err)
	}
	if len(rec.Events) != 0 {
		t.Errorf("expected no events, got %v", rec.Events)
	}
}

func TestUpgrader_SourceNotBlueprint(t *testing.T) {
	w := workloadFixture("1.0.0")
	w.Spec.Source = aifv1.WorkloadSource{
		Kind: aifv1.WorkloadSourceKindApp,
		App:  &aifv1.AppRef{Repo: "https://x", Chart: "y", Version: "1.0.0"},
	}
	u, _, _, rec := newTestUpgrader(t, []*aifv1.Workload{w}, nil)
	_, err := u.Upgrade(context.Background(), "team-a", "rag-prod", "1.1.0", "alice")
	if !errors.Is(err, ErrSourceNotBlueprint) {
		t.Errorf("expected ErrSourceNotBlueprint, got %v", err)
	}
	if len(rec.Events) != 0 {
		t.Errorf("expected no events, got %v", rec.Events)
	}
}

func TestUpgrader_BlueprintVersionNotFound(t *testing.T) {
	u, _, _, rec := newTestUpgrader(t,
		[]*aifv1.Workload{workloadFixture("1.0.0")},
		nil, // no blueprint CRs seeded
	)
	_, err := u.Upgrade(context.Background(), "team-a", "rag-prod", "1.1.0", "alice")
	if !errors.Is(err, ErrBlueprintVersionNotFound) {
		t.Errorf("expected ErrBlueprintVersionNotFound, got %v", err)
	}
	if len(rec.Events) != 0 {
		t.Errorf("expected no events, got %v", rec.Events)
	}
}

func TestUpgrader_CrossLineageUpgrade(t *testing.T) {
	// Workload sources from lineage "rag", but the target blueprint CR's
	// spec.blueprintName is "vision" — cross-lineage. The CR is looked up
	// by NAME "rag.1.1.0" (constructed from the workload's current lineage)
	// so we seed a CR with that exact name but mismatched spec.blueprintName.
	bp := blueprintFixture("rag", "1.1.0", aifv1.BlueprintPhaseActive)
	bp.Spec.BlueprintName = "vision" // wrong lineage in spec
	u, _, _, rec := newTestUpgrader(t,
		[]*aifv1.Workload{workloadFixture("1.0.0")},
		[]*aifv1.Blueprint{bp},
	)
	_, err := u.Upgrade(context.Background(), "team-a", "rag-prod", "1.1.0", "alice")
	if !errors.Is(err, ErrCrossLineageUpgrade) {
		t.Errorf("expected ErrCrossLineageUpgrade, got %v", err)
	}
	if !strings.Contains(err.Error(), "Cross-lineage upgrade not allowed") {
		t.Errorf("expected AC verbatim message, got %q", err.Error())
	}
	if len(rec.Events) != 0 {
		t.Errorf("expected no events, got %v", rec.Events)
	}
}

func TestUpgrader_TargetWithdrawn(t *testing.T) {
	u, _, _, rec := newTestUpgrader(t,
		[]*aifv1.Workload{workloadFixture("1.0.0")},
		[]*aifv1.Blueprint{blueprintFixture("rag", "1.1.0", aifv1.BlueprintPhaseWithdrawn)},
	)
	_, err := u.Upgrade(context.Background(), "team-a", "rag-prod", "1.1.0", "alice")
	if !errors.Is(err, ErrTargetWithdrawn) {
		t.Errorf("expected ErrTargetWithdrawn, got %v", err)
	}
	if !strings.Contains(err.Error(), "Cannot upgrade to a Withdrawn Blueprint version") {
		t.Errorf("expected AC verbatim message, got %q", err.Error())
	}
	if len(rec.Events) != 0 {
		t.Errorf("expected no events, got %v", rec.Events)
	}
}

func TestUpgrader_DowngradeNotSupported(t *testing.T) {
	// Current = 1.5.0, target = 1.4.0 — downgrade.
	u, _, _, rec := newTestUpgrader(t,
		[]*aifv1.Workload{workloadFixture("1.5.0")},
		[]*aifv1.Blueprint{blueprintFixture("rag", "1.4.0", aifv1.BlueprintPhaseActive)},
	)
	_, err := u.Upgrade(context.Background(), "team-a", "rag-prod", "1.4.0", "alice")
	if !errors.Is(err, ErrDowngradeNotSupported) {
		t.Errorf("expected ErrDowngradeNotSupported, got %v", err)
	}
	if !strings.Contains(err.Error(), "Upgrade must target a higher version") {
		t.Errorf("expected AC verbatim message, got %q", err.Error())
	}
	if len(rec.Events) != 0 {
		t.Errorf("expected no events, got %v", rec.Events)
	}
}

func TestUpgrader_DowngradeSameVersion(t *testing.T) {
	// Current = 1.0.0, target = 1.0.0 — not strictly greater, also downgrade.
	u, _, _, _ := newTestUpgrader(t,
		[]*aifv1.Workload{workloadFixture("1.0.0")},
		[]*aifv1.Blueprint{blueprintFixture("rag", "1.0.0", aifv1.BlueprintPhaseActive)},
	)
	_, err := u.Upgrade(context.Background(), "team-a", "rag-prod", "1.0.0", "alice")
	if !errors.Is(err, ErrDowngradeNotSupported) {
		t.Errorf("expected ErrDowngradeNotSupported for same version, got %v", err)
	}
}

func TestUpgrader_HappyPath(t *testing.T) {
	u, wRepo, _, rec := newTestUpgrader(t,
		[]*aifv1.Workload{workloadFixture("1.0.0")},
		[]*aifv1.Blueprint{blueprintFixture("rag", "1.1.0", aifv1.BlueprintPhaseActive)},
	)
	result, err := u.Upgrade(context.Background(), "team-a", "rag-prod", "1.1.0", "alice")
	if err != nil {
		t.Fatalf("Upgrade: %v", err)
	}
	if result.OldVersion != "1.0.0" || result.NewVersion != "1.1.0" {
		t.Errorf("result version mismatch: %+v", result)
	}
	if result.BlueprintName != "rag" {
		t.Errorf("expected BlueprintName=rag, got %q", result.BlueprintName)
	}

	// Verify the spec was patched.
	got, _ := wRepo.Get(context.Background(), "team-a", "rag-prod")
	if got.Spec.Source.Blueprint.Version != "1.1.0" {
		t.Errorf("expected stored version 1.1.0, got %s", got.Spec.Source.Blueprint.Version)
	}

	// Verify the event was recorded with the right payload.
	if len(rec.Events) != 1 {
		t.Fatalf("expected 1 event, got %d: %v", len(rec.Events), rec.Events)
	}
	if rec.Events[0] != "UpgradeStarted:team-a/rag-prod:1.0.0→1.1.0" {
		t.Errorf("unexpected event payload: %q", rec.Events[0])
	}
}

func TestUpgrader_EventRecordedBeforePatchOnConflict(t *testing.T) {
	// Inject a Conflict on Patch. Event MUST still be recorded — audit-before-patch
	// is required by PROJECT_PLAN.md AC line 2004 ("emit the event BEFORE the
	// spec patch so the audit trail records the intent even if the patch races").
	u, wRepo, _, rec := newTestUpgrader(t,
		[]*aifv1.Workload{workloadFixture("1.0.0")},
		[]*aifv1.Blueprint{blueprintFixture("rag", "1.1.0", aifv1.BlueprintPhaseActive)},
	)
	wRepo.PatchErr = apierrors.NewConflict(
		schema.GroupResource{Group: "ai.suse.com", Resource: "workloads"},
		"rag-prod",
		errors.New("simulated"),
	)
	_, err := u.Upgrade(context.Background(), "team-a", "rag-prod", "1.1.0", "alice")
	if !errors.Is(err, ErrUpgradeConflict) {
		t.Errorf("expected ErrUpgradeConflict, got %v", err)
	}
	if len(rec.Events) != 1 {
		t.Errorf("event must be recorded BEFORE patch (audit-before-patch); got events=%v", rec.Events)
	}
}

func TestUpgrader_PreservesOtherSpecFields(t *testing.T) {
	w := workloadFixture("1.0.0")
	replicas := int32(5)
	w.Spec.Replicas = &replicas
	w.Spec.ValueOverrides = map[string]string{"nim-llm": "key: value"}
	w.Spec.TargetClusters = []string{"prod-east"}

	u, wRepo, _, _ := newTestUpgrader(t,
		[]*aifv1.Workload{w},
		[]*aifv1.Blueprint{blueprintFixture("rag", "1.1.0", aifv1.BlueprintPhaseActive)},
	)
	_, err := u.Upgrade(context.Background(), "team-a", "rag-prod", "1.1.0", "alice")
	if err != nil {
		t.Fatalf("Upgrade: %v", err)
	}

	got, _ := wRepo.Get(context.Background(), "team-a", "rag-prod")
	if got.Spec.Replicas == nil || *got.Spec.Replicas != 5 {
		t.Errorf("Replicas not preserved: %v", got.Spec.Replicas)
	}
	if got.Spec.ValueOverrides["nim-llm"] != "key: value" {
		t.Errorf("ValueOverrides not preserved: %v", got.Spec.ValueOverrides)
	}
	if len(got.Spec.TargetClusters) != 1 || got.Spec.TargetClusters[0] != "prod-east" {
		t.Errorf("TargetClusters not preserved: %v", got.Spec.TargetClusters)
	}
}
