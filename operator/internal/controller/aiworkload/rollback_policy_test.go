package aiworkload

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	ctrl "sigs.k8s.io/controller-runtime"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	infrahelm "github.com/SUSE/aif-operator/internal/infra/helm"
	"github.com/SUSE/aif-operator/internal/naming"
)

func ragBP(version string, policy aiplatformv1alpha1.RollbackPolicy) *aiplatformv1alpha1.Blueprint {
	return &aiplatformv1alpha1.Blueprint{
		ObjectMeta: metav1.ObjectMeta{Name: naming.BlueprintCRName("rag", version)},
		Spec:       aiplatformv1alpha1.BlueprintSpec{DisplayName: "RAG", Version: version, Rollback: policy},
	}
}

func TestUpgradeTrigger_OlderTargetHonoursRollbackPolicy(t *testing.T) {
	for _, tc := range []struct {
		name        string
		policy      aiplatformv1alpha1.RollbackPolicy
		wantVersion string
		wantJournal bool
	}{
		{"disabled", aiplatformv1alpha1.RollbackDisabled, "2.0.0", false},
		{"allowed", "", "1.0.0", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			w := wlWithBlueprint("2.0.0")
			w.Annotations = map[string]string{upgradeRequestAnnotation: "u1|1.0.0"}
			w.Status.DeployedSource = &aiplatformv1alpha1.DeployedSourceSnapshot{Version: "2.0.0"}
			cl := fakeClient(w, ragBP("2.0.0", tc.policy), ragBP("1.0.0", ""))
			rec := record.NewFakeRecorder(16)
			r := &AIWorkloadReconciler{Client: cl, Scheme: cl.Scheme(), Recorder: rec}

			if handled, err := r.handleTriggers(ctx, w); err != nil || !handled {
				t.Fatalf("handleTriggers = %v, %v", handled, err)
			}
			got := &aiplatformv1alpha1.AIWorkload{}
			if err := cl.Get(ctx, types.NamespacedName{Name: w.Name, Namespace: w.Namespace}, got); err != nil {
				t.Fatal(err)
			}
			if got.Spec.Source.Blueprint.Version != tc.wantVersion {
				t.Errorf("version = %s, want %s", got.Spec.Source.Blueprint.Version, tc.wantVersion)
			}
			if _, ok := got.Annotations[upgradeRequestAnnotation]; ok {
				t.Error("upgrade-request annotation should be consumed")
			}
			if _, ok := readJournal(got); ok != tc.wantJournal {
				t.Errorf("journal present = %v, want %v", ok, tc.wantJournal)
			}
			if !tc.wantJournal {
				e := <-rec.Events
				if !strings.Contains(e, "Warning "+reasonRollbackDisabled) {
					t.Errorf("unexpected event %q", e)
				}
			}
		})
	}
}

