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
// Rules per ARCHITECTURE.md §4.4 "Phase computation rules", first match wins:
//
//  1. No components yet                                       → Pending
//  2. Any component in pending-install / pending-upgrade /
//     uninstalling / orphan-uninstall-failed / unknown status → Deploying
//  3. Any component failed:
//     if RecoveryFailureCount >= FailureThreshold           → Failed
//     else                                                  → Degraded
//  4. All components deployed AND ReadyReplicas >= DesiredReplicas
//     → Running
//  5. All components deployed AND ReadyReplicas < DesiredReplicas
//     → Degraded
//  6. Otherwise, preserve PriorPhase. The RecoveryInProgress path survives
//     across reconciles until rule 4 promotes it to Running or rule 3
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
		case "pending-install", "pending-upgrade", "uninstalling", ComponentStatusOrphanUninstallFailed:
			hasInFlight = true
			allDeployed = false
		default:
			// Unknown helm statuses treated as in-flight.
			hasInFlight = true
			allDeployed = false
		}
	}

	// Rule 2 — in-flight wins over failed when no failure has actually
	// landed yet (matches the existing P4-2 ordering: pending beats deployed,
	// failed beats pending). But rule 3 in the spec says "any component
	// failed" → Degraded/Failed; the spec's first-match-wins ordering puts
	// rule 2 (pending) ahead of rule 3 (failed), so we honor that here:
	// in-flight surfaces as Deploying even if another component is failed,
	// because the in-flight one may resolve and clear the failure.
	if hasInFlight {
		return PhaseDeploying
	}

	// Rule 3
	if hasFailed {
		if in.RecoveryFailureCount >= in.FailureThreshold {
			return PhaseFailed
		}
		return PhaseDegraded
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
