// Rancher UI path builders for linking from the Workloads page into Rancher's
// native explorer/apps views. See docs/superpowers/specs/2026-08-10-workloads-rancher-links-design.md

import type { AIWorkload } from '../types/aiworkload-types';
import type { ClusterInfo } from '../types/rancher-types';

export function namespaceLink(clusterId: string, namespace: string): string {
  return `/c/${ encodeURIComponent(clusterId) }/explorer/namespace/${ encodeURIComponent(namespace) }`;
}

export function appLink(clusterId: string, namespace: string, release: string): string {
  return `/c/${ encodeURIComponent(clusterId) }/apps/catalog.cattle.io.app/${ encodeURIComponent(namespace) }/${ encodeURIComponent(release) }`;
}

export interface RancherLinkTarget {
  clusterId:   string;
  clusterName: string;
  url:         string;
}

export type WorkloadLinkKind = 'app' | 'namespace';

export interface WorkloadLink {
  kind:           WorkloadLinkKind;
  targets:        RancherLinkTarget[];
  disabled:       boolean;
  disabledReason: string; // '' | 'noTargetCluster' | 'noNamespace'
}

export interface WorkloadRancherLinks {
  primary: WorkloadLink;
}

// Builds a Rancher URL for one cluster; the descriptor maps it over every target.
type LinkBuilder = (clusterId: string) => string;

export function workloadTargetClusters(workload: AIWorkload): string[] {
  return (workload.spec.targetClusters ?? []).filter(Boolean);
}

function clusterDisplayName(clusterId: string, clusters: ClusterInfo[]): string {
  return clusters.find(c => c.id === clusterId)?.name || clusterId;
}

// Every target cluster is navigable. We do not gate on per-cluster status: the
// destination may not exist yet (still deploying) or the user may lack access,
// but Rancher renders a clean native "not found" page for either case, so we
// always let the user through rather than guessing existence from status (which
// the Helm strategy never populates anyway). See the design doc's
// "navigate-always" decision.
function makeTargets(
  clusterIds: string[],
  clusters:   ClusterInfo[],
  build:      LinkBuilder,
): RancherLinkTarget[] {
  return clusterIds.map(id => ({
    clusterId:   id,
    clusterName: clusterDisplayName(id, clusters),
    url:         build(id),
  }));
}

function disabledLink(kind: WorkloadLinkKind, reason: string): WorkloadLink {
  return { kind, targets: [], disabled: true, disabledReason: reason };
}

// Wrap per-cluster targets into an always-enabled link group. The only disabled
// states are structural (no cluster / no namespace) and are built via
// disabledLink; once we have targets, the affordance is navigable.
function linkFromTargets(kind: WorkloadLinkKind, targets: RancherLinkTarget[]): WorkloadLink {
  return {
    kind,
    targets,
    disabled:       false,
    disabledReason: '',
  };
}

export function workloadRancherLinks(
  workload: AIWorkload,
  clusters: ClusterInfo[] = [],
): WorkloadRancherLinks {
  const clusterIds = workloadTargetClusters(workload);
  const namespace  = workload.spec.targetNamespace || workload.metadata.namespace || '';
  const sourceType = workload.spec.source.sourceType;

  const targetsFor = (build: LinkBuilder): RancherLinkTarget[] =>
    makeTargets(clusterIds, clusters, build);

  const primaryKind: WorkloadLinkKind = sourceType === 'App' ? 'app' : 'namespace';
  let primary: WorkloadLink;
  if (!clusterIds.length) {
    primary = disabledLink(primaryKind, 'noTargetCluster');
  } else if (!namespace) {
    primary = disabledLink(primaryKind, 'noNamespace');
  } else if (sourceType === 'App') {
    // Apps deep-link to the Helm release detail. If no release was recorded
    // (essentially a non-case — the wizard always sets one), fall back to the
    // namespace detail page so the button stays enabled rather than dead-ending.
    const release = workload.spec.source.app?.release || '';
    primary = release
      ? linkFromTargets('app', targetsFor(id => appLink(id, namespace, release)))
      : linkFromTargets('app', targetsFor(id => namespaceLink(id, namespace)));
  } else {
    // Blueprint -> the namespace detail page, which lists the namespace's pods.
    // Rancher's pods-list namespace filter is not URL-addressable (it lives in the
    // `ns-by-cluster` user preference, not the route), so the namespace view is the
    // closest stable deep link.
    primary = linkFromTargets('namespace', targetsFor(id => namespaceLink(id, namespace)));
  }

  return { primary };
}
