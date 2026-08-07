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
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	helmClient "github.com/SUSE/aif-operator/internal/infra/helm"
)

// A failed marker cleanup must not become the reconcile's answer. The caller
// treats a nil error from handlePendingRelease as "the release is not pending,
// deal with the EnsureRelease error yourself" — so returning the cleanup's error
// instead skips that entirely and the CR is left with no explanation of why the
// install failed, only an unrelated write conflict on the reconcile.
func TestHandlePendingRelease_KeepsTheInstallErrorWhenClearingTheMarkerFails(t *testing.T) {
	ext := helmExtension()
	markPendingSince(ext, time.Minute)

	installErr := errors.New("chart pull refused")
	stub := &stubHelmClient{
		deployed:  &helmClient.ReleaseInfo{Version: requestedVersion, Status: helmClient.StatusDeployed, Revision: 1},
		last:      &helmClient.ReleaseInfo{Version: requestedVersion, Status: helmClient.StatusFailed, Revision: 2},
		ensureErr: installErr,
	}
	writeErr := errors.New("the object has been modified")
	r := wiringReconcilerWith(t, ext, stub, interceptor.Funcs{
		Update: func(context.Context, client.WithWatch, client.Object, ...client.UpdateOption) error {
			return writeErr
		},
	})

	_, err := r.reconcileHelmSource(context.Background(), ext, wiringNamespace)

	if err != nil {
		t.Fatalf("reconcile error = %v, want nil; a marker cleanup failure is retried next pass, "+
			"it is not the outcome of the reconcile", err)
	}
	cond := meta.FindStatusCondition(ext.Status.Conditions, conditionTypeHelmInstalled)
	if cond == nil {
		t.Fatal("no HelmInstalled condition; the real install failure was never recorded")
	}
	if cond.Reason != "InstallFailed" {
		t.Errorf("HelmInstalled reason = %q, want InstallFailed", cond.Reason)
	}
	if !containsAll(cond.Message, installErr.Error()) {
		t.Errorf("HelmInstalled message = %q, want it to name the install failure %q",
			cond.Message, installErr)
	}
}

// With nothing else to report, the cleanup failure is the only signal there is,
// so it has to surface. Otherwise a marker that cannot be written stays put
// silently and shortens the next genuine wait.
func TestHandlePendingRelease_SurfacesTheClearFailureOnAnOtherwiseCleanPass(t *testing.T) {
	ext := helmExtension()
	markPendingSince(ext, time.Minute)

	stub := &stubHelmClient{
		deployed: &helmClient.ReleaseInfo{Version: requestedVersion, Status: helmClient.StatusDeployed, Revision: 2},
		last:     &helmClient.ReleaseInfo{Version: requestedVersion, Status: helmClient.StatusDeployed, Revision: 2},
	}
	writeErr := errors.New("the object has been modified")
	r := wiringReconcilerWith(t, ext, stub, interceptor.Funcs{
		Update: func(context.Context, client.WithWatch, client.Object, ...client.UpdateOption) error {
			return writeErr
		},
	})

	_, _, err := r.handlePendingRelease(context.Background(), ext, conditionTypeHelmInstalled, nil)

	if !errors.Is(err, writeErr) {
		t.Fatalf("error = %v, want the write failure; nothing else would report it", err)
	}
}
