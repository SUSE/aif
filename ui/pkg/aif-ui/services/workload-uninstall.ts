// Local Helm cleanup uses the operator ServiceAccount. Fleet/GitOps cleanup
// uses Fleet. Direct downstream Helm still uses the caller's Rancher session;
// no personal token is minted, stored, or sent to the operator.
import { deleteApp, waitForAppUninstall } from './rancher-apps';
import { deleteAIWorkload } from '../utils/operator-api';
import type { AIWorkload } from '../types/aiworkload-types';
import { rancherHttpStatus } from '../utils/rancher-request';

export function isHelmStrategy(workload: AIWorkload): boolean {
  // Strategy is optional on the CR; App-sourced workloads default to Helm.
  const s = workload.spec.deployStrategy;
  return (s ?? 'Helm') === 'Helm';
}

export async function uninstallWorkload(store: any, workload: AIWorkload): Promise<void> {
  const { name, namespace } = workload.metadata;
  const clusters = workload.spec.targetClusters || [];
  const clusterId = clusters[0] || 'local';
  if (isHelmStrategy(workload) && clusters.length > 1) {
    throw new Error('Direct Helm uninstall requires a single Helm target.');
  }

  if (isHelmStrategy(workload) && clusterId !== 'local') {
    if (clusters.length !== 1 || !workload.metadata.uid) {
      throw new Error('Refresh this workload before uninstalling: its identity and single Helm target are required.');
    }
    const release = workload.spec.source.app?.release || name;
    try {
      await deleteApp(store, clusterId, workload.spec.targetNamespace, release);
    } catch (e) {
      if (rancherHttpStatus(e) !== 404) {
        throw e; // real failure — keep the CR so the user can retry
      }
    }
    // An accepted Rancher operation is asynchronous. Confirm disappearance
    // before allowing the local finalizer to release this workload.
    await waitForAppUninstall(store, clusterId, workload.spec.targetNamespace, release);
    const confirmation = [workload.metadata.uid, clusterId, workload.spec.targetNamespace, release].join('/');
    await deleteAIWorkload(namespace, name, confirmation);
    return;
  }

  await deleteAIWorkload(namespace, name);
}
