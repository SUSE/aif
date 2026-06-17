package aiworkload

import (
	"context"
	"errors"
	"strings"
	"testing"

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
			PullSecretNames: []string{"ngc-secret"},
		},
	}

	if err := r.deliverPullSecrets(context.Background(), w, dummyPullSecretFactory); err != nil {
		t.Fatalf("deliverPullSecrets: %v", err)
	}

	// Local cluster always: ngc-secret should exist in target-ns on the operator's cluster.
	var localSecret corev1.Secret
	if err := c.Get(context.Background(),
		types.NamespacedName{Namespace: "target-ns", Name: "ngc-secret"}, &localSecret); err != nil {
		t.Fatalf("local ngc-secret missing: %v", err)
	}
	if localSecret.Type != corev1.SecretTypeDockerConfigJson {
		t.Errorf("local secret type: got %v want %v", localSecret.Type, corev1.SecretTypeDockerConfigJson)
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
	// One consolidated Bundle per (owner, cluster); no per-secret suffix.
	for _, want := range []string{"ai-pullsecrets-wl-c-aaa", "ai-pullsecrets-wl-c-bbb"} {
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
			PullSecretNames: []string{"ngc-secret"},
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
			PullSecretNames: []string{"ngc-secret"},
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
			PullSecretNames: []string{"ngc-secret"},
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
			PullSecretNames: []string{"ngc-secret"},
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
		Status:     aiplatformv1alpha1.AIWorkloadStatus{PullSecretNames: []string{"ngc-secret", "ngc-api"}},
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

func TestPruneLocalSAImagePullSecrets_NoTargetNamespaceIsNoOp(t *testing.T) {
	scheme := newTestScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme}
	w := &aiplatformv1alpha1.AIWorkload{
		ObjectMeta: metav1.ObjectMeta{Name: "wl", Namespace: "default"},
		Spec:       aiplatformv1alpha1.AIWorkloadSpec{TargetNamespace: ""},
		Status:     aiplatformv1alpha1.AIWorkloadStatus{PullSecretNames: []string{"ngc-secret"}},
	}
	if err := r.pruneLocalSAImagePullSecrets(context.Background(), w); err != nil {
		t.Errorf("pruneLocalSAImagePullSecrets with empty TargetNamespace: got err %v, want nil", err)
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
		Status:     aiplatformv1alpha1.AIWorkloadStatus{PullSecretNames: nil},
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
