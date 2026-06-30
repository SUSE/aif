package aiworkload

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
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
		// The namespace "default" SA carries NO Helm label — charts and bundled
		// subcharts (e.g. litellm's postgresql dependency) run pods under it.
		// The operator must still patch it so those pods get image-pull creds,
		// even though reconcile otherwise scopes to chart-managed SAs.
		&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{
			Name: "default", Namespace: ns,
		}},
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
		Status: aiplatformv1alpha1.AIWorkloadStatus{
			PullSecretDeliveries: []aiplatformv1alpha1.PullSecretDelivery{
				{Namespace: ns, Names: []string{secretName}},
			},
		},
	}

	settled, err := r.reconcilePullSecrets(context.Background(), w)
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

// TestTargetsLocalCluster verifies the local-cluster predicate: empty targets
// mean local-default; an explicit "local"/"" entry counts; a purely downstream
// target list does not.
func TestTargetsLocalCluster(t *testing.T) {
	cases := []struct {
		name    string
		targets []string
		want    bool
	}{
		{"empty is local-default", nil, true},
		{"explicit local", []string{"local"}, true},
		{"empty-string entry", []string{""}, true},
		{"downstream only", []string{"c-abc"}, false},
		{"mixed includes local", []string{"c-abc", "local"}, true},
	}
	for _, tc := range cases {
		w := &aiplatformv1alpha1.AIWorkload{
			Spec: aiplatformv1alpha1.AIWorkloadSpec{TargetClusters: tc.targets},
		}
		if got := targetsLocalCluster(w); got != tc.want {
			t.Errorf("%s: targetsLocalCluster = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestReconcilePullSecrets_SkipsLocalWhenDownstreamOnly verifies that a
// downstream-only workload does not merge pull-secret refs into local
// ServiceAccounts (review #4): the local namespace just happens to share a
// name and must not be touched.
func TestReconcilePullSecrets_SkipsLocalWhenDownstreamOnly(t *testing.T) {
	scheme := newTestScheme(t)
	ns := "test-ns"
	secretName := "ngc-secret"

	objs := []client.Object{
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}},
		&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{
			Name: "default", Namespace: ns,
			Labels: map[string]string{chartManagedByLabel: chartManagedByHelm},
		}},
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
		Spec: aiplatformv1alpha1.AIWorkloadSpec{
			TargetNamespace: ns,
			TargetClusters:  []string{"downstream-a"},
		},
		Status: aiplatformv1alpha1.AIWorkloadStatus{
			PullSecretDeliveries: []aiplatformv1alpha1.PullSecretDelivery{
				{Namespace: ns, Names: []string{secretName}},
			},
		},
	}

	settled, err := r.reconcilePullSecrets(context.Background(), w)
	if err != nil {
		t.Fatalf("reconcilePullSecrets: %v", err)
	}
	if !settled {
		t.Errorf("expected settled=true (no local SA work for downstream-only), got false")
	}

	var sa corev1.ServiceAccount
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: "default"}, &sa); err != nil {
		t.Fatalf("get SA: %v", err)
	}
	if len(sa.ImagePullSecrets) != 0 {
		t.Errorf("expected local SA untouched for downstream-only workload, got %+v", sa.ImagePullSecrets)
	}
}

// TestReconcilePullSecrets_SkipsUnlabeledSA verifies the new label-scope
// contract: SAs without app.kubernetes.io/managed-by=Helm are NOT patched,
// even if they share the namespace with the workload.
func TestReconcilePullSecrets_SkipsUnlabeledSA(t *testing.T) {
	scheme := newTestScheme(t)
	ns := "test-ns"
	secretName := "ngc-secret"

	objs := []client.Object{
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}},
		&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "admin-sa", Namespace: ns}}, // no label
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme}

	w := &aiplatformv1alpha1.AIWorkload{
		ObjectMeta: metav1.ObjectMeta{Name: "wl", Namespace: "default"},
		Spec:       aiplatformv1alpha1.AIWorkloadSpec{TargetNamespace: ns},
		Status: aiplatformv1alpha1.AIWorkloadStatus{
			PullSecretDeliveries: []aiplatformv1alpha1.PullSecretDelivery{
				{Namespace: ns, Names: []string{secretName}},
			},
		},
	}
	settled, err := r.reconcilePullSecrets(context.Background(), w)
	if err != nil {
		t.Fatalf("reconcilePullSecrets: %v", err)
	}
	if !settled {
		t.Errorf("expected settled=true (no SAs matched the Helm label), got false")
	}
	var sa corev1.ServiceAccount
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: "admin-sa"}, &sa); err != nil {
		t.Fatalf("get SA: %v", err)
	}
	if len(sa.ImagePullSecrets) != 0 {
		t.Errorf("expected admin-sa.imagePullSecrets to remain empty (unlabeled SAs are out of scope), got %+v", sa.ImagePullSecrets)
	}
}

// newTestScheme builds a runtime.Scheme with the types tests in this package need.
func newTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := corev1.AddToScheme(s); err != nil {
		t.Fatalf("add corev1: %v", err)
	}
	if err := appsv1.AddToScheme(s); err != nil {
		// apps/v1 is needed for the ReplicaSet controllerRef the
		// pod-bounce path resolves and patches with the retry counter.
		t.Fatalf("add appsv1: %v", err)
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

// podWithContainerWaiting builds a Helm-labeled pod whose main container
// (or init container if init=true) is in the Waiting state with the given
// reason. The pod carries an owner reference to a ReplicaSet named "<name>-rs"
// so the bounce-cap path has a controller annotation target. Callers that
// exercise the bounce path must also include the matching ReplicaSet in their
// fake-client object list (see helmReplicaSet).
func podWithContainerWaiting(name string, init bool, reason string) *corev1.Pod {
	cs := corev1.ContainerStatus{
		Name:  "c",
		State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: reason}},
	}
	tru := true
	p := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name: name, Namespace: "test-ns",
		Labels: map[string]string{chartManagedByLabel: chartManagedByHelm},
		OwnerReferences: []metav1.OwnerReference{{
			APIVersion: "apps/v1", Kind: "ReplicaSet",
			Name: name + "-rs", UID: types.UID("rs-uid-" + name),
			Controller: &tru,
		}},
	}}
	if init {
		p.Status.InitContainerStatuses = []corev1.ContainerStatus{cs}
	} else {
		p.Status.ContainerStatuses = []corev1.ContainerStatus{cs}
	}
	return p
}

