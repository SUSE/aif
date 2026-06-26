import { ensureRegistrySecretSimple } from './rancher-apps';
import { TIMEOUT_VALUES } from '../utils/constants';
import logger from '../utils/logger';
import type { RancherStore } from '../types/rancher-types';

export interface FleetBundleParams {
  bundleName:              string;
  chartRepo:               string; // ClusterRepo name (used to look up repo URL)
  chartRepoUrl:            string; // actual OCI/Helm URL for the bundle spec
  chartName:               string;
  chartVersion:            string;
  values:                  Record<string, unknown>;
  targetNamespace:         string;
  targetClusterIds:        string[];
  additionalPullSecretNames?: string[]; // pre-created pull secrets for extra registries (e.g. subchart registries)
  library?:                'suse-ai' | 'nvidia'; // library source to determine imagePullSecrets handling
}

// buildBundleName returns a deterministic Fleet HelmOp name for an app install.
export function buildBundleName(release: string, namespace: string): string {
  return `suse-ai-${ release }-${ namespace }`.replace(/[^a-z0-9-]/g, '-').slice(0, 63);
}

// 53 = 63 (K8s DNS-1123 label max) − 10 bytes Helm reserves for generated
// suffixes. Fleet validates spec.helm.releaseName against this.
const HELM_RELEASE_NAME_MAX = 53; // Helm/Fleet reject release names longer than this.
const HELM_HASH_LEN         = 6;  // base36 suffix; 36^6 ≈ 2.2e9 distinct values, ample for collision avoidance.

// Fleet validates spec.helm.releaseName against Helm's 53-byte limit, but a
// bundle name can be up to 63 (a valid K8s object name). Cap the release name,
// appending a short deterministic hash when truncating so distinct bundle names
// don't collide on the same prefix. The result is always a valid DNS-1123 label
// (no leading/trailing '-'), even for pathological inputs.
//
// Uses the same algorithm (FNV-1a / base36) as the operator's Go capReleaseName
// so both sides produce identical names for the same input. They don't strictly
// need to match — a single install's releaseName is produced by exactly one side,
// and the operator looks workloads up by bundle (object) name, never by
// releaseName — but keeping them aligned avoids confusion.
//
// Callers pass ASCII names (buildBundleName strips non-[a-z0-9-]), so .length
// (UTF-16 units) equals the byte count here; this is not safe for arbitrary
// multibyte input.
export function capReleaseName(name: string): string {
  if (name.length <= HELM_RELEASE_NAME_MAX) return name;
  const hash = fnv1a32(name).toString(36).slice(0, HELM_HASH_LEN);
  const head = name.slice(0, HELM_RELEASE_NAME_MAX - hash.length - 1).replace(/^-+|-+$/g, '');
  return head ? `${ head }-${ hash }` : hash;
}

// fnv1a32 is the 32-bit FNV-1a hash, matching Go's hash/fnv New32a() byte-for-byte
// for ASCII input. Math.imul does the 32-bit multiply without precision loss.
function fnv1a32(s: string): number {
  let h = 0x811c9dc5; // offset basis (2166136261)
  for (let i = 0; i < s.length; i++) {
    h ^= s.charCodeAt(i);
    h = Math.imul(h, 0x01000193); // FNV prime (16777619)
  }
  return h >>> 0;
}

interface ClientSecretRef { name: string; namespace: string; }

// Read the clientSecret ref from a Rancher ClusterRepo resource.
// Rancher stores spec.clientSecret as {name, namespace} for OCI repos,
// or as a plain string in older versions.
async function readClusterRepoClientSecret(store: RancherStore, repoName: string): Promise<ClientSecretRef | null> {
  try {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const res: any = await store.dispatch('management/find', { // Rancher store dispatch returns untyped response
      type: 'catalog.cattle.io.clusterrepo',
      id:   repoName,
    });
    const cs = res?.spec?.clientSecret;
    if (!cs) return null;
    if (typeof cs === 'object' && cs?.name) return { name: cs.name, namespace: cs.namespace || 'cattle-system' };
    if (typeof cs === 'string' && cs)       return { name: cs, namespace: 'cattle-system' };
    return null;
  } catch { return null; }
}

// Read a kubernetes.io/basic-auth secret and return decoded credentials.
async function readAuthSecret(store: RancherStore, ref: ClientSecretRef): Promise<{ username: string; password: string } | null> {
  try {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const res: any = await store.dispatch('rancher/request', { // Rancher store dispatch returns untyped response
      url:     `/k8s/clusters/local/api/v1/namespaces/${ref.namespace}/secrets/${ref.name}`,
      timeout: TIMEOUT_VALUES.CLUSTER,
    });
    const secretObj = res?.kind === 'Secret' ? res : (res?.data?.kind === 'Secret' ? res.data : res);
    const dataMap   = secretObj?.data || {};
    const decode    = (k: string): string | null => dataMap[k] ? atob(String(dataMap[k])) : null;
    const username  = decode('username');
    const password  = decode('password');
    return username && password ? { username, password } : null;
  } catch { return null; }
}

