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

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

// failingSince builds a CR that has been reporting Ready=False for d, which is
// the only input the backoff has. Backdating the stamp rather than injecting a
// clock is how the rest of this package tests its waits — see the pending-marker
// tests — and it keeps the seam out of production code.
func failingSince(d time.Duration) *v1alpha1.InstallAIExtension {
	ext := &v1alpha1.InstallAIExtension{}
	ext.Status.Conditions = []metav1.Condition{{
		Type:               conditionTypeReady,
		Status:             metav1.ConditionFalse,
		Reason:             "RegistryUnreachable",
		Message:            "dial tcp: no route to host",
		LastTransitionTime: metav1.NewTime(time.Now().Add(-d)),
	}}
	return ext
}

// near allows for the wall clock moving between building the CR and reading it.
func near(t *testing.T, got, want time.Duration, what string) {
	t.Helper()
	const tolerance = 5 * time.Second
	if got < want-tolerance || got > want+tolerance {
		t.Errorf("%s: RequeueAfter = %v, want ~%v", what, got, want)
	}
}

func TestFailureRetry_BacksOffWithTheAgeOfTheFailure(t *testing.T) {
	// The point of the change: a CR that has been failing for a while is retried
	// less often than one that just started. A flat healthCheckInterval passes the
	// first case and fails every other one.
	cases := []struct {
		failingFor time.Duration
		want       time.Duration
		why        string
	}{
		{0, healthCheckInterval, "a failure that just happened waits the floor"},
		{30 * time.Second, healthCheckInterval, "under the floor still waits the floor"},
		{2 * time.Minute, 2 * time.Minute, "waits about as long as it has been failing"},
		{8 * time.Minute, 8 * time.Minute, "and keeps doubling"},
		{maxFailureRetryInterval, maxFailureRetryInterval, "up to the cap"},
		{3 * time.Hour, maxFailureRetryInterval, "and never past it"},
		{30 * 24 * time.Hour, maxFailureRetryInterval, "not even after a month"},
	}

	for _, c := range cases {
		got := failureRetryInterval(failingSince(c.failingFor))
		near(t, got, c.want, c.why)
	}
}

func TestFailureRetry_IsNeverFlat(t *testing.T) {
	// Guards the property rather than the arithmetic: whatever the curve, an old
	// failure must be retried strictly less often than a fresh one. Reverting to a
	// constant fails here even if every boundary above were adjusted to match it.
	//
	// Through setFailureAndRetry, not failureRetryInterval, deliberately. The
	// regression this file exists to catch is the Result going back to a flat
	// healthCheckInterval, and a test that calls the helper directly still passes
	// while the caller ignores it entirely.
	fresh := setFailureAndRetry(failingSince(0),
		conditionTypeHelmInstalled, "PullFailed", "boom").RequeueAfter
	old := setFailureAndRetry(failingSince(10*time.Minute),
		conditionTypeHelmInstalled, "PullFailed", "boom").RequeueAfter

	if old <= fresh {
		t.Fatalf("a 10-minute-old failure requeues after %v, no less often than a fresh one at %v — "+
			"the backoff is flat", old, fresh)
	}
	if old > maxFailureRetryInterval {
		t.Fatalf("requeue %v exceeds the cap %v", old, maxFailureRetryInterval)
	}
}

func TestFailureRetry_RecoveryResetsTheBackoff(t *testing.T) {
	// A CR that recovered and then failed again is a new failure, not a
	// continuation of the old one — otherwise a flapping extension inherits the
	// widest interval it ever reached and stops being watched closely.
	ext := &v1alpha1.InstallAIExtension{}
	ext.Status.Conditions = []metav1.Condition{{
		Type:               conditionTypeReady,
		Status:             metav1.ConditionTrue,
		Reason:             "Installed",
		LastTransitionTime: metav1.NewTime(time.Now().Add(-2 * time.Hour)),
	}}

	// Ready flips True -> False here, so SetStatusCondition restamps it.
	result := setFailureAndRetry(ext, conditionTypeHelmInstalled, "UpgradeFailed", "boom")

	near(t, result.RequeueAfter, healthCheckInterval,
		"first failure after a two-hour-old success")
}

func TestFailureRetry_SustainedFailureKeepsItsAnchor(t *testing.T) {
	// The mirror-image case, and the one that makes the backoff work at all: a CR
	// still failing must NOT be restamped, or every pass would look like a first
	// failure and the interval would never widen. This is a property of
	// meta.SetStatusCondition, so it is asserted rather than assumed.
	ext := failingSince(6 * time.Minute)
	anchor := meta.FindStatusCondition(ext.Status.Conditions, conditionTypeReady).LastTransitionTime

	// A different reason and message, still False — a registry that changed its
	// mind about why it is broken.
	result := setFailureAndRetry(ext, conditionTypeHelmInstalled, "PullFailed", "unauthorized")

	after := meta.FindStatusCondition(ext.Status.Conditions, conditionTypeReady).LastTransitionTime
	if !after.Equal(&anchor) {
		t.Fatalf("LastTransitionTime moved from %v to %v while still False — "+
			"the backoff would restart on every pass", anchor, after)
	}
	near(t, result.RequeueAfter, 6*time.Minute, "a six-minute-old failure")
}

func TestFailureRetry_DegenerateStampsFallBackToTheFloor(t *testing.T) {
	// None of these should produce a negative or zero wait: a Result with
	// RequeueAfter <= 0 is not a delayed requeue at all, and would spin.
	cases := []struct {
		name string
		ext  *v1alpha1.InstallAIExtension
	}{
		{"no conditions at all", &v1alpha1.InstallAIExtension{}},
		{"a stamp in the future", failingSince(-30 * time.Minute)},
		{"a zero stamp", func() *v1alpha1.InstallAIExtension {
			ext := failingSince(time.Hour)
			ext.Status.Conditions[0].LastTransitionTime = metav1.Time{}
			return ext
		}()},
	}

	for _, c := range cases {
		got := failureRetryInterval(c.ext)
		if got != healthCheckInterval {
			t.Errorf("%s: interval = %v, want the floor %v", c.name, got, healthCheckInterval)
		}
	}
}
