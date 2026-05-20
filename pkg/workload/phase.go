// Package workload — phase machine.
//
// This file MUST remain aifv1-free per CLAUDE.md layering rules:
// RecomputePhase consumes a PhaseInput domain projection (built by
// conversions.PhaseInputFromCR), not a *aifv1.Workload.
package workload

// RecomputePhase is the canonical phase function. Pure: no ctx, no I/O,
// no clock, no logging. Called by the controller after every Deploy.
// Safe to call twice in one reconcile (for example, before and after
// incrementing RecoveryFailureCount) because it has no side effects.
//
// Rules per ARCHITECTURE.md §4.4 "Phase computation rules" (lines 723–729),
// first match wins:
//
//  1. No components yet                                       → Pending
//  2. Any component failed:
//     if RecoveryFailureCount >= FailureThreshold           → Failed
//     else                                                  → Degraded
//     (P5-2 will add the AutomaticRecovery branch that routes to
//     RecoveryInProgress between Degraded and Failed.)
//  3. Any component in pending-install / pending-upgrade /
//     pending-rollback / uninstalling / superseded /
//     orphan-uninstall-failed / unknown status               → Deploying
//  4. All components deployed AND ReadyReplicas >= DesiredReplicas
//     → Running
//  5. All components deployed AND ReadyReplicas < DesiredReplicas
//     → Degraded
//  6. Otherwise, preserve PriorPhase. The RecoveryInProgress path survives
//     across reconciles until rule 4 promotes it to Running or rule 2
//     demotes it to Degraded/Failed; P5-2 owns entry via the PDE watch.
func RecomputePhase(in PhaseInput) Phase {
	// Rule 1
	if len(in.Components) == 0 {
		return PhasePending
	}

	hasFailed := false
	hasInFlight := false
	allDeployed := true
	for _, c := range in.Components {
		switch c.Status {
		case "failed":
			hasFailed = true
			allDeployed = false
		case "deployed":
			// no-op
		case "pending-install", "pending-upgrade", "pending-rollback",
			"uninstalling", "superseded", ComponentStatusOrphanUninstallFailed:
			// Helm release statuses named in ARCHITECTURE.md §4.4 rule 3,
			// plus the AIF-internal orphan-uninstall-failed marker. All
			// classify as in-flight.
			hasInFlight = true
			allDeployed = false
		default:
			// Unknown helm statuses treated as in-flight defensively.
			hasInFlight = true
			allDeployed = false
		}
	}

	// Rule 2 (ARCHITECTURE.md §4.4 line 724) — failure beats in-flight.
	// First-match-wins ordering puts rule 2 (any failed) ahead of rule 3
	// (any pending-*), so a {failed, pending-install} mix surfaces as
	// Degraded/Failed, not Deploying. We do NOT wait for the in-flight
	// release to resolve before reporting the failure.
	if hasFailed {
		if in.RecoveryFailureCount >= in.FailureThreshold {
			return PhaseFailed
		}
		return PhaseDegraded
	}

	// Rule 3 (ARCHITECTURE.md §4.4 lines 727–729) — any pending-*,
	// uninstalling, superseded, orphan-uninstall-failed, or unknown
	// status surfaces as Deploying when no component has failed outright.
	if hasInFlight {
		return PhaseDeploying
	}

	// Rules 4 & 5
	if allDeployed {
		if in.ReadyReplicas >= in.DesiredReplicas {
			return PhaseRunning
		}
		return PhaseDegraded
	}

	// Rule 6
	if in.PriorPhase != "" {
		return in.PriorPhase
	}
	return PhasePending
}
