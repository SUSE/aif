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
	"os"
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/SUSE/aif-operator/internal/infra/helm"
)

// managerGracePeriod reads the grace period the operator's own Pod is given,
// from the chart that deploys it.
//
// Read rather than duplicated on purpose: the number this file reasons about
// is enforced by the kubelet, not by anything in Go, so a copy here would go
// stale the moment someone edits the chart — silently, and in the direction
// that reintroduces the SIGKILL.
func managerGracePeriod(t *testing.T) time.Duration {
	t.Helper()

	const chart = "../../charts/aif-operator/templates/manager/manager.yaml"

	manifest, err := os.ReadFile(chart)
	if err != nil {
		t.Fatalf("read %s: %v", chart, err)
	}

	m := regexp.MustCompile(`(?m)^\s*terminationGracePeriodSeconds:\s*(\d+)\s*$`).
		FindSubmatch(manifest)
	if m == nil {
		t.Fatalf("no terminationGracePeriodSeconds in %s; this test cannot check the "+
			"budget it exists to check", chart)
	}

	seconds, err := strconv.Atoi(string(m[1]))
	if err != nil {
		t.Fatalf("parse terminationGracePeriodSeconds %q: %v", m[1], err)
	}

	return time.Duration(seconds) * time.Second
}

// TestShutdownFitsInTheGracePeriod pins the budget that makes releasing the
// leader lease on shutdown safe.
//
// Releasing the lease lets the incoming operator take over in about a second
// instead of waiting out the ~15s lease expiry. It is not free: it is a live
// Get+Update against the API server, and the manager blocks on it after every
// runnable has stopped. client-go bounds it by RenewDeadline, so it can cost
// ten seconds — and it costs the most exactly when the API server is slow,
// which is also when the pod is most likely being drained.
//
// The manager therefore spends its grace period twice: once draining
// reconciles, once handing back the lease. Left at the controller-runtime
// default, the drain alone is 30s and consumes the whole period, so the
// release pushes the pod past it and the kubelet SIGKILLs it mid-work. A Helm
// upgrade killed that way leaves the release pending-upgrade, which is the
// state the surrounding fix exists to prevent.
func TestShutdownFitsInTheGracePeriod(t *testing.T) {
	grace := managerGracePeriod(t)

	if total := managerGracefulShutdownTimeout + leaseReleaseBudget; total > grace {
		t.Errorf("shutdown budget = %s (drain %s + lease release %s), grace period = %s; "+
			"the operator would be SIGKILLed before it finished shutting down",
			total, managerGracefulShutdownTimeout, leaseReleaseBudget, grace)
	}
}

// TestDrainOutlastsAReconcilePass keeps the drain from being shrunk into
// uselessness by a future edit chasing headroom.
//
// The budget above has two terms and only one of them is ours to tune, so the
// tempting fix for any grace-period pressure is to cut the drain. Cut it far
// enough and every shutdown truncates the pass it was meant to let finish,
// which trades a rare SIGKILL for a guaranteed one.
//
// Uninstall is the binding case. action.Uninstall.Run takes no context at all,
// so the drain is the *only* thing bounding it, and a kill partway through
// leaves the release in `uninstalling` — a state Helm's IsPending does not
// cover, so the pending-release recovery will not pick it up either.
func TestDrainOutlastsAReconcilePass(t *testing.T) {
	const shortestUsefulDrain = 10 * time.Second

	if managerGracefulShutdownTimeout < shortestUsefulDrain {
		t.Errorf("managerGracefulShutdownTimeout = %s, want >= %s; below this the drain "+
			"cannot finish an in-flight pass and every shutdown cuts one short",
			managerGracefulShutdownTimeout, shortestUsefulDrain)
	}
}

// TestHelmGraceExpiresBeforeTheDrainGivesUp pins the ordering the Helm grace
// depends on, from the side that can see both numbers.
//
// helm.ShutdownGrace lets a Helm write keep going after SIGTERM so a restart
// stops being recorded as a failed release. That only helps while the manager
// is still waiting for the reconcile worker holding it. Invert the two and the
// helper becomes actively harmful: the drain gives up first, the process exits,
// and the write is killed rather than cancelled — leaving the revision in the
// pending state Helm wrote before applying, which is worse than the `failed`
// the grace was introduced to avoid.
func TestHelmGraceExpiresBeforeTheDrainGivesUp(t *testing.T) {
	if helm.ShutdownGrace >= managerGracefulShutdownTimeout {
		t.Errorf("helm.ShutdownGrace = %s, drain = %s; the grace must expire while the "+
			"manager is still waiting, or the write is killed instead of cancelled",
			helm.ShutdownGrace, managerGracefulShutdownTimeout)
	}
}
