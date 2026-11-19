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
	stderrors "errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	apixv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

type fakeFacts struct {
	ip, class   string
	err         error
	ipCalls     int
	classCalls  int
	lastCluster string
}

func (f *fakeFacts) IngressIP(_ context.Context, clusterID string) (string, error) {
	f.ipCalls++
	f.lastCluster = clusterID
	return f.ip, f.err
}

func (f *fakeFacts) DefaultIngressClass(_ context.Context, clusterID string) (string, error) {
	f.classCalls++
	f.lastCluster = clusterID
	return f.class, f.err
}

func rawValues(s string) *apixv1.JSON { return &apixv1.JSON{Raw: []byte(s)} }

func autoWorkload(targets ...string) *aiplatformv1alpha1.AIWorkload {
	return &aiplatformv1alpha1.AIWorkload{Spec: aiplatformv1alpha1.AIWorkloadSpec{TargetClusters: targets}}
}

func domainComponent() aiplatformv1alpha1.BlueprintComponent {
	return aiplatformv1alpha1.BlueprintComponent{ChartName: "control-plane", Values: rawValues(`{"global":{"domain":"aif:auto-domain:runai"}}`)}
}

func TestPinAutoValues_AutoDomain(t *testing.T) {
	facts := &fakeFacts{ip: "10.0.0.12"}
	r := &AIWorkloadReconciler{ClusterFacts: facts}
	w := autoWorkload("local")
	changed, err := r.pinAutoValues(context.Background(), w, []aiplatformv1alpha1.BlueprintComponent{domainComponent()})
	if err != nil || !changed {
		t.Fatalf("pinAutoValues = %v, %v", changed, err)
	}
	want := []aiplatformv1alpha1.ResolvedValue{{ComponentName: "control-plane", Path: "global.domain", Value: "runai.10.0.0.12.sslip.io", Source: "auto", ClusterID: "local"}}
	if !reflect.DeepEqual(w.Status.ResolvedValues, want) {
		t.Fatalf("ResolvedValues = %+v, want %+v", w.Status.ResolvedValues, want)
	}
	if facts.lastCluster != "local" {
		t.Errorf("facts queried for %q, want local", facts.lastCluster)
	}
}

func TestPinAutoValues_IngressClassAndEmptyTargetsMeansLocal(t *testing.T) {
	facts := &fakeFacts{ip: "10.0.0.12", class: "nginx"}
	r := &AIWorkloadReconciler{ClusterFacts: facts}
	w := autoWorkload()
	comps := []aiplatformv1alpha1.BlueprintComponent{
		{ChartName: "a", Values: rawValues(`{"ingress":{"className":"aif:auto-ingress-class","host":"aif:auto-domain:a"}}`)},
		{ChartName: "b", Values: rawValues(`{"host":"aif:auto-domain:b"}`)},
	}
	if _, err := r.pinAutoValues(context.Background(), w, comps); err != nil {
		t.Fatal(err)
	}
	want := []aiplatformv1alpha1.ResolvedValue{
		{ComponentName: "a", Path: "ingress.className", Value: "nginx", Source: "auto", ClusterID: "local"},
		{ComponentName: "a", Path: "ingress.host", Value: "a.10.0.0.12.sslip.io", Source: "auto", ClusterID: "local"},
		{ComponentName: "b", Path: "host", Value: "b.10.0.0.12.sslip.io", Source: "auto", ClusterID: "local"},
	}
	if !reflect.DeepEqual(w.Status.ResolvedValues, want) {
		t.Fatalf("ResolvedValues = %+v, want %+v", w.Status.ResolvedValues, want)
	}
	if facts.ipCalls != 1 || facts.lastCluster != "local" {
		t.Errorf("IngressIP calls = %d (want 1, cached), cluster %q", facts.ipCalls, facts.lastCluster)
	}
}

