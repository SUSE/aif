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
	"encoding/base64"
	"strings"
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const testPEM = "-----BEGIN CERTIFICATE-----\nMIIBtest\n-----END CERTIFICATE-----\n"

func resolveRepoSpec(t *testing.T, spec map[string]any) (clusterRepoInfo, error) {
	t.Helper()
	scheme := gitRepoTestScheme()
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(repoObj("repo", spec)).Build()
	r := &AIWorkloadReconciler{Client: cl, Scheme: scheme}
	return r.resolveClusterRepo(context.Background(), "repo")
}

func TestResolveClusterRepo_ReadsTLSFields(t *testing.T) {
	info, err := resolveRepoSpec(t, map[string]any{
		"url":                   "https://charts.example.com",
		"caBundle":              base64.StdEncoding.EncodeToString([]byte(testPEM)),
		"insecureSkipTLSVerify": true,
	})
	if err != nil {
		t.Fatalf("resolveClusterRepo: %v", err)
	}
	if string(info.CABundle) != testPEM {
		t.Fatalf("CABundle = %q, want the decoded PEM", info.CABundle)
	}
	if !info.InsecureSkipTLSVerify || info.InsecurePlainHTTP {
		t.Fatalf("InsecureSkipTLSVerify=%v InsecurePlainHTTP=%v, want true/false", info.InsecureSkipTLSVerify, info.InsecurePlainHTTP)
	}
}

// Git overlays are cloned directly, so a git-backed repo's TLS settings matter too.
func TestResolveClusterRepo_GitReadsTLSFields(t *testing.T) {
	info, err := resolveRepoSpec(t, map[string]any{
		"gitRepo":               "https://git.example.com/overlays.git",
		"caBundle":              base64.StdEncoding.EncodeToString([]byte(testPEM)),
		"insecureSkipTLSVerify": true,
	})
	if err != nil {
		t.Fatalf("resolveClusterRepo: %v", err)
	}
	if info.Kind != repoKindGit || string(info.CABundle) != testPEM || !info.InsecureSkipTLSVerify {
		t.Fatalf("kind=%v CABundle=%q InsecureSkipTLSVerify=%v, want git/PEM/true", info.Kind, info.CABundle, info.InsecureSkipTLSVerify)
	}
}

func TestResolveClusterRepo_OCIPlainHTTP(t *testing.T) {
	info, err := resolveRepoSpec(t, map[string]any{"url": "oci://registry.example.com/charts", "insecurePlainHttp": true})
	if err != nil {
		t.Fatalf("resolveClusterRepo: %v", err)
	}
	if info.Kind != repoKindOCI || !info.InsecurePlainHTTP {
		t.Fatalf("kind=%v InsecurePlainHTTP=%v, want oci/true", info.Kind, info.InsecurePlainHTTP)
	}
}

func TestResolveClusterRepo_NoTLSFieldsLeavesZeroValues(t *testing.T) {
	info, err := resolveRepoSpec(t, map[string]any{"url": "https://charts.example.com"})
	if err != nil {
		t.Fatalf("resolveClusterRepo: %v", err)
	}
	if info.CABundle != nil || info.InsecureSkipTLSVerify || info.InsecurePlainHTTP {
		t.Fatalf("TLS fields = %v/%v/%v, want zero values", info.CABundle, info.InsecureSkipTLSVerify, info.InsecurePlainHTTP)
	}
}

func TestResolveClusterRepo_InvalidCABundleErrors(t *testing.T) {
	_, err := resolveRepoSpec(t, map[string]any{"url": "https://charts.example.com", "caBundle": "not base64!"})
	if err == nil || !strings.Contains(err.Error(), "caBundle") {
		t.Fatalf("want an error naming caBundle, got %v", err)
	}
}