// helmReplicaSet returns the ReplicaSet a podWithContainerWaiting pod would
// reference, labeled Helm-managed. Tests must include the corresponding RS
// in the fake-client objects when they expect the pod-bounce path to run.
func helmReplicaSet(podName string, annotations map[string]string) *appsv1.ReplicaSet {
	return &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: podName + "-rs", Namespace: "test-ns",
			UID:         types.UID("rs-uid-" + podName),
			Labels:      map[string]string{chartManagedByLabel: chartManagedByHelm},
			Annotations: annotations,
		},
	}
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
				ObjectMeta: metav1.ObjectMeta{Name: "running", Namespace: "test-ns",
					Labels: map[string]string{chartManagedByLabel: chartManagedByHelm}},
				Status: corev1.PodStatus{Phase: corev1.PodRunning},
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
	objs := make([]client.Object, 0, len(cases)*2)
	wantDel := 0
	wantRemain := map[string]struct{}{}
	for _, tc := range cases {
		objs = append(objs, tc.pod)
		// Bounce-cap path needs the ReplicaSet to read/patch the counter;
		// add it for every Helm-owned pod with an OwnerReference.
		if len(tc.pod.OwnerReferences) > 0 {
			objs = append(objs, helmReplicaSet(tc.pod.Name, nil))
		}
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

func TestMergePullSecretDelivery(t *testing.T) {
	t.Run("empty inputs are no-op", func(t *testing.T) {
		got := mergePullSecretDelivery(nil, "", nil)
		if got != nil {
			t.Errorf("expected nil, got %v", got)
		}
		got = mergePullSecretDelivery(nil, "ns", nil)
		if got != nil {
			t.Errorf("empty names: expected nil, got %v", got)
		}
		got = mergePullSecretDelivery(nil, "", []string{"a"})
		if got != nil {
			t.Errorf("empty namespace: expected nil, got %v", got)
		}
	})

	t.Run("appends a new namespace bucket", func(t *testing.T) {
		got := mergePullSecretDelivery(nil, "ns-a", []string{"x", "y"})
		if len(got) != 1 || got[0].Namespace != "ns-a" || !equalStrings(got[0].Names, []string{"x", "y"}) {
			t.Errorf("got %+v", got)
		}
	})

	t.Run("merges names into existing bucket; dedupes", func(t *testing.T) {
		existing := []aiplatformv1alpha1.PullSecretDelivery{
			{Namespace: "ns-a", Names: []string{"x"}},
		}
		got := mergePullSecretDelivery(existing, "ns-a", []string{"x", "y"})
		if len(got) != 1 || got[0].Namespace != "ns-a" || !equalStrings(got[0].Names, []string{"x", "y"}) {
			t.Errorf("got %+v", got)
		}
	})

	t.Run("multi-namespace blueprint produces multiple buckets in input order", func(t *testing.T) {
		got := mergePullSecretDelivery(nil, "ns-a", []string{"x"})
		got = mergePullSecretDelivery(got, "ns-b", []string{"y", "z"})
		got = mergePullSecretDelivery(got, "ns-a", []string{"x2"}) // existing bucket appends
		if len(got) != 2 {
			t.Fatalf("expected 2 buckets, got %d (%+v)", len(got), got)
		}
		if got[0].Namespace != "ns-a" || !equalStrings(got[0].Names, []string{"x", "x2"}) {
			t.Errorf("bucket[0]: got %+v", got[0])
		}
		if got[1].Namespace != "ns-b" || !equalStrings(got[1].Names, []string{"y", "z"}) {
			t.Errorf("bucket[1]: got %+v", got[1])
		}
	})

	t.Run("input dedupe within Names", func(t *testing.T) {
		got := mergePullSecretDelivery(nil, "ns", []string{"x", "x", "y"})
		if len(got) != 1 || !equalStrings(got[0].Names, []string{"x", "y"}) {
			t.Errorf("got %+v", got)
		}
	})
}

// TestRestartImagePullBackOffPods_BounceCap verifies the per-controller
// retry cap (review #2): once chartPodMaxBounces bounces are recorded on
// the controller's annotation, further pods owned by that controller are
// left alone so the ImagePullBackOff is visible rather than masked by
// infinite churn.
func TestRestartImagePullBackOffPods_BounceCap(t *testing.T) {
	scheme := newTestScheme(t)
	ns := "test-ns"

	cases := []struct {
		name          string
		preCount      string // annotation value already on the RS
		expectBounced bool
		expectNew     string // annotation value after the call
	}{
		{name: "no prior bounces", preCount: "", expectBounced: true, expectNew: "1"},
		{name: "below cap", preCount: "1", expectBounced: true, expectNew: "2"},
		{name: "at cap-1", preCount: "2", expectBounced: true, expectNew: "3"},
		{name: "at cap", preCount: "3", expectBounced: false, expectNew: "3"},
		{name: "above cap", preCount: "5", expectBounced: false, expectNew: "5"},
		{name: "garbage annotation treated as 0", preCount: "not-a-number", expectBounced: true, expectNew: "1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pod := podWithContainerWaiting("stuck", false, "ImagePullBackOff")
			var anns map[string]string
			if tc.preCount != "" {
				anns = map[string]string{chartPodBounceAnnotation: tc.preCount}
			}
			rs := helmReplicaSet("stuck", anns)
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod, rs).Build()
			r := &AIWorkloadReconciler{Client: c, Scheme: scheme}

			bounced, err := r.restartImagePullBackOffPods(context.Background(), ns)
			if err != nil {
				t.Fatalf("restartImagePullBackOffPods: %v", err)
			}
			gotBounced := bounced > 0
			if gotBounced != tc.expectBounced {
				t.Errorf("bounced: got %d (=%v), want %v", bounced, gotBounced, tc.expectBounced)
			}

			// Check the pod was deleted iff we bounced.
			var podGot corev1.Pod
			err = c.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: "stuck"}, &podGot)
			podGone := errors.Is(err, nil) == false
			if podGone != tc.expectBounced {
				t.Errorf("pod deletion: gone=%v, expected bounced=%v", podGone, tc.expectBounced)
			}

			// Check the annotation reflects the new count.
			var rsGot appsv1.ReplicaSet
			if err := c.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: "stuck-rs"}, &rsGot); err != nil {
				t.Fatalf("get RS: %v", err)
			}
			gotNew := rsGot.Annotations[chartPodBounceAnnotation]
			if gotNew != tc.expectNew {
				t.Errorf("annotation: got %q, want %q", gotNew, tc.expectNew)
			}
		})
	}
}

// TestRestartImagePullBackOffPods_SkipsUnlabeled verifies pods without the
// Helm management label are NOT touched (review #2).
func TestRestartImagePullBackOffPods_SkipsUnlabeled(t *testing.T) {
	scheme := newTestScheme(t)
	ns := "test-ns"
	// Pod is in ImagePullBackOff but lacks the Helm label.
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "external", Namespace: ns},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				{Name: "c", State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff"}}},
			},
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod).Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme}
	bounced, err := r.restartImagePullBackOffPods(context.Background(), ns)
	if err != nil {
		t.Fatalf("restartImagePullBackOffPods: %v", err)
	}
	if bounced != 0 {
		t.Errorf("expected 0 bounces (pod lacks Helm label), got %d", bounced)
	}
	var got corev1.Pod
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: "external"}, &got); err != nil {
		t.Fatalf("pod should still exist, got: %v", err)
	}
}

