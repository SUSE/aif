package aiworkload

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	aiplatformv1alpha1 "github.com/SUSE/suse-ai-operator/api/v1alpha1"
)

// TestReconcilePullSecrets_PatchesDefaultSA verifies that when the dockerconfigjson
// Secret exists in the target namespace, the default ServiceAccount gets the secret
// merged into its .imagePullSecrets.
func TestReconcilePullSecrets_PatchesDefaultSA(t *testing.T) {
	scheme := newTestScheme(t)
	ns := "test-ns"
	secretName := "ngc-secret"

	objs := []client.Object{
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}},
		&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "default", Namespace: ns}},
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: ns},
			Type:       corev1.SecretTypeDockerConfigJson,
			Data:       map[string][]byte{corev1.DockerConfigJsonKey: []byte(`{"auths":{}}`)},
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme}

	w := &aiplatformv1alpha1.AIWorkload{
		ObjectMeta: metav1.ObjectMeta{Name: "wl", Namespace: "default"},
		Spec:       aiplatformv1alpha1.AIWorkloadSpec{TargetNamespace: ns},
	}

	settled, err := r.reconcilePullSecrets(context.Background(), w, []string{secretName})
	if err != nil {
		t.Fatalf("reconcilePullSecrets: %v", err)
	}
	if settled {
		t.Errorf("expected settled=false on first reconcile (SA was mutated), got true")
	}

	var sa corev1.ServiceAccount
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: "default"}, &sa); err != nil {
		t.Fatalf("get SA: %v", err)
	}
	if len(sa.ImagePullSecrets) != 1 || sa.ImagePullSecrets[0].Name != secretName {
		t.Errorf("expected SA.imagePullSecrets=[{Name:%q}], got %+v", secretName, sa.ImagePullSecrets)
	}
}

// newTestScheme builds a runtime.Scheme with the types tests in this package need.
func newTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := corev1.AddToScheme(s); err != nil {
		t.Fatalf("add corev1: %v", err)
	}
	if err := aiplatformv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("add aiplatform: %v", err)
	}
	return s
}

func TestMergeImagePullSecrets_AdditiveAndIdempotent(t *testing.T) {
	cases := []struct {
		name     string
		existing []corev1.LocalObjectReference
		add      []string
		want     []corev1.LocalObjectReference
		mutated  bool
	}{
		{
			name:     "empty list, add one",
			existing: nil,
			add:      []string{"ngc-secret"},
			want:     []corev1.LocalObjectReference{{Name: "ngc-secret"}},
			mutated:  true,
		},
		{
			name:     "preserve existing entry not in add list",
			existing: []corev1.LocalObjectReference{{Name: "regcred"}},
			add:      []string{"ngc-secret"},
			want:     []corev1.LocalObjectReference{{Name: "regcred"}, {Name: "ngc-secret"}},
			mutated:  true,
		},
		{
			name:     "idempotent — same entry already present",
			existing: []corev1.LocalObjectReference{{Name: "ngc-secret"}},
			add:      []string{"ngc-secret"},
			want:     []corev1.LocalObjectReference{{Name: "ngc-secret"}},
			mutated:  false,
		},
		{
			name:     "add multiple, deduplicate within add list",
			existing: nil,
			add:      []string{"a", "b", "a"},
			want:     []corev1.LocalObjectReference{{Name: "a"}, {Name: "b"}},
			mutated:  true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sa := &corev1.ServiceAccount{ImagePullSecrets: append([]corev1.LocalObjectReference{}, tc.existing...)}
			got := mergeImagePullSecrets(sa, tc.add)
			if got != tc.mutated {
				t.Errorf("mutated: got %v want %v", got, tc.mutated)
			}
			if !equalRefs(sa.ImagePullSecrets, tc.want) {
				t.Errorf("imagePullSecrets: got %+v want %+v", sa.ImagePullSecrets, tc.want)
			}
		})
	}
}

func equalRefs(a, b []corev1.LocalObjectReference) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Name != b[i].Name {
			return false
		}
	}
	return true
}

// podWithContainerWaiting builds a pod whose main container (or init container
// if init=true) is in the Waiting state with the given reason.
func podWithContainerWaiting(name string, init bool, reason string) *corev1.Pod {
	cs := corev1.ContainerStatus{
		Name:  "c",
		State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: reason}},
	}
	p := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "test-ns"}}
	if init {
		p.Status.InitContainerStatuses = []corev1.ContainerStatus{cs}
	} else {
		p.Status.ContainerStatuses = []corev1.ContainerStatus{cs}
	}
	return p
}

