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

package helm

import (
	"context"
	"errors"
	"testing"
	"time"
)

// The grace is short enough to keep the suite fast and long enough that a
// scheduling hiccup cannot be mistaken for expiry.
const testGrace = 120 * time.Millisecond

type graceKey struct{}

// TestShutdownGraceOutlivesTheParentCancellation is the whole point of the
// helper: SIGTERM must stop being an instant verdict on a Helm operation.
//
// Both Helm write paths race the caller's context against their own work and
// resolve a cancellation by marking the release failed — Upgrade through
// handleContext, Install through the select in performInstallCtx. Neither waits
// to find out whether the apply was about to succeed. Since the manager cancels
// every reconcile context at SIGTERM, restarting the operator during an upgrade
// recorded `failed: context canceled` against a chart that was fine.
func TestShutdownGraceOutlivesTheParentCancellation(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.Background())
	ctx, cancel := withShutdownGrace(parent, testGrace)
	defer cancel()

	cancelParent()
	time.Sleep(testGrace / 3)

	if err := ctx.Err(); err != nil {
		t.Fatalf("ctx.Err() = %v immediately after the parent was cancelled; the Helm "+
			"operation must get its grace to finish rather than being failed on the spot", err)
	}
}

// TestShutdownGraceExpiresAfterTheGrace is the other half, and the reason this
// is not simply context.WithoutCancel.
//
// Detaching outright would leave nothing to stop the operation, so an apply
// that outlasted the manager's drain would be killed with the process — and
// Helm writes the new revision to storage as pending *before* it applies
// anything, so a hard kill leaves the release wedged in a pending state that
// no reconcile recovers from. Expiring on our own terms lands on `failed`
// instead, which the next pass simply retries.
func TestShutdownGraceExpiresAfterTheGrace(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.Background())
	ctx, cancel := withShutdownGrace(parent, testGrace)
	defer cancel()

	cancelParent()

	select {
	case <-ctx.Done():
		if !errors.Is(ctx.Err(), context.Canceled) {
			t.Errorf("ctx.Err() = %v, want context.Canceled", ctx.Err())
		}
	case <-time.After(testGrace * 8):
		t.Fatal("ctx never expired after the parent was cancelled; an apply that " +
			"outlasts the drain would be SIGKILLed mid-write and wedge the release")
	}
}

// TestShutdownGraceIgnoresAnUncancelledParent guards the case that is not a
// shutdown at all. A normal upgrade is allowed the action's own Timeout, ten
// minutes, because chart hooks legitimately take that long. The grace must
// start counting at cancellation, not at creation — which is exactly why this
// cannot be a context.WithTimeout.
func TestShutdownGraceIgnoresAnUncancelledParent(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.Background())
	defer cancelParent()

	ctx, cancel := withShutdownGrace(parent, testGrace)
	defer cancel()

	time.Sleep(testGrace * 3)

	if err := ctx.Err(); err != nil {
		t.Errorf("ctx.Err() = %v with the parent still live; the grace clock must not "+
			"run until shutdown starts, or every slow-but-healthy upgrade fails", err)
	}
}

// TestShutdownGraceKeepsParentValues covers what would silently regress if
// someone simplified this to context.Background(): the logger and any trace
// state ride on the context, and Helm's own logging is wired through it.
func TestShutdownGraceKeepsParentValues(t *testing.T) {
	parent := context.WithValue(context.Background(), graceKey{}, "carried")

	ctx, cancel := withShutdownGrace(parent, testGrace)
	defer cancel()

	if got := ctx.Value(graceKey{}); got != "carried" {
		t.Errorf("ctx.Value = %v, want \"carried\"; detaching from cancellation must not "+
			"detach from the context's values", got)
	}
}

// TestShutdownGraceCancelIsImmediate keeps the returned cancel honest. Callers
// defer it on every path, including the happy one, and it is what releases the
// helper's goroutine — a cancel that only took effect after the grace would
// leak one per Helm operation.
func TestShutdownGraceCancelIsImmediate(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.Background())
	defer cancelParent()

	ctx, cancel := withShutdownGrace(parent, time.Hour)
	cancel()

	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("cancel() did not take effect; the helper's goroutine outlives the call")
	}
}
