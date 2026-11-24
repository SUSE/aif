package settings

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	"github.com/SUSE/aif-operator/internal/credentials"
)

func TestEnqueueSettingsForSecret_MatchesReferencedSettingsSecrets(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = aiplatformv1alpha1.AddToScheme(scheme)

	settings := &aiplatformv1alpha1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: credentials.SettingsName, Namespace: "aif"},
		Spec: aiplatformv1alpha1.SettingsSpec{
			Fleet: aiplatformv1alpha1.FleetSettings{
				GitRepoSource: aiplatformv1alpha1.GitRepoSource{
					CredSecretRef:     &aiplatformv1alpha1.SecretKeyRef{Name: "git-creds", Key: "token"},
					CABundleSecretRef: &aiplatformv1alpha1.SecretKeyRef{Name: "git-ca", Key: "ca.crt"},
				},
			},
			ApplicationCollection: aiplatformv1alpha1.ApplicationCollectionSettings{
				CABundleSecretRef: &aiplatformv1alpha1.SecretKeyRef{Name: "appco-ca", Key: "ca.crt"},
			},
			SUSERegistry: aiplatformv1alpha1.SUSERegistrySettings{
				UserSecretRef:     &aiplatformv1alpha1.SecretKeyRef{Name: "suse-creds", Key: "username"},
				CABundleSecretRef: &aiplatformv1alpha1.SecretKeyRef{Name: "suse-ca", Key: "ca.crt"},
			},
			Nvidia: aiplatformv1alpha1.NvidiaSettings{
				CABundleSecretRef: &aiplatformv1alpha1.SecretKeyRef{Name: "nvidia-ca", Key: "ca.crt"},
			},
			RancherCatalog: aiplatformv1alpha1.RancherCatalogSettings{
				TokenSecretRef:    &aiplatformv1alpha1.SecretKeyRef{Name: "rc-token", Key: "token"},
				CABundleSecretRef: &aiplatformv1alpha1.SecretKeyRef{Name: "rc-ca", Key: "ca.crt"},
			},
			BlueprintCatalogs: []aiplatformv1alpha1.BlueprintCatalogSource{{
				Name: "partner-acme",
				GitRepoSource: aiplatformv1alpha1.GitRepoSource{
					CredSecretRef:     &aiplatformv1alpha1.SecretKeyRef{Name: "acme-git-creds", Key: "token"},
					CABundleSecretRef: &aiplatformv1alpha1.SecretKeyRef{Name: "acme-git-ca", Key: "ca.crt"},
				},
			}},
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(settings).Build()
	r := &SettingsReconciler{Client: cl, Scheme: scheme, OperatorNamespace: "aif"}

	cases := []struct {
		name      string
		secret    *corev1.Secret
		wantMatch bool
	}{
		{"token secret", &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "rc-token", Namespace: "aif"}}, false},
		{"ca bundle secret", &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "rc-ca", Namespace: "aif"}}, false},
		{"Git credential", &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "git-creds", Namespace: "aif"}}, true},
		{"Git CA bundle", &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "git-ca", Namespace: "aif"}}, true},
		{"AppCo CA bundle", &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "appco-ca", Namespace: "aif"}}, true},
		{"SUSE explicit credentials", &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "suse-creds", Namespace: "aif"}}, true},
		{"SUSE CA bundle", &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "suse-ca", Namespace: "aif"}}, true},
		{"NVIDIA CA bundle", &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "nvidia-ca", Namespace: "aif"}}, true},
		{"catalog credential", &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "acme-git-creds", Namespace: "aif"}}, true},
		{"catalog CA bundle", &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "acme-git-ca", Namespace: "aif"}}, true},
		{"unrelated secret", &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "nope", Namespace: "aif"}}, false},
		{"right name wrong namespace", &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "rc-token", Namespace: "other"}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reqs := r.enqueueSettingsForSecret(context.Background(), tc.secret)
			if got := len(reqs) > 0; got != tc.wantMatch {
				t.Fatalf("enqueued=%v, want %v (reqs=%v)", got, tc.wantMatch, reqs)
			}
		})
	}
}