func TestPinAutoValues_UserOverrideIsPinnedAsUser(t *testing.T) {
	facts := &fakeFacts{ip: "10.0.0.12"}
	r := &AIWorkloadReconciler{ClusterFacts: facts}
	w := autoWorkload("local", "c-abc")
	w.Spec.ComponentValues = []aiplatformv1alpha1.ComponentValueOverride{
		{ComponentName: "control-plane", Values: rawValues(`{"global":{"domain":"runai.example.com"}}`)},
	}
	changed, err := r.pinAutoValues(context.Background(), w, []aiplatformv1alpha1.BlueprintComponent{domainComponent()})
	if err != nil || !changed {
		t.Fatalf("pinAutoValues = %v, %v", changed, err)
	}
	want := []aiplatformv1alpha1.ResolvedValue{{ComponentName: "control-plane", Path: "global.domain", Value: "runai.example.com", Source: "user"}}
	if !reflect.DeepEqual(w.Status.ResolvedValues, want) {
		t.Fatalf("ResolvedValues = %+v, want %+v", w.Status.ResolvedValues, want)
	}
	if facts.ipCalls != 0 {
		t.Errorf("IngressIP called %d times for a user-set value", facts.ipCalls)
	}

	w.Spec.ComponentValues[0].Values = rawValues(`{"global":{"domain":"runai2.example.com"}}`)
	changed, err = r.pinAutoValues(context.Background(), w, []aiplatformv1alpha1.BlueprintComponent{domainComponent()})
	if err != nil || changed || w.Status.ResolvedValues[0].Value != "runai.example.com" {
		t.Fatalf("edited override must not re-pin: changed=%v err=%v pins=%+v", changed, err, w.Status.ResolvedValues)
	}
}

func TestPinAutoValues_ExistingPinNotRecomputed(t *testing.T) {
	facts := &fakeFacts{ip: "10.9.9.9"}
	r := &AIWorkloadReconciler{ClusterFacts: facts}
	w := autoWorkload("local")
	pin := aiplatformv1alpha1.ResolvedValue{ComponentName: "control-plane", Path: "global.domain", Value: "runai.10.0.0.12.sslip.io", Source: "auto", ClusterID: "local"}
	w.Status.ResolvedValues = []aiplatformv1alpha1.ResolvedValue{pin}
	changed, err := r.pinAutoValues(context.Background(), w, []aiplatformv1alpha1.BlueprintComponent{domainComponent()})
	if err != nil || changed {
		t.Fatalf("pinAutoValues = %v, %v; want unchanged", changed, err)
	}
	if !reflect.DeepEqual(w.Status.ResolvedValues, []aiplatformv1alpha1.ResolvedValue{pin}) || facts.ipCalls != 0 {
		t.Fatalf("pin recomputed: %+v (ipCalls %d)", w.Status.ResolvedValues, facts.ipCalls)
	}
}

func TestPinAutoValues_LegacyPinAdoptsTargetWithoutResolving(t *testing.T) {
	facts := &fakeFacts{ip: "10.9.9.9"}
	r := &AIWorkloadReconciler{ClusterFacts: facts}
	w := autoWorkload("c-abc")
	w.Status.ResolvedValues = []aiplatformv1alpha1.ResolvedValue{{ComponentName: "control-plane", Path: "global.domain", Value: "runai.10.0.0.12.sslip.io", Source: "auto"}}
	changed, err := r.pinAutoValues(context.Background(), w, []aiplatformv1alpha1.BlueprintComponent{domainComponent()})
	if err != nil || !changed {
		t.Fatalf("pinAutoValues = %v, %v; want adopted", changed, err)
	}
	want := []aiplatformv1alpha1.ResolvedValue{{ComponentName: "control-plane", Path: "global.domain", Value: "runai.10.0.0.12.sslip.io", Source: "auto", ClusterID: "c-abc"}}
	if !reflect.DeepEqual(w.Status.ResolvedValues, want) || facts.ipCalls != 0 {
		t.Fatalf("ResolvedValues = %+v (ipCalls %d), want %+v", w.Status.ResolvedValues, facts.ipCalls, want)
	}
}

func TestPinAutoValues_RetargetReresolvesAutoPinsOnly(t *testing.T) {
	facts := &fakeFacts{ip: "10.9.9.9", class: "traefik"}
	rec := record.NewFakeRecorder(8)
	r := &AIWorkloadReconciler{ClusterFacts: facts, Recorder: rec}
	w := autoWorkload("c-new")
	w.Spec.ComponentValues = []aiplatformv1alpha1.ComponentValueOverride{
		{ComponentName: "control-plane", Values: rawValues(`{"ingress":{"className":"nginx"}}`)},
	}
	c := aiplatformv1alpha1.BlueprintComponent{ChartName: "control-plane", Values: rawValues(`{"global":{"domain":"aif:auto-domain:runai"},"ingress":{"className":"aif:auto-ingress-class"}}`)}
	userPin := aiplatformv1alpha1.ResolvedValue{ComponentName: "control-plane", Path: "ingress.className", Value: "nginx", Source: "user"}
	w.Status.ResolvedValues = []aiplatformv1alpha1.ResolvedValue{
		{ComponentName: "control-plane", Path: "global.domain", Value: "runai.10.0.0.12.sslip.io", Source: "auto", ClusterID: "c-old"},
		userPin,
	}
	changed, err := r.pinAutoValues(context.Background(), w, []aiplatformv1alpha1.BlueprintComponent{c})
	if err != nil || !changed {
		t.Fatalf("pinAutoValues = %v, %v", changed, err)
	}
	want := []aiplatformv1alpha1.ResolvedValue{
		{ComponentName: "control-plane", Path: "global.domain", Value: "runai.10.9.9.9.sslip.io", Source: "auto", ClusterID: "c-new"},
		userPin,
	}
	if !reflect.DeepEqual(w.Status.ResolvedValues, want) {
		t.Fatalf("ResolvedValues = %+v, want %+v", w.Status.ResolvedValues, want)
	}
	if facts.lastCluster != "c-new" || facts.classCalls != 0 {
		t.Errorf("facts cluster %q, classCalls %d", facts.lastCluster, facts.classCalls)
	}
	if e := <-rec.Events; !strings.Contains(e, "AutoValueReresolved") || !strings.Contains(e, "c-old") {
		t.Errorf("unexpected event %q", e)
	}
}

