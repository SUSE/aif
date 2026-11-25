/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package aiworkload

import (
	"context"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	infrahelm "github.com/SUSE/aif-operator/internal/infra/helm"
)

func TestSwitchRequeueAfter(t *testing.T) {
	for _, tc := range []struct {
		elapsed, want time.Duration
	}{
		{0, switchRequeue},
		{59 * time.Second, switchRequeue},
		{time.Minute, 15 * time.Second},
		{5*time.Minute - time.Second, 15 * time.Second},
		{5 * time.Minute, 30 * time.Second},
		{switchStallAfter - time.Second, 30 * time.Second},
		{switchStallAfter, time.Minute},
		{24 * time.Hour, time.Minute},
	} {
		if got := switchRequeueAfter(tc.elapsed); got != tc.want {
			t.Errorf("switchRequeueAfter(%v) = %v, want %v", tc.elapsed, got, tc.want)
		}
	}
}

// readySince seeds Ready=False/reason as if it had been set ago.
func readySince(w *aiplatformv1alpha1.AIWorkload, reason string, ago time.Duration) {
	w.Status.Conditions = []metav1.Condition{{
		Type: conditionTypeReady, Status: metav1.ConditionFalse, Reason: reason, Message: "m",
		LastTransitionTime: metav1.NewTime(time.Now().Add(-ago)),
	}}
}

func drainEvents(rec *record.FakeRecorder) []string {
	var out []string
	for {
		select {
		case e := <-rec.Events:
			out = append(out, e)
		default:
			return out
		}
	}
}

func TestMarkSwitching_AgeAndStall(t *testing.T) {
	const step = `Component "comp" is switching to an embedded Bundle: waiting for keepResources on cluster-fleet-local-local (ErrApplied).`
	for _, tc := range []struct {
		name        string
		seedReason  string
		ago         time.Duration
		wantReason  string
		wantRequeue time.Duration
		wantKept    bool
	}{
		{"fresh switch", "", 0, reasonSwitching, switchRequeue, false},
		{"Ready already False for another reason starts afresh", "WaitingForDependencies", time.Hour, reasonSwitching, switchRequeue, false},
		{"aging switch keeps its start", reasonSwitching, 3 * time.Minute, reasonSwitching, 15 * time.Second, true},
		{"stalls after switchStallAfter", reasonSwitching, 11 * time.Minute, reasonSwitchStalled, time.Minute, true},
		{"stays stalled", reasonSwitchStalled, 40 * time.Minute, reasonSwitchStalled, time.Minute, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := wlWithBlueprint("1.0.0")
			if tc.seedReason != "" {
				readySince(w, tc.seedReason, tc.ago)
			}
			seeded := meta.FindStatusCondition(w.Status.Conditions, conditionTypeReady).DeepCopy()
			rec := record.NewFakeRecorder(8)
			r := &AIWorkloadReconciler{Recorder: rec}
			if got := r.markSwitching(w, step); got != tc.wantRequeue {
				t.Fatalf("requeue = %v, want %v", got, tc.wantRequeue)
			}
			c := meta.FindStatusCondition(w.Status.Conditions, conditionTypeReady)
			if c.Reason != tc.wantReason || !strings.Contains(c.Message, step) {
				t.Fatalf("Ready = %s %q", c.Reason, c.Message)
			}
			if kept := seeded != nil && c.LastTransitionTime.Equal(&seeded.LastTransitionTime); kept != tc.wantKept {
				t.Fatalf("start kept = %v, want %v", kept, tc.wantKept)
			}
			events := drainEvents(rec)
			wantEvent := tc.wantReason == reasonSwitchStalled && tc.seedReason != reasonSwitchStalled
			if (len(events) == 1) != wantEvent || len(events) > 1 {
				t.Fatalf("events = %v, want one stall event: %v", events, wantEvent)
			}
			if tc.wantReason == reasonSwitchStalled && !strings.HasPrefix(c.Message, "The component switch has not finished after ") {
				t.Fatalf("stalled message = %q", c.Message)
			}
		})
	}
}

func TestReconcile_SwitchStallsOnceThenRecovers(t *testing.T) {
	tgz := compChartTgz(t)
	w, r, c := switchReconcileEnv(t, func(context.Context, infrahelm.ChartArchiveRequest) ([]byte, error) { return tgz, nil })
	rec := record.NewFakeRecorder(16)
	r.Recorder = rec
	readySince(w, reasonSwitching, 12*time.Minute)

	for i := 0; i < 2; i++ {
		res, _, err := r.reconcileBlueprintStatus(context.Background(), w)
		if err != nil {
			t.Fatal(err)
		}
		if res.RequeueAfter != time.Minute {
			t.Fatalf("pass %d: RequeueAfter = %v, want 1m", i, res.RequeueAfter)
		}
		ready := meta.FindStatusCondition(w.Status.Conditions, conditionTypeReady)
		if ready.Reason != reasonSwitchStalled || !strings.Contains(ready.Message, "after 12 minutes") || !strings.Contains(ready.Message, `Component "comp" is switching`) {
			t.Fatalf("pass %d: Ready = %s %q", i, ready.Reason, ready.Message)
		}
	}
	if _, ok := objExists(t, c, true, blueprintBundleName(w.Name, "other")); !ok {
		t.Fatal("a stalled switch must not stop the other components")
	}
	if ev := drainEvents(rec); len(ev) != 1 || !strings.Contains(ev[0], "Warning "+reasonSwitchStalled) {
		t.Fatalf("events = %v, want exactly one SwitchStalled warning", ev)
	}

	// Once the old HelmOp is gone the switch completes and Ready recovers.
	if ho, ok := objExists(t, c, true, blueprintBundleName(w.Name, "comp")); ok {
		if err := c.Delete(context.Background(), ho); err != nil {
			t.Fatal(err)
		}
	}
	var res ctrl.Result
	for i := 0; i < 3 && readyReason(w) != reasonReconciled; i++ {
		var err error
		if res, _, err = r.reconcileBlueprintStatus(context.Background(), w); err != nil {
			t.Fatal(err)
		}
	}
	if ready := meta.FindStatusCondition(w.Status.Conditions, conditionTypeReady); ready.Reason != reasonReconciled || res.RequeueAfter != 0 {
		t.Fatalf("after recovery Ready = %s %q, res %+v", ready.Reason, ready.Message, res)
	}
}

func TestMarkSwitching_UsesReconcilerClock(t *testing.T) {
	w := wlWithBlueprint("1.0.0")
	readySince(w, reasonSwitching, 0)
	start := meta.FindStatusCondition(w.Status.Conditions, conditionTypeReady).LastTransitionTime.Time
	r := &AIWorkloadReconciler{now: func() time.Time { return start.Add(switchStallAfter + time.Minute) }}
	r.markSwitching(w, "step")
	if c := meta.FindStatusCondition(w.Status.Conditions, conditionTypeReady); c.Reason != reasonSwitchStalled {
		t.Fatalf("Ready reason = %s, want the injected clock to age the switch", c.Reason)
	}
}
