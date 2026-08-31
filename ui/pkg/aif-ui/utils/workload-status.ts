// Shared AIWorkload status presentation helpers. Extracted from AIWorkloads.vue
// so the workloads table and the detail modal render identical phase badges and
// surface the same failure message.
import type { AIWorkload, AIWorkloadPhase } from '../types/aiworkload-types';

export function phaseBadgeColor(phase: AIWorkloadPhase | string | undefined): string {
  switch (phase) {
    case 'Running':  return 'bg-success';
    case 'Degraded': return 'bg-warning';
    case 'Failed':   return 'bg-error';
    default:         return 'bg-info';
  }
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
// per-cluster message. Empty string when there's nothing to surface.
export function workloadStatusMessage(w: AIWorkload): string {
  const ready = (w.status?.conditions || []).find(
    (c: any) => c?.type === 'Ready' && c?.status === 'False',
  );
  if (ready?.message) return ready.message;
  const clusterMsg = (w.status?.clusterStatuses || []).find((s) => s.message);
  return clusterMsg?.message || '';
}