func TestReconcileBlueprintStatus_SpecDowngradeHonoursRollbackPolicy(t *testing.T) {
	scheme := clusterRepoErrorScheme(t)
	w := newBlueprintWorkload(time.Now().Add(-24 * time.Hour))
	w.Status.DeployedSource = &aiplatformv1alpha1.DeployedSourceSnapshot{Version: "2.0.0"}
	older := newBlueprintCR("application-collection")
	deployed := newBlueprintCR("application-collection")
	deployed.Name = naming.BlueprintCRName("mini", "2.0.0")
	deployed.Spec.Rollback = aiplatformv1alpha1.RollbackDisabled
	repo := &unstructured.Unstructured{}
	repo.SetGroupVersionKind(clusterRepoGVK)
	repo.SetName("application-collection")
	_ = unstructured.SetNestedField(repo.Object, "https://charts.example.com", "spec", "url")
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(w, older, deployed, repo).
		WithStatusSubresource(&aiplatformv1alpha1.AIWorkload{}).Build()
	rec := record.NewFakeRecorder(16)
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme, OperatorNamespace: "aif-operator", Recorder: rec}

	res, _, err := r.reconcileBlueprintStatus(context.Background(), w)
	if err != nil || !res.IsZero() {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	cond := meta.FindStatusCondition(w.Status.Conditions, conditionTypeReady)
	if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason != reasonRollbackDisabled || !strings.Contains(cond.Message, "upgrade back to 2.0.0") {
		t.Fatalf("Ready condition = %+v", cond)
	}
	helmOps := &unstructured.UnstructuredList{}
	helmOps.SetGroupVersionKind(helmOpGVK.GroupVersion().WithKind("HelmOpList"))
	if err := c.List(context.Background(), helmOps); err != nil || len(helmOps.Items) != 0 {
		t.Fatalf("rendered %d HelmOps (err %v); want none", len(helmOps.Items), err)
	}
	if e := <-rec.Events; !strings.Contains(e, "Warning "+reasonRollbackDisabled) {
		t.Errorf("unexpected event %q", e)
	}

	r.refuseRender(w, reasonRollbackDisabled, cond.Message)
	select {
	case e := <-rec.Events:
		t.Errorf("repeated refusal emitted another event %q", e)
	default:
	}

	deployed.Spec.Rollback = ""
	if err := c.Update(context.Background(), deployed); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.reconcileBlueprintStatus(context.Background(), w); err != nil {
		t.Fatal(err)
	}
	if cond := meta.FindStatusCondition(w.Status.Conditions, conditionTypeReady); cond != nil && cond.Reason == reasonRollbackDisabled {
		t.Fatalf("allowed downgrade still refused: %+v", cond)
	}
}

func TestRefuseRender_Phase(t *testing.T) {
	for _, tc := range []struct {
		from, want aiplatformv1alpha1.AIWorkloadPhase
	}{
		{aiplatformv1alpha1.AIWorkloadPhaseRunning, aiplatformv1alpha1.AIWorkloadPhaseDegraded},
		{aiplatformv1alpha1.AIWorkloadPhaseDegraded, aiplatformv1alpha1.AIWorkloadPhaseDegraded},
		{aiplatformv1alpha1.AIWorkloadPhasePending, aiplatformv1alpha1.AIWorkloadPhaseFailed},
		{"", aiplatformv1alpha1.AIWorkloadPhaseFailed},
	} {
		w := newBlueprintWorkload(time.Now().Add(-24 * time.Hour))
		w.Status.Phase = tc.from
		(&AIWorkloadReconciler{}).refuseRender(w, reasonLockedValueChanged, "m")
		if w.Status.Phase != tc.want {
			t.Errorf("from %q: phase = %s, want %s", tc.from, w.Status.Phase, tc.want)
		}
	}
}

func TestOperationOutcome_RefusedSpecFailsImmediately(t *testing.T) {
	w := wlWithBlueprint("1.0.0")
	w.Generation = 3
	op := aiplatformv1alpha1.AIWorkloadOperation{
		Type: aiplatformv1alpha1.OperationTypeRollback, State: aiplatformv1alpha1.OperationStateInProgress,
		IntentDigest: intentDigest(w.Spec), RequestedAt: metav1.Now(),
	}
	for _, reason := range []string{reasonRollbackDisabled, reasonLockedValueChanged, reasonInvalidValuesFrom, reasonCRDOutdated} {
		w.Status.Conditions = nil
		setCondition(&w.Status.Conditions, conditionTypeReady, metav1.ConditionFalse, reason, "m", w.Generation)
		out := operationOutcome(w, op, false, false, false, defaultOperationDeadline)
		if out.State != aiplatformv1alpha1.OperationStateFailed || out.Reason != reason {
			t.Errorf("%s: outcome = %s/%s, want Failed/%s", reason, out.State, out.Reason, reason)
		}
	}
	w.Status.Conditions = nil
	setCondition(&w.Status.Conditions, conditionTypeReady, metav1.ConditionFalse, reasonLockedValueChanged, "m", w.Generation-1)
	if out := operationOutcome(w, op, false, false, false, defaultOperationDeadline); out.State != aiplatformv1alpha1.OperationStateInProgress {
		t.Errorf("stale refusal from an older generation: outcome = %s, want InProgress", out.State)
	}
	w.Status.Conditions = nil
	setCondition(&w.Status.Conditions, conditionTypeReady, metav1.ConditionFalse, reasonClusterRepoNotReady, "m", w.Generation)
	if out := operationOutcome(w, op, false, false, false, defaultOperationDeadline); out.State != aiplatformv1alpha1.OperationStateInProgress {
		t.Errorf("transient reason: outcome = %s, want InProgress", out.State)
	}
}