// TestRestartImagePullBackOffPods_SkipsNoControllerRef verifies that
// chart-labeled pods without a controllerRef are skipped (no counter to
// track retries on).
func TestRestartImagePullBackOffPods_SkipsNoControllerRef(t *testing.T) {
	scheme := newTestScheme(t)
	ns := "test-ns"
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "orphan", Namespace: ns,
			Labels: map[string]string{chartManagedByLabel: chartManagedByHelm}},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				{Name: "c", State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff"}}},
			},
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod).Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme}
	bounced, err := r.restartImagePullBackOffPods(context.Background(), ns)
	if err != nil {
		t.Fatalf("restartImagePullBackOffPods: %v", err)
	}
	if bounced != 0 {
		t.Errorf("expected 0 bounces (no controllerRef to anchor retry counter), got %d", bounced)
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

	tru := true
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}},
		// Helm-labeled SA — qualifies for the operator's label-scoped patch path.
		&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{
			Name: "default", Namespace: ns,
			Labels: map[string]string{chartManagedByLabel: chartManagedByHelm},
		}},
		// Pod stuck in ImagePullBackOff — should be deleted, settled should be false.
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "stuck", Namespace: ns,
				Labels: map[string]string{chartManagedByLabel: chartManagedByHelm},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion: "apps/v1", Kind: "ReplicaSet",
					Name: "stuck-rs", UID: "stuck-rs-uid",
					Controller: &tru,
				}},
			},
			Status: corev1.PodStatus{
				ContainerStatuses: []corev1.ContainerStatus{
					{Name: "c", State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff"}}},
				},
			},
		},
		// ReplicaSet target for the bounce-cap annotation.
		&appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{
			Name: "stuck-rs", Namespace: ns, UID: "stuck-rs-uid",
			Labels: map[string]string{chartManagedByLabel: chartManagedByHelm},
		}},
	).Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme}

	w := &aiplatformv1alpha1.AIWorkload{
		ObjectMeta: metav1.ObjectMeta{Name: "wl", Namespace: "default"},
		Spec:       aiplatformv1alpha1.AIWorkloadSpec{TargetNamespace: ns},
		Status: aiplatformv1alpha1.AIWorkloadStatus{
			PullSecretDeliveries: []aiplatformv1alpha1.PullSecretDelivery{
				{Namespace: ns, Names: []string{secretName}},
			},
		},
	}

	// Round 1: SA gets patched AND stuck pod bounced — settled=false on both counts.
	settled, err := r.reconcilePullSecrets(context.Background(), w)
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
	settled, err = r.reconcilePullSecrets(context.Background(), w)
	if err != nil {
		t.Fatalf("round 2: %v", err)
	}
	if !settled {
		t.Errorf("round 2: expected settled=true")
	}
}

func TestDeliverPullSecrets_EmitsBundlePerDownstreamCluster(t *testing.T) {
	scheme := newTestScheme(t)
	// Register Bundle/BundleList GVK so the fake client accepts unstructured
	// bundle objects.
	bundleGVK := schema.GroupVersionKind{Group: "fleet.cattle.io", Version: "v1alpha1", Kind: "Bundle"}
	bundleListGVK := schema.GroupVersionKind{Group: "fleet.cattle.io", Version: "v1alpha1", Kind: "BundleList"}
	scheme.AddKnownTypeWithName(bundleGVK, &unstructured.Unstructured{})
	scheme.AddKnownTypeWithName(bundleListGVK, &unstructured.UnstructuredList{})

	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme}

	w := &aiplatformv1alpha1.AIWorkload{
		ObjectMeta: metav1.ObjectMeta{Name: "wl", Namespace: "default"},
		Spec: aiplatformv1alpha1.AIWorkloadSpec{
			TargetNamespace: "target-ns",
			TargetClusters:  []string{"c-aaa", "c-bbb"},
		},
		Status: aiplatformv1alpha1.AIWorkloadStatus{
			PullSecretDeliveries: []aiplatformv1alpha1.PullSecretDelivery{
				{Namespace: "target-ns", Names: []string{"ngc-secret"}},
			},
		},
	}

	if err := r.deliverPullSecrets(context.Background(), w, dummyPullSecretFactory); err != nil {
		t.Fatalf("deliverPullSecrets: %v", err)
	}

	// Downstream-only targets: the operator's own cluster must NOT receive the
	// secret (review #4 — no writing into a same-named local namespace the
	// workload never installs into here).
	var localSecret corev1.Secret
	if err := c.Get(context.Background(),
		types.NamespacedName{Namespace: "target-ns", Name: "ngc-secret"}, &localSecret); err == nil {
		t.Errorf("local ngc-secret should not be written for downstream-only targets")
	}

	// Downstream: exactly 2 bundles in fleet-default (one per cluster).
	var bundles unstructured.UnstructuredList
	bundles.SetGroupVersionKind(bundleListGVK)
	if err := c.List(context.Background(), &bundles, client.InNamespace("fleet-default")); err != nil {
		t.Fatalf("list bundles: %v", err)
	}
	if len(bundles.Items) != 2 {
		names := make([]string, 0, len(bundles.Items))
		for _, b := range bundles.Items {
			names = append(names, b.GetName())
		}
		t.Errorf("expected 2 bundles in fleet-default (one per cluster), got %d: %v", len(bundles.Items), names)
	}
	gotNames := map[string]bool{}
	for _, b := range bundles.Items {
		gotNames[b.GetName()] = true
	}
	// One consolidated Bundle per (owner, cluster, target namespace). The
	// namespace suffix lets blueprints whose components fan across multiple
	// namespaces produce one Bundle per namespace per cluster without
	// overwriting each other in fleet-default.
	for _, want := range []string{"ai-pullsecrets-wl-c-aaa-target-ns", "ai-pullsecrets-wl-c-bbb-target-ns"} {
		if !gotNames[want] {
			t.Errorf("missing bundle %q; got %+v", want, gotNames)
		}
	}
}

