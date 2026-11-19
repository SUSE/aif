package aiworkload

import (
	"context"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
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
	for _, reason := range []string{reasonRollbackDisabled, reasonLockedValueChanged, reasonInvalidValuesFrom} {
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