// floorCase builds a one-component "mini" 1.0.0 workload (component "app")
// whose Blueprint disables rollback, on one render path.
type floorCase struct {
	gitOps     bool
	overlay    bool // install overlay: embedded Bundle
	seedHelmOp bool // with overlay: an existing HelmOp makes it a kind switch
	puller     chartArchivePuller
	failFloor  bool // the status write carrying the floor fails
	pruneFloor bool // an outdated CRD prunes the field
	mutate     func(w *aiplatformv1alpha1.AIWorkload, bp *aiplatformv1alpha1.Blueprint)
}

type floorEnv struct {
	r           *AIWorkloadReconciler
	c           client.Client
	w           *aiplatformv1alpha1.AIWorkload
	remote      string
	statusWrite int // non-dry-run status updates
}

func newFloorEnv(t *testing.T, fc floorCase) *floorEnv {
	t.Helper()
	scheme := clusterRepoErrorScheme(t)
	addBundleScheme(scheme)
	e := &floorEnv{}
	w := newBlueprintWorkload(time.Now().Add(-24 * time.Hour))
	w.UID = "uid-floor"
	bp := miniBP("1.0.0", aiplatformv1alpha1.RollbackDisabled)
	if fc.overlay {
		w.Spec.ComponentValues = []aiplatformv1alpha1.ComponentValueOverride{{ComponentName: "app", Kustomize: inlineOverlay("inst", emptyPatches)}}
	}
	repo := &unstructured.Unstructured{}
	repo.SetGroupVersionKind(clusterRepoGVK)
	repo.SetName("application-collection")
	_ = unstructured.SetNestedField(repo.Object, "https://charts.example.com", "spec", "url")
	objs := []client.Object{repo}
	if fc.gitOps {
		w.Spec.DeployStrategy = aiplatformv1alpha1.AIWorkloadDeployGitOps
		e.remote = newBlueprintGitOpsRemote(t)
		objs = append(objs, &aiplatformv1alpha1.Settings{
			ObjectMeta: metav1.ObjectMeta{Name: operatorSettingsName, Namespace: "aif-operator"},
			Spec: aiplatformv1alpha1.SettingsSpec{
				Fleet: aiplatformv1alpha1.FleetSettings{GitRepoSource: aiplatformv1alpha1.GitRepoSource{RepoURL: e.remote, Branch: "main"}},
			},
		})
	}
	if fc.seedHelmOp {
		objs = append(objs, seedHelmOp(blueprintBundleName(w.Name, "app"), string(w.UID), false))
	}
	if fc.mutate != nil {
		fc.mutate(w, bp)
	}
	objs = append(objs, w, bp)
	funcs := interceptor.Funcs{SubResourceUpdate: func(ctx context.Context, cl client.Client, sub string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
		o := &client.SubResourceUpdateOptions{}
		o.ApplyOptions(opts)
		if len(o.DryRun) == 0 {
			e.statusWrite++
		}
		if wl, ok := obj.(*aiplatformv1alpha1.AIWorkload); ok && wl.Status.MinimumSafeVersion != "" {
			if fc.failFloor {
				return fmt.Errorf("injected status write failure")
			}
			if fc.pruneFloor {
				wl.Status.MinimumSafeVersion = ""
			}
		}
		return cl.SubResource(sub).Update(ctx, obj, opts...)
	}}
	e.c = fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).
		WithStatusSubresource(&aiplatformv1alpha1.AIWorkload{}).WithInterceptorFuncs(funcs).Build()
	puller := fc.puller
	if puller == nil {
		tgz := makeChartTgz(t, map[string]string{"app/Chart.yaml": "apiVersion: v2\nname: app\nversion: 1.0.0\n"})
		puller = func(context.Context, infrahelm.ChartArchiveRequest) ([]byte, error) { return tgz, nil }
	}
	e.r = &AIWorkloadReconciler{Client: e.c, Scheme: scheme, OperatorNamespace: "aif-operator", Recorder: record.NewFakeRecorder(16),
		ChartPuller: puller, ClusterFacts: &fakeFacts{ip: "10.9.9.9"}}
	e.w = w
	return e
}

