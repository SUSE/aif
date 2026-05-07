package controller_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	aifv1 "github.com/SUSE/aif/api/v1alpha1"
	"github.com/SUSE/aif/pkg/conditions"
)

var _ = Describe("SettingsReconciler", func() {
	const timeout = 30 * time.Second
	const interval = 250 * time.Millisecond

	ctx := context.Background()

	findReady := func(conds []metav1.Condition) *metav1.Condition {
		for i := range conds {
			if conds[i].Type == conditions.TypeReady {
				return &conds[i]
			}
		}
		return nil
	}

	// Ensure the "aif" namespace exists (Settings is singleton in aif namespace)
	BeforeEach(func() {
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: "aif"},
		}
		err := k8sClient.Create(ctx, ns)
		if err != nil && !errors.IsAlreadyExists(err) {
			Expect(err).NotTo(HaveOccurred())
		}
	})

	It("should reconcile valid credentials to Ready=True", func() {
		// Create Secrets
		suseRegSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "suse-reg-creds",
				Namespace: "aif",
			},
			Data: map[string][]byte{
				"username": []byte("test-user"),
				"password": []byte("test-pass"),
			},
		}
		appCollSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "app-coll-creds",
				Namespace: "aif",
			},
			Data: map[string][]byte{
				"user":  []byte("coll-user"),
				"token": []byte("coll-token"),
			},
		}
		Expect(k8sClient.Create(ctx, suseRegSecret)).To(Succeed())
		Expect(k8sClient.Create(ctx, appCollSecret)).To(Succeed())

		settings := &aifv1.Settings{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "valid-settings",
				Namespace: "aif",
			},
			Spec: aifv1.SettingsSpec{
				SUSERegistry: &aifv1.SUSERegistryConfig{
					UserSecretRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "suse-reg-creds"},
						Key:                  "username",
					},
					TokenSecretRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "suse-reg-creds"},
						Key:                  "password",
					},
				},
				ApplicationCollection: &aifv1.ApplicationCollectionConfig{
					UserSecretRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "app-coll-creds"},
						Key:                  "user",
					},
					TokenSecretRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "app-coll-creds"},
						Key:                  "token",
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, settings)).To(Succeed())

		Eventually(func(g Gomega) {
			var fetched aifv1.Settings
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(settings), &fetched)).To(Succeed())
			rc := findReady(fetched.Status.Conditions)
			g.Expect(rc).NotTo(BeNil())
			g.Expect(rc.Status).To(Equal(metav1.ConditionTrue))
			g.Expect(rc.Reason).To(Equal(conditions.ReasonReconciled))
			g.Expect(fetched.Status.ObservedGeneration).To(Equal(fetched.Generation))
			g.Expect(fetched.Status.LastApplied.IsZero()).To(BeFalse())
		}, timeout, interval).Should(Succeed())

		// Verify finalizer
		var fetched aifv1.Settings
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(settings), &fetched)).To(Succeed())
		Expect(fetched.Finalizers).To(ContainElement("ai.suse.com/cleanup"))
	})

	It("should set SecretNotFound when ApplicationCollection Secret is missing", func() {
		settings := &aifv1.Settings{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "missing-appcoll-secret",
				Namespace: "aif",
			},
			Spec: aifv1.SettingsSpec{
				ApplicationCollection: &aifv1.ApplicationCollectionConfig{
					UserSecretRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "nonexistent-secret"},
						Key:                  "user",
					},
				},
				SUSERegistry: &aifv1.SUSERegistryConfig{},
				Fleet:        &aifv1.FleetConfig{},
			},
		}
		Expect(k8sClient.Create(ctx, settings)).To(Succeed())

		Eventually(func(g Gomega) {
			var fetched aifv1.Settings
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(settings), &fetched)).To(Succeed())
			rc := findReady(fetched.Status.Conditions)
			g.Expect(rc).NotTo(BeNil())
			g.Expect(rc.Status).To(Equal(metav1.ConditionFalse))
			g.Expect(rc.Reason).To(Equal(conditions.ReasonSecretNotFound))
			g.Expect(fetched.Status.LastApplied.IsZero()).To(BeTrue())
		}, timeout, interval).Should(Succeed())
	})

	It("should set InvalidSecretKey when ApplicationCollection Secret key is missing", func() {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "appcoll-wrong-key",
				Namespace: "aif",
			},
			Data: map[string][]byte{
				"wrongkey": []byte("value"),
			},
		}
		Expect(k8sClient.Create(ctx, secret)).To(Succeed())

		settings := &aifv1.Settings{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "invalid-appcoll-key",
				Namespace: "aif",
			},
			Spec: aifv1.SettingsSpec{
				ApplicationCollection: &aifv1.ApplicationCollectionConfig{
					UserSecretRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "appcoll-wrong-key"},
						Key:                  "user",
					},
				},
				SUSERegistry: &aifv1.SUSERegistryConfig{},
				Fleet:        &aifv1.FleetConfig{},
			},
		}
		Expect(k8sClient.Create(ctx, settings)).To(Succeed())

		Eventually(func(g Gomega) {
			var fetched aifv1.Settings
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(settings), &fetched)).To(Succeed())
			rc := findReady(fetched.Status.Conditions)
			g.Expect(rc).NotTo(BeNil())
			g.Expect(rc.Status).To(Equal(metav1.ConditionFalse))
			g.Expect(rc.Reason).To(Equal(conditions.ReasonInvalidSecretKey))
			g.Expect(fetched.Status.LastApplied.IsZero()).To(BeTrue())
		}, timeout, interval).Should(Succeed())
	})

	It("should accept nil SecretKeyRefs (optional fields)", func() {
		settings := &aifv1.Settings{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "optional-settings",
				Namespace: "aif",
			},
			Spec: aifv1.SettingsSpec{
				ApplicationCollection: &aifv1.ApplicationCollectionConfig{
					UserSecretRef:  nil,
					TokenSecretRef: nil,
				},
				SUSERegistry: &aifv1.SUSERegistryConfig{
					UserSecretRef:  nil,
					TokenSecretRef: nil,
				},
				Fleet: &aifv1.FleetConfig{
					CredSecretRef: nil,
				},
			},
		}
		Expect(k8sClient.Create(ctx, settings)).To(Succeed())

		Eventually(func(g Gomega) {
			var fetched aifv1.Settings
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(settings), &fetched)).To(Succeed())
			rc := findReady(fetched.Status.Conditions)
			g.Expect(rc).NotTo(BeNil())
			g.Expect(rc.Status).To(Equal(metav1.ConditionTrue))
			g.Expect(fetched.Status.LastApplied.IsZero()).To(BeFalse())
		}, timeout, interval).Should(Succeed())
	})

	It("should add and remove finalizer on deletion", func() {
		settings := &aifv1.Settings{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "finalizer-settings",
				Namespace: "aif",
			},
			Spec: aifv1.SettingsSpec{
				ApplicationCollection: &aifv1.ApplicationCollectionConfig{},
				SUSERegistry:          &aifv1.SUSERegistryConfig{},
				Fleet:                 &aifv1.FleetConfig{},
			},
		}
		Expect(k8sClient.Create(ctx, settings)).To(Succeed())

		// Wait for finalizer
		Eventually(func(g Gomega) {
			var fetched aifv1.Settings
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(settings), &fetched)).To(Succeed())
			g.Expect(fetched.Finalizers).To(ContainElement("ai.suse.com/cleanup"))
		}, timeout, interval).Should(Succeed())

		// Delete
		Expect(k8sClient.Delete(ctx, settings)).To(Succeed())

		// Wait for full deletion
		Eventually(func() bool {
			var fetched aifv1.Settings
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(settings), &fetched)
			return errors.IsNotFound(err)
		}, timeout, interval).Should(BeTrue())
	})

	It("should set SecretNotFound when SUSERegistry Secret is missing", func() {
		settings := &aifv1.Settings{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "missing-registry-settings",
				Namespace: "aif",
			},
			Spec: aifv1.SettingsSpec{
				ApplicationCollection: &aifv1.ApplicationCollectionConfig{},
				SUSERegistry: &aifv1.SUSERegistryConfig{
					TokenSecretRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "nonexistent-registry-secret"},
						Key:                  "token",
					},
				},
				Fleet: &aifv1.FleetConfig{},
			},
		}
		Expect(k8sClient.Create(ctx, settings)).To(Succeed())

		Eventually(func(g Gomega) {
			var fetched aifv1.Settings
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(settings), &fetched)).To(Succeed())
			rc := findReady(fetched.Status.Conditions)
			g.Expect(rc).NotTo(BeNil())
			g.Expect(rc.Reason).To(Equal(conditions.ReasonSecretNotFound))
		}, timeout, interval).Should(Succeed())
	})

	It("should set SecretNotFound when Fleet Secret is missing", func() {
		settings := &aifv1.Settings{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "missing-fleet-settings",
				Namespace: "aif",
			},
			Spec: aifv1.SettingsSpec{
				ApplicationCollection: &aifv1.ApplicationCollectionConfig{},
				SUSERegistry:          &aifv1.SUSERegistryConfig{},
				Fleet: &aifv1.FleetConfig{
					CredSecretRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "nonexistent-fleet-secret"},
						Key:                  "cred",
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, settings)).To(Succeed())

		Eventually(func(g Gomega) {
			var fetched aifv1.Settings
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(settings), &fetched)).To(Succeed())
			rc := findReady(fetched.Status.Conditions)
			g.Expect(rc).NotTo(BeNil())
			g.Expect(rc.Reason).To(Equal(conditions.ReasonSecretNotFound))
		}, timeout, interval).Should(Succeed())
	})

	It("should set InvalidSecretKey when SUSERegistry key is missing", func() {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "registry-wrong-key",
				Namespace: "aif",
			},
			Data: map[string][]byte{
				"wrongkey": []byte("value"),
			},
		}
		Expect(k8sClient.Create(ctx, secret)).To(Succeed())

		settings := &aifv1.Settings{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "invalid-registry-key",
				Namespace: "aif",
			},
			Spec: aifv1.SettingsSpec{
				ApplicationCollection: &aifv1.ApplicationCollectionConfig{},
				SUSERegistry: &aifv1.SUSERegistryConfig{
					UserSecretRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "registry-wrong-key"},
						Key:                  "user",
					},
				},
				Fleet: &aifv1.FleetConfig{},
			},
		}
		Expect(k8sClient.Create(ctx, settings)).To(Succeed())

		Eventually(func(g Gomega) {
			var fetched aifv1.Settings
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(settings), &fetched)).To(Succeed())
			rc := findReady(fetched.Status.Conditions)
			g.Expect(rc).NotTo(BeNil())
			g.Expect(rc.Reason).To(Equal(conditions.ReasonInvalidSecretKey))
		}, timeout, interval).Should(Succeed())
	})

	It("should set InvalidSecretKey when Fleet key is missing", func() {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "fleet-wrong-key",
				Namespace: "aif",
			},
			Data: map[string][]byte{
				"wrongkey": []byte("value"),
			},
		}
		Expect(k8sClient.Create(ctx, secret)).To(Succeed())

		settings := &aifv1.Settings{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "invalid-fleet-key",
				Namespace: "aif",
			},
			Spec: aifv1.SettingsSpec{
				ApplicationCollection: &aifv1.ApplicationCollectionConfig{},
				SUSERegistry:          &aifv1.SUSERegistryConfig{},
				Fleet: &aifv1.FleetConfig{
					CredSecretRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "fleet-wrong-key"},
						Key:                  "cred",
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, settings)).To(Succeed())

		Eventually(func(g Gomega) {
			var fetched aifv1.Settings
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(settings), &fetched)).To(Succeed())
			rc := findReady(fetched.Status.Conditions)
			g.Expect(rc).NotTo(BeNil())
			g.Expect(rc.Reason).To(Equal(conditions.ReasonInvalidSecretKey))
		}, timeout, interval).Should(Succeed())
	})
})