func TestDeliverPullSecrets_LocalOnlyWhenNoTargetClusters(t *testing.T) {
	scheme := newTestScheme(t)
	bundleGVK := schema.GroupVersionKind{Group: "fleet.cattle.io", Version: "v1alpha1", Kind: "Bundle"}
	bundleListGVK := schema.GroupVersionKind{Group: "fleet.cattle.io", Version: "v1alpha1", Kind: "BundleList"}
	scheme.AddKnownTypeWithName(bundleGVK, &unstructured.Unstructured{})
	scheme.AddKnownTypeWithName(bundleListGVK, &unstructured.UnstructuredList{})

	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme}

	w := &aiplatformv1alpha1.AIWorkload{
		ObjectMeta: metav1.ObjectMeta{Name: "wl", Namespace: "default"},
		Spec: aiplatformv1alpha1.AIWorkloadSpec{
			TargetNamespace: "target-ns",
			TargetClusters:  nil, // local-only
		},
		Status: aiplatformv1alpha1.AIWorkloadStatus{
			PullSecretDeliveries: []aiplatformv1alpha1.PullSecretDelivery{
				{Namespace: "target-ns", Names: []string{"ngc-secret"}},
			},
		},
	}

	if err := r.deliverPullSecrets(context.Background(), w, dummyPullSecretFactory); err != nil {
		t.Fatalf("deliverPullSecrets: %v", err)
	}

	var localSecret corev1.Secret
	if err := c.Get(context.Background(),
		types.NamespacedName{Namespace: "target-ns", Name: "ngc-secret"}, &localSecret); err != nil {
		t.Fatalf("local ngc-secret missing: %v", err)
	}

	// No bundles should exist when there are no downstream clusters.
	var bundles unstructured.UnstructuredList
	bundles.SetGroupVersionKind(bundleListGVK)
	if err := c.List(context.Background(), &bundles); err != nil {
		t.Fatalf("list bundles: %v", err)
	}
	if len(bundles.Items) != 0 {
		t.Errorf("expected 0 bundles for local-only workload, got %d", len(bundles.Items))
	}
}

func TestDeliverPullSecrets_SkipsLocalEntryInTargetClusters(t *testing.T) {
	// If TargetClusters contains "local", that means the local cluster IS the
	// target — we already handle the local case unconditionally, so "local"
	// in TargetClusters should NOT produce a Bundle (which would be redundant
	// AND wrong — Fleet's fleet-local workspace targets the local cluster
	// differently; we'd be duplicating delivery and confusing the model).
	scheme := newTestScheme(t)
	bundleGVK := schema.GroupVersionKind{Group: "fleet.cattle.io", Version: "v1alpha1", Kind: "Bundle"}
	bundleListGVK := schema.GroupVersionKind{Group: "fleet.cattle.io", Version: "v1alpha1", Kind: "BundleList"}
	scheme.AddKnownTypeWithName(bundleGVK, &unstructured.Unstructured{})
	scheme.AddKnownTypeWithName(bundleListGVK, &unstructured.UnstructuredList{})

	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme}

	w := &aiplatformv1alpha1.AIWorkload{
		ObjectMeta: metav1.ObjectMeta{Name: "wl", Namespace: "default"},
		Spec: aiplatformv1alpha1.AIWorkloadSpec{
			TargetNamespace: "target-ns",
			TargetClusters:  []string{"local", "c-bbb"},
		},
		Status: aiplatformv1alpha1.AIWorkloadStatus{
			PullSecretDeliveries: []aiplatformv1alpha1.PullSecretDelivery{
				{Namespace: "target-ns", Names: []string{"ngc-secret"}},
			},
		},
	}

	if err := r.deliverPullSecrets(context.Background(), w, dummyPullSecretFactory); err != nil {
		t.Fatalf("deliverPullSecrets: %v", err)
	}

	var localSecret corev1.Secret
	if err := c.Get(context.Background(),
		types.NamespacedName{Namespace: "target-ns", Name: "ngc-secret"}, &localSecret); err != nil {
		t.Fatalf("local ngc-secret missing: %v", err)
	}

	// Expect exactly 1 bundle (for c-bbb), NOT 2.
	var bundles unstructured.UnstructuredList
	bundles.SetGroupVersionKind(bundleListGVK)
	if err := c.List(context.Background(), &bundles); err != nil {
		t.Fatalf("list bundles: %v", err)
	}
	if len(bundles.Items) != 1 {
		names := make([]string, 0, len(bundles.Items))
		for _, b := range bundles.Items {
			names = append(names, b.GetName())
		}
		t.Errorf("expected 1 bundle (c-bbb only), got %d: %v", len(bundles.Items), names)
	}
}

func TestDeliverPullSecrets_FactoryReturningNilSkipsThatSecret(t *testing.T) {
	// When the factory returns (nil, nil) for a secret name (meaning "not
	// configured"), deliverPullSecrets should skip it without erroring and
	// not write anything for that name.
	scheme := newTestScheme(t)
	bundleGVK := schema.GroupVersionKind{Group: "fleet.cattle.io", Version: "v1alpha1", Kind: "Bundle"}
	bundleListGVK := schema.GroupVersionKind{Group: "fleet.cattle.io", Version: "v1alpha1", Kind: "BundleList"}
	scheme.AddKnownTypeWithName(bundleGVK, &unstructured.Unstructured{})
	scheme.AddKnownTypeWithName(bundleListGVK, &unstructured.UnstructuredList{})

	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme}

	w := &aiplatformv1alpha1.AIWorkload{
		ObjectMeta: metav1.ObjectMeta{Name: "wl", Namespace: "default"},
		Spec: aiplatformv1alpha1.AIWorkloadSpec{
			TargetNamespace: "target-ns",
			TargetClusters:  []string{"c-aaa"},
		},
		Status: aiplatformv1alpha1.AIWorkloadStatus{
			PullSecretDeliveries: []aiplatformv1alpha1.PullSecretDelivery{
				{Namespace: "target-ns", Names: []string{"ngc-secret"}},
			},
		},
	}

	nilFactory := func(ns, name string) (*corev1.Secret, error) { return nil, nil }
	if err := r.deliverPullSecrets(context.Background(), w, nilFactory); err != nil {
		t.Fatalf("deliverPullSecrets: %v", err)
	}

	// No local secret.
	var localSecret corev1.Secret
	err := c.Get(context.Background(),
		types.NamespacedName{Namespace: "target-ns", Name: "ngc-secret"}, &localSecret)
	if err == nil {
		t.Errorf("expected local ngc-secret to be absent, got: %+v", localSecret)
	}
	// No bundles.
	var bundles unstructured.UnstructuredList
	bundles.SetGroupVersionKind(bundleListGVK)
	if err := c.List(context.Background(), &bundles); err != nil {
		t.Fatalf("list bundles: %v", err)
	}
	if len(bundles.Items) != 0 {
		t.Errorf("expected 0 bundles, got %d", len(bundles.Items))
	}
}

