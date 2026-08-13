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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

// Runs against envtest rather than the fake client on purpose. The clobber this
// pins is produced by the API server stripping status from a main-resource Update
// and the typed client decoding the stored copy back into the object; the fake
// client applies the status change to its own tracked copy and never writes back,
// so on a fake this test passes whether or not the fix is present.
var _ = Describe("annotation writes", func() {
	const name = "annotation-write-probe"

	var (
		ctx context.Context
		r   *InstallAIExtensionReconciler
		ext *v1alpha1.InstallAIExtension
	)

	BeforeEach(func() {
		ctx = context.Background()
		r = &InstallAIExtensionReconciler{Client: k8sClient}

		ext = &v1alpha1.InstallAIExtension{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Spec: v1alpha1.InstallAIExtensionSpec{
				Extension: v1alpha1.ExtensionConfig{Name: "aif-ui", Version: "1.0.0"},
				Source: v1alpha1.ExtensionSource{
					Kind: v1alpha1.ExtensionSourceKindHelm,
					Helm: &v1alpha1.HelmSource{ChartURL: "oci://example.com/aif-ui", Version: "1.0.0"},
				},
			},
		}
		Expect(k8sClient.Create(ctx, ext)).To(Succeed())

		// The stored state of a CR that finished installing.
		ext.Status.Phase = v1alpha1.InstallAIExtensionPhaseInstalled
		ext.Status.ObservedGeneration = ext.Generation
		meta.SetStatusCondition(&ext.Status.Conditions, metav1.Condition{
			Type: conditionTypeReady, Status: metav1.ConditionTrue,
			Reason: "Installed", Message: "installed", ObservedGeneration: ext.Generation,
		})
		Expect(k8sClient.Status().Update(ctx, ext)).To(Succeed())
	})

	AfterEach(func() {
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, ext))).To(Succeed())
	})

	It("keeps status the reconcile pass has already set", func() {
		var obj v1alpha1.InstallAIExtension
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: name}, &obj)).To(Succeed())

		// What a pass looks like by the time it writes a marker: phase moved off
		// Installed, and a condition set that has not been persisted yet.
		obj.Status.Phase = v1alpha1.InstallAIExtensionPhaseInstalling
		setCondition(&obj.Status.Conditions, conditionTypeHelmInstalled, metav1.ConditionFalse,
			"ReleasePending", "waiting", obj.Generation)

		r.setWaitingSince(&obj, annotationReleasePendingSince)
		Expect(r.updateAnnotations(ctx, &obj)).To(Succeed())

		Expect(obj.Status.Phase).To(Equal(v1alpha1.InstallAIExtensionPhaseInstalling),
			"Update echoed the stored status back over the pass's own")

		cond := meta.FindStatusCondition(obj.Status.Conditions, conditionTypeHelmInstalled)
		Expect(cond).NotTo(BeNil(), "condition set before the write did not survive it")
		Expect(cond.Reason).To(Equal("ReleasePending"))

		// The annotation still has to reach the server — preserving status must not
		// cost the write its actual purpose.
		var stored v1alpha1.InstallAIExtension
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: name}, &stored)).To(Succeed())
		Expect(stored.Annotations).To(HaveKey(annotationReleasePendingSince))
	})

	// Reconcile stamps ObservedGeneration only when the pass ends on Phase=Installed.
	// The clobber turned a pending pass into exactly that, marking a generation
	// applied that the cluster never received.
	It("does not let a pending pass look like a completed one", func() {
		var cur v1alpha1.InstallAIExtension
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: name}, &cur)).To(Succeed())
		cur.Spec.Extension.Version = "2.0.0"
		cur.Spec.Source.Helm.Version = "2.0.0"
		Expect(k8sClient.Update(ctx, &cur)).To(Succeed())

		var obj v1alpha1.InstallAIExtension
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: name}, &obj)).To(Succeed())
		Expect(obj.Status.ObservedGeneration).To(BeNumerically("<", obj.Generation))

		obj.Status.Phase = v1alpha1.InstallAIExtensionPhaseInstalling

		result, handled, err := r.handlePendingRelease(ctx, &obj, conditionTypeHelmInstalled,
			pendingErr("pending-upgrade"))
		Expect(err).NotTo(HaveOccurred())
		Expect(handled).To(BeTrue())
		Expect(result.RequeueAfter).To(Equal(pendingReleaseRequeue))

		Expect(obj.Status.Phase).NotTo(Equal(v1alpha1.InstallAIExtensionPhaseInstalled),
			"Reconcile would stamp ObservedGeneration for a spec that was never applied")

		// The first observation reports the wait, rather than leaving the previous
		// pass's success standing for a whole requeue interval.
		ready := meta.FindStatusCondition(obj.Status.Conditions, conditionTypeReady)
		Expect(ready).NotTo(BeNil())
		Expect(ready.Status).To(Equal(metav1.ConditionFalse))
		Expect(ready.Reason).To(Equal("ReleasePending"))
	})

	It("clears the marker without disturbing status", func() {
		var obj v1alpha1.InstallAIExtension
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: name}, &obj)).To(Succeed())

		obj.Annotations = map[string]string{
			annotationReleasePendingSince: time.Now().Format(time.RFC3339),
		}
		Expect(k8sClient.Update(ctx, &obj)).To(Succeed())

		setCondition(&obj.Status.Conditions, conditionTypeClusterRepo, metav1.ConditionTrue,
			"Created", "ClusterRepo created", obj.Generation)

		_, handled, err := r.handlePendingRelease(ctx, &obj, conditionTypeUIPlugin, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(handled).To(BeFalse())

		Expect(meta.FindStatusCondition(obj.Status.Conditions, conditionTypeClusterRepo)).
			NotTo(BeNil(), "clearing the marker dropped a condition set earlier in the pass")

		var stored v1alpha1.InstallAIExtension
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: name}, &stored)).To(Succeed())
		Expect(stored.Annotations).NotTo(HaveKey(annotationReleasePendingSince))
	})
})