func TestPinAutoValues_ExistingPinsKeepRenderingOnMultipleTargets(t *testing.T) {
	facts := &fakeFacts{ip: "10.9.9.9"}
	rec := record.NewFakeRecorder(8)
	r := &AIWorkloadReconciler{ClusterFacts: facts, Recorder: rec}
	w := autoWorkload("local", "c-abc")
	pin := aiplatformv1alpha1.ResolvedValue{ComponentName: "control-plane", Path: "global.domain", Value: "runai.10.0.0.12.sslip.io", Source: "auto"}
	w.Status.ResolvedValues = []aiplatformv1alpha1.ResolvedValue{pin}
	for pass := 0; pass < 2; pass++ {
		changed, err := r.pinAutoValues(context.Background(), w, []aiplatformv1alpha1.BlueprintComponent{domainComponent()})
		if err != nil || changed {
			t.Fatalf("pass %d: pinAutoValues = %v, %v; want unchanged", pass, changed, err)
		}
	}
	if !reflect.DeepEqual(w.Status.ResolvedValues, []aiplatformv1alpha1.ResolvedValue{pin}) || facts.ipCalls != 0 {
		t.Fatalf("pin changed: %+v (ipCalls %d)", w.Status.ResolvedValues, facts.ipCalls)
	}
	got, err := resolveComponentValues(w, domainComponent())
	if err != nil || got["global"].(map[string]any)["domain"] != pin.Value {
		t.Fatalf("render = %v, %v", got, err)
	}
	cond := meta.FindStatusCondition(w.Status.Conditions, conditionTypeAutoValueTarget)
	if cond == nil || cond.Reason != reasonAutoValueMultipleTargets || !strings.Contains(cond.Message, "control-plane global.domain") {
		t.Fatalf("AutoValueTarget condition = %+v", cond)
	}
	if e := <-rec.Events; !strings.Contains(e, "Warning "+reasonAutoValueMultipleTargets) {
		t.Errorf("unexpected event %q", e)
	}
	select {
	case e := <-rec.Events:
		t.Errorf("warning repeated: %q", e)
	default:
	}

	w.Spec.TargetClusters = []string{"local"}
	if _, err := r.pinAutoValues(context.Background(), w, []aiplatformv1alpha1.BlueprintComponent{domainComponent()}); err != nil {
		t.Fatal(err)
	}
	if meta.FindStatusCondition(w.Status.Conditions, conditionTypeAutoValueTarget) != nil {
		t.Error("AutoValueTarget condition kept after returning to a single target")
	}
}

func pendingSince(w *aiplatformv1alpha1.AIWorkload, clusterID string, ago time.Duration) {
	w.Status.Conditions = []metav1.Condition{{
		Type: conditionTypeReady, Status: metav1.ConditionFalse, Reason: reasonAutoValuePending,
		Message:            "Waiting" + pendingClusterTag(clusterID) + " lb pending",
		LastTransitionTime: metav1.NewTime(time.Now().Add(-ago)),
	}}
}