func TestDeliverPullSecrets_FactoryErrorPropagates(t *testing.T) {
	scheme := newTestScheme(t)
	bundleGVK := schema.GroupVersionKind{Group: "fleet.cattle.io", Version: "v1alpha1", Kind: "Bundle"}
	bundleListGVK := schema.GroupVersionKind{Group: "fleet.cattle.io", Version: "v1alpha1", Kind: "BundleList"}
	scheme.AddKnownTypeWithName(bundleGVK, &unstructured.Unstructured{})
	scheme.AddKnownTypeWithName(bundleListGVK, &unstructured.UnstructuredList{})

	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme}

	w := &aiplatformv1alpha1.AIWorkload{
		ObjectMeta: metav1.ObjectMeta{Name: "wl", Namespace: "default"},
		Spec: aiplatformv1alpha1.AIWorkloadSpec{
			TargetNamespace: "target-ns",
			TargetClusters:  []string{"c-aaa"},
		},
		Status: aiplatformv1alpha1.AIWorkloadStatus{
			PullSecretDeliveries: []aiplatformv1alpha1.PullSecretDelivery{
				{Namespace: "target-ns", Names: []string{"ngc-secret"}},
			},
		},
	}

	boom := errors.New("creds-read failed")
	errFactory := func(ns, name string) (*corev1.Secret, error) { return nil, boom }

	err := r.deliverPullSecrets(context.Background(), w, errFactory)
	if err == nil {
		t.Fatal("expected error from factory to propagate; got nil")
	}
	if !errors.Is(err, boom) {
		t.Errorf("expected wrapped boom error; got %v", err)
	}
	if !strings.Contains(err.Error(), "ngc-secret") {
		t.Errorf("expected error to mention secret name; got %v", err)
	}
	// deliverPullSecrets now collects all built secrets before writing
	// anywhere, so a factory error happens during the build phase and the
	// wrapping mentions "build pull secret …" rather than a specific target.

	// Nothing should have been written (factory failed during build before any write).
	var bundles unstructured.UnstructuredList
	bundles.SetGroupVersionKind(bundleListGVK)
	if err := c.List(context.Background(), &bundles); err != nil {
		t.Fatalf("list bundles: %v", err)
	}
	if len(bundles.Items) != 0 {
		t.Errorf("expected 0 bundles after factory error, got %d", len(bundles.Items))
	}
}

// dummyPullSecretFactory builds a stub dockerconfigjson Secret for tests.
func dummyPullSecretFactory(targetNamespace, secretName string) (*corev1.Secret, error) {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: targetNamespace},
		Type:       corev1.SecretTypeDockerConfigJson,
		Data:       map[string][]byte{corev1.DockerConfigJsonKey: []byte(`{"auths":{}}`)},
	}, nil
}

func TestPullSecretFactory_NvidiaImagePullSecret(t *testing.T) {
	scheme := newTestScheme(t)
	opNS := "suse-ai-operator"
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "ngc-user", Namespace: opNS},
			Data:       map[string][]byte{"username": []byte("$oauthtoken")},
		},
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "ngc-token", Namespace: opNS},
			Data:       map[string][]byte{"token": []byte("nvapi-test")},
		},
		&aiplatformv1alpha1.Settings{
			ObjectMeta: metav1.ObjectMeta{Name: "settings", Namespace: opNS},
			Spec: aiplatformv1alpha1.SettingsSpec{
				Nvidia: aiplatformv1alpha1.NvidiaSettings{
					UserSecretRef:  &aiplatformv1alpha1.SecretKeyRef{Name: "ngc-user", Key: "username"},
					TokenSecretRef: &aiplatformv1alpha1.SecretKeyRef{Name: "ngc-token", Key: "token"},
				},
			},
		},
	).Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme, OperatorNamespace: opNS}

	factory := r.pullSecretFactory(context.Background())

	// ngc-secret: dockerconfigjson with non-empty data
	sec, err := factory("target-ns", nvidiaImagePullSecretName)
	if err != nil {
		t.Fatalf("factory(ngc-secret): %v", err)
	}
	if sec == nil {
		t.Fatal("expected non-nil dockerconfigjson secret; got nil")
	}
	if sec.Type != corev1.SecretTypeDockerConfigJson {
		t.Errorf("type: got %v want %v", sec.Type, corev1.SecretTypeDockerConfigJson)
	}
	if len(sec.Data[corev1.DockerConfigJsonKey]) == 0 {
		t.Errorf("dockerconfigjson data is empty")
	}

	// ngc-api: Opaque with NGC_API_KEY
	api, err := factory("target-ns", nvidiaAPISecretName)
	if err != nil {
		t.Fatalf("factory(ngc-api): %v", err)
	}
	if api == nil {
		t.Fatal("expected non-nil ngc-api secret; got nil")
	}
	if api.Type != corev1.SecretTypeOpaque {
		t.Errorf("type: got %v want Opaque", api.Type)
	}
	// All three NVIDIA env-var conventions must carry the same token so
	// charts that read any one of them work without per-chart tuning.
	for _, k := range nvidiaAPISecretKeys {
		if string(api.Data[k]) != "nvapi-test" {
			t.Errorf("token at key %s: got %q want nvapi-test", k, api.Data[k])
		}
	}

	// Unknown name: returns (nil, nil)
	unk, err := factory("target-ns", "unknown")
	if err != nil {
		t.Fatalf("factory(unknown): %v", err)
	}
	if unk != nil {
		t.Errorf("expected nil for unknown name; got %+v", unk)
	}
}

func TestPullSecretFactory_NoCredsReturnsNil(t *testing.T) {
	scheme := newTestScheme(t)
	opNS := "suse-ai-operator"
	// No Settings, no secrets — factory should return (nil, nil) for ngc-secret.
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme, OperatorNamespace: opNS}

	factory := r.pullSecretFactory(context.Background())
	sec, err := factory("target-ns", nvidiaImagePullSecretName)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	if sec != nil {
		t.Errorf("expected nil when creds not configured; got %+v", sec)
	}
}

