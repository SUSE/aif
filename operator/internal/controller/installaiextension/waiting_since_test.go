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
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

// The deployment-readiness gate relies on the waiting-since marker: it is set
// when the deployment is first seen not-ready, drives the readiness timeout
// (time.Since(waitingSince) > ReadinessTimeout), and — since the deployment-ready
// path now clears it and continues in the same reconcile — must round-trip
// correctly so a *future* not-ready episode measures from a fresh timestamp
// rather than an old one.

func TestWaitingSince_SetThenClearRoundTrips(t *testing.T) {
	r := &InstallAIExtensionReconciler{}
	ext := &v1alpha1.InstallAIExtension{}

	// No annotation yet -> zero.
	if got := r.getWaitingSince(ext, annotationWaitingSince); !got.IsZero() {
		t.Fatalf("expected zero time before set, got %v", got)
	}

	// Set stamps ~now (RFC3339, second precision).
	r.setWaitingSince(ext, annotationWaitingSince)
	got := r.getWaitingSince(ext, annotationWaitingSince)
	if got.IsZero() {
		t.Fatalf("expected non-zero time after setWaitingSince")
	}
	if d := time.Since(got); d < -2*time.Second || d > time.Minute {
		t.Fatalf("expected waiting-since near now, got delta %v", d)
	}

	// Clear zeroes it and removes the annotation entirely (so a later not-ready
	// episode starts a fresh timeout window).
	r.clearWaitingSince(ext, annotationWaitingSince)
	if got := r.getWaitingSince(ext, annotationWaitingSince); !got.IsZero() {
		t.Fatalf("expected zero time after clearWaitingSince, got %v", got)
	}
	if _, present := ext.Annotations[annotationWaitingSince]; present {
		t.Fatalf("expected waiting-since annotation to be removed")
	}
}

func TestWaitingSince_InvalidOrMissingReturnsZero(t *testing.T) {
	r := &InstallAIExtensionReconciler{}

	// Unparseable timestamp -> treated as zero (no accidental instant timeout).
	bad := &v1alpha1.InstallAIExtension{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{annotationWaitingSince: "not-a-timestamp"},
		},
	}
	if got := r.getWaitingSince(bad, annotationWaitingSince); !got.IsZero() {
		t.Fatalf("expected zero time for invalid timestamp, got %v", got)
	}

	// A start time in the future is just as unusable: both callers measure with
	// time.Since, which goes negative, so the wait can never exceed its bound.
	// Zero instead, and the caller re-stamps from now.
	for _, key := range []string{annotationWaitingSince, annotationReleasePendingSince} {
		future := &v1alpha1.InstallAIExtension{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{key: time.Now().Add(time.Hour).Format(time.RFC3339)},
			},
		}
		if got := r.getWaitingSince(future, key); !got.IsZero() {
			t.Errorf("%s: expected zero time for a future timestamp, got %v", key, got)
		}
	}

	// Nil annotations must not panic and returns zero; clear on nil is a no-op.
	empty := &v1alpha1.InstallAIExtension{}
	if got := r.getWaitingSince(empty, annotationWaitingSince); !got.IsZero() {
		t.Fatalf("expected zero time for nil annotations, got %v", got)
	}
	r.clearWaitingSince(empty, annotationWaitingSince) // must not panic
}