func TestPinAutoValues_PendingLoadBalancerFallsBackAfterTimeout(t *testing.T) {
	pending := &ingressIPPendingError{Service: "kube-system/lb", FallbackIP: "1.2.3.4"}

	r := &AIWorkloadReconciler{ClusterFacts: &fakeFacts{err: pending}}
	w := autoWorkload("local")
	pendingSince(w, "local", time.Minute)
	var ave *autoValueError
	if _, err := r.pinAutoValues(context.Background(), w, []aiplatformv1alpha1.BlueprintComponent{domainComponent()}); !stderrors.As(err, &ave) || !ave.Pending {
		t.Fatalf("within the wait: want pending, got %v", err)
	}

	pendingSince(w, "c-old", ingressIPPendingTimeout+time.Second)
	if _, err := r.pinAutoValues(context.Background(), w, []aiplatformv1alpha1.BlueprintComponent{domainComponent()}); !stderrors.As(err, &ave) || !ave.Pending {
		t.Fatalf("a wait on another cluster must not count: want pending, got %v", err)
	}

	rec := record.NewFakeRecorder(8)
	r = &AIWorkloadReconciler{ClusterFacts: &fakeFacts{err: pending}, Recorder: rec}
	pendingSince(w, "local", ingressIPPendingTimeout+time.Second)
	if _, err := r.pinAutoValues(context.Background(), w, []aiplatformv1alpha1.BlueprintComponent{domainComponent()}); err != nil {
		t.Fatalf("after the wait: %v", err)
	}
	if got := w.Status.ResolvedValues[0].Value; got != "runai.1.2.3.4.sslip.io" {
		t.Fatalf("pinned %q, want the node IP fallback", got)
	}
	if e := <-rec.Events; !strings.Contains(e, "Warning "+reasonAutoValueNodeIPFallback) || !strings.Contains(e, "global.domain") || !strings.Contains(e, "explicitly") {
		t.Errorf("unexpected event %q", e)
	}
}

func TestMarkAutoValuePending_KeepsStartAndRunningPhase(t *testing.T) {
	w := autoWorkload("local")
	w.Status.Phase = aiplatformv1alpha1.AIWorkloadPhaseRunning
	w.Status.Conditions = []metav1.Condition{{Type: conditionTypeReady, Status: metav1.ConditionFalse, Reason: "Other",
		LastTransitionTime: metav1.NewTime(time.Now().Add(-time.Hour))}}
	markAutoValuePending(w, "Waiting"+pendingClusterTag("local"), "local")
	if d := autoValuePendingFor(w, "local"); d > time.Minute {
		t.Fatalf("wait started %s ago; entering AutoValuePending must restart it", d)
	}
	pendingSince(w, "local", 3*time.Minute)
	markAutoValuePending(w, "Still waiting"+pendingClusterTag("local"), "local")
	if d := autoValuePendingFor(w, "local"); d < 3*time.Minute {
		t.Fatalf("wait restarted (%s); it must keep its start", d)
	}
	if w.Status.Phase != aiplatformv1alpha1.AIWorkloadPhaseRunning {
		t.Errorf("phase = %s, want Running kept", w.Status.Phase)
	}
	markAutoValuePending(w, "Waiting"+pendingClusterTag("c-new"), "c-new")
	if d := autoValuePendingFor(w, "c-new"); d > time.Minute {
		t.Fatalf("retargeted wait inherited %s; it must start afresh", d)
	}
	w.Status.Phase = ""
	markAutoValuePending(w, "Waiting"+pendingClusterTag("c-new"), "c-new")
	if w.Status.Phase != aiplatformv1alpha1.AIWorkloadPhasePending {
		t.Errorf("phase = %s, want Pending", w.Status.Phase)
	}
}

func TestReconcileBlueprintStatus_PendingWithoutNodeIPKeepsWaiting(t *testing.T) {
	scheme := clusterRepoErrorScheme(t)
	w := newBlueprintWorkload(time.Now().Add(-24 * time.Hour))
	w.Status.Phase = aiplatformv1alpha1.AIWorkloadPhaseRunning
	bp := newBlueprintCR("application-collection")
	bp.Spec.Components[0].Values = rawValues(`{"global":{"domain":"aif:auto-domain:runai"}}`)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(w, bp).
		WithStatusSubresource(&aiplatformv1alpha1.AIWorkload{}).Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme, OperatorNamespace: "aif-operator",
		ClusterFacts: &fakeFacts{err: &ingressIPPendingError{Service: "kube-system/lb", FallbackErr: stderrors.New("list nodes: timeout")}}}

	if res, _, err := r.reconcileBlueprintStatus(context.Background(), w); err != nil || res.RequeueAfter != 15*time.Second {
		t.Fatalf("first pass: res=%+v err=%v", res, err)
	}
	cond := meta.FindStatusCondition(w.Status.Conditions, conditionTypeReady)
	if cond == nil || cond.Reason != reasonAutoValuePending {
		t.Fatalf("first pass: Ready = %+v", cond)
	}
	start := metav1.NewTime(time.Now().Add(-ingressIPPendingTimeout - time.Minute))
	cond.LastTransitionTime = start

	if res, _, err := r.reconcileBlueprintStatus(context.Background(), w); err != nil || res.RequeueAfter != 15*time.Second {
		t.Fatalf("second pass: res=%+v err=%v", res, err)
	}
	cond = meta.FindStatusCondition(w.Status.Conditions, conditionTypeReady)
	if cond == nil || cond.Reason != reasonAutoValuePending || !cond.LastTransitionTime.Equal(&start) ||
		!strings.Contains(cond.Message, "no node IP to fall back to") || !strings.Contains(cond.Message, "waiting for the LoadBalancer") {
		t.Fatalf("second pass: Ready = %+v; want the same wait, still pending", cond)
	}
	if w.Status.Phase != aiplatformv1alpha1.AIWorkloadPhaseRunning {
		t.Errorf("phase = %s, want Running kept", w.Status.Phase)
	}
	if len(w.Status.ResolvedValues) != 0 {
		t.Errorf("pinned while waiting: %+v", w.Status.ResolvedValues)
	}
}