func TestCleanupPullSecretBundles(t *testing.T) {
	scheme := newTestScheme(t)
	bundleGVK := schema.GroupVersionKind{Group: "fleet.cattle.io", Version: "v1alpha1", Kind: "Bundle"}
	bundleListGVK := schema.GroupVersionKind{Group: "fleet.cattle.io", Version: "v1alpha1", Kind: "BundleList"}
	scheme.AddKnownTypeWithName(bundleGVK, &unstructured.Unstructured{})
	scheme.AddKnownTypeWithName(bundleListGVK, &unstructured.UnstructuredList{})

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		newOwnedBundle("ai-pullsecrets-wl-c-aaa-ngc-secret", "fleet-default", "wl", "default"),
		newOwnedBundle("ai-pullsecrets-wl-c-bbb-ngc-secret", "fleet-default", "wl", "default"),
		newOwnedBundle("ai-pullsecrets-wl-c-aaa-ngc-api", "fleet-default", "wl", "default"),
		// Unrelated bundle owned by a different workload — must NOT be deleted.
		newOwnedBundle("ai-pullsecrets-other-c-aaa-ngc-secret", "fleet-default", "other", "default"),
		// Bundle owned by a DIFFERENT workload that happens to share the
		// same name in a different namespace — must NOT be deleted.
		// This proves the label selector's AND semantics across both labels.
		newOwnedBundle("ai-pullsecrets-wl-c-aaa-other-secret", "fleet-default", "wl", "other"),
		// Bundle in a different fleet workspace owned by this workload — current
		// scope is fleet-default only, but if cleanup uses label selector it
		// should sweep this too. Document the chosen scope in the impl.
	).Build()

	r := &AIWorkloadReconciler{Client: c, Scheme: scheme}
	w := &aiplatformv1alpha1.AIWorkload{ObjectMeta: metav1.ObjectMeta{Name: "wl", Namespace: "default"}}

	if err := r.cleanupPullSecretBundles(context.Background(), w); err != nil {
		t.Fatalf("cleanupPullSecretBundles: %v", err)
	}

	var bundles unstructured.UnstructuredList
	bundles.SetGroupVersionKind(bundleListGVK)
	if err := c.List(context.Background(), &bundles); err != nil {
		t.Fatalf("list bundles: %v", err)
	}
	if len(bundles.Items) != 2 {
		names := make([]string, 0, len(bundles.Items))
		for _, b := range bundles.Items {
			names = append(names, b.GetName())
		}
		t.Errorf("expected 2 unrelated bundles to remain (different owner-name AND different owner-namespace cases), got %d: %+v", len(bundles.Items), names)
	}
	// Verify both unrelated bundles are present, by name.
	remaining := map[string]bool{}
	for _, b := range bundles.Items {
		remaining[b.GetName()] = true
	}
	if !remaining["ai-pullsecrets-other-c-aaa-ngc-secret"] {
		t.Errorf("missing unrelated bundle (different owner-name); have %+v", remaining)
	}
	if !remaining["ai-pullsecrets-wl-c-aaa-other-secret"] {
		t.Errorf("missing unrelated bundle (different owner-namespace); have %+v", remaining)
	}
}

// newOwnedBundle constructs an unstructured Fleet Bundle with the operator's
// owner labels for cleanup tests.
func newOwnedBundle(name, ns, ownerName, ownerNS string) *unstructured.Unstructured {
	b := &unstructured.Unstructured{}
	b.SetGroupVersionKind(schema.GroupVersionKind{Group: "fleet.cattle.io", Version: "v1alpha1", Kind: "Bundle"})
	b.SetName(name)
	b.SetNamespace(ns)
	b.SetLabels(map[string]string{
		"ai-platform.suse.com/owner-name":      ownerName,
		"ai-platform.suse.com/owner-namespace": ownerNS,
	})
	return b
}

func TestPruneLocalSAImagePullSecrets(t *testing.T) {
	scheme := newTestScheme(t)
	ns := "target-ns"

	// SA has a workload-owned secret AND a pre-existing one. Only the
	// workload-owned entry should be removed.
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{Name: "default", Namespace: ns},
		ImagePullSecrets: []corev1.LocalObjectReference{
			{Name: "regcred"},    // pre-existing, must survive
			{Name: "ngc-secret"}, // workload-owned, must be removed
			{Name: "ngc-api"},    // workload-owned, must be removed
		},
	}
	// Second SA with only a workload-owned entry — should end with empty list.
	sa2 := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{Name: "other", Namespace: ns},
		ImagePullSecrets: []corev1.LocalObjectReference{
			{Name: "ngc-secret"},
		},
	}
	// Third SA with no overlap — must not be mutated (no Update call should
	// happen for it; fake client doesn't track Update vs not, but checking
	// the final state is sufficient).
	sa3 := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{Name: "untouched", Namespace: ns},
		ImagePullSecrets: []corev1.LocalObjectReference{
			{Name: "regcred"},
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(sa, sa2, sa3).Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme}

	w := &aiplatformv1alpha1.AIWorkload{
		ObjectMeta: metav1.ObjectMeta{Name: "wl", Namespace: "default"},
		Spec:       aiplatformv1alpha1.AIWorkloadSpec{TargetNamespace: ns},
		Status: aiplatformv1alpha1.AIWorkloadStatus{
			PullSecretDeliveries: []aiplatformv1alpha1.PullSecretDelivery{
				{Namespace: ns, Names: []string{"ngc-secret", "ngc-api"}},
			},
		},
	}
	if err := r.pruneLocalSAImagePullSecrets(context.Background(), w); err != nil {
		t.Fatalf("pruneLocalSAImagePullSecrets: %v", err)
	}

	var got corev1.ServiceAccount

	if err := c.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: "default"}, &got); err != nil {
		t.Fatalf("get default SA: %v", err)
	}
	if !equalRefs(got.ImagePullSecrets, []corev1.LocalObjectReference{{Name: "regcred"}}) {
		t.Errorf("default SA: expected [regcred] to remain, got %+v", got.ImagePullSecrets)
	}

	if err := c.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: "other"}, &got); err != nil {
		t.Fatalf("get other SA: %v", err)
	}
	if len(got.ImagePullSecrets) != 0 {
		t.Errorf("other SA: expected empty imagePullSecrets, got %+v", got.ImagePullSecrets)
	}

	if err := c.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: "untouched"}, &got); err != nil {
		t.Fatalf("get untouched SA: %v", err)
	}
	if !equalRefs(got.ImagePullSecrets, []corev1.LocalObjectReference{{Name: "regcred"}}) {
		t.Errorf("untouched SA: expected [regcred] preserved, got %+v", got.ImagePullSecrets)
	}
}

// TestPruneLocalSAImagePullSecrets_EmptyDeliveryNamespaceIsSkipped verifies
// that PullSecretDelivery entries with an empty Namespace (which shouldn't
// occur from production code paths, but is worth pinning as a guard) are
// silently skipped — not an error, just a no-op for that entry.
func TestPruneLocalSAImagePullSecrets_EmptyDeliveryNamespaceIsSkipped(t *testing.T) {
	scheme := newTestScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme}
	w := &aiplatformv1alpha1.AIWorkload{
		ObjectMeta: metav1.ObjectMeta{Name: "wl", Namespace: "default"},
		Spec:       aiplatformv1alpha1.AIWorkloadSpec{TargetNamespace: ""},
		Status: aiplatformv1alpha1.AIWorkloadStatus{
			PullSecretDeliveries: []aiplatformv1alpha1.PullSecretDelivery{
				{Namespace: "", Names: []string{"ngc-secret"}},
			},
		},
	}
	if err := r.pruneLocalSAImagePullSecrets(context.Background(), w); err != nil {
		t.Errorf("pruneLocalSAImagePullSecrets with empty delivery namespace: got err %v, want nil", err)
	}
}

