package workload

import (
	"context"
	"errors"
	"testing"

	aifv1 "github.com/SUSE/aif/api/v1alpha1"
	"github.com/SUSE/aif/pkg/workload"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// fullSpecFixture returns a Workload populated with every spec field the
// upgrade test wants to exercise for preservation: Replicas (pointer to
// non-default), ValueOverrides (map), TargetClusters (slice), DeployStrategy
// (enum), Strategy (nested struct with RollingUpdate + AutomaticRecovery
// sub-strategies), Scaling (HPA + VPA). The adapter MUST round-trip all of
// these — it must NOT zero, replace, or alias any of them.
func fullSpecFixture() *aifv1.Workload {
	replicas := int32(5)
	minReplicas := int32(2)
	maxReplicas := int32(10)
	cpuTarget := int32(75)
	maxSurge := intstr.FromInt(2)
	maxUnavailable := intstr.FromString("25%")
	failureThreshold := int32(3)
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
				Blueprint: &aifv1.BlueprintRef{Name: "rag", Version: "1.0.0"},
			},
			Replicas:       &replicas,
			TargetClusters: []string{"prod-east", "prod-west"},
			ValueOverrides: map[string]string{"nim-llm": "key: value", "frontend": "image: tag"},
			DeployStrategy: aifv1.DeployStrategyTypeHelm,
			Paused:         true,
			Strategy: &aifv1.DeploymentStrategy{
				Type: aifv1.StrategyTypeRollingUpdate,
				RollingUpdate: &aifv1.RollingUpdateStrategy{
					MaxSurge:       &maxSurge,
					MaxUnavailable: &maxUnavailable,
				},
				AutomaticRecovery: &aifv1.AutomaticRecoveryStrategy{
					Enabled:          true,
					FailureThreshold: &failureThreshold,
				},
			},
			Scaling: &aifv1.ScalingConfig{
				MinReplicas:                 &minReplicas,
				MaxReplicas:                 &maxReplicas,
				TargetCPUUtilizationPercent: &cpuTarget,
				VPA: &aifv1.VPAConfig{
					Enabled:    true,
					UpdateMode: aifv1.VPAUpdateMode("Auto"),
				},
			},
		},
	}
}

func newStoreRig(t *testing.T, seed ...*aifv1.Workload) (*UpgradeStore, *workload.FakeRepository) {
	t.Helper()
	repo := workload.NewFakeRepository()
	repo.Seed(seed...)
	return NewUpgradeStore(repo), repo
}

// TestUpgradeStore_GetUpgradeView_ProjectsBlueprintSource verifies the
// adapter copies the four fields the upgrader needs (Namespace, Name, RV,
// SourceKind, Blueprint) and that aifv1-typed sources translate to the
// domain SourceKind enum.
func TestUpgradeStore_GetUpgradeView_ProjectsBlueprintSource(t *testing.T) {
	store, _ := newStoreRig(t, fullSpecFixture())
	view, err := store.GetUpgradeView(context.Background(), "team-a", "rag-prod")
	if err != nil {
		t.Fatalf("GetUpgradeView: %v", err)
	}
	if view.Namespace != "team-a" || view.Name != "rag-prod" {
		t.Errorf("identity mismatch: %+v", view)
	}
	if view.ResourceVersion != "100" {
		t.Errorf("RV mismatch: got %q", view.ResourceVersion)
	}
	if view.SourceKind != workload.SourceKindBlueprint {
		t.Errorf("SourceKind mismatch: got %q", view.SourceKind)
	}
	if view.Blueprint == nil || view.Blueprint.Name != "rag" || view.Blueprint.Version != "1.0.0" {
		t.Errorf("Blueprint not projected: %+v", view.Blueprint)
	}
}

func TestUpgradeStore_GetUpgradeView_NonBlueprintSourceHasNilBlueprint(t *testing.T) {
	w := &aifv1.Workload{
		ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "app-wl", ResourceVersion: "1"},
		Spec: aifv1.WorkloadSpec{
			Name: "app-wl",
			Source: aifv1.WorkloadSource{
				Kind: aifv1.WorkloadSourceKindApp,
				App:  &aifv1.AppRef{Repo: "https://x", Chart: "y", Version: "1.0.0"},
			},
		},
	}
	store, _ := newStoreRig(t, w)
	view, err := store.GetUpgradeView(context.Background(), "team-a", "app-wl")
	if err != nil {
		t.Fatalf("GetUpgradeView: %v", err)
	}
	if view.SourceKind != workload.SourceKindApp {
		t.Errorf("expected SourceKindApp, got %q", view.SourceKind)
	}
	if view.Blueprint != nil {
		t.Errorf("Blueprint must be nil for non-Blueprint sources, got %+v", view.Blueprint)
	}
}

