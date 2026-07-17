package aiworkload

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

func TestHandleTriggers(t *testing.T) {
	ctx := context.Background()

	newWL := func(name string) *aiplatformv1alpha1.AIWorkload {
		return &aiplatformv1alpha1.AIWorkload{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec: aiplatformv1alpha1.AIWorkloadSpec{
				DisplayName:     name,
				TargetNamespace: "ai",
				DeployStrategy:  aiplatformv1alpha1.AIWorkloadDeployFleetBundle,
				TargetClusters:  []string{"local"},
				Source: aiplatformv1alpha1.AIWorkloadSource{
					SourceType: aiplatformv1alpha1.AIWorkloadSourceBlueprint,
					Blueprint:  &aiplatformv1alpha1.BlueprintSource{Name: "rag", Version: "1.0.0"},
				},
			},
		}
	}

	t.Run("processes an upgrade-request into spec + journal atomically, once", func(t *testing.T) {
		w := newWL("trig-up")
		cl := fakeClient(w)
		r := &AIWorkloadReconciler{Client: cl, Scheme: cl.Scheme(), Recorder: record.NewFakeRecorder(16)}

		w.Annotations = map[string]string{upgradeRequestAnnotation: "n1|2.0.0"}
		if err := cl.Update(ctx, w); err != nil {
			t.Fatalf("failed to add upgrade annotation: %v", err)
		}

		handled, err := r.handleTriggers(ctx, w)
		if err != nil {
			t.Fatalf("handleTriggers failed: %v", err)
		}
		if !handled {
			t.Fatal("expected handled=true for upgrade-request")
		}

		// Re-fetch to see persisted changes
		got := &aiplatformv1alpha1.AIWorkload{}
		if err := cl.Get(ctx, types.NamespacedName{Name: w.Name, Namespace: w.Namespace}, got); err != nil {
			t.Fatalf("failed to re-fetch workload: %v", err)
		}

		if got.Spec.Source.Blueprint.Version != "2.0.0" {
			t.Errorf("expected version 2.0.0, got %s", got.Spec.Source.Blueprint.Version)
		}
		if _, ok := got.Annotations[upgradeRequestAnnotation]; ok {
			t.Error("upgrade-request annotation should be removed")
		}

		op, ok := readJournal(got)
		if !ok {
			t.Fatal("expected operation journal to exist")
		}
		if op.Type != aiplatformv1alpha1.OperationTypeUpgrade {
			t.Errorf("expected Upgrade operation, got %s", op.Type)
		}
		if op.State != aiplatformv1alpha1.OperationStateInProgress {
			t.Errorf("expected InProgress state, got %s", op.State)
		}

		// Re-running with the same handled nonce is a no-op
		handled2, err := r.handleTriggers(ctx, got)
		if err != nil {
			t.Fatalf("handleTriggers second call failed: %v", err)
		}
		if handled2 {
			t.Fatal("expected handled=false on second call (idempotent)")
		}
	})

	t.Run("skips rollback when there is no deployedSource", func(t *testing.T) {
		w := newWL("trig-rb")
		cl := fakeClient(w)
		r := &AIWorkloadReconciler{Client: cl, Scheme: cl.Scheme(), Recorder: record.NewFakeRecorder(16)}

		w.Annotations = map[string]string{rollbackRequestAnnotation: "r1"}
		if err := cl.Update(ctx, w); err != nil {
			t.Fatalf("failed to add rollback annotation: %v", err)
		}

		_, err := r.handleTriggers(ctx, w)
		if err != nil {
			t.Fatalf("handleTriggers failed: %v", err)
		}

		got := &aiplatformv1alpha1.AIWorkload{}
		if err := cl.Get(ctx, types.NamespacedName{Name: w.Name, Namespace: w.Namespace}, got); err != nil {
			t.Fatalf("failed to re-fetch workload: %v", err)
		}

		if _, ok := got.Annotations[rollbackRequestAnnotation]; ok {
			t.Error("rollback-request annotation should be removed")
		}

		_, ok := readJournal(got)
		if ok {
			t.Error("journal should not exist for skipped rollback")
		}
	})
}