func TestPruneLocalSAImagePullSecrets_EmptyStatusIsNoOp(t *testing.T) {
	scheme := newTestScheme(t)
	sa := &corev1.ServiceAccount{
		ObjectMeta:       metav1.ObjectMeta{Name: "default", Namespace: "target-ns"},
		ImagePullSecrets: []corev1.LocalObjectReference{{Name: "regcred"}},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(sa).Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme}
	w := &aiplatformv1alpha1.AIWorkload{
		ObjectMeta: metav1.ObjectMeta{Name: "wl", Namespace: "default"},
		Spec:       aiplatformv1alpha1.AIWorkloadSpec{TargetNamespace: "target-ns"},
		Status:     aiplatformv1alpha1.AIWorkloadStatus{PullSecretDeliveries: nil},
	}
	if err := r.pruneLocalSAImagePullSecrets(context.Background(), w); err != nil {
		t.Fatalf("pruneLocalSAImagePullSecrets: %v", err)
	}
	var got corev1.ServiceAccount
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "target-ns", Name: "default"}, &got); err != nil {
		t.Fatalf("get SA: %v", err)
	}
	if !equalRefs(got.ImagePullSecrets, []corev1.LocalObjectReference{{Name: "regcred"}}) {
		t.Errorf("expected regcred preserved (no-op), got %+v", got.ImagePullSecrets)
	}
}

// TestPullSecretFactory_SUSECombined verifies the third factory case wires
// through to buildSUSECombinedDockerConfig so downstream-delivery of the
// suseInjector's combined pull secret actually emits a Secret payload.
// Before this case existed, the factory's default branch returned (nil, nil)
// and deliverPullSecrets skipped the secret — pods on downstream clusters
// then ImagePullBackOff'd against dp.apps.rancher.io.
func TestPullSecretFactory_SUSECombined(t *testing.T) {
	scheme := newTestScheme(t)
	opNS := "suse-ai-operator"
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		// AppCollection + SUSE Registry + NVIDIA creds all present so the
		// resulting dockerconfigjson covers every image host the combined
		// secret is expected to authenticate.
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "appco", Namespace: opNS},
			Data: map[string][]byte{
				"username": []byte("user@example.com"),
				"token":    []byte("appco-token"),
			},
		},
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "susereg", Namespace: opNS},
			Data: map[string][]byte{
				"username": []byte("regcode"),
				"token":    []byte("susereg-token"),
			},
		},
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "ngc", Namespace: opNS},
			Data: map[string][]byte{
				"username": []byte("$oauthtoken"),
				"token":    []byte("nvapi-token"),
			},
		},
		&aiplatformv1alpha1.Settings{
			ObjectMeta: metav1.ObjectMeta{Name: "settings", Namespace: opNS},
			Spec: aiplatformv1alpha1.SettingsSpec{
				ApplicationCollection: aiplatformv1alpha1.ApplicationCollectionSettings{
					UserSecretRef:  &aiplatformv1alpha1.SecretKeyRef{Name: "appco", Key: "username"},
					TokenSecretRef: &aiplatformv1alpha1.SecretKeyRef{Name: "appco", Key: "token"},
				},
				SUSERegistry: aiplatformv1alpha1.SUSERegistrySettings{
					UserSecretRef:  &aiplatformv1alpha1.SecretKeyRef{Name: "susereg", Key: "username"},
					TokenSecretRef: &aiplatformv1alpha1.SecretKeyRef{Name: "susereg", Key: "token"},
				},
				Nvidia: aiplatformv1alpha1.NvidiaSettings{
					UserSecretRef:  &aiplatformv1alpha1.SecretKeyRef{Name: "ngc", Key: "username"},
					TokenSecretRef: &aiplatformv1alpha1.SecretKeyRef{Name: "ngc", Key: "token"},
				},
			},
		},
	).Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme, OperatorNamespace: opNS}

	factory := r.pullSecretFactory(context.Background())
	sec, err := factory("target-ns", combinedPullSecretName)
	if err != nil {
		t.Fatalf("factory(suse-ai-pull-combined): %v", err)
	}
	if sec == nil {
		t.Fatal("expected non-nil combined secret; got nil")
	}
	if sec.Name != combinedPullSecretName {
		t.Errorf("name: got %q want %q", sec.Name, combinedPullSecretName)
	}
	if sec.Namespace != "target-ns" {
		t.Errorf("namespace: got %q want target-ns", sec.Namespace)
	}
	if sec.Type != corev1.SecretTypeDockerConfigJson {
		t.Errorf("type: got %v want %v", sec.Type, corev1.SecretTypeDockerConfigJson)
	}

	// Auth payload should include all three image hosts. We don't assert
	// exact base64 values — that's already covered by the lower-level
	// ensureCombinedPullSecret tests in blueprint_pullsecret_test.go. Here
	// we only prove the factory case correctly routes through to the helper.
	var cfg struct {
		Auths map[string]any `json:"auths"`
	}
	if err := json.Unmarshal(sec.Data[corev1.DockerConfigJsonKey], &cfg); err != nil {
		t.Fatalf("parse dockerconfigjson: %v", err)
	}
	for _, host := range []string{defaultAppCollectionHost, defaultSUSERegistryHost, defaultNvidiaHost} {
		if _, ok := cfg.Auths[host]; !ok {
			t.Errorf("auths missing host %q; got hosts: %v", host, mapKeys(cfg.Auths))
		}
	}
}

// TestPullSecretFactory_SUSECombined_NoCreds covers the (nil, nil) skip path:
// when no Settings credentials are configured, deliverPullSecrets should not
// emit a Bundle for the combined secret at all (rather than emit one with an
// empty auths map and confuse Helm downstream).
func TestPullSecretFactory_SUSECombined_NoCreds(t *testing.T) {
	scheme := newTestScheme(t)
	opNS := "suse-ai-operator"
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		// Settings exists but has no credential refs — addSUSESettingsAuths
		// produces an empty auths map and the helper returns (nil, nil).
		&aiplatformv1alpha1.Settings{
			ObjectMeta: metav1.ObjectMeta{Name: "settings", Namespace: opNS},
			Spec:       aiplatformv1alpha1.SettingsSpec{},
		},
	).Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme, OperatorNamespace: opNS}

	factory := r.pullSecretFactory(context.Background())
	sec, err := factory("target-ns", combinedPullSecretName)
	if err != nil {
		t.Fatalf("factory(suse-ai-pull-combined): %v", err)
	}
	if sec != nil {
		t.Errorf("expected nil secret when no creds configured; got %+v", sec)
	}
}

