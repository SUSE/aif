package workload

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

// workloadViewFixture returns a Blueprint-sourced UpgradeWorkloadView at the
// given version. RV is fixed at "100" so PatchBlueprintVersion succeeds on
// the happy path.
func workloadViewFixture(version string) *UpgradeWorkloadView {
	return &UpgradeWorkloadView{
		Namespace:       "team-a",
		Name:            "rag-prod",
		ResourceVersion: "100",
		SourceKind:      SourceKindBlueprint,
		Blueprint:       &BlueprintRef{Name: "rag", Version: version},
	}
}

// blueprintViewFixture returns an UpgradeBlueprintView with lineage "rag" and
// the given version (CR name is the conventional lineage.version).
// withdrawn=true mirrors what BlueprintReader.GetForUpgrade returns for a
// Withdrawn-phase Blueprint.
func blueprintViewFixture(lineage, version string, withdrawn bool) *UpgradeBlueprintView {
	return &UpgradeBlueprintView{
		Name:      lineage + "." + version,
		Lineage:   lineage,
		Withdrawn: withdrawn,
	}
}

type testWriter struct{ t *testing.T }

func (w testWriter) Write(p []byte) (int, error) { w.t.Log(string(p)); return len(p), nil }

func newTestUpgrader(t *testing.T, workloads []*UpgradeWorkloadView, blueprints []*UpgradeBlueprintView) (Upgrader, *FakeWorkloadStore, *FakeBlueprintReader, *FakeUpgradeEventRecorder) {
	t.Helper()
	wStore := NewFakeWorkloadStore()
	wStore.Seed(workloads...)
	bpReader := NewFakeBlueprintReader()
	bpReader.Seed(blueprints...)
	rec := &FakeUpgradeEventRecorder{}
	logger := slog.New(slog.NewTextHandler(testWriter{t}, nil))
	return NewUpgrader(wStore, bpReader, rec, logger), wStore, bpReader, rec
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
	v := workloadViewFixture("1.0.0")
	v.SourceKind = SourceKindApp
	v.Blueprint = nil
	u, _, _, rec := newTestUpgrader(t, []*UpgradeWorkloadView{v}, nil)
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
		[]*UpgradeWorkloadView{workloadViewFixture("1.0.0")},
		nil, // no blueprint views seeded
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
	// Workload sources from lineage "rag", but the target blueprint view's
	// Lineage is "vision" — cross-lineage. The view is looked up by NAME
	// "rag.1.1.0" (constructed from the workload's current lineage) so we
	// seed a view with that exact name but mismatched Lineage.
	bp := blueprintViewFixture("rag", "1.1.0", false)
	bp.Lineage = "vision" // wrong lineage in spec
	u, _, _, rec := newTestUpgrader(t,
		[]*UpgradeWorkloadView{workloadViewFixture("1.0.0")},
		[]*UpgradeBlueprintView{bp},
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
		[]*UpgradeWorkloadView{workloadViewFixture("1.0.0")},
		[]*UpgradeBlueprintView{blueprintViewFixture("rag", "1.1.0", true)},
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
		[]*UpgradeWorkloadView{workloadViewFixture("1.5.0")},
		[]*UpgradeBlueprintView{blueprintViewFixture("rag", "1.4.0", false)},
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
		[]*UpgradeWorkloadView{workloadViewFixture("1.0.0")},
		[]*UpgradeBlueprintView{blueprintViewFixture("rag", "1.0.0", false)},
	)
	_, err := u.Upgrade(context.Background(), "team-a", "rag-prod", "1.0.0", "alice")
	if !errors.Is(err, ErrDowngradeNotSupported) {
		t.Errorf("expected ErrDowngradeNotSupported for same version, got %v", err)
	}
}

func TestUpgrader_HappyPath(t *testing.T) {
	u, wStore, _, rec := newTestUpgrader(t,
		[]*UpgradeWorkloadView{workloadViewFixture("1.0.0")},
		[]*UpgradeBlueprintView{blueprintViewFixture("rag", "1.1.0", false)},
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

	// Verify the stored view's blueprint version was patched.
	got, _ := wStore.GetUpgradeView(context.Background(), "team-a", "rag-prod")
	if got.Blueprint.Version != "1.1.0" {
		t.Errorf("expected stored version 1.1.0, got %s", got.Blueprint.Version)
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
	// Inject a Conflict on PatchBlueprintVersion. Event MUST still be recorded
	// — audit-before-patch is required by PROJECT_PLAN.md AC line 2004 ("emit
	// the event BEFORE the spec patch so the audit trail records the intent
	// even if the patch races").
	u, wStore, _, rec := newTestUpgrader(t,
		[]*UpgradeWorkloadView{workloadViewFixture("1.0.0")},
		[]*UpgradeBlueprintView{blueprintViewFixture("rag", "1.1.0", false)},
	)
	wStore.PatchErr = ErrUpgradeConflict
	_, err := u.Upgrade(context.Background(), "team-a", "rag-prod", "1.1.0", "alice")
	if !errors.Is(err, ErrUpgradeConflict) {
		t.Errorf("expected ErrUpgradeConflict, got %v", err)
	}
	if len(rec.Events) != 1 {
		t.Errorf("event must be recorded BEFORE patch (audit-before-patch); got events=%v", rec.Events)
	}
}

func TestUpgrader_NonConflictPatchErrorWrapped(t *testing.T) {
	// Force a non-conflict patch failure (e.g. webhook rejection, apiserver
	// blip). The upgrader must NOT return ErrUpgradeConflict (the handler
	// would map that to 409 — wrong status); it must wrap the underlying
	// error so the handler's default arm yields 500. The audit event was
	// already recorded; the upgrader logs at Error so operators can correlate.
	sentinel := errors.New("simulated webhook rejection")
	u, wStore, _, rec := newTestUpgrader(t,
		[]*UpgradeWorkloadView{workloadViewFixture("1.0.0")},
		[]*UpgradeBlueprintView{blueprintViewFixture("rag", "1.1.0", false)},
	)
	wStore.PatchErr = sentinel

	_, err := u.Upgrade(context.Background(), "team-a", "rag-prod", "1.1.0", "alice")
	if err == nil {
		t.Fatal("expected non-nil error")
	}
	if errors.Is(err, ErrUpgradeConflict) {
		t.Errorf("non-conflict patch failure must NOT classify as ErrUpgradeConflict, got %v", err)
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("expected wrapped sentinel reachable via errors.Is, got %v", err)
	}
	if len(rec.Events) != 1 {
		t.Errorf("event must still be recorded before patch on non-conflict failures; got events=%v", rec.Events)
	}
}
