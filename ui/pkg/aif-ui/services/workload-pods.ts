// Fetches a workload's chart-managed pods from each target cluster via Rancher's
// authenticated proxy and surfaces the unhealthy ones. UI-only; no operator changes.
import type { AIWorkload } from '../types/aiworkload-types';
import type { ClusterPodStatus, WorkloadPod, PodContainerIssue } from '../types/workload-pods-types';
import { getHelmReleaseFromLabels, isManagedByHelm } from '../config/labels-annotations';
import { capReleaseName } from '../utils/helm-release';
import { TIMEOUT_VALUES } from '../utils/constants';
import logger from '../utils/logger';

interface Dispatchable { dispatch(action: string, payload?: any): Promise<any>; }

interface RawContainerState {
  waiting?:    { reason?: string; message?: string };
  terminated?: { reason?: string; message?: string; exitCode?: number };
}
interface RawContainerStatus { name?: string; state?: RawContainerState; }
interface RawPod {
  metadata?: { name?: string; namespace?: string; labels?: Record<string, string> };
  status?:   { phase?: string; containerStatuses?: RawContainerStatus[]; initContainerStatuses?: RawContainerStatus[] };
}

// Every cluster — including the local management cluster — is addressed via the
// raw k8s API through Rancher's proxy. 'local' is a normal cluster id here
// (see Settings.vue:286, rancher-apps.ts:883). Normalize '' → 'local'.
function clusterIdOrLocal(clusterId: string): string {
  return clusterId === '' ? 'local' : clusterId;
}

// Expected app.kubernetes.io/instance values for this workload's own releases.
// Blueprint: capReleaseName(componentName) — componentName == chartName (see plan
// Global Constraints). App: the stored release name.
function expectedReleaseNames(w: AIWorkload): Set<string> {
  const names = new Set<string>();
  if (w.spec.source.sourceType === 'App') {
    const rel = w.spec.source.app?.release;
    // capReleaseName is identity for names ≤ 53 chars (App release names are capped
    // to 53 at install), so this is defensive symmetry with the operator, not a parity fix.
    if (rel) names.add(capReleaseName(rel));
    return names;
  }
  for (const cs of w.status?.componentStatuses || []) {
    if (cs.componentName) names.add(capReleaseName(cs.componentName));
  }
  return names;
}

function extractIssues(pod: RawPod): PodContainerIssue[] {
  const issues: PodContainerIssue[] = [];
  const scan = (list: RawContainerStatus[] | undefined, init: boolean) => {
    for (const cs of list || []) {
      const w = cs.state?.waiting;
      if (w?.reason) { issues.push({ container: cs.name || '', reason: w.reason, message: w.message, init }); continue; }
      const t = cs.state?.terminated;
      if (t && typeof t.exitCode === 'number' && t.exitCode !== 0) {
        issues.push({ container: cs.name || '', reason: t.reason || `Exited(${ t.exitCode })`, message: t.message, init });
      }
    }
  };
  scan(pod.status?.initContainerStatuses, true);
  scan(pod.status?.containerStatuses, false);
  return issues;
}

// A completed Job pod (Succeeded) with no issues is healthy; Running is healthy.
function isUnhealthy(pod: RawPod, issues: PodContainerIssue[]): boolean {
  const phase = pod.status?.phase;
  return issues.length > 0 || (phase !== 'Running' && phase !== 'Succeeded');
}

function toWorkloadPod(pod: RawPod): WorkloadPod {
  return { name: pod.metadata?.name || '', phase: pod.status?.phase || 'Unknown', issues: extractIssues(pod) };
}

// Raw k8s namespaced list via Rancher's proxy. Namespace scoping bounds payload;
// there is deliberately NO server-side Helm label filter (many charts stamp
// app.kubernetes.io/instance on pods but apply managed-by=Helm only to top-level
// resources — a server-side filter can drop the exact failing pod we need).
async function fetchNamespacePods(store: Dispatchable, clusterId: string, namespace: string): Promise<RawPod[]> {
  const url = `/k8s/clusters/${ encodeURIComponent(clusterIdOrLocal(clusterId)) }`
    + `/api/v1/namespaces/${ encodeURIComponent(namespace) }/pods?limit=500`;
  const res = await store.dispatch('rancher/request', { url, timeout: TIMEOUT_VALUES.CLUSTER });
  // `rancher/request` resolves to the response BODY directly (steve actions.js
  // responseObject → out = res.data). A raw k8s PodList body carries its items at
  // the top level (`res.items`); Steve collections use `.data`. Prefer `.items`.
  const items = res?.items ?? res?.data?.items ?? res?.data ?? [];
  return Array.isArray(items) ? items : [];
}

async function fetchClusterPodStatus(
  store: Dispatchable, clusterId: string, namespaces: string[], releaseNames: Set<string>,
): Promise<ClusterPodStatus> {
  // A Blueprint component may pin its own targetNamespace, overriding the workload's,
  // so a workload can span several namespaces. Fetch each; one namespace failing
  // (404/403) must not blank the whole cluster — only mark unavailable if all fail.
  const settled = await Promise.allSettled(
    namespaces.map(ns => fetchNamespacePods(store, clusterId, ns)),
  );
  if (!settled.some(r => r.status === 'fulfilled')) {
    logger.debug(`workload-pods: cluster ${ clusterId } unavailable (all namespaces failed)`);
    return { clusterId, unavailable: true, pods: [], unattributed: [] };
  }
  const pods = settled.flatMap(r => (r.status === 'fulfilled' ? r.value : []));

  const attributed: WorkloadPod[] = [];
  const labelless:  WorkloadPod[] = [];
  let anyPreciseMatch = false;

  for (const pod of pods) {
    const instance = getHelmReleaseFromLabels(pod.metadata?.labels);
    if (instance && releaseNames.has(instance)) {
      anyPreciseMatch = true;
      const wp = toWorkloadPod(pod);
      if (isUnhealthy(pod, wp.issues)) attributed.push(wp);
    } else if (!instance && isManagedByHelm(pod.metadata?.labels)) {
      const wp = toWorkloadPod(pod);
      if (isUnhealthy(pod, wp.issues)) labelless.push(wp);
    }
    // A pod with a non-matching instance label belongs to another release — excluded.
    // A label-less non-Helm pod is also excluded.
  }

  // Scoped fallback: surface label-less pods only when precise matching found
  // nothing on this cluster (otherwise a stray pod is almost certainly someone else's).
  return { clusterId, unavailable: false, pods: attributed, unattributed: anyPreciseMatch ? [] : labelless };
}

// namespaces: every namespace this workload deploys into (workload targetNamespace
// plus any per-component pins). When omitted, falls back to the workload namespace.
export async function fetchWorkloadPodStatus(store: Dispatchable, w: AIWorkload, namespaces?: string[]): Promise<ClusterPodStatus[]> {
  const releaseNames = expectedReleaseNames(w);
  const ns = (namespaces && namespaces.length) ? [...new Set(namespaces)] : [w.spec.targetNamespace];
  const targets = (w.spec.targetClusters && w.spec.targetClusters.length) ? w.spec.targetClusters : ['local'];
  const settled = await Promise.allSettled(
    targets.map(c => fetchClusterPodStatus(store, c, ns, releaseNames)),
  );
  return settled.map((r, i) =>
    r.status === 'fulfilled'
      ? r.value
      : { clusterId: targets[i], unavailable: true, pods: [], unattributed: [] },
  );
}
