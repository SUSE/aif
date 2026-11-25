package aiworkload

import (
	"context"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	"github.com/SUSE/aif-operator/internal/naming"
)

func wlWithBlueprint(version string) *aiplatformv1alpha1.AIWorkload {
	return &aiplatformv1alpha1.AIWorkload{
		ObjectMeta: metav1.ObjectMeta{Name: "w", Namespace: "default"},
		Spec: aiplatformv1alpha1.AIWorkloadSpec{
			TargetNamespace: "ai", DeployStrategy: aiplatformv1alpha1.AIWorkloadDeployFleetBundle,
			TargetClusters: []string{"local"},
			Source: aiplatformv1alpha1.AIWorkloadSource{
				SourceType: aiplatformv1alpha1.AIWorkloadSourceBlueprint,
				Blueprint:  &aiplatformv1alpha1.BlueprintSource{Name: "rag", Version: version},
			},
		},
	}
}

func TestProjectOperationReconstructsFromJournal(t *testing.T) {
	w := wlWithBlueprint("1.0.0")
	op := aiplatformv1alpha1.AIWorkloadOperation{Type: aiplatformv1alpha1.OperationTypeUpgrade, Nonce: "n", State: aiplatformv1alpha1.OperationStateInProgress, IntentDigest: intentDigest(w.Spec)}
	s, _ := encodeOperation(op)
	w.Annotations = map[string]string{operationAnnotation: s}
	w.Status.ActiveOperation = nil

	(&AIWorkloadReconciler{}).projectOperation(w)
	if w.Status.ActiveOperation == nil || w.Status.ActiveOperation.Nonce != "n" {
		t.Fatalf("expected activeOperation reconstructed from journal, got %+v", w.Status.ActiveOperation)
	}
}

func TestProjectOperationClearsStaleTerminal(t *testing.T) {
	w := wlWithBlueprint("2.0.0") // current intent is v2
	// A terminal op from a DIFFERENT (v1) intent, no journal present.
	w.Status.ActiveOperation = &aiplatformv1alpha1.AIWorkloadOperation{
		Type: aiplatformv1alpha1.OperationTypeUpgrade, State: aiplatformv1alpha1.OperationStateFailed,
		IntentDigest: intentDigest(wlWithBlueprint("1.0.0").Spec),
	}
	(&AIWorkloadReconciler{}).projectOperation(w)
	if w.Status.ActiveOperation != nil {
		t.Fatalf("stale terminal op (intent mismatch, no journal) should be cleared, got %+v", w.Status.ActiveOperation)
	}
}

func TestProjectOperationKeepsMatchingTerminal(t *testing.T) {
	w := wlWithBlueprint("1.0.0")
	w.Status.ActiveOperation = &aiplatformv1alpha1.AIWorkloadOperation{
		Type: aiplatformv1alpha1.OperationTypeUpgrade, State: aiplatformv1alpha1.OperationStateFailed,
		IntentDigest: intentDigest(w.Spec),
	}
	(&AIWorkloadReconciler{}).projectOperation(w)
	if w.Status.ActiveOperation == nil {
		t.Fatalf("terminal op matching current intent must be retained")
	}
}