func TestReconcileBlueprintStatus_UnresolvedOnRunningWorkloadIsDegraded(t *testing.T) {
	scheme := clusterRepoErrorScheme(t)
	w := newBlueprintWorkload(time.Now().Add(-24 * time.Hour))
	w.Status.Phase = aiplatformv1alpha1.AIWorkloadPhaseRunning
	w.Spec.TargetClusters = []string{"local", "c-abc"}
	bp := newBlueprintCR("application-collection")
	bp.Spec.Components[0].Values = rawValues(`{"global":{"domain":"aif:auto-domain:runai"}}`)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(w, bp).
		WithStatusSubresource(&aiplatformv1alpha1.AIWorkload{}).Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme, OperatorNamespace: "aif-operator", ClusterFacts: &fakeFacts{ip: "10.0.0.12"}}
	if _, _, err := r.reconcileBlueprintStatus(context.Background(), w); err != nil {
		t.Fatal(err)
	}
	if cond := meta.FindStatusCondition(w.Status.Conditions, conditionTypeReady); cond == nil || cond.Reason != reasonAutoValueNeedsSingleTarget {
		t.Fatalf("Ready = %+v", cond)
	}
	if w.Status.Phase != aiplatformv1alpha1.AIWorkloadPhaseDegraded {
		t.Errorf("phase = %s, want Degraded", w.Status.Phase)
	}
}

func TestPinAutoValues_IngressIPPendingWaits(t *testing.T) {
	r := &AIWorkloadReconciler{ClusterFacts: &fakeFacts{err: fmt.Errorf("%w: lb pending", errIngressIPPending)}}
	w := autoWorkload("local")
	_, err := r.pinAutoValues(context.Background(), w, []aiplatformv1alpha1.BlueprintComponent{domainComponent()})
	var ave *autoValueError
	if !stderrors.As(err, &ave) || ave.Reason != reasonAutoValuePending || !ave.Pending {
		t.Fatalf("want pending %s, got %v", reasonAutoValuePending, err)
	}
	if len(w.Status.ResolvedValues) != 0 {
		t.Fatalf("pinned while pending: %+v", w.Status.ResolvedValues)
	}
}

func TestPinAutoValues_NonStringUserValuePinnedAsJSON(t *testing.T) {
	r := &AIWorkloadReconciler{ClusterFacts: &fakeFacts{}}
	w := autoWorkload("local")
	w.Spec.ComponentValues = []aiplatformv1alpha1.ComponentValueOverride{
		{ComponentName: "control-plane", Values: rawValues(`{"global":{"domain":1000000}}`)},
	}
	if _, err := r.pinAutoValues(context.Background(), w, []aiplatformv1alpha1.BlueprintComponent{domainComponent()}); err != nil {
		t.Fatal(err)
	}
	if got := w.Status.ResolvedValues[0].Value; got != "1000000" {
		t.Fatalf("pinned %q, want 1000000", got)
	}
}

func TestPinAutoValues_MultiTargetFails(t *testing.T) {
	r := &AIWorkloadReconciler{ClusterFacts: &fakeFacts{ip: "10.0.0.12"}}
	w := autoWorkload("local", "c-abc")
	_, err := r.pinAutoValues(context.Background(), w, []aiplatformv1alpha1.BlueprintComponent{domainComponent()})
	if !stderrors.Is(err, errAutoValueUnresolved) {
		t.Fatalf("want errAutoValueUnresolved, got %v", err)
	}
	var ave *autoValueError
	if !stderrors.As(err, &ave) || ave.Reason != reasonAutoValueNeedsSingleTarget {
		t.Fatalf("want reason %s, got %+v", reasonAutoValueNeedsSingleTarget, err)
	}
	if !strings.Contains(err.Error(), "set it explicitly") {
		t.Errorf("message %q lacks \"set it explicitly\"", err.Error())
	}
	if len(w.Status.ResolvedValues) != 0 {
		t.Errorf("unexpected pins: %+v", w.Status.ResolvedValues)
	}
}