func TestUpgradeStore_GetUpgradeView_NotFoundTranslated(t *testing.T) {
	store, _ := newStoreRig(t)
	_, err := store.GetUpgradeView(context.Background(), "team-a", "ghost")
	if !errors.Is(err, workload.ErrWorkloadNotFound) {
		t.Errorf("expected ErrWorkloadNotFound, got %v", err)
	}
}

// TestUpgradeStore_PatchBlueprintVersion_PreservesEntireSpec is the
// load-bearing test for adapter behavior. It verifies that every field on
// the spec — Replicas, ValueOverrides, TargetClusters, DeployStrategy,
// Strategy (including nested RollingUpdate + AutomaticRecovery), Scaling
// (HPA + VPA), Paused — survives the patch round-trip. If any of these
// fields gets reset to zero, the adapter is silently mutating production
// workloads on every upgrade.
func TestUpgradeStore_PatchBlueprintVersion_PreservesEntireSpec(t *testing.T) {
	store, repo := newStoreRig(t, fullSpecFixture())

	view, err := store.GetUpgradeView(context.Background(), "team-a", "rag-prod")
	if err != nil {
		t.Fatalf("GetUpgradeView: %v", err)
	}
	if err := store.PatchBlueprintVersion(context.Background(), view, "1.1.0"); err != nil {
		t.Fatalf("PatchBlueprintVersion: %v", err)
	}

	// Pull the stored Workload back through the repo, bypassing the adapter
	// projection, so we can inspect every field.
	got, err := repo.Get(context.Background(), "team-a", "rag-prod")
	if err != nil {
		t.Fatalf("Get after patch: %v", err)
	}

	// Version was bumped.
	if got.Spec.Source.Blueprint == nil || got.Spec.Source.Blueprint.Version != "1.1.0" {
		t.Errorf("Blueprint.Version not patched: %+v", got.Spec.Source.Blueprint)
	}
	if got.Spec.Source.Blueprint.Name != "rag" {
		t.Errorf("Blueprint.Name was reset: %q", got.Spec.Source.Blueprint.Name)
	}

	// Top-level scalar/slice/map fields preserved.
	if got.Spec.Replicas == nil || *got.Spec.Replicas != 5 {
		t.Errorf("Replicas not preserved: %v", got.Spec.Replicas)
	}
	if len(got.Spec.TargetClusters) != 2 || got.Spec.TargetClusters[0] != "prod-east" {
		t.Errorf("TargetClusters not preserved: %v", got.Spec.TargetClusters)
	}
	if got.Spec.ValueOverrides["nim-llm"] != "key: value" || got.Spec.ValueOverrides["frontend"] != "image: tag" {
		t.Errorf("ValueOverrides not preserved: %v", got.Spec.ValueOverrides)
	}
	if got.Spec.DeployStrategy != aifv1.DeployStrategyTypeHelm {
		t.Errorf("DeployStrategy not preserved: %q", got.Spec.DeployStrategy)
	}
	if !got.Spec.Paused {
		t.Errorf("Paused not preserved")
	}

	// Strategy + nested sub-strategies preserved.
	if got.Spec.Strategy == nil {
		t.Fatal("Strategy was zeroed out")
	}
	if got.Spec.Strategy.Type != aifv1.StrategyTypeRollingUpdate {
		t.Errorf("Strategy.Type not preserved: %q", got.Spec.Strategy.Type)
	}
	if got.Spec.Strategy.RollingUpdate == nil ||
		got.Spec.Strategy.RollingUpdate.MaxSurge == nil ||
		got.Spec.Strategy.RollingUpdate.MaxSurge.IntValue() != 2 {
		t.Errorf("RollingUpdate.MaxSurge not preserved: %+v", got.Spec.Strategy.RollingUpdate)
	}
	if got.Spec.Strategy.RollingUpdate.MaxUnavailable == nil ||
		got.Spec.Strategy.RollingUpdate.MaxUnavailable.StrVal != "25%" {
		t.Errorf("RollingUpdate.MaxUnavailable not preserved: %+v", got.Spec.Strategy.RollingUpdate.MaxUnavailable)
	}
	if got.Spec.Strategy.AutomaticRecovery == nil || !got.Spec.Strategy.AutomaticRecovery.Enabled ||
		got.Spec.Strategy.AutomaticRecovery.FailureThreshold == nil ||
		*got.Spec.Strategy.AutomaticRecovery.FailureThreshold != 3 {
		t.Errorf("AutomaticRecovery not preserved: %+v", got.Spec.Strategy.AutomaticRecovery)
	}

	// Scaling (HPA + VPA) preserved.
	if got.Spec.Scaling == nil {
		t.Fatal("Scaling was zeroed out")
	}
	if got.Spec.Scaling.MinReplicas == nil || *got.Spec.Scaling.MinReplicas != 2 {
		t.Errorf("Scaling.MinReplicas not preserved: %v", got.Spec.Scaling.MinReplicas)
	}
	if got.Spec.Scaling.MaxReplicas == nil || *got.Spec.Scaling.MaxReplicas != 10 {
		t.Errorf("Scaling.MaxReplicas not preserved: %v", got.Spec.Scaling.MaxReplicas)
	}
	if got.Spec.Scaling.TargetCPUUtilizationPercent == nil || *got.Spec.Scaling.TargetCPUUtilizationPercent != 75 {
		t.Errorf("Scaling.TargetCPU not preserved: %v", got.Spec.Scaling.TargetCPUUtilizationPercent)
	}
	if got.Spec.Scaling.VPA == nil || !got.Spec.Scaling.VPA.Enabled || got.Spec.Scaling.VPA.UpdateMode != "Auto" {
		t.Errorf("Scaling.VPA not preserved: %+v", got.Spec.Scaling.VPA)
	}
}