// Create (or skip-if-exists) a basic-auth secret in a fleet workspace namespace for HelmOp chart pull auth.
async function ensureFleetHelmAuthSecret(
  store: RancherStore, fleetNamespace: string, secretName: string, username: string, password: string,
): Promise<void> {
  const base = `/k8s/clusters/local/api/v1/namespaces/${fleetNamespace}/secrets`;
  const body = {
    apiVersion: 'v1',
    kind:       'Secret',
    metadata:   { name: secretName, namespace: fleetNamespace },
    type:       'kubernetes.io/basic-auth',
    data:       { username: btoa(username), password: btoa(password) },
  };
  try {
    await store.dispatch('rancher/request', { url: base, method: 'POST', data: body });
  } catch (e) {
    if ((e as { code?: number })?.code !== 409) {
      logger.warn(`FleetHelmOp: failed to create helm auth secret in ${fleetNamespace}`, { data: e });
      return;
    }
    try {
      await store.dispatch('rancher/request', { url: `${base}/${secretName}`, method: 'PUT', data: body });
    } catch (putErr) {
      logger.warn(`FleetHelmOp: failed to update helm auth secret in ${fleetNamespace}`, { data: putErr });
    }
  }
}

async function upsertFleetHelmOp(store: RancherStore, fleetNamespace: string, name: string, spec: Record<string, unknown>): Promise<void> {
  const baseUrl = `/k8s/clusters/local/apis/fleet.cattle.io/v1alpha1/namespaces/${fleetNamespace}/helmops`;
  const body = {
    apiVersion: 'fleet.cattle.io/v1alpha1',
    kind:       'HelmOp',
    metadata:   { name, namespace: fleetNamespace },
    spec,
  };

  try {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const res: any = await store.dispatch('rancher/request', { // Rancher store dispatch returns untyped response
      url:     `${baseUrl}/${name}`,
      timeout: TIMEOUT_VALUES.CLUSTER,
    });
    const existing = res?.data || res;

    await store.dispatch('rancher/request', {
      url:    `${baseUrl}/${name}`,
      method: 'PUT',
      data:   { ...body, metadata: { ...body.metadata, resourceVersion: existing?.metadata?.resourceVersion } },
      timeout: TIMEOUT_VALUES.MUTATION,
    });
  } catch {
    await store.dispatch('rancher/request', {
      url:    baseUrl,
      method: 'POST',
      data:   body,
      timeout: TIMEOUT_VALUES.MUTATION,
    });
  }
}

// buildFleetBundleYAML produces the Fleet HelmOp manifest as a YAML string (used by GitOps path).
export function buildFleetBundleYAML(params: {
  bundleName:       string;
  chartName:        string;
  chartVersion:     string;
  chartRepoUrl:     string;
  helmSecretName:   string | null;
  values:           Record<string, unknown>;
  pullSecretNames:  string[];
  targetClusterIds: string[];
  targetNamespace:  string;
  library?:         'suse-ai' | 'nvidia';
}): string {
  const targets = params.targetClusterIds.map(id =>
    id === 'local'
      ? { clusterName: 'local' }
      : { clusterSelector: { matchLabels: { 'management.cattle.io/cluster-name': id } } }
  );
  const isLocalOnly    = params.targetClusterIds.every(id => id === 'local');
  const fleetNamespace = isLocalOnly ? 'fleet-local' : 'fleet-default';

  const values = JSON.parse(JSON.stringify(params.values));
  if (params.pullSecretNames.length > 0 && params.library !== 'nvidia') {
    // NVIDIA charts don't have imagePullSecrets in their original values, so don't add them
    const secrets = params.pullSecretNames.map(name => ({ name }));
    values.global = { ...(values.global || {}), imagePullSecrets: secrets };
    values.imagePullSecrets = secrets;
  }

  const isOCI = params.chartRepoUrl.startsWith('oci://');
  const spec: Record<string, unknown> = {
    // defaultNamespace (not namespace): targets the release namespace without
    // forcing every resource into it. Fleet's strict `namespace` field rejects
    // any cluster-scoped resource (ClusterRole, CRD, webhook), which breaks
    // operator/CRD-bearing charts.
    defaultNamespace: params.targetNamespace,
    helm: {
      ...(isOCI ? {} : { chart: params.chartName }),
      version:     params.chartVersion,
      repo:        isOCI ? `${ params.chartRepoUrl }/${ params.chartName }` : params.chartRepoUrl,
      releaseName: capReleaseName(params.bundleName),
      values,
      // Disable Fleet's ${ } value templating: we resolve all values ourselves,
      // and upstream charts legitimately use ${ } (e.g. OTel ${env:MY_POD_IP}),
      // which Fleet would otherwise mis-parse as a template function.
      disablePreProcess: true,
    },
    targets,
  };
  if (params.helmSecretName) {
    spec.helmSecretName = params.helmSecretName;
  }

  return JSON.stringify({
    apiVersion: 'fleet.cattle.io/v1alpha1',
    kind:       'HelmOp',
    metadata:   { name: params.bundleName, namespace: fleetNamespace },
    spec,
  }, null, 2);
}