// TestBuildSUSECombinedDockerConfig_HonorsRegistryEndpointsOverride pins the
// air-gap behavior: when Settings.spec.registryEndpoints rewrites AppCollection
// or SUSERegistry to a mirror host, the combined dockerconfigjson uses the
// mirror host as the auth key (so kubelet finds the credentials when pulling
// from the mirror). NVIDIA is intentionally NOT remapped — registryEndpoints.nvidia
// is the chart-repo OCI URL, not an image host; image pulls stay on nvcr.io.
func TestBuildSUSECombinedDockerConfig_HonorsRegistryEndpointsOverride(t *testing.T) {
	scheme := newTestScheme(t)
	opNS := "suse-ai-operator"
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "appco", Namespace: opNS},
			Data:       map[string][]byte{"username": []byte("u"), "token": []byte("p")},
		},
		&aiplatformv1alpha1.Settings{
			ObjectMeta: metav1.ObjectMeta{Name: "settings", Namespace: opNS},
			Spec: aiplatformv1alpha1.SettingsSpec{
				ApplicationCollection: aiplatformv1alpha1.ApplicationCollectionSettings{
					UserSecretRef:  &aiplatformv1alpha1.SecretKeyRef{Name: "appco", Key: "username"},
					TokenSecretRef: &aiplatformv1alpha1.SecretKeyRef{Name: "appco", Key: "token"},
				},
				RegistryEndpoints: &aiplatformv1alpha1.RegistryEndpointsSettings{
					ApplicationCollection: "oci://mirror.internal/charts",
				},
			},
		},
	).Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme, OperatorNamespace: opNS}

	cfgBytes, err := r.buildSUSECombinedDockerConfig(context.Background())
	if err != nil {
		t.Fatalf("buildSUSECombinedDockerConfig: %v", err)
	}
	if cfgBytes == nil {
		t.Fatal("expected non-nil cfg with AppCollection creds")
	}
	var cfg struct {
		Auths map[string]any `json:"auths"`
	}
	if err := json.Unmarshal(cfgBytes, &cfg); err != nil {
		t.Fatalf("parse dockerconfigjson: %v", err)
	}
	if _, ok := cfg.Auths["mirror.internal"]; !ok {
		t.Errorf("auths missing override host mirror.internal; got hosts: %v", mapKeys(cfg.Auths))
	}
	if _, ok := cfg.Auths[defaultAppCollectionHost]; ok {
		t.Errorf("default host %q should NOT appear when override is set; got hosts: %v",
			defaultAppCollectionHost, mapKeys(cfg.Auths))
	}
}

func mapKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestDeliverPullSecrets_MultiNamespaceBlueprint covers the case that
// motivated PullSecretDeliveries: a Blueprint whose components opt in to
// different BlueprintComponent.TargetNamespace values produces multiple
// per-namespace buckets in Status. deliverPullSecrets must:
//   - write the local copy of each secret into its bucket's namespace, and
//   - emit one consolidated Fleet Bundle per (cluster, namespace), not one
//     per cluster only (which would clobber the second namespace's bundle
//     in fleet-default).
func TestDeliverPullSecrets_MultiNamespaceBlueprint(t *testing.T) {
	scheme := newTestScheme(t)
	bundleGVK := schema.GroupVersionKind{Group: "fleet.cattle.io", Version: "v1alpha1", Kind: "Bundle"}
	bundleListGVK := schema.GroupVersionKind{Group: "fleet.cattle.io", Version: "v1alpha1", Kind: "BundleList"}
	scheme.AddKnownTypeWithName(bundleGVK, &unstructured.Unstructured{})
	scheme.AddKnownTypeWithName(bundleListGVK, &unstructured.UnstructuredList{})

	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme}

	w := &aiplatformv1alpha1.AIWorkload{
		ObjectMeta: metav1.ObjectMeta{Name: "wl", Namespace: "default"},
		Spec: aiplatformv1alpha1.AIWorkloadSpec{
			TargetNamespace: "install-ns", // unused — every secret is in a component namespace
			// Includes "local" so the local per-namespace placement below is
			// exercised; "c-down" drives the downstream bundle assertions.
			TargetClusters: []string{"local", "c-down"},
		},
		Status: aiplatformv1alpha1.AIWorkloadStatus{
			PullSecretDeliveries: []aiplatformv1alpha1.PullSecretDelivery{
				{Namespace: "comp-a-ns", Names: []string{"ngc-secret"}},
				{Namespace: "comp-b-ns", Names: []string{"ngc-secret", "ngc-api"}},
			},
		},
	}

	if err := r.deliverPullSecrets(context.Background(), w, dummyPullSecretFactory); err != nil {
		t.Fatalf("deliverPullSecrets: %v", err)
	}

	// Local cluster: each component's secrets must land in its OWN namespace.
	for _, want := range []struct {
		ns, name string
	}{
		{"comp-a-ns", "ngc-secret"},
		{"comp-b-ns", "ngc-secret"},
		{"comp-b-ns", "ngc-api"},
	} {
		var s corev1.Secret
		if err := c.Get(context.Background(), types.NamespacedName{Namespace: want.ns, Name: want.name}, &s); err != nil {
			t.Errorf("expected local secret %s/%s: %v", want.ns, want.name, err)
		}
	}
	// And the cross-namespace negative: comp-a-ns must NOT have ngc-api
	// (only comp-b-ns does), nor should the workload's install-ns have any.
	var stray corev1.Secret
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "comp-a-ns", Name: "ngc-api"}, &stray); err == nil {
		t.Errorf("unexpected secret comp-a-ns/ngc-api leaked across namespaces")
	}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "install-ns", Name: "ngc-secret"}, &stray); err == nil {
		t.Errorf("unexpected secret install-ns/ngc-secret — workload's install-ns shouldn't receive component-scoped deliveries")
	}

	// Downstream: one Bundle per (cluster, namespace). For one downstream
	// cluster with two namespaces, that's 2 bundles named
	// `ai-pullsecrets-wl-c-down-<ns>`.
	var bundles unstructured.UnstructuredList
	bundles.SetGroupVersionKind(bundleListGVK)
	if err := c.List(context.Background(), &bundles, client.InNamespace("fleet-default")); err != nil {
		t.Fatalf("list bundles: %v", err)
	}
	got := map[string]bool{}
	for _, b := range bundles.Items {
		got[b.GetName()] = true
	}
	for _, want := range []string{
		"ai-pullsecrets-wl-c-down-comp-a-ns",
		"ai-pullsecrets-wl-c-down-comp-b-ns",
	} {
		if !got[want] {
			t.Errorf("missing bundle %q; have %+v", want, mapKeysBool(got))
		}
	}
}

func mapKeysBool(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
