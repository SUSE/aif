package workload

import "time"

// DefaultFailureThreshold is the consecutive-failure count at which a
// Degraded Workload (AutomaticRecovery.Enabled=true) transitions to
// RecoveryInProgress; P5-6 owns the rollback exit from RecoveryInProgress
// to either Running or Failed. With recovery disabled there is no Degraded
// intermediate at all — the first failed component routes directly to
// Failed. See ARCHITECTURE.md §4.4 rule 2.
//
// Matches the CRD kubebuilder default for
// spec.strategy.automaticRecovery.failureThreshold.
const DefaultFailureThreshold int32 = 3

// Per-phase requeue cadence from ARCHITECTURE.md §4.4. The controller
// picks one via requeueForPhase after every reconcile.
const (
	RequeuePending            = 30 * time.Second
	RequeueDeploying          = 30 * time.Second
	RequeueRunning            = 60 * time.Second
	RequeueDegraded           = 15 * time.Second
	RequeueFailed             = time.Duration(0) // no requeue; wait for spec change
	RequeueRecoveryInProgress = time.Duration(0) // no requeue; wait for recovery-complete event
)
