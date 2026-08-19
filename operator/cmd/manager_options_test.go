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

package main

import (
	"testing"

	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

// The shutdown timings in main_test.go all reason about constants. Constants
// are not behaviour: managerGracefulShutdownTimeout can be perfectly budgeted
// against the Pod's grace period and still never reach the manager, and
// TestShutdownFitsInTheGracePeriod would go on passing. These tests close that
// gap by reading the options main hands to ctrl.NewManager.
func TestManagerHandsTheLeaseBackOnShutdown(t *testing.T) {
	opts := managerOptions(metricsserver.Options{}, nil, ":8081", true)

	if !opts.LeaderElectionReleaseOnCancel {
		t.Error("LeaderElectionReleaseOnCancel = false; the incoming operator would wait " +
			"out the ~15s lease expiry on every upgrade, with the new Pod healthy and " +
			"idle and any extension needing work left untouched")
	}
}

// The drain has to be the value the grace-period budget was computed from.
// Leaving it nil takes controller-runtime's own 30s default, which is the same
// number today — so the two agree by coincidence, and the first edit to the
// constant silently breaks the budget TestShutdownFitsInTheGracePeriod checks.
func TestManagerDrainIsTheBudgetedOne(t *testing.T) {
	opts := managerOptions(metricsserver.Options{}, nil, ":8081", true)

	if opts.GracefulShutdownTimeout == nil {
		t.Fatalf("GracefulShutdownTimeout is nil; the drain falls back to controller-runtime's "+
			"default rather than the %s the Pod's grace period was sized for",
			managerGracefulShutdownTimeout)
	}
	if *opts.GracefulShutdownTimeout != managerGracefulShutdownTimeout {
		t.Errorf("GracefulShutdownTimeout = %s, want %s; the constant the shutdown budget "+
			"is computed from is not the one the manager uses",
			*opts.GracefulShutdownTimeout, managerGracefulShutdownTimeout)
	}
}

// Leader election itself is a flag, so the only thing to pin is that the flag
// reaches the manager. Without this, the release-on-cancel test above is
// vacuous: a manager that never takes the lease has none to hand back.
func TestLeaderElectionFlagReachesTheManager(t *testing.T) {
	if opts := managerOptions(metricsserver.Options{}, nil, ":8081", true); !opts.LeaderElection {
		t.Error("LeaderElection = false with the flag on; the operator would run without " +
			"a lease and two replicas would reconcile the same extension")
	}
	if opts := managerOptions(metricsserver.Options{}, nil, ":8081", false); opts.LeaderElection {
		t.Error("LeaderElection = true with the flag off")
	}
}
