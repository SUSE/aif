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
	stderrors "errors"
	"sync"
	"testing"

	helmClient "github.com/SUSE/aif-operator/internal/infra/helm"
)

// The convergence latch and the downloaded-chart cache both live on the Helm
// client. Building one per reconcile discards them before the next pass can
// read them, which restores the exact loop they were added to remove: latch
// written, client dropped, chart pulled again next minute to rediscover the
// same verdict.
//
// This is the seam where that regression hides. Every test in internal/infra/
// helm holds a single client across its passes, so all of them keep passing
// while the operator in a cluster pulls once a minute forever. Nothing below
// the controller can catch it.
func TestHelmForBuildsOneClientPerNamespace(t *testing.T) {
	var mu sync.Mutex
	builds := map[string]int{}

	r := &InstallAIExtensionReconciler{
		helmClientFor: func(ns string) (helmClient.HelmClient, error) {
			mu.Lock()
			defer mu.Unlock()
			builds[ns]++
			return &stubHelmClient{}, nil
		},
	}

	first, err := r.helmFor("ns-one")
	if err != nil {
		t.Fatalf("helmFor() error = %v", err)
	}

	// Stands in for the health-check timer re-reconciling a settled extension.
	for range 10 {
		again, err := r.helmFor("ns-one")
		if err != nil {
			t.Fatalf("helmFor() error = %v", err)
		}
		if again != first {
			t.Fatal("a later reconcile got a different client; the latch and chart " +
				"cache it carries are being thrown away between passes")
		}
	}

	if builds["ns-one"] != 1 {
		t.Errorf("built %d clients for one namespace over 11 reconciles, want 1", builds["ns-one"])
	}
}

// Namespaces must not share one: the client's cli.EnvSettings is scoped to a
// namespace, so a shared client would resolve releases in the wrong one.
func TestHelmForSeparatesNamespaces(t *testing.T) {
	r := &InstallAIExtensionReconciler{
		helmClientFor: func(string) (helmClient.HelmClient, error) {
			return &stubHelmClient{}, nil
		},
	}

	one, err := r.helmFor("ns-one")
	if err != nil {
		t.Fatalf("helmFor() error = %v", err)
	}
	two, err := r.helmFor("ns-two")
	if err != nil {
		t.Fatalf("helmFor() error = %v", err)
	}

	if one == two {
		t.Error("two namespaces were served the same client")
	}
}

// A build that fails must not be remembered as a failure, or one bad pass would
// wedge the namespace for the life of the process.
func TestHelmForDoesNotCacheAFailedBuild(t *testing.T) {
	fail := true
	r := &InstallAIExtensionReconciler{
		helmClientFor: func(string) (helmClient.HelmClient, error) {
			if fail {
				return nil, stderrors.New("registry client unavailable")
			}
			return &stubHelmClient{}, nil
		},
	}

	if _, err := r.helmFor("ns"); err == nil {
		t.Fatal("helmFor() succeeded, want the build error")
	}

	fail = false
	got, err := r.helmFor("ns")
	if err != nil {
		t.Fatalf("helmFor() after recovery error = %v", err)
	}
	if got == nil {
		t.Error("helmFor() returned no client after the build recovered")
	}
}

// The production path is the one that was broken, and it is the one the
// helmClientFor seam bypasses in tests, so it needs asserting directly.
// newHelmClientForNamespace builds a registry client and reads cli.EnvSettings;
// it does not contact a cluster, so this runs without envtest.
func TestHelmForCachesTheRealClient(t *testing.T) {
	r := &InstallAIExtensionReconciler{}

	first, err := r.helmFor("ns-one")
	if err != nil {
		t.Fatalf("helmFor() error = %v", err)
	}
	second, err := r.helmFor("ns-one")
	if err != nil {
		t.Fatalf("helmFor() error = %v", err)
	}

	if first != second {
		t.Error("the production path built a second client for the same namespace")
	}
}

// Two extensions in one namespace can reconcile at once. Whichever loses the
// race has to adopt the winner's client rather than install a rival whose latch
// starts empty — otherwise the two would alternate and neither would ever hold.
func TestHelmForIsRaceSafe(t *testing.T) {
	r := &InstallAIExtensionReconciler{
		helmClientFor: func(string) (helmClient.HelmClient, error) {
			return &stubHelmClient{}, nil
		},
	}

	const goroutines = 16
	got := make([]helmClient.HelmClient, goroutines)
	var wg sync.WaitGroup
	for i := range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, err := r.helmFor("ns-one")
			if err != nil {
				return
			}
			got[i] = c
		}()
	}
	wg.Wait()

	for i, c := range got {
		if c == nil {
			t.Fatalf("goroutine %d got no client", i)
		}
		if c != got[0] {
			t.Fatalf("goroutine %d got a different client than goroutine 0", i)
		}
	}
}