// TestUpgradeStore_PatchBlueprintVersion_ConflictOnRVMismatch verifies that
// the pre-flight RV check catches mid-flight mutations (someone bumped the
// stored RV between GetUpgradeView and Patch) and surfaces ErrUpgradeConflict.
func TestUpgradeStore_PatchBlueprintVersion_ConflictOnRVMismatch(t *testing.T) {
	store, repo := newStoreRig(t, fullSpecFixture())

	view, err := store.GetUpgradeView(context.Background(), "team-a", "rag-prod")
	if err != nil {
		t.Fatalf("GetUpgradeView: %v", err)
	}

	// Simulate a concurrent writer bumping the stored ResourceVersion.
	stored, _ := repo.Get(context.Background(), "team-a", "rag-prod")
	stored.ResourceVersion = "200"
	_ = repo.Update(context.Background(), stored)

	err = store.PatchBlueprintVersion(context.Background(), view, "1.1.0")
	if !errors.Is(err, workload.ErrUpgradeConflict) {
		t.Errorf("expected ErrUpgradeConflict on RV mismatch, got %v", err)
	}
}

// TestUpgradeStore_PatchBlueprintVersion_ApiserverConflictTranslated covers
// the case where the pre-flight RV check passes (a writer races between the
// adapter's Get and Patch) and the apiserver itself rejects the patch with
// apierrors.IsConflict. The adapter MUST translate that to ErrUpgradeConflict.
func TestUpgradeStore_PatchBlueprintVersion_ApiserverConflictTranslated(t *testing.T) {
	store, repo := newStoreRig(t, fullSpecFixture())
	view, err := store.GetUpgradeView(context.Background(), "team-a", "rag-prod")
	if err != nil {
		t.Fatalf("GetUpgradeView: %v", err)
	}
	repo.PatchErr = apierrors.NewConflict(
		schema.GroupResource{Group: "ai.suse.com", Resource: "workloads"},
		"rag-prod", errors.New("simulated apiserver conflict"))

	err = store.PatchBlueprintVersion(context.Background(), view, "1.1.0")
	if !errors.Is(err, workload.ErrUpgradeConflict) {
		t.Errorf("expected ErrUpgradeConflict, got %v", err)
	}
}

func TestUpgradeStore_PatchBlueprintVersion_NotFoundTranslated(t *testing.T) {
	store, _ := newStoreRig(t)
	view := &workload.UpgradeWorkloadView{
		Namespace: "team-a", Name: "ghost", ResourceVersion: "1",
		SourceKind: workload.SourceKindBlueprint,
		Blueprint:  &workload.BlueprintRef{Name: "rag", Version: "1.0.0"},
	}
	err := store.PatchBlueprintVersion(context.Background(), view, "1.1.0")
	if !errors.Is(err, workload.ErrWorkloadNotFound) {
		t.Errorf("expected ErrWorkloadNotFound, got %v", err)
	}
}
