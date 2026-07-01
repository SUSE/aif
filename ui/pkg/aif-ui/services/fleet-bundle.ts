import { ensureRegistrySecretSimple } from './rancher-apps';
import { APP_COLLECTION_REPO_URL } from './app-collection';
import { TIMEOUT_VALUES } from '../utils/constants';

// The operator-managed AppCollection ClusterRepo name. SUSE-registry charts
// frequently bundle subcharts whose container images come from AppCollection
// (e.g. litellm's postgresql at dp.apps.rancher.io). A chart that sets its own
// pod-spec imagePullSecrets makes Kubernetes IGNORE the ServiceAccount's
// imagePullSecrets, so AppCollection creds must be wired into the chart values
// directly — delivering them only via the SA is not enough.
const APP_COLLECTION_REPO_NAME = 'application-collection';

// SUSE_AI_COMBINED_PULL_SECRET is the operator-managed combined
// dockerconfigjson secret (see aif-operator combinedPullSecretName). It covers
// EVERY configured registry (AppCollection, SUSE Registry, NVIDIA) in a single
// secret, and the operator delivers it to every target cluster+namespace,
// driven off the AIWorkload CR — which the wizards record BEFORE the HelmOp, so
// delivery leads the chart install. For suse-ai charts we reference THIS secret
// in the pod-spec imagePullSecrets instead of relying solely on the per-registry
// secrets the UI creates per-cluster: a downstream per-registry write that fails
// (cross-cluster proxy timing / namespace-not-yet-present) is swallowed and
// silently drops that registry from imagePullSecrets, which broke image pulls
// (litellm-database on registry.suse.com → ImagePullBackOff on downstream
// clusters while local worked). The combined secret is the single robust source;
// any per-registry names collected remain as harmless extras. Mirrors the
// operator's suseInjector, which already does this for Blueprint installs.
const SUSE_AI_COMBINED_PULL_SECRET = 'suse-ai-pull-combined';

// withCombinedPullSecret guarantees the operator-managed combined pull secret is
// referenced (first, de-duplicated) for suse-ai charts. No-op for other
// libraries so NVIDIA / generic charts keep their existing handling.
function withCombinedPullSecret(names: string[], library?: 'suse-ai' | 'nvidia'): string[] {
  if (library !== 'suse-ai') {
    return names;
  }
  return [SUSE_AI_COMBINED_PULL_SECRET, ...names.filter(n => n !== SUSE_AI_COMBINED_PULL_SECRET)];
}

