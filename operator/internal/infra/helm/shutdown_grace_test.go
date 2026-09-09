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

	"go.uber.org/goleak"
)

// The grace is short enough to keep the suite fast and long enough that a
// scheduling hiccup cannot be mistaken for expiry.
const testGrace = 120 * time.Millisecond

type graceKey struct{}

// noGoroutineLeak fails the test if withShutdownGrace's goroutine is still
// running when it ends.
//
// Every test below asserts on a context, and a context is exactly the wrong
// instrument for this: the helper hands back a real cancel, so ctx.Done()
// closes whether or not the goroutine watching it ever returns. The whole suite
// would pass over a helper that parked a goroutine per Helm operation forever.
//
// That is not hypothetical. The goroutine blocks on parent.Done() first, and
// the parent here is the manager's reconcile context — one that outlives every
// individual operation and, on the steady-state health-check path, is only
// cancelled at shutdown. A missed ctx.Done() arm in either select accumulates
// one goroutine per reconcile, and the operator reconciles on a 60s timer.
//
// t.Cleanup rather than defer, so it runs after the test's own cancel() has.
func noGoroutineLeak(t *testing.T) {
	t.Helper()
	t.Cleanup(func() { goleak.VerifyNone(t) })
}

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
	noGoroutineLeak(t)

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
	noGoroutineLeak(t)

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
	noGoroutineLeak(t)

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
	noGoroutineLeak(t)

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
//
// The parent stays live here, so this only ever exercises the ctx.Done() arm of
// the *first* select. The second one is
// TestShutdownGraceStopsWaitingWhenTheWorkFinishesDuringTheGrace.
func TestShutdownGraceCancelIsImmediate(t *testing.T) {
	noGoroutineLeak(t)

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

// TestShutdownGraceStopsWaitingWhenTheWorkFinishesDuringTheGrace covers the
// ctx.Done() arm of the second select — the one reached only after the parent
// has been cancelled, when a Helm write finishes inside its grace window. That
// is the ordinary shutdown case, not a corner: the grace is sized so the write
// normally lands well inside it.
//
// Without the arm the goroutine sleeps out the whole grace and then calls
// cancel() on a context whose operation already returned. Helm parks a goroutine
// of its own on that context for the life of the operation — handleContext, in
// upgrade.go — and it answers cancellation by *sending* the failure report back
// to performUpgrade. Cancel after performUpgrade has returned and there is no
// longer anyone receiving, so Helm's goroutine blocks on that send and never
// comes back. A permanent leak inside a dependency, caused from here.
//
// An hour of grace, deliberately. The other tests run on a 120ms testGrace,
// which lets a goroutine that wrongly sleeps out the grace finish before goleak
// looks — measured at catching the missing arm in only about half of runs, and
// `make test` does not pass -race. At an hour there is no ambiguity in either
// direction: with the arm the goroutine is gone at once, without it goleak is
// certain to still find it parked. Nothing here waits on the clock, so the test
// costs milliseconds.
func TestShutdownGraceStopsWaitingWhenTheWorkFinishesDuringTheGrace(t *testing.T) {
	noGoroutineLeak(t)

	parent, cancelParent := context.WithCancel(context.Background())

	ctx, cancel := withShutdownGrace(parent, time.Hour)

	// Shutdown starts, so the goroutine moves past the first select and begins
	// waiting on the grace timer.
	cancelParent()
	select {
	case <-ctx.Done():
		t.Fatal("ctx was cancelled with the parent; the grace never applied")
	case <-time.After(testGrace):
	}

	// The write finishes and the caller releases the context, as every caller
	// does. The goroutine must give up on the timer here rather than outlive it.
	cancel()

	if err := ctx.Err(); !errors.Is(err, context.Canceled) {
		t.Fatalf("ctx.Err() = %v after cancel(), want context.Canceled", err)
	}

	// goleak, in the cleanup noGoroutineLeak registered, is what actually makes
	// the assertion: ctx.Done() is closed by cancel() whether or not the
	// goroutine watching it ever returned.
}
