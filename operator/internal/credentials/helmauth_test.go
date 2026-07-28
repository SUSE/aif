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

package credentials_test

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	"github.com/SUSE/aif-operator/internal/credentials"
)

const chartURL = "oci://registry.example.com/charts/aif-ui-server"

func TestResolveHelmAuth_Nil(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	got, err := credentials.ResolveHelmAuth(context.Background(), c, "aif-operator", nil, chartURL)
	if err != nil || got != nil {
		t.Fatalf("nil auth: want (nil,nil), got (%+v,%v)", got, err)
	}
}

func TestResolveHelmAuth_Basic(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "creds", Namespace: "aif-operator"},
			Data:       map[string][]byte{"user": []byte("alice"), "token": []byte("s3cr3t")},
		},
	).Build()
	auth := &aiplatformv1alpha1.HelmAuth{Basic: &aiplatformv1alpha1.BasicAuth{
		UserSecretRef:  aiplatformv1alpha1.SecretKeyRef{Name: "creds", Key: "user"},
		TokenSecretRef: aiplatformv1alpha1.SecretKeyRef{Name: "creds", Key: "token"},
	}}
	got, err := credentials.ResolveHelmAuth(context.Background(), c, "aif-operator", auth, chartURL)
	if err != nil {
		t.Fatal(err)
	}
	if got.Username != "alice" || got.Password != "s3cr3t" {
		t.Fatalf("got %+v", got)
	}
}

func TestResolveHelmAuth_TokenDefaultUser(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "tok", Namespace: "aif-operator"},
			Data:       map[string][]byte{"token": []byte("nvapi-xyz")},
		},
	).Build()
	auth := &aiplatformv1alpha1.HelmAuth{Token: &aiplatformv1alpha1.TokenAuth{
		TokenSecretRef: aiplatformv1alpha1.SecretKeyRef{Name: "tok", Key: "token"},
	}}
	got, err := credentials.ResolveHelmAuth(context.Background(), c, "aif-operator", auth, chartURL)
	if err != nil {
		t.Fatal(err)
	}
	if got.Username != credentials.NvidiaDefaultUsername || got.Password != "nvapi-xyz" {
		t.Fatalf("got %+v", got)
	}
}

func TestResolveHelmAuth_DockerConfig(t *testing.T) {
	blob := []byte(`{"auths":{"registry.example.com":{"username":"bob","password":"pw"}}}`)
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "dc", Namespace: "aif-operator"},
			Type:       corev1.SecretTypeDockerConfigJson,
			Data:       map[string][]byte{corev1.DockerConfigJsonKey: blob},
		},
	).Build()
	auth := &aiplatformv1alpha1.HelmAuth{DockerConfig: &aiplatformv1alpha1.DockerConfigAuth{
		SecretRef: aiplatformv1alpha1.LocalSecretRef{Name: "dc"},
	}}
	got, err := credentials.ResolveHelmAuth(context.Background(), c, "aif-operator", auth, chartURL)
	if err != nil {
		t.Fatal(err)
	}
	if got.Username != "bob" || got.Password != "pw" {
		t.Fatalf("got %+v", got)
	}
}

func TestResolveHelmAuth_MissingSecret(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	auth := &aiplatformv1alpha1.HelmAuth{Basic: &aiplatformv1alpha1.BasicAuth{
		UserSecretRef:  aiplatformv1alpha1.SecretKeyRef{Name: "nope", Key: "user"},
		TokenSecretRef: aiplatformv1alpha1.SecretKeyRef{Name: "nope", Key: "token"},
	}}
	_, err := credentials.ResolveHelmAuth(context.Background(), c, "aif-operator", auth, chartURL)
	if err == nil {
		t.Fatal("expected error for missing secret")
	}
}
