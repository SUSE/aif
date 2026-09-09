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
	"time"
)

// ShutdownGrace is how long a Helm write may keep running after the operator
// has been told to shut down.
//
// Sized against two numbers it has to sit between. Below it, the work: an
// observed apply on a live cluster takes about two seconds, so ten leaves room
// for a slow API server without being a wait anyone notices. Above it, the
// manager's drain, which must still be waiting when this expires — see
// withShutdownGrace, and cmd.TestHelmGraceExpiresBeforeTheDrainGivesUp for the
// assertion, which lives there because only cmd can see both numbers.
//
// The drain is only above it on the shutdown paths that have one, and a *lost*
// lease is not one. controller-runtime's OnStoppedLeading sets
// gracefulShutdownTimeout to zero before pushing "leader election lost" onto
// the manager's error channel, so there is no drain at all: the process exits
// with the write still running, which is the killed-mid-write, release-left-
// pending case this grace exists to avoid, reached by another road. Nothing
// here can close that — by then another operator may already hold the lease,
// and waiting would put two writers on one release. It takes losing the lease
// to reach, which is an API-server partition or a stalled process rather than
// an ordinary rollout.
const ShutdownGrace = 10 * time.Second

// withShutdownGrace derives a context that shutdown no longer cancels outright,
// but does put on a clock.
//
// Both Helm write paths treat a cancelled context as a verdict on the release.
// Upgrade runs handleContext alongside the apply and answers cancellation with
// failRelease; Install does the same thing through the select in
// performInstallCtx, whose result RunWithContext hands to failRelease. Neither
// waits to see whether the apply was about to succeed, and the manager cancels
// every reconcile context at SIGTERM — so restarting the operator during an
// upgrade recorded `failed: context canceled` against a chart that was fine,
// and the same was reachable, more rarely, on a first install.
//
// Detaching completely — Run, or a bare context.WithoutCancel — fixes that and
// introduces something worse. Both paths write the new revision to storage in a
// pending state *before* applying anything (upgrade.go:375, install.go:404).
// Cancellation is what converts that pending record into `failed`, which the
// next reconcile simply retries. With nothing left to cancel it, an operation
// that outlasts the manager's drain is killed with the process, and the release
// stays pending — a state Helm's own IsPending covers but no reconcile here
// recovers from, since it is Helm that refuses to act on it.
//
// So: detached from cancellation, still bounded by it. The clock starts when
// the parent is cancelled rather than when the context is created, which is
// what rules out context.WithTimeout — a normal upgrade is entitled to the
// action's full Timeout for its hooks, and clamping every upgrade to the grace
// would fail slow, healthy ones.
//
// The caller must call cancel on every path; it is what releases the goroutine.
func withShutdownGrace(
	parent context.Context,
	grace time.Duration,
) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.WithoutCancel(parent))

	go func() {
		select {
		case <-parent.Done():
		case <-ctx.Done():
			// Finished, or the caller gave up. Either way the clock is moot.
			return
		}

		timer := time.NewTimer(grace)
		defer timer.Stop()

		select {
		case <-timer.C:
			cancel()
		case <-ctx.Done():
		}
	}()

	return ctx, cancel
}