func TestPinAutoValues_FactsErrorSurfaces(t *testing.T) {
	r := &AIWorkloadReconciler{ClusterFacts: &fakeFacts{err: stderrors.New("no nodes visible")}}
	w := autoWorkload("local")
	_, err := r.pinAutoValues(context.Background(), w, []aiplatformv1alpha1.BlueprintComponent{domainComponent()})
	var ave *autoValueError
	if !stderrors.As(err, &ave) || ave.Reason != reasonAutoValueUnresolved || !stderrors.Is(err, errAutoValueUnresolved) {
		t.Fatalf("want %s autoValueError, got %v", reasonAutoValueUnresolved, err)
	}
	for _, s := range []string{"control-plane", "global.domain", "no nodes visible"} {
		if !strings.Contains(ave.Message, s) {
			t.Errorf("message %q lacks %q", ave.Message, s)
		}
	}
}

func TestPinAutoValues_UpgradeAddsAndDropsPaths(t *testing.T) {
	r := &AIWorkloadReconciler{ClusterFacts: &fakeFacts{class: "traefik"}}
	w := autoWorkload("local")
	old := aiplatformv1alpha1.ResolvedValue{ComponentName: "control-plane", Path: "global.domain", Value: "runai.10.0.0.12.sslip.io", Source: "auto"}
	w.Status.ResolvedValues = []aiplatformv1alpha1.ResolvedValue{old}
	c := aiplatformv1alpha1.BlueprintComponent{ChartName: "control-plane", Values: rawValues(`{"global":{"ingress":{"ingressClass":"aif:auto-ingress-class"}}}`)}
	changed, err := r.pinAutoValues(context.Background(), w, []aiplatformv1alpha1.BlueprintComponent{c})
	if err != nil || !changed {
		t.Fatalf("pinAutoValues = %v, %v", changed, err)
	}
	want := []aiplatformv1alpha1.ResolvedValue{
		old,
		{ComponentName: "control-plane", Path: "global.ingress.ingressClass", Value: "traefik", Source: "auto", ClusterID: "local"},
	}
	if !reflect.DeepEqual(w.Status.ResolvedValues, want) {
		t.Fatalf("ResolvedValues = %+v, want %+v", w.Status.ResolvedValues, want)
	}
}

func TestPinAutoValues_ExcludedComponentSkipped(t *testing.T) {
	facts := &fakeFacts{err: stderrors.New("must not be called")}
	r := &AIWorkloadReconciler{ClusterFacts: facts}
	w := autoWorkload("local", "c-abc")
	disabled := false
	w.Spec.ComponentValues = []aiplatformv1alpha1.ComponentValueOverride{{ComponentName: "control-plane", Enabled: &disabled}}
	changed, err := r.pinAutoValues(context.Background(), w, []aiplatformv1alpha1.BlueprintComponent{domainComponent()})
	if err != nil || changed || facts.ipCalls != 0 {
		t.Fatalf("pinAutoValues = %v, %v (ipCalls %d); want skipped", changed, err, facts.ipCalls)
	}
}

func TestResolveComponentValues_Substitutes(t *testing.T) {
	w := autoWorkload("local")
	w.Status.ResolvedValues = []aiplatformv1alpha1.ResolvedValue{
		{ComponentName: "control-plane", Path: "global.domain", Value: "runai.10.0.0.12.sslip.io", Source: "auto"},
		{ComponentName: "other", Path: "global.ingress.ingressClass", Value: "wrong", Source: "auto"},
	}
	got, err := resolveComponentValues(w, domainComponent())
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"global": map[string]any{"domain": "runai.10.0.0.12.sslip.io"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}

	c := aiplatformv1alpha1.BlueprintComponent{ChartName: "control-plane", Values: rawValues(`{"global":{"domain":"aif:auto-domain:runai","ingress":{"ingressClass":"aif:auto-ingress-class"}}}`)}
	if _, err := resolveComponentValues(w, c); !stderrors.Is(err, errAutoValueUnresolved) {
		t.Fatalf("unpinned sentinel: want errAutoValueUnresolved, got %v", err)
	}
}

