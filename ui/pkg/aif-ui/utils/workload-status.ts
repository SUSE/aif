// Shared AIWorkload status presentation helpers. Extracted from AIWorkloads.vue
// so the workloads table and the detail drawer render identical phase badges and
// surface the same failure message.
import type { AIWorkload, AIWorkloadPhase } from '../types/aiworkload-types';
import type { Blueprint } from '../types/blueprint-types';
import { semverCompare } from './blueprint-api';

// A subset of the shell's StateColor, so StateDot and BadgeState agree on a phase.
export type PhaseColor = 'success' | 'warning' | 'error' | 'info';

export function phaseColor(phase: AIWorkloadPhase | string | undefined): PhaseColor {
  switch (phase) {
    case 'Running':  return 'success';
    case 'Degraded': return 'warning';
    case 'Failed':   return 'error';
    default:         return 'info';
  }
}

export function phaseBadgeColor(phase: AIWorkloadPhase | string | undefined): string {
  return `bg-${ phaseColor(phase) }`;
}

export function phaseBadgeIcon(phase: AIWorkloadPhase | string | undefined): string {
  switch (phase) {
    case 'Running':  return 'icon-checkmark';
    case 'Degraded': return 'icon-warning';
    case 'Failed':   return 'icon-x';
    default:         return 'icon-info';
  }
}

// workloadStatusMessage returns a human-readable reason when a workload is not
// healthy: the Ready=False condition message (set by the operator when a
// ClusterRepo can't be resolved), falling back to the first non-empty
// per-cluster message. Empty string when there's nothing to surface. Running
// workloads suppress the message — the success text ("Helm install complete…")
// is noise once the badge already says Running.
export function workloadStatusMessage(w: AIWorkload): string {
  if (w.status?.phase === 'Running') return '';
  const ready = (w.status?.conditions || []).find(
    (c: any) => c?.type === 'Ready' && c?.status === 'False',
  );
  if (ready?.message) return ready.message;
  const clusterMsg = (w.status?.clusterStatuses || []).find((s) => s.message);
  return clusterMsg?.message || '';
}

export function hasRollbackTarget(w: AIWorkload): boolean {
  const ds = w.status?.deployedSource;

  return !!ds && !!w.spec.source.blueprint && ds.version !== w.spec.source.blueprint.version;
}

// The Blueprint version format the CRD accepts.
const SEMVER = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(-[a-zA-Z0-9.-]+)?(\+[a-zA-Z0-9.-]+)?$/;

// Mirrors blueprintpolicy.IsRollback: to is older than from; non-semver is never a rollback.
export function isRollback(from: string | undefined, to: string | undefined): boolean {
  return !!from && !!to && SEMVER.test(from) && SEMVER.test(to) && semverCompare(to, from) < 0;
}

// Mirrors blueprintpolicy.Highest: the highest semver among versions, '' when none parses.
export function highestVersion(...versions: Array<string | undefined>): string {
  return versions.reduce<string>((best, v) => (v && SEMVER.test(v) && (!best || semverCompare(v, best) > 0) ? v : best), '');
}

// Mirrors blueprintpolicy.FloorFor: status.minimumSafeVersion is "<family>@<version>";
// a bare version applies to any family, another family's floor to none.
export function floorFor(stored: string | undefined, family: string): string {
  if (!stored) return '';
  const i = stored.lastIndexOf('@');
  if (i < 0) return stored;

  return stored.slice(0, i) === family ? stored.slice(i + 1) : '';
}

// The workload's recorded minimum safe version, '' when unset or for another Blueprint.
export function minimumSafeVersion(w: AIWorkload): string {
  return floorFor(w.status?.minimumSafeVersion, w.spec.source.blueprint?.name || '');
}

// The version to point the user at instead: Highest(floor, deployed), as the operator reports.
export function rollbackFloor(w: AIWorkload): string {
  return highestVersion(minimumSafeVersion(w), w.status?.deployedSource?.version);
}

// True when the deployed version is below the minimum safe version, so the operator refuses a rollback to it.
export function rollbackBelowMinimumSafe(w: AIWorkload): boolean {
  return isRollback(minimumSafeVersion(w), w.status?.deployedSource?.version);
}

// Mirrors the operator: the current or the deployed Blueprint version can each
// disable rollback, and the deployed version must not be below minimumSafeVersion.
export function canRollback(w: AIWorkload, blueprints: Array<Blueprint | null | undefined>): boolean {
  return hasRollbackTarget(w) && !rollbackBelowMinimumSafe(w) && !blueprints.some((bp) => bp?.spec.rollback === 'Disabled');
}

// Mirrors blueprintpolicy.MoveBlocked for a move to version to: below the
// minimum safe version, or a rollback from the deployed version that either
// version's Blueprint disables. blueprintFor returns the family's Blueprint at a
// version (null when unknown, which allows, as the operator fails open).
export function moveBlocked(w: AIWorkload, to: string, blueprintFor: (version: string) => Blueprint | null | undefined): boolean {
  if (isRollback(minimumSafeVersion(w), to)) return true;
  const deployed = w.status?.deployedSource?.version || '';
  if (!isRollback(deployed, to)) return false;

  return [deployed, to].some((v) => blueprintFor(v)?.spec.rollback === 'Disabled');
}
