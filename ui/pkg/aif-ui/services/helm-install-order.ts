export interface HelmInstallSteps {
  recordPending: (clusterId: string) => Promise<void>;
  install:       () => Promise<void>;
  recordResult:  (clusterId: string) => Promise<void>;
}

// installRecordingWorkloadsFirst records each cluster's AIWorkload before the
// Helm install starts. The operator delivers image pull secrets from the
// AIWorkload, so recording it first lets delivery run alongside the install
// instead of after the install wait. The final result is recorded even when
// the install throws.
export async function installRecordingWorkloadsFirst(clusterIds: string[], steps: HelmInstallSteps): Promise<void> {
  await Promise.all(clusterIds.map(id => steps.recordPending(id)));
  try {
    await steps.install();
  } finally {
    for (const id of clusterIds) {
      await steps.recordResult(id);
    }
  }
}