func TestResolveComponentValues_UnsupportedSentinelPositions(t *testing.T) {
	w := autoWorkload("local")
	for name, values := range map[string]string{
		"dotted key": `{"ingress":{"annotations":{"external-dns.alpha.kubernetes.io/hostname":"aif:auto-domain:runai"}}}`,
		"list":       `{"hosts":["aif:auto-domain:runai"]}`,
	} {
		c := aiplatformv1alpha1.BlueprintComponent{ChartName: "control-plane", Values: rawValues(values)}
		_, err := resolveComponentValues(w, c)
		if !stderrors.Is(err, errAutoValueUnresolved) || !strings.Contains(err.Error(), "control-plane") || !strings.Contains(err.Error(), "inside a list or under a key containing") {
			t.Errorf("%s: want unsupported-position errAutoValueUnresolved naming the component, got %v", name, err)
		}

		r := &AIWorkloadReconciler{ClusterFacts: &fakeFacts{ip: "10.0.0.12"}}
		_, err = r.pinAutoValues(context.Background(), w, []aiplatformv1alpha1.BlueprintComponent{c})
		var ave *autoValueError
		if !stderrors.As(err, &ave) || ave.Reason != reasonAutoValueUnresolved {
			t.Errorf("%s: pinAutoValues want %s, got %v", name, reasonAutoValueUnresolved, err)
		}
	}
}

func TestPinAutoValues_PersistsBeforeRender(t *testing.T) {
	scheme := clusterRepoErrorScheme(t)
	w := newBlueprintWorkload(time.Now())
	bp := newBlueprintCR("application-collection")
	bp.Spec.Components[0].Values = rawValues(`{"global":{"domain":"aif:auto-domain:runai"}}`)

	repo := &unstructured.Unstructured{}
	repo.SetGroupVersionKind(clusterRepoGVK)
	repo.SetName("application-collection")
	_ = unstructured.SetNestedField(repo.Object, "https://charts.example.com", "spec", "url")

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(w, bp, repo).
		WithStatusSubresource(&aiplatformv1alpha1.AIWorkload{}).Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme, OperatorNamespace: "aif-operator", ClusterFacts: &fakeFacts{ip: "10.0.0.12"}}
	ctx := context.Background()

	res, _, err := r.reconcileBlueprintStatus(ctx, w)
	if err != nil || res != (ctrl.Result{Requeue: true}) {
		t.Fatalf("first pass: res=%+v err=%v; want Requeue", res, err)
	}
	helmOpKey := types.NamespacedName{Namespace: "fleet-local", Name: blueprintBundleName(w.Name, "app")}
	helmOp := &unstructured.Unstructured{}
	helmOp.SetGroupVersionKind(helmOpGVK)
	if err := c.Get(ctx, helmOpKey, helmOp); err == nil {
		t.Fatal("HelmOp created before the pin was persisted")
	}
	var stored aiplatformv1alpha1.AIWorkload
	if err := c.Get(ctx, types.NamespacedName{Namespace: w.Namespace, Name: w.Name}, &stored); err != nil {
		t.Fatal(err)
	}
	if len(stored.Status.ResolvedValues) != 1 || stored.Status.ResolvedValues[0].Value != "runai.10.0.0.12.sslip.io" {
		t.Fatalf("persisted ResolvedValues = %+v", stored.Status.ResolvedValues)
	}

	if _, _, err := r.reconcileBlueprintStatus(ctx, &stored); err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if err := c.Get(ctx, helmOpKey, helmOp); err != nil {
		t.Fatalf("HelmOp after second pass: %v", err)
	}
	domain, _, _ := unstructured.NestedString(helmOp.Object, "spec", "helm", "values", "global", "domain")
	if domain != "runai.10.0.0.12.sslip.io" {
		t.Fatalf("rendered domain = %q", domain)
	}
	if stored.Status.Customized {
		t.Error("a substituted automatic value must not mark the workload customized")
	}
}

func TestReconcileBlueprintStatus_AutoValueNeedsSingleTarget(t *testing.T) {
	scheme := clusterRepoErrorScheme(t)
	w := newBlueprintWorkload(time.Now().Add(-24 * time.Hour))
	w.Spec.TargetClusters = []string{"local", "c-abc"}
	bp := newBlueprintCR("application-collection")
	bp.Spec.Components[0].Values = rawValues(`{"global":{"domain":"aif:auto-domain:runai"}}`)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(w, bp).
		WithStatusSubresource(&aiplatformv1alpha1.AIWorkload{}).Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme, OperatorNamespace: "aif-operator", ClusterFacts: &fakeFacts{ip: "10.0.0.12"}}

	res, _, err := r.reconcileBlueprintStatus(context.Background(), w)
	if err != nil || res.RequeueAfter != time.Minute {
		t.Fatalf("res=%+v err=%v; want RequeueAfter 1m", res, err)
	}
	cond := meta.FindStatusCondition(w.Status.Conditions, conditionTypeReady)
	if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason != reasonAutoValueNeedsSingleTarget {
		t.Fatalf("Ready condition = %+v", cond)
	}
	if w.Status.Phase != aiplatformv1alpha1.AIWorkloadPhaseFailed {
		t.Errorf("phase = %v, want Failed", w.Status.Phase)
	}
}

