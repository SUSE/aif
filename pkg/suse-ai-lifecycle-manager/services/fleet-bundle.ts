import { ensureRegistrySecretSimple } from './rancher-apps';

export interface FleetBundleParams {
  bundleName:              string;
  chartRepo:               string; // ClusterRepo name (used to look up repo URL)
  chartRepoUrl:            string; // actual OCI/Helm URL for the bundle spec
  chartName:               string;
  chartVersion:            string;
  values:                  Record<string, any>;
  targetNamespace:         string;
  targetClusterIds:        string[];
  additionalPullSecretNames?: string[]; // pre-created pull secrets for extra registries (e.g. subchart registries)
  library?:                'suse-ai' | 'nvidia'; // library source to determine imagePullSecrets handling
}

// buildBundleName returns a deterministic Fleet HelmOp name for an app install.
export function buildBundleName(release: string, namespace: string): string {
  return `suse-ai-${ release }-${ namespace }`.replace(/[^a-z0-9-]/g, '-').slice(0, 63);
}

interface ClientSecretRef { name: string; namespace: string; }

// Read the clientSecret ref from a Rancher ClusterRepo resource.
// Rancher stores spec.clientSecret as {name, namespace} for OCI repos,
// or as a plain string in older versions.
async function readClusterRepoClientSecret(store: any, repoName: string): Promise<ClientSecretRef | null> {
  try {
    const res = await store.dispatch('management/find', {
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
async function readAuthSecret(store: any, ref: ClientSecretRef): Promise<{ username: string; password: string } | null> {
  try {
    const res = await store.dispatch('rancher/request', {
      url:     `/k8s/clusters/local/api/v1/namespaces/${ref.namespace}/secrets/${ref.name}`,
      timeout: 10000,
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
  store: any, fleetNamespace: string, secretName: string, username: string, password: string,
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
  } catch (e: any) {
    if (e?.code !== 409) {
      console.warn('[SUSE-AI] FleetHelmOp: failed to create helm auth secret in', fleetNamespace, e);
      return;
    }
    try {
      await store.dispatch('rancher/request', { url: `${base}/${secretName}`, method: 'PUT', data: body });
    } catch (putErr: any) {
      console.warn('[SUSE-AI] FleetHelmOp: failed to update helm auth secret in', fleetNamespace, putErr);
    }
  }
}

async function upsertFleetHelmOp(store: any, fleetNamespace: string, name: string, spec: Record<string, any>): Promise<void> {
  const baseUrl = `/k8s/clusters/local/apis/fleet.cattle.io/v1alpha1/namespaces/${fleetNamespace}/helmops`;
  const body = {
    apiVersion: 'fleet.cattle.io/v1alpha1',
    kind:       'HelmOp',
    metadata:   { name, namespace: fleetNamespace },
    spec,
  };

  try {
    const res = await store.dispatch('rancher/request', {
      url:     `${baseUrl}/${name}`,
      timeout: 10000,
    });
    const existing = res?.data || res;

    await store.dispatch('rancher/request', {
      url:    `${baseUrl}/${name}`,
      method: 'PUT',
      data:   { ...body, metadata: { ...body.metadata, resourceVersion: existing?.metadata?.resourceVersion } },
      timeout: 20000,
    });
  } catch {
    await store.dispatch('rancher/request', {
      url:    baseUrl,
      method: 'POST',
      data:   body,
      timeout: 20000,
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
  values:           Record<string, any>;
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
  const spec: Record<string, any> = {
    namespace: params.targetNamespace,
    helm: {
      ...(isOCI ? {} : { chart: params.chartName }),
      version:     params.chartVersion,
      repo:        isOCI ? `${ params.chartRepoUrl }/${ params.chartName }` : params.chartRepoUrl,
      releaseName: params.bundleName,
      values,
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
export async function createFleetBundle(store: any, params: FleetBundleParams): Promise<string> {
  const localClusters      = params.targetClusterIds.filter(id => id === 'local');
  const downstreamClusters = params.targetClusterIds.filter(id => id !== 'local');

  const secretRef = await readClusterRepoClientSecret(store, params.chartRepo);
  const pullCreds = secretRef ? await readAuthSecret(store, secretRef) : null;

  if (!pullCreds && secretRef) {
    console.warn('[SUSE-AI] FleetHelmOp: could not read auth secret', secretRef.name, '— chart pull auth will be skipped');
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
        console.warn('[SUSE-AI] pull-secret creation failed for cluster', clusterId, e);
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
  const helmSpec: Record<string, any> = {
    ...(isOCI ? {} : { chart: params.chartName }),
    version:     params.chartVersion,
    repo:        ociRepo,
    releaseName: params.bundleName,
    values:      addPullSecretsToValues(params.values, pullSecretNames, params.library),
  };

  const baseSpec: Record<string, any> = { namespace: params.targetNamespace, helm: helmSpec };
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

function addPullSecretsToValues(values: Record<string, any>, names: string[], library?: 'suse-ai' | 'nvidia'): Record<string, any> {
  if (names.length === 0 || library === 'nvidia') return values;
  const secrets = names.map(name => ({ name }));
  return {
    ...values,
    global:           { ...(values.global || {}), imagePullSecrets: secrets },
    imagePullSecrets: secrets,
  };
}
