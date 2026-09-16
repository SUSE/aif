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

package controller

import (
	"context"
	"errors"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

// Runs against envtest: the object has to actually carry a DeletionTimestamp
// and be governed by the API server's finalizer semantics (an object with
// finalizers set stays retrievable after Delete; it vanishes only once the
// last one clears), which the fake client does not reproduce faithfully enough
// to trust this on.

// failingCleanupRancherManager fails every delete cleanup() attempts, so a
// test can drive handleDeletion through the retry-then-give-up path without
// depending on what specific error a real cluster would produce.
type failingCleanupRancherManager struct {
	stubRancherManager
	err error
}

func (f *failingCleanupRancherManager) DeleteClusterRepo(context.Context, string) error {
	return f.err
}

func (f *failingCleanupRancherManager) DeleteUIPlugin(context.Context, string, string) error {
	return f.err
}

var _ = Describe("bounded finalizer", func() {
	const name = "finalizer-bound-probe"

	var (
		ctx context.Context
		r   *InstallAIExtensionReconciler
		mgr *failingCleanupRancherManager
		rec *record.FakeRecorder
		ext *v1alpha1.InstallAIExtension
	)

	BeforeEach(func() {
		ctx = context.Background()
		mgr = &failingCleanupRancherManager{err: errors.New("admission webhook denied the request")}
		rec = record.NewFakeRecorder(10)
		r = &InstallAIExtensionReconciler{Client: k8sClient, rancherMgr: mgr, Recorder: rec}

		ext = &v1alpha1.InstallAIExtension{
			ObjectMeta: metav1.ObjectMeta{Name: name, Finalizers: []string{finalizerName}},
			Spec: v1alpha1.InstallAIExtensionSpec{
				Extension: v1alpha1.ExtensionConfig{Name: "aif-ui", Version: "1.0.0"},
				Source: v1alpha1.ExtensionSource{
					Kind: v1alpha1.ExtensionSourceKindHelm,
					Helm: &v1alpha1.HelmSource{ChartURL: "oci://example.com/aif-ui", Version: "1.0.0"},
				},
			},
		}
		Expect(k8sClient.Create(ctx, ext)).To(Succeed())
		Expect(k8sClient.Delete(ctx, ext)).To(Succeed())
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: name}, ext)).To(Succeed())
		Expect(ext.DeletionTimestamp).NotTo(BeNil())
	})

	AfterEach(func() {
		// Best-effort: most cases remove the finalizer themselves and the object
		// is already gone by the time this runs.
		ext.Finalizers = nil
		_ = k8sClient.Update(ctx, ext)
		_ = client.IgnoreNotFound(k8sClient.Delete(ctx, ext))
	})

	It("retries on the first failure instead of giving up immediately", func() {
		result, err := r.handleDeletion(ctx, ext)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(cleanupRetryInterval))

		var stored v1alpha1.InstallAIExtension
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: name}, &stored)).To(Succeed())
		Expect(stored.Finalizers).To(ContainElement(finalizerName),
			"one failure must not remove the finalizer; that would abandon cleanup entirely")
		Expect(stored.Annotations).To(HaveKey(annotationCleanupWaitingSince))
	})

	It("keeps retrying inside the bound without resetting the clock", func() {
		_, err := r.handleDeletion(ctx, ext)
		Expect(err).NotTo(HaveOccurred())

		var stored v1alpha1.InstallAIExtension
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: name}, &stored)).To(Succeed())
		firstMarker := stored.Annotations[annotationCleanupWaitingSince]

		result, err := r.handleDeletion(ctx, &stored)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(cleanupRetryInterval))
		Expect(stored.Annotations[annotationCleanupWaitingSince]).To(Equal(firstMarker),
			"a pass still inside the bound must not restart the wait it is timing")
	})

	It("gives up past the bound: removes the finalizer, lets the object go, and warns", func() {
		_, err := r.handleDeletion(ctx, ext)
		Expect(err).NotTo(HaveOccurred())

		var stored v1alpha1.InstallAIExtension
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: name}, &stored)).To(Succeed())

		// Backdate the marker past cleanupTimeout rather than sleeping for it —
		// the annotation is the only state the bound reads (see getWaitingSince),
		// so this reproduces "the bound has elapsed" exactly.
		stored.Annotations[annotationCleanupWaitingSince] =
			time.Now().Add(-cleanupTimeout - time.Second).Format(time.RFC3339)
		Expect(k8sClient.Update(ctx, &stored)).To(Succeed())

		result, err := r.handleDeletion(ctx, &stored)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.IsZero()).To(BeTrue())

		var gone v1alpha1.InstallAIExtension
		getErr := k8sClient.Get(ctx, client.ObjectKey{Name: name}, &gone)
		Expect(client.IgnoreNotFound(getErr)).To(Succeed())
		if getErr == nil {
			Expect(gone.Finalizers).To(BeEmpty(),
				"the finalizer must be gone even though cleanup never succeeded")
		}

		Expect(rec.Events).To(Receive(ContainSubstring("CleanupIncomplete")))
	})

	It("clears the marker on a cleanup that eventually succeeds", func() {
		_, err := r.handleDeletion(ctx, ext)
		Expect(err).NotTo(HaveOccurred())

		var stored v1alpha1.InstallAIExtension
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: name}, &stored)).To(Succeed())
		Expect(stored.Annotations).To(HaveKey(annotationCleanupWaitingSince))

		mgr.err = nil
		_, err = r.handleDeletion(ctx, &stored)
		Expect(err).NotTo(HaveOccurred())

		var gone v1alpha1.InstallAIExtension
		getErr := k8sClient.Get(ctx, client.ObjectKey{Name: name}, &gone)
		Expect(client.IgnoreNotFound(getErr)).To(Succeed())
		if getErr == nil {
			Expect(gone.Finalizers).To(BeEmpty())
		}
	})
})

// Confirms handleDeletion is a no-op once the finalizer is already gone
// (e.g. a stale watch event replaying after another pass already finished),
// rather than re-running cleanup against objects that were already removed.
var _ = Describe("finalizer already removed", func() {
	It("does nothing", func() {
		ctx := context.Background()
		r := &InstallAIExtensionReconciler{Client: k8sClient}
		now := metav1.Now()
		ext := &v1alpha1.InstallAIExtension{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "finalizer-already-gone",
				DeletionTimestamp: &now,
			},
		}
		result, err := r.handleDeletion(ctx, ext)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.IsZero()).To(BeTrue())
	})
})