// createFleetBundle creates Fleet HelmOp CR(s) which pull and deploy the external OCI Helm chart.
// fleet-local workspace serves the management cluster; fleet-default serves downstream clusters.
// When both are selected we create one HelmOp in each workspace.
export async function createFleetBundle(store: RancherStore, params: FleetBundleParams): Promise<string> {
  const localClusters      = params.targetClusterIds.filter(id => id === 'local');
  const downstreamClusters = params.targetClusterIds.filter(id => id !== 'local');

  const secretRef = await readClusterRepoClientSecret(store, params.chartRepo);
  const pullCreds = secretRef ? await readAuthSecret(store, secretRef) : null;

  if (!pullCreds && secretRef) {
    logger.warn(`FleetHelmOp: could not read auth secret ${secretRef.name} — chart pull auth will be skipped`);
  }

  // Seed with any pre-created secrets passed by the caller (covers additional registries such as
  // subchart image registries that differ from the parent chart's registry).
  const pullSecretNames: string[] = [...(params.additionalPullSecretNames || [])];

  // Create imagePullSecrets in the target namespace on each cluster (for container image pulls).
  if (pullCreds) {
    const registryHost = params.chartRepoUrl.replace(/^oci:\/\//, '').split('/')[0];
    for (const clusterId of params.targetClusterIds) {
      try {
        const hostSlug   = registryHost.replace(/[^a-z0-9]/g, '-');
        const secretName = await ensureRegistrySecretSimple(
          store, clusterId, params.targetNamespace,
          registryHost, hostSlug, pullCreds.username, pullCreds.password,
        );
        if (secretName && !pullSecretNames.includes(secretName)) pullSecretNames.push(secretName);
      } catch (e) {
        logger.warn(`pull-secret creation failed for cluster ${clusterId}`, { data: e });
      }
    }
  }

  // Create helm auth secrets in the fleet workspace namespaces so HelmOp can pull the chart.
  if (pullCreds && secretRef) {
    const fleetNamespaces = [
      ...(localClusters.length > 0      ? ['fleet-local']   : []),
      ...(downstreamClusters.length > 0 ? ['fleet-default'] : []),
    ];
    for (const ns of fleetNamespaces) {
      await ensureFleetHelmAuthSecret(store, ns, secretRef.name, pullCreds.username, pullCreds.password);
    }
  }

  const isOCI   = params.chartRepoUrl.startsWith('oci://');
  const ociRepo = isOCI ? `${ params.chartRepoUrl }/${ params.chartName }` : params.chartRepoUrl;
  const helmSpec: Record<string, unknown> = {
    ...(isOCI ? {} : { chart: params.chartName }),
    version:     params.chartVersion,
    repo:        ociRepo,
    releaseName: capReleaseName(params.bundleName),
    values:      addPullSecretsToValues(params.values, pullSecretNames, params.library),
    // Disable Fleet's ${ } value templating: we resolve all values ourselves,
    // and upstream charts legitimately use ${ } (e.g. OTel ${env:MY_POD_IP}),
    // which Fleet would otherwise mis-parse as a template function.
    disablePreProcess: true,
  };

  // defaultNamespace (not namespace): targets the release namespace without
  // forcing every resource into it. Fleet's strict `namespace` field rejects
  // any cluster-scoped resource (ClusterRole, CRD, webhook), which breaks
  // operator/CRD-bearing charts.
  const baseSpec: Record<string, unknown> = { defaultNamespace: params.targetNamespace, helm: helmSpec };
  if (pullCreds && secretRef) {
    baseSpec.helmSecretName = secretRef.name;
  }

  if (localClusters.length > 0) {
    await upsertFleetHelmOp(store, 'fleet-local', params.bundleName, {
      ...baseSpec,
      targets: [{ clusterName: 'local' }],
    });
  }

  if (downstreamClusters.length > 0) {
    await upsertFleetHelmOp(store, 'fleet-default', params.bundleName, {
      ...baseSpec,
      targets: downstreamClusters.map(id => ({
        clusterSelector: { matchLabels: { 'management.cattle.io/cluster-name': id } },
      })),
    });
  }

  return params.bundleName;
}

function addPullSecretsToValues(values: Record<string, unknown>, names: string[], library?: 'suse-ai' | 'nvidia'): Record<string, unknown> {
  if (names.length === 0 || library === 'nvidia') return values;
  const secrets = names.map(name => ({ name }));
  return {
    ...values,
    global:           { ...(values.global as Record<string, unknown> || {}), imagePullSecrets: secrets },
    imagePullSecrets: secrets,
  };
}
