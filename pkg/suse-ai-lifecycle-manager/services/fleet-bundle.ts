import { ensureRegistrySecretSimple } from './rancher-apps';

export interface FleetBundleParams {
  bundleName:       string;
  chartRepo:        string; // ClusterRepo name (used to look up repo URL)
  chartRepoUrl:     string; // actual OCI/Helm URL for the bundle spec
  chartName:        string;
  chartVersion:     string;
  values:           Record<string, any>;
  targetNamespace:  string;
  targetClusterIds: string[];
}

// buildBundleName returns a deterministic Fleet HelmOp name for an app install.
export function buildBundleName(release: string, namespace: string): string {
  return `suse-ai-${ release }-${ namespace }`.replace(/[^a-z0-9-]/g, '-').slice(0, 63);
}

// Maps known OCI repo URLs to the basic-auth secrets created by the Settings page.
const REPO_AUTH_SECRETS: Record<string, string> = {
  'oci://dp.apps.rancher.io/charts':   'application-collection-auth',
  'oci://registry.suse.com/ai/charts': 'suse-ai-registry-auth',
};

// Read a kubernetes.io/basic-auth secret from cattle-system and return decoded credentials.
async function readAuthSecret(store: any, secretName: string): Promise<{ username: string; password: string } | null> {
  try {
    const res = await store.dispatch('rancher/request', {
      url:     `/k8s/clusters/local/api/v1/namespaces/cattle-system/secrets/${secretName}`,
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

// buildFleetBundleYAML produces the Fleet HelmOp manifest as a YAML string (used by GitOps path).
export function buildFleetBundleYAML(params: {
  bundleName:       string;
  chartName:        string;
  chartVersion:     string;
  chartRepoUrl:     string;
  values:           Record<string, any>;
  pullSecretNames:  string[];
  targetClusterIds: string[];
  targetNamespace:  string;
}): string {
  const targets = params.targetClusterIds.map(id =>
    id === 'local'
      ? { clusterName: 'local' }
      : { clusterSelector: { matchLabels: { 'management.cattle.io/cluster-name': id } } }
  );
  const isLocalOnly    = params.targetClusterIds.every(id => id === 'local');
  const fleetNamespace = isLocalOnly ? 'fleet-local' : 'fleet-default';

  const values = JSON.parse(JSON.stringify(params.values));
  if (params.pullSecretNames.length > 0) {
    const secrets = params.pullSecretNames.map(name => ({ name }));
    values.global        = { ...(values.global || {}), imagePullSecrets: secrets };
    values.imagePullSecrets = secrets;
  }

  const helmOp = {
    apiVersion: 'fleet.cattle.io/v1alpha1',
    kind:       'HelmOp',
    metadata:   { name: params.bundleName, namespace: fleetNamespace },
    spec: {
      namespace:      params.targetNamespace,
      helmSecretName: REPO_AUTH_SECRETS[params.chartRepoUrl] ?? undefined,
      helm: {
        ...(params.chartRepoUrl.startsWith('oci://') ? {} : { chart: params.chartName }),
        version:     params.chartVersion,
        repo:        params.chartRepoUrl.startsWith('oci://')
          ? `${ params.chartRepoUrl }/${ params.chartName }`
          : params.chartRepoUrl,
        releaseName: params.bundleName,
        values,
      },
      targets,
    },
  };

  return JSON.stringify(helmOp, null, 2);
}

// createFleetBundle creates Fleet HelmOp CR(s) which pull and deploy the external OCI Helm chart.
// fleet-local workspace serves the management cluster; fleet-default serves downstream clusters.
// When both are selected we create one HelmOp in each workspace.
export async function createFleetBundle(store: any, params: FleetBundleParams): Promise<string> {
  const localClusters      = params.targetClusterIds.filter(id => id === 'local');
  const downstreamClusters = params.targetClusterIds.filter(id => id !== 'local');

  const authSecretName = REPO_AUTH_SECRETS[params.chartRepoUrl];
  const pullCreds      = authSecretName ? await readAuthSecret(store, authSecretName) : null;

  if (!pullCreds && authSecretName) {
    console.warn('[SUSE-AI] FleetHelmOp: could not read auth secret', authSecretName, '— chart pull auth will be skipped');
  }

  // Create imagePullSecrets in the target namespace on each cluster (for container image pulls).
  const pullSecretNames: string[] = [];
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
  if (pullCreds && authSecretName) {
    const fleetNamespaces = [
      ...(localClusters.length > 0      ? ['fleet-local']   : []),
      ...(downstreamClusters.length > 0 ? ['fleet-default'] : []),
    ];
    for (const ns of fleetNamespaces) {
      await ensureFleetHelmAuthSecret(store, ns, authSecretName, pullCreds.username, pullCreds.password);
    }
  }

  const isOCI   = params.chartRepoUrl.startsWith('oci://');
  const ociRepo = isOCI ? `${ params.chartRepoUrl }/${ params.chartName }` : params.chartRepoUrl;
  const helmSpec: Record<string, any> = {
    ...(isOCI ? {} : { chart: params.chartName }),
    version:     params.chartVersion,
    repo:        ociRepo,
    releaseName: params.bundleName,
    values:      addPullSecretsToValues(params.values, pullSecretNames),
  };

  const baseSpec: Record<string, any> = { namespace: params.targetNamespace, helm: helmSpec };
  if (pullCreds && authSecretName) {
    baseSpec.helmSecretName = authSecretName;
  }

  const postHelmOp = (fleetNamespace: string, targets: any[]) =>
    store.dispatch('management/request', {
      url:    '/v1/fleet.cattle.io.helmops',
      method: 'POST',
      data:   {
        apiVersion: 'fleet.cattle.io/v1alpha1',
        kind:       'HelmOp',
        metadata:   { name: params.bundleName, namespace: fleetNamespace },
        spec:       { ...baseSpec, targets },
      },
    });

  if (localClusters.length > 0) {
    await postHelmOp('fleet-local', [{ clusterName: 'local' }]);
  }

  if (downstreamClusters.length > 0) {
    await postHelmOp('fleet-default', downstreamClusters.map(id => ({
      clusterSelector: { matchLabels: { 'management.cattle.io/cluster-name': id } },
    })));
  }

  return params.bundleName;
}

function addPullSecretsToValues(values: Record<string, any>, names: string[]): Record<string, any> {
  if (names.length === 0) return values;
  const secrets = names.map(name => ({ name }));
  return {
    ...values,
    global:           { ...(values.global || {}), imagePullSecrets: secrets },
    imagePullSecrets: secrets,
  };
}