func TestRestartImagePullBackOffPods(t *testing.T) {
	cases := []struct {
		name      string
		pod       *corev1.Pod
		shouldDel bool
	}{
		{
			name:      "main container ImagePullBackOff is bounced",
			pod:       podWithContainerWaiting("main-ipbo", false, "ImagePullBackOff"),
			shouldDel: true,
		},
		{
			name:      "main container ErrImagePull is bounced",
			pod:       podWithContainerWaiting("main-eip", false, "ErrImagePull"),
			shouldDel: true,
		},
		{
			name:      "init container ImagePullBackOff is bounced",
			pod:       podWithContainerWaiting("init-ipbo", true, "ImagePullBackOff"),
			shouldDel: true,
		},
		{
			name:      "init container ErrImagePull is bounced",
			pod:       podWithContainerWaiting("init-eip", true, "ErrImagePull"),
			shouldDel: true,
		},
		{
			name: "Running pod is preserved",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "running", Namespace: "test-ns"},
				Status:     corev1.PodStatus{Phase: corev1.PodRunning},
			},
			shouldDel: false,
		},
		{
			name:      "main container CrashLoopBackOff is preserved",
			pod:       podWithContainerWaiting("crashloop", false, "CrashLoopBackOff"),
			shouldDel: false,
		},
	}

	scheme := newTestScheme(t)
	ns := "test-ns"
	objs := make([]client.Object, 0, len(cases))
	wantDel := 0
	wantRemain := map[string]struct{}{}
	for _, tc := range cases {
		objs = append(objs, tc.pod)
		if tc.shouldDel {
			wantDel++
		} else {
			wantRemain[tc.pod.Name] = struct{}{}
		}
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme}

	bounced, err := r.restartImagePullBackOffPods(context.Background(), ns)
	if err != nil {
		t.Fatalf("restartImagePullBackOffPods: %v", err)
	}
	if bounced != wantDel {
		t.Errorf("bounced: got %d want %d", bounced, wantDel)
	}

	var got corev1.PodList
	if err := c.List(context.Background(), &got, client.InNamespace(ns)); err != nil {
		t.Fatalf("list pods: %v", err)
	}
	gotNames := map[string]struct{}{}
	for _, p := range got.Items {
		gotNames[p.Name] = struct{}{}
	}
	for name := range wantRemain {
		if _, ok := gotNames[name]; !ok {
			t.Errorf("expected pod %q to remain but it was deleted", name)
		}
	}
	for name := range gotNames {
		if _, ok := wantRemain[name]; !ok {
			t.Errorf("expected pod %q to be deleted but it remains", name)
		}
	}

	// Also run each case as a self-named subtest so failures localize cleanly.
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, deleted := wantRemain[tc.pod.Name]
			deleted = !deleted
			if deleted != tc.shouldDel {
				t.Errorf("pod %q: deleted=%v want %v", tc.pod.Name, deleted, tc.shouldDel)
			}
		})
	}
}

func TestMergePullSecretNames(t *testing.T) {
	cases := []struct {
		name     string
		existing []string
		add      []string
		want     []string
	}{
		{"both empty", nil, nil, nil},
		{"existing empty, add one", nil, []string{"a"}, []string{"a"}},
		{"add empty, existing preserved", []string{"a"}, nil, []string{"a"}},
		{"dedup against existing", []string{"a"}, []string{"a"}, []string{"a"}},
		{"append new", []string{"a"}, []string{"b"}, []string{"a", "b"}},
		{"dedup within add list", nil, []string{"a", "b", "a"}, []string{"a", "b"}},
		{"mixed", []string{"a"}, []string{"b", "a", "c"}, []string{"a", "b", "c"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mergePullSecretNames(tc.existing, tc.add)
			if !equalStrings(got, tc.want) {
				t.Errorf("got %+v want %+v", got, tc.want)
			}
		})
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestReconcilePullSecrets_BouncesBackOffPodAndUnsettles(t *testing.T) {
	scheme := newTestScheme(t)
	ns := "test-ns"
	secretName := "ngc-secret"

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}},
		&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "default", Namespace: ns}},
		// Pod stuck in ImagePullBackOff — should be deleted, settled should be false.
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "stuck", Namespace: ns},
			Status: corev1.PodStatus{
				ContainerStatuses: []corev1.ContainerStatus{
					{Name: "c", State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff"}}},
				},
			},
		},
	).Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme}

	w := &aiplatformv1alpha1.AIWorkload{
		ObjectMeta: metav1.ObjectMeta{Name: "wl", Namespace: "default"},
		Spec:       aiplatformv1alpha1.AIWorkloadSpec{TargetNamespace: ns},
	}

	// Round 1: SA gets patched AND stuck pod bounced — settled=false on both counts.
	settled, err := r.reconcilePullSecrets(context.Background(), w, []string{secretName})
	if err != nil {
		t.Fatalf("round 1: %v", err)
	}
	if settled {
		t.Errorf("round 1: expected settled=false (SA mutated or pod bounced)")
	}
	// Confirm the stuck pod was bounced.
	var pods corev1.PodList
	if err := c.List(context.Background(), &pods, client.InNamespace(ns)); err != nil {
		t.Fatalf("list pods after round 1: %v", err)
	}
	if len(pods.Items) != 0 {
		t.Errorf("expected stuck pod to be deleted after round 1, got %d pods remaining", len(pods.Items))
	}

	// Round 2: SA already patched, pod is gone — settled=true.
	settled, err = r.reconcilePullSecrets(context.Background(), w, []string{secretName})
	if err != nil {
		t.Fatalf("round 2: %v", err)
	}
	if !settled {
		t.Errorf("round 2: expected settled=true")
	}
}
