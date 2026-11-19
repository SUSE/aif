// Shared AIWorkload status presentation helpers. Extracted from AIWorkloads.vue
// so the workloads table and the detail drawer render identical phase badges and
// surface the same failure message.
import type { AIWorkload, AIWorkloadPhase } from '../types/aiworkload-types';
import type { Blueprint } from '../types/blueprint-types';

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

// Mirrors the operator: the current or the deployed Blueprint version can each disable rollback.
export function canRollback(w: AIWorkload, blueprints: Array<Blueprint | null | undefined>): boolean {
  return hasRollbackTarget(w) && !blueprints.some((bp) => bp?.spec.rollback === 'Disabled');
}
