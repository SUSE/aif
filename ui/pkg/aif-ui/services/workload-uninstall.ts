// Orchestrates uninstalling an AIWorkload. For the Helm (App) strategy the
// uninstall is performed via Rancher's catalog action=uninstall under the
// logged-in user's session (deleteApp), then the AIWorkload CR is removed. This
// mirrors how install works and how the Apps page uninstalls — the operator's
// finalizer is only a safety-net, so we must clear the release here.
//
// FleetBundle/GitOps workloads are torn down by the operator finalizer (it
// deletes the Fleet HelmOp/Bundle or the git file), so we only delete the CR.
import { deleteApp } from './rancher-apps';
import { deleteAIWorkload } from '../utils/operator-api';
import type { AIWorkload } from '../types/aiworkload-types';
import { isRancherError, type RancherError } from '../types/rancher-types';

export function isHelmStrategy(workload: AIWorkload): boolean {
  // Strategy is optional on the CR; App-sourced workloads default to Helm.
  const s = workload.spec.deployStrategy;
  return (s ?? 'Helm') === 'Helm';
}

// True when the error indicates the App/release is already gone — uninstall is
// then idempotently successful and we may proceed to delete the CR. Rancher
// surfaces the HTTP status differently depending on the transport: a top-level
// `status`/`code` (Steve store errors) or a nested `response.status`
// (axios-style), so we check every shape.
function isAlreadyGone(e: unknown): boolean {
  if (isRancherError(e) && (e.status ?? e.code) === 404) {
    return true;
  }
  // isRancherError only narrows on a numeric top-level status/code, so a 404
  // carried solely on the nested response object still needs its own check.
  return (e as RancherError | undefined)?.response?.status === 404;
}

export async function uninstallWorkload(store: any, workload: AIWorkload): Promise<void> {
  const { name, namespace } = workload.metadata;

  if (isHelmStrategy(workload)) {
    const release = workload.spec.source.app?.release || name;
    const clusterId = workload.spec.targetClusters?.[0] || 'local';
    try {
      await deleteApp(store, clusterId, workload.spec.targetNamespace, release);
    } catch (e) {
      if (!isAlreadyGone(e)) {
        throw e; // real failure — keep the CR so the user can retry
      }
    }
  }

  await deleteAIWorkload(namespace, name);
}