func (e *floorEnv) reconcile() (ctrl.Result, error) {
	res, _, err := e.r.reconcileBlueprintStatus(context.Background(), e.w)
	return res, err
}

func (e *floorEnv) storedFloor(t *testing.T) string {
	t.Helper()
	got := &aiplatformv1alpha1.AIWorkload{}
	if err := e.c.Get(context.Background(), client.ObjectKeyFromObject(e.w), got); err != nil {
		t.Fatal(err)
	}
	return got.Status.MinimumSafeVersion
}

// rendered reports whether the path's render write happened.
func (e *floorEnv) rendered(t *testing.T, fc floorCase) bool {
	t.Helper()
	name := blueprintBundleName(e.w.Name, "app")
	switch {
	case fc.gitOps:
		repo, err := gogit.PlainOpen(strings.TrimPrefix(e.remote, "file://"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := repo.Reference(plumbing.NewBranchReferenceName("main"), true); err != nil {
			return false // nothing pushed yet
		}
		return gitOpsFileExists(t, e.remote, "workloads/"+name+".yaml")
	case fc.seedHelmOp:
		ho, _ := fleetObject(t, e.c, "HelmOp", name)
		keep, _, _ := unstructured.NestedBool(ho.Object, "spec", "keepResources")
		return keep
	case fc.overlay:
		_, ok := fleetObject(t, e.c, "Bundle", name)
		return ok
	default:
		_, ok := fleetObject(t, e.c, "HelmOp", name)
		return ok
	}
}

var floorPaths = map[string]floorCase{
	"HelmOp":          {},
	"embedded Bundle": {overlay: true},
	"GitOps publish":  {gitOps: true},
	"kind switch":     {overlay: true, seedHelmOp: true},
}

func TestMinimumSafeVersion_PersistedBeforeFirstWriteOnEveryPath(t *testing.T) {
	for name, fc := range floorPaths {
		t.Run(name, func(t *testing.T) {
			fail := fc
			fail.failFloor = true
			e := newFloorEnv(t, fail)
			if _, err := e.reconcile(); err == nil {
				t.Fatal("want the injected status failure")
			}
			if e.rendered(t, fail) {
				t.Fatal("rendered although the floor could not be persisted")
			}

			e = newFloorEnv(t, fc)
			_, _ = e.reconcile()
			if !e.rendered(t, fc) {
				t.Fatalf("did not render; Ready = %+v", meta.FindStatusCondition(e.w.Status.Conditions, conditionTypeReady))
			}
			if got := e.storedFloor(t); got != "mini@1.0.0" {
				t.Fatalf("stored minimumSafeVersion = %q, want mini@1.0.0", got)
			}
		})
	}
}

func TestMinimumSafeVersion_PreRenderRefusalLeavesFloorUnset(t *testing.T) {
	for name, fc := range map[string]floorCase{
		"InvalidDependsOn": {mutate: func(_ *aiplatformv1alpha1.AIWorkload, bp *aiplatformv1alpha1.Blueprint) {
			bp.Spec.Components[0].DependsOn = []string{"missing"}
		}},
		"LockedValueChanged": {mutate: func(w *aiplatformv1alpha1.AIWorkload, bp *aiplatformv1alpha1.Blueprint) {
			bp.Spec.Components[0].Values = rawValues(`{"global":{"domain":"aif:auto-domain:app"}}`)
			w.Spec.ComponentValues = []aiplatformv1alpha1.ComponentValueOverride{{ComponentName: "app", Values: rawValues(`{"global":{"domain":"edited.example.com"}}`)}}
			w.Status.ResolvedValues = []aiplatformv1alpha1.ResolvedValue{{ComponentName: "app", Path: "global.domain", Value: "app.10.0.0.12.sslip.io", Source: "auto", ClusterID: "local"}}
		}},
		"ChartTooLarge": {overlay: true, puller: func(context.Context, infrahelm.ChartArchiveRequest) ([]byte, error) {
			return nil, infrahelm.ErrArchiveTooLarge
		}},
	} {
		t.Run(name, func(t *testing.T) {
			e := newFloorEnv(t, fc)
			_, _ = e.reconcile()
			cond := meta.FindStatusCondition(e.w.Status.Conditions, conditionTypeReady)
			if cond == nil || cond.Status != metav1.ConditionFalse {
				t.Fatalf("want a refusal, Ready = %+v", cond)
			}
			if e.w.Status.MinimumSafeVersion != "" || e.storedFloor(t) != "" {
				t.Fatalf("refused render raised the floor to %q (stored %q)", e.w.Status.MinimumSafeVersion, e.storedFloor(t))
			}
		})
	}
}

func TestMinimumSafeVersion_RollbackAllowedLeavesFloorUnset(t *testing.T) {
	e := newFloorEnv(t, floorCase{mutate: func(_ *aiplatformv1alpha1.AIWorkload, bp *aiplatformv1alpha1.Blueprint) { bp.Spec.Rollback = "" }})
	if _, err := e.reconcile(); err != nil {
		t.Fatal(err)
	}
	if !e.rendered(t, floorCase{}) || e.storedFloor(t) != "" {
		t.Fatalf("want rendered with no floor, stored %q", e.storedFloor(t))
	}
}

func TestMinimumSafeVersion_PrunedRefusesWithoutRepeatedWrites(t *testing.T) {
	fc := floorCase{pruneFloor: true}
	e := newFloorEnv(t, fc)
	res, err := e.reconcile()
	if err != nil || res.RequeueAfter != time.Minute {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if cond := meta.FindStatusCondition(e.w.Status.Conditions, conditionTypeReady); cond == nil || cond.Reason != reasonCRDOutdated {
		t.Fatalf("Ready = %+v, want %s", cond, reasonCRDOutdated)
	}
	if e.rendered(t, fc) {
		t.Fatal("rendered without a persisted minimumSafeVersion")
	}
	e.statusWrite = 0
	if _, err := e.reconcile(); err != nil {
		t.Fatal(err)
	}
	if e.statusWrite != 0 || e.rendered(t, fc) {
		t.Fatalf("known-pruned pass: %d status writes, rendered=%v; want a dry-run probe only", e.statusWrite, e.rendered(t, fc))
	}
}

func TestRaiseMinimumSafeVersion_NeverLowersAndFollowsFamily(t *testing.T) {
	for _, tc := range []struct{ stored, want string }{
		{"mini@3.0.0", "mini@3.0.0"},
		{"3.0.0", "3.0.0"}, // bare floor from an earlier build
		{"other@3.0.0", "mini@1.0.0"},
		{"mini@0.9.0", "mini@1.0.0"},
	} {
		e := newFloorEnv(t, floorCase{mutate: func(w *aiplatformv1alpha1.AIWorkload, _ *aiplatformv1alpha1.Blueprint) {
			w.Status.MinimumSafeVersion = tc.stored
		}})
		e.w.Status.MinimumSafeVersion = tc.stored
		if err := e.r.raiseMinimumSafeVersion(context.Background(), e.w); err != nil {
			t.Fatal(err)
		}
		if e.w.Status.MinimumSafeVersion != tc.want {
			t.Errorf("stored %q: minimumSafeVersion = %q, want %q", tc.stored, e.w.Status.MinimumSafeVersion, tc.want)
		}
	}
}

// v1 certified, rollback-disabled v2 started rendering (floor recorded), then
// the spec is edited back to v1 directly: refused even with v2's Blueprint gone.
func TestReconcileBlueprintStatus_SpecBelowMinimumSafeVersionRefused(t *testing.T) {
	e := newFloorEnv(t, floorCase{mutate: func(w *aiplatformv1alpha1.AIWorkload, bp *aiplatformv1alpha1.Blueprint) {
		bp.Spec.Rollback = ""
		w.Status.DeployedSource = &aiplatformv1alpha1.DeployedSourceSnapshot{Version: "1.0.0"}
		w.Status.MinimumSafeVersion = "mini@2.0.0"
	}})
	if _, err := e.reconcile(); err != nil {
		t.Fatal(err)
	}
	cond := meta.FindStatusCondition(e.w.Status.Conditions, conditionTypeReady)
	if cond == nil || cond.Reason != reasonRollbackDisabled || !strings.Contains(cond.Message, "upgrade back to 2.0.0") {
		t.Fatalf("Ready = %+v, want RollbackDisabled pointing at 2.0.0", cond)
	}
	if e.rendered(t, floorCase{}) {
		t.Fatal("rendered a version below minimumSafeVersion")
	}
}

func TestTriggers_ReturnToCertifiedV1(t *testing.T) {
	for _, tc := range []struct {
		name        string
		minimumSafe string
		bps         []*aiplatformv1alpha1.Blueprint
		trigger     string
		wantBlocked bool
	}{
		{"floor set, v2 disabled, upgrade", "rag@2.0.0", []*aiplatformv1alpha1.Blueprint{ragBP("2.0.0", aiplatformv1alpha1.RollbackDisabled), ragBP("1.0.0", "")}, upgradeRequestAnnotation, true},
		{"floor set, v2 deleted, upgrade", "rag@2.0.0", []*aiplatformv1alpha1.Blueprint{ragBP("1.0.0", "")}, upgradeRequestAnnotation, true},
		{"floor set, v2 deleted, rollback", "rag@2.0.0", []*aiplatformv1alpha1.Blueprint{ragBP("1.0.0", "")}, rollbackRequestAnnotation, true},
		{"floor of another family, upgrade", "other@2.0.0", []*aiplatformv1alpha1.Blueprint{ragBP("1.0.0", "")}, upgradeRequestAnnotation, false},
		// v2 never rendered: the spec version alone does not block the upgrade path.
		{"no floor, v2 disabled, upgrade", "", []*aiplatformv1alpha1.Blueprint{ragBP("2.0.0", aiplatformv1alpha1.RollbackDisabled), ragBP("1.0.0", "")}, upgradeRequestAnnotation, false},
		{"no floor, typo spec, upgrade", "", []*aiplatformv1alpha1.Blueprint{ragBP("1.0.0", "")}, upgradeRequestAnnotation, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			w := wlWithBlueprint("2.0.0")
			w.Annotations = map[string]string{upgradeRequestAnnotation: "u1|1.0.0"}
			if tc.trigger == rollbackRequestAnnotation {
				w.Annotations = map[string]string{rollbackRequestAnnotation: "r1"}
			}
			w.Status.DeployedSource = &aiplatformv1alpha1.DeployedSourceSnapshot{Version: "1.0.0"}
			w.Status.MinimumSafeVersion = tc.minimumSafe
			objs := []client.Object{w}
			for _, bp := range tc.bps {
				objs = append(objs, bp)
			}
			cl := fakeClient(objs...)
			rec := record.NewFakeRecorder(16)
			r := &AIWorkloadReconciler{Client: cl, Scheme: cl.Scheme(), Recorder: rec}
			if _, err := r.handleTriggers(ctx, w); err != nil {
				t.Fatal(err)
			}
			got := &aiplatformv1alpha1.AIWorkload{}
			if err := cl.Get(ctx, client.ObjectKeyFromObject(w), got); err != nil {
				t.Fatal(err)
			}
			if blocked := got.Spec.Source.Blueprint.Version == "2.0.0"; blocked != tc.wantBlocked {
				t.Fatalf("version = %s, blocked = %v, want %v", got.Spec.Source.Blueprint.Version, blocked, tc.wantBlocked)
			}
			if tc.wantBlocked {
				if e := <-rec.Events; !strings.Contains(e, "Warning "+reasonRollbackDisabled) {
					t.Fatalf("event %q, want RollbackDisabled", e)
				}
			}
		})
	}
}

func miniBP(version string, policy aiplatformv1alpha1.RollbackPolicy) *aiplatformv1alpha1.Blueprint {
	bp := newBlueprintCR("application-collection")
	bp.Name = naming.BlueprintCRName("mini", version)
	bp.Spec.Version = version
	bp.Spec.Rollback = policy
	return bp
}