func TestRollbackTrigger_DisabledIsSkipped(t *testing.T) {
	ctx := context.Background()
	w := wlWithBlueprint("2.0.0")
	w.Annotations = map[string]string{rollbackRequestAnnotation: "r1"}
	w.Status.DeployedSource = &aiplatformv1alpha1.DeployedSourceSnapshot{Version: "1.0.0"}
	bp := &aiplatformv1alpha1.Blueprint{
		ObjectMeta: metav1.ObjectMeta{Name: naming.BlueprintCRName("rag", "2.0.0")},
		Spec:       aiplatformv1alpha1.BlueprintSpec{DisplayName: "RAG", Version: "2.0.0", Rollback: aiplatformv1alpha1.RollbackDisabled},
	}
	cl := fakeClient(w, bp)
	rec := record.NewFakeRecorder(16)
	r := &AIWorkloadReconciler{Client: cl, Scheme: cl.Scheme(), Recorder: rec}

	if _, err := r.handleTriggers(ctx, w); err != nil {
		t.Fatalf("handleTriggers: %v", err)
	}

	got := &aiplatformv1alpha1.AIWorkload{}
	if err := cl.Get(ctx, types.NamespacedName{Name: w.Name, Namespace: w.Namespace}, got); err != nil {
		t.Fatal(err)
	}
	if got.Spec.Source.Blueprint.Version != "2.0.0" {
		t.Errorf("version changed to %s", got.Spec.Source.Blueprint.Version)
	}
	if _, ok := got.Annotations[rollbackRequestAnnotation]; ok {
		t.Error("rollback-request annotation should be removed")
	}
	if _, ok := readJournal(got); ok {
		t.Error("journal should not exist for a disabled rollback")
	}
	if !isHandled(decodeHandled(got.Annotations[handledOpsAnnotation]), handledID(aiplatformv1alpha1.OperationTypeRollback, "r1")) {
		t.Error("rollback trigger id should be recorded as handled")
	}
	select {
	case e := <-rec.Events:
		if !strings.Contains(e, "Warning RollbackDisabled") {
			t.Errorf("unexpected event %q", e)
		}
	default:
		t.Error("expected a RollbackDisabled event")
	}
}

// inFlightVersionOp journals an in-progress Upgrade/Rollback to version, requested at requestedAt.
func inFlightVersionOp(w *aiplatformv1alpha1.AIWorkload, opType, version, expectedDigest string, requestedAt time.Time) {
	op := aiplatformv1alpha1.AIWorkloadOperation{
		Type: opType, Nonce: "n", TargetVersion: version, ExpectedDigest: expectedDigest,
		RequestedAt: metav1.NewTime(requestedAt), IntentDigest: intentDigest(w.Spec),
		State: aiplatformv1alpha1.OperationStateInProgress,
	}
	s, _ := encodeOperation(op)
	metav1.SetMetaDataAnnotation(&w.ObjectMeta, operationAnnotation, s)
}

func TestReconcileOperation_VersionOpNeedsCertificationAfterRequest(t *testing.T) {
	requested := time.Now().Add(-time.Minute).Truncate(time.Second)
	cases := []struct {
		name        string
		opType      string
		certifiedAt time.Time
		wantState   string
	}{
		{"rollback certified before request stays in progress", aiplatformv1alpha1.OperationTypeRollback, requested.Add(-time.Hour), aiplatformv1alpha1.OperationStateInProgress},
		{"rollback certified after request succeeds", aiplatformv1alpha1.OperationTypeRollback, requested.Add(time.Second), aiplatformv1alpha1.OperationStateSucceeded},
		{"upgrade certified after request succeeds", aiplatformv1alpha1.OperationTypeUpgrade, requested.Add(time.Second), aiplatformv1alpha1.OperationStateSucceeded},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			w := wlWithBlueprint("1.0.0")
			w.Status.DeployedSource = &aiplatformv1alpha1.DeployedSourceSnapshot{
				Version: "1.0.0", RenderDigest: "sha256:d", CertifiedAt: metav1.NewTime(tc.certifiedAt),
			}
			inFlightVersionOp(w, tc.opType, "1.0.0", "sha256:d", requested)
			cl := fakeClient(w)
			r := &AIWorkloadReconciler{Client: cl, Scheme: cl.Scheme()}

			res, err := r.reconcileOperation(ctx, w)
			if err != nil {
				t.Fatalf("reconcileOperation: %v", err)
			}
			got := &aiplatformv1alpha1.AIWorkload{}
			if err := cl.Get(ctx, client.ObjectKeyFromObject(w), got); err != nil {
				t.Fatal(err)
			}
			if tc.wantState == aiplatformv1alpha1.OperationStateInProgress {
				if _, ok := readJournal(got); !ok || res.RequeueAfter == 0 {
					t.Fatalf("expected in-flight journal and a requeue, got journal=%v result=%+v", ok, res)
				}
				return
			}
			if _, ok := readJournal(got); ok {
				t.Fatal("terminal operation must clear the journal")
			}
			if got.Status.ActiveOperation == nil || got.Status.ActiveOperation.State != tc.wantState {
				t.Fatalf("expected %s, got %+v", tc.wantState, got.Status.ActiveOperation)
			}
		})
	}
}