export interface FleetBundleParams {
  bundleName:              string;
  // release is the user-facing Helm release name (the value the user picks
  // in the wizard, defaulted to the chart name). Threaded separately from
  // bundleName so chart sub-resources templated as `{{ .Release.Name }}-foo`
  // don't blow past the K8s 63-char DNS-label limit when the bundle name is
  // long. See decoupling note in createFleetBundle / buildFleetBundleYAML.
  release:                 string;
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

// BUNDLE_NAME_MAX is the K8s metadata.name (DNS-1123 label) limit a Fleet
// HelmOp/Bundle name must satisfy.
const BUNDLE_NAME_MAX = 63;

// capBundleName returns a valid DNS-1123 label (<=63 chars, no leading/trailing
// '-') for a Fleet HelmOp/Bundle name. A naive `.slice(0, 63)` can cut mid-
// segment and leave a TRAILING '-' (e.g. a long
// `suse-ai-<release>-<namespace>-c-skg6s` truncated to `...-system-c-`), which
// the API server rejects with "must start and end with an alphanumeric
// character". When truncation is needed we trim trailing '-' and append a
// deterministic FNV-1a/base36 suffix so distinct long names that share a prefix
// don't collide. Mirrors crNameForCluster and the operator's
// pullSecretBundleName. Inputs already valid and within the limit are unchanged.
function capBundleName(name: string): string {
  const safe = name.toLowerCase().replace(/[^a-z0-9-]+/g, '-').replace(/^-+|-+$/g, '');
  if (safe.length <= BUNDLE_NAME_MAX) {
    return safe;
  }
  const hash = fnv1a32(safe).toString(36).slice(0, HELM_HASH_LEN);
  const head = safe.slice(0, BUNDLE_NAME_MAX - hash.length - 1).replace(/-+$/g, '');
  return head ? `${ head }-${ hash }` : hash;
}

// buildBundleName returns a deterministic Fleet HelmOp name for an app install.
export function buildBundleName(release: string, namespace: string): string {
  return capBundleName(`suse-ai-${ release }-${ namespace }`);
}

// buildBundleNameForCluster returns a per-cluster HelmOp name so two AIWorkloads
// installing the same chart to different downstream clusters don't clobber each
// other in fleet-default (which uses (namespace, name) for bundle identity, the
// same as any K8s object). The cluster suffix is the Rancher cluster ID
// (`local`, `c-zh74k`, …), sanitized defensively into a DNS-1123 label.
export function buildBundleNameForCluster(release: string, namespace: string, clusterId: string): string {
  return capBundleName(`suse-ai-${ release }-${ namespace }-${ clusterId }`);
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
  // See FleetBundleParams.release — same rationale on the GitOps path.
  release:          string;
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
  const pullSecretNames = withCombinedPullSecret(params.pullSecretNames, params.library);
  if (pullSecretNames.length > 0 && params.library !== 'nvidia') {
    // NVIDIA charts don't have imagePullSecrets in their original values, so don't add them
    const secrets = pullSecretNames.map(name => ({ name }));
    values.global = { ...(values.global || {}), imagePullSecrets: secrets };
    values.imagePullSecrets = secrets;
  }
  disableNvidiaChartSecrets(values, params.library);

  const isOCI = params.chartRepoUrl.startsWith('oci://');
  const spec: Record<string, any> = {
    // defaultNamespace (not namespace): targets the release namespace without
    // forcing every resource into it. Fleet's strict `namespace` field rejects
    // any cluster-scoped resource (ClusterRole, CRD, webhook), which breaks
    // operator/CRD-bearing charts.
    defaultNamespace: params.targetNamespace,
    helm: {
      ...(isOCI ? {} : { chart: params.chartName }),
      version:     params.chartVersion,
      repo:        isOCI ? `${ params.chartRepoUrl }/${ params.chartName }` : params.chartRepoUrl,
      // releaseName uses the user's `release` (not the Fleet bundleName) so
      // chart sub-resources templated as `{{ .Release.Name }}-foo` fit under
      // the 63-char DNS-label limit even when bundleName approaches its own
      // 63-char cap. Pre-decoupling, the longest sub-name in the
      // nvidia-blueprint-rag chart (`-etcd-headless`, 14 chars) tipped the
      // overall name over 63. See createFleetBundle for the rationale.
      releaseName: capReleaseName(params.release),
      values,
      // Disable Fleet's ${ } value templating: we resolve all values ourselves,
      // and upstream charts legitimately use ${ } (e.g. OTel ${env:MY_POD_IP}),
      // which Fleet would otherwise mis-parse as a template function.
      disablePreProcess: true,
      // See createFleetBundle for the takeOwnership rationale — same
      // "adopt operator-delivered pull secrets" need on the GitOps path.
      takeOwnership: true,
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

// ensureAppCollectionPullSecrets creates an AppCollection image-pull secret in
// the target namespace on each cluster and appends its name to pullSecretNames.
// Resolves credentials from the application-collection ClusterRepo (independent
// of the operator credentials API, so it works even if that call is slow/fails).
// No-op when AppCollection credentials can't be resolved. Used for suse-ai
// charts so subchart images pulled from AppCollection authenticate.
export async function ensureAppCollectionPullSecrets(
  store: any, targetNamespace: string, clusterIds: string[], pullSecretNames: string[],
): Promise<void> {
  try {
    const ref = await readClusterRepoClientSecret(store, APP_COLLECTION_REPO_NAME);
    const creds = ref ? await readAuthSecret(store, ref) : null;
    if (!creds) return;
    const host = APP_COLLECTION_REPO_URL.replace(/^oci:\/\//, '').split('/')[0];
    const slug = host.replace(/[^a-z0-9]/g, '-');
    for (const clusterId of clusterIds) {
      try {
        const name = await ensureRegistrySecretSimple(
          store, clusterId, targetNamespace, host, slug, creds.username, creds.password,
        );
        if (name && !pullSecretNames.includes(name)) pullSecretNames.push(name);
      } catch (e) {
        console.warn('[SUSE-AI] AppCollection pull-secret failed for cluster', clusterId, e);
      }
    }
  } catch (e) {
    console.warn('[SUSE-AI] AppCollection pull-secret resolution skipped:', e);
  }
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

  // SUSE-registry charts can bundle subcharts whose images come from
  // AppCollection (e.g. litellm's postgresql at dp.apps.rancher.io). Wire those
  // creds into the values too — they cannot be delivered via the ServiceAccount
  // alone, since a chart that sets pod-spec imagePullSecrets makes Kubernetes
  // ignore the SA's imagePullSecrets.
  if (params.library === 'suse-ai') {
    await ensureAppCollectionPullSecrets(store, params.targetNamespace, params.targetClusterIds, pullSecretNames);
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
    // releaseName uses the user's `release` (not the Fleet bundleName) so
    // chart sub-resources templated as `{{ .Release.Name }}-foo` fit under
    // the 63-char DNS-label limit even when bundleName approaches its own
    // 63-char cap. Pre-decoupling, the longest sub-name in the
    // nvidia-blueprint-rag chart (`-etcd-headless`, 14 chars) tipped the
    // overall name over 63 (release would be ~52 chars + 14 = 66, rejected).
    // bundleName stays Fleet-unique; releaseName drives chart .Release.Name.
    releaseName: capReleaseName(params.release),
    values:      addPullSecretsToValues(params.values, pullSecretNames, params.library),
    // Disable Fleet's ${ } value templating: we resolve all values ourselves,
    // and upstream charts legitimately use ${ } (e.g. OTel ${env:MY_POD_IP}),
    // which Fleet would otherwise mis-parse as a template function.
    disablePreProcess: true,
    // takeOwnership lets this chart's Helm install adopt resources the operator
    // pre-delivered (ngc-secret, ngc-api, suse-ai-pull-combined via the
    // pull-secret bundle). Many NVIDIA NIM-family charts template their own
    // ngc-secret with `imagePullSecret.create: true` by default — without
    // takeOwnership the install aborts with "Secret … cannot be imported into
    // the current release: invalid ownership metadata" because the pull-secret
    // bundle's Helm wrapper already stamped a different release-name. The
    // alternative — setting `imagePullSecret.create: false` per-chart in
    // values — pushes that concern onto every user, every install.
    takeOwnership: true,
  };

  // defaultNamespace (not namespace): targets the release namespace without
  // forcing every resource into it. Fleet's strict `namespace` field rejects
  // any cluster-scoped resource (ClusterRole, CRD, webhook), which breaks
  // operator/CRD-bearing charts.
  const baseSpec: Record<string, any> = { defaultNamespace: params.targetNamespace, helm: helmSpec };
  if (pullCreds && secretRef) {
    baseSpec.helmSecretName = secretRef.name;
  }

  // Defensive final override: regardless of how values were assembled above,
  // ensure NVIDIA charts NEVER receive `imagePullSecret.create: true` or
  // `ngcApiSecret.create: true`. This is what protects the operator's
  // pre-delivered ngc-secret / ngc-api from being overwritten by the
  // chart's template (the `password: ""` defaults) under takeOwnership.
  // Done late so it covers every code path that might have repopulated the
  // values map. Mutates helmSpec.values in place.
  if (params.library === 'nvidia' && helmSpec.values && typeof helmSpec.values === 'object') {
    helmSpec.values = JSON.parse(JSON.stringify(helmSpec.values));
    disableNvidiaChartSecrets(helmSpec.values, 'nvidia');
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
  const effective = withCombinedPullSecret(names, library);
  if (effective.length === 0 || library === 'nvidia') return values;
  const secrets = effective.map(name => ({ name }));
  return {
    ...values,
    global:           { ...(values.global || {}), imagePullSecrets: secrets },
    imagePullSecrets: secrets,
  };
}

// disableNvidiaChartSecrets flips the `create` flag on the conventional NVIDIA
// chart secret blocks so the chart skips templating its own ngc-secret /
// ngc-api. Otherwise, charts whose `imagePullSecret.password` /
// `ngcApiSecret.password` default to "" combined with takeOwnership:true on
// the workload HelmOp end up overwriting the operator-delivered Secret with
// an empty-password template, breaking image pulls with 403 from nvcr.io.
//
// Mutates `values` in place. Safe to call on any vendor; pass library to
// gate it to NVIDIA charts only.
function disableNvidiaChartSecrets(values: Record<string, any>, library?: 'suse-ai' | 'nvidia'): void {
  if (library !== 'nvidia') return;
  for (const [key, fallbackName] of [
    ['imagePullSecret', 'ngc-secret'],
    ['ngcApiSecret',    'ngc-api'],
  ] as const) {
    const existing = values[key];
    if (existing && typeof existing === 'object' && !Array.isArray(existing)) {
      existing.create = false;
      if (!existing.name) existing.name = fallbackName;
    } else {
      values[key] = { create: false, name: fallbackName };
    }
  }
}