func TestReconcileBlueprintStatus_AutoValuePendingRequeues(t *testing.T) {
	scheme := clusterRepoErrorScheme(t)
	w := newBlueprintWorkload(time.Now().Add(-24 * time.Hour))
	bp := newBlueprintCR("application-collection")
	bp.Spec.Components[0].Values = rawValues(`{"global":{"domain":"aif:auto-domain:runai"}}`)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(w, bp).
		WithStatusSubresource(&aiplatformv1alpha1.AIWorkload{}).Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme, OperatorNamespace: "aif-operator",
		ClusterFacts: &fakeFacts{err: fmt.Errorf("%w: lb pending", errIngressIPPending)}}

	res, _, err := r.reconcileBlueprintStatus(context.Background(), w)
	if err != nil || res.RequeueAfter != 15*time.Second {
		t.Fatalf("res=%+v err=%v; want RequeueAfter 15s", res, err)
	}
	cond := meta.FindStatusCondition(w.Status.Conditions, conditionTypeReady)
	if cond == nil || cond.Reason != reasonAutoValuePending || w.Status.Phase != aiplatformv1alpha1.AIWorkloadPhasePending {
		t.Fatalf("Ready condition = %+v, phase %s", cond, w.Status.Phase)
	}
}

func TestReconcileBlueprintStatus_LockedValueChangeRefusesRender(t *testing.T) {
	scheme := clusterRepoErrorScheme(t)
	w := newBlueprintWorkload(time.Now().Add(-24 * time.Hour))
	w.Spec.ComponentValues = []aiplatformv1alpha1.ComponentValueOverride{
		{ComponentName: "app", Values: rawValues(`{"global":{"domain":"edited.example.com"}}`)},
	}
	w.Status.ResolvedValues = []aiplatformv1alpha1.ResolvedValue{
		{ComponentName: "app", Path: "global.domain", Value: "runai.10.0.0.12.sslip.io", Source: "auto", ClusterID: "local"},
	}
	bp := newBlueprintCR("application-collection")
	bp.Spec.Components[0].Values = rawValues(`{"global":{"domain":"aif:auto-domain:runai"}}`)
	repo := &unstructured.Unstructured{}
	repo.SetGroupVersionKind(clusterRepoGVK)
	repo.SetName("application-collection")
	_ = unstructured.SetNestedField(repo.Object, "https://charts.example.com", "spec", "url")
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(w, bp, repo).
		WithStatusSubresource(&aiplatformv1alpha1.AIWorkload{}).Build()
	rec := record.NewFakeRecorder(8)
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme, OperatorNamespace: "aif-operator", ClusterFacts: &fakeFacts{ip: "10.9.9.9"}, Recorder: rec}

	res, _, err := r.reconcileBlueprintStatus(context.Background(), w)
	if err != nil || !res.IsZero() {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	cond := meta.FindStatusCondition(w.Status.Conditions, conditionTypeReady)
	if cond == nil || cond.Reason != reasonLockedValueChanged {
		t.Fatalf("Ready condition = %+v", cond)
	}
	for _, s := range []string{"app", "global.domain", "runai.10.0.0.12.sslip.io", "edited.example.com"} {
		if !strings.Contains(cond.Message, s) {
			t.Errorf("message %q lacks %q", cond.Message, s)
		}
	}
	if w.Status.ResolvedValues[0].Value != "runai.10.0.0.12.sslip.io" {
		t.Errorf("pin rewritten to %q", w.Status.ResolvedValues[0].Value)
	}
	helmOp := &unstructured.Unstructured{}
	helmOp.SetGroupVersionKind(helmOpGVK)
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "fleet-local", Name: blueprintBundleName(w.Name, "app")}, helmOp); err == nil {
		t.Fatal("HelmOp rendered despite a locked value change")
	}
	if e := <-rec.Events; !strings.Contains(e, "Warning "+reasonLockedValueChanged) {
		t.Errorf("unexpected event %q", e)
	}

	w.Spec.ComponentValues[0].Values = rawValues(`{"global":{"domain":"runai.10.0.0.12.sslip.io"}}`)
	if _, _, err := r.reconcileBlueprintStatus(context.Background(), w); err != nil {
		t.Fatal(err)
	}
	if cond := meta.FindStatusCondition(w.Status.Conditions, conditionTypeReady); cond != nil && cond.Reason == reasonLockedValueChanged {
		t.Fatalf("matching override still refused: %+v", cond)
	}
}
