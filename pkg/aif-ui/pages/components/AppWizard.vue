<script lang="ts" setup>
import { defineProps, withDefaults, ref, computed, onMounted, getCurrentInstance, watch } from 'vue';
import { useT } from '../../composables/useT';
import yaml from 'js-yaml';
import { Banner } from '@components/Banner';
import Loading from '@shell/components/Loading';
import AsyncButton from '@shell/components/AsyncButton';
import BasicInfoStep from './wizard/BasicInfoStep.vue';
import TargetStep from './wizard/TargetStep.vue';
import ValuesStep from './wizard/ValuesStep.vue';
import ReviewStep from './wizard/ReviewStep.vue';
import InstallProgressModal, { type ClusterInstallProgress } from './wizard/InstallProgressModal.vue';
import {
  findChartInRepo,
  ensureNamespace,
  createOrUpgradeApp,
  listChartVersions,
  fetchChartDefaultValues,
  fetchChartArchiveSize,
  ensureRegistrySecretSimple,
  ensureServiceAccountPullSecret,
  ensurePullSecretOnAllSAs,
  waitForAppInstall,
  getClusters,
  getInstalledHelmDetails,
  inferClusterRepoForChart,
  listClusterRepos,
  fetchUserNamespaces,
} from '../../services/rancher-apps';
import { persistLoad, persistSave, persistClear } from '../../services/ui-persist';
import { validateReleaseName, instanceNameError } from '../../validators/appInstallation';
import { fetchSuseAiApps, getClusterRepoNameFromUrl, getLibraryFromRepoUrl } from '../../services/app-collection';
import { isChartArchiveOversized } from '../../services/chart-values';
import { createAIWorkload, updateAIWorkload, listAIWorkloads, getRegistryCredentials } from '../../utils/operator-api';
import { useFleetGitConfigured } from '../../composables/useFleetGitConfigured';
import { createFleetBundle, buildBundleName, buildBundleNameForCluster, ensureAppCollectionPullSecrets } from '../../services/fleet-bundle';
import { publishToFleetGit }                          from '../../services/git-publish';
import { crNameForCluster } from '../../utils/workload-name';
import type { AIWorkloadClusterStatus, AIWorkloadPhase } from '../../types/aiworkload-types';

const REPO_CLUSTER = 'local' as const;

type WizardMode = 'install' | 'manage';

type WizardForm = {
  release:      string;
  namespace:    string;
  clusters:     string[];
  chartRepo:    string;
  chartName:    string;
  chartVersion: string;
  values:       Record<string, any>;
  deployType:   'Helm' | 'FleetBundle' | 'GitOps';
};

interface Props {
  slug: string;
  mode?: WizardMode;
}

const props = withDefaults(defineProps<Props>(), {
  mode: 'install'
});

const vm = getCurrentInstance()!.proxy as any;
const store = vm.$store;
const router = vm.$router;
const route = vm.$route;

const t = useT();

const loading = ref(true);
const loadingNamespaces = ref(false);
const submitting = ref(false);
const error = ref<string | null>(null);
const versions = ref<string[]>([]);
const loadingVersions = ref(false);
const loadingValues = ref(false);
const versionInfo = ref<any | null>(null);
const questionsLoading = ref(false);
const versionInfoKey = ref('');
const defaultValuesSnapshot = ref<Record<string, any>>({});
const namespaceOptions = ref<{label: string, value: string}[]>([]);

// Multi-cluster install progress state
const showProgressModal = ref(false);
const installProgress = ref<ClusterInstallProgress[]>([]);
const managedFleetBundleNames = ref<string[]>([]);

const PKEY = `${props.mode}.${props.slug}`;
const TTL = 1000 * 60 * 60;

const form = ref<WizardForm>({
  release:      props.slug,
  namespace:    `${ props.slug }-system`,
  clusters:     [],
  chartRepo:    '',
  chartName:    props.slug,
  chartVersion: '',
  values:       {},
  deployType:   'FleetBundle',
});

// Mode and version computed properties - must be declared before wizardSteps
const isInstallMode = computed(() => props.mode === 'install');
const isManageMode = computed(() => props.mode === 'manage');

// The instance name becomes a Helm release / Kubernetes resource name, so it must be a
// valid DNS-1123 label of at most 53 chars (the Helm release-name limit). In manage mode
// the name is fixed and read-only, so we never block on it there.
const releaseNameValid = computed(() => !isInstallMode.value || validateReleaseName(form.value.release).valid);

const versionOptions = computed(() =>
  (versions.value || []).map(v => ({ label: v, value: v }))
);

async function fetchAllNamespaces() {
  if (!store) return;
  const desiredDefault = `${props.slug}-system`;
  namespaceOptions.value = await fetchUserNamespaces(store, desiredDefault);
  if (isInstallMode.value) {
    form.value.namespace = desiredDefault;
  }
}

// Wizard step configuration for Rancher Wizard component
const wizardSteps = computed(() => [
  {
    name: 'basic-info',
    label: t('suseai.wizard.steps.basicInfo', 'Basic Information'),
    ready: releaseNameValid.value,
    weight: 1
  },
  {
    name: 'target',
    label: t('suseai.wizard.steps.targetCluster', 'Target Cluster'),
    ready: !!form.value.chartRepo && !!form.value.chartVersion && !loadingVersions.value,
    weight: 2
  },
  {
    name: 'values',
    label: t('suseai.wizard.steps.configuration', 'Configuration'),
    ready: form.value.clusters.length > 0 && !loadingValues.value,
    weight: 3
  },
  {
    name: 'review',
    label: t('suseai.wizard.steps.review', 'Review'),
    ready: form.value.clusters.length > 0,
    weight: 4
  }
]);

const currentStep = ref(0);

// Restore persisted state
const saved = persistLoad<{ step?: number; form?: Partial<WizardForm> }>(PKEY, {}, TTL);
if (saved.form) Object.assign(form.value, saved.form);
if (typeof saved.step === 'number') currentStep.value = Math.min(saved.step, wizardSteps.value.length - 1);

const wizardTitle = computed(() => isInstallMode.value ? 'Install' : 'Manage');
const inStore = computed(() => store?.getters?.currentStore?.() || 'cluster');
const PSP_VARIABLE_MAP: Record<string, string> = {
  epinio:                     'global.rbac.pspEnabled',
  longhorn:                   'enablePSP',
  'rancher-alerting-drivers': 'global.cattle.psp.enabled',
  neuvector:                  'global.cattle.psp.enabled',
  'prometheus-federator':     'global.rbac.pspEnabled'
};
const ignoreVariables = computed(() => {
  const key = PSP_VARIABLE_MAP[form.value.chartName as keyof typeof PSP_VARIABLE_MAP];

  return key ? [key] : [];
});
const hasQuestions = computed(() => !!versionInfo.value?.questions);

watch(() => [form.value.chartRepo, form.value.chartName, form.value.chartVersion], () => {
  versionInfo.value = null;
  versionInfoKey.value = '';
  defaultValuesSnapshot.value = {};
});

// Helm installs require the ClusterRepo on the target cluster, which we only ensure
// on local. Force FleetBundle for any non-local cluster selection.
watch(() => form.value.clusters, (clusters) => {
  const hasNonLocal = clusters.some(c => c !== REPO_CLUSTER);
  if (hasNonLocal && form.value.deployType === 'Helm') {
    form.value.deployType = 'FleetBundle';
  }
});

// Cache of measured chart archive sizes, keyed by `${repo}|${chart}|${version}`.
// A cached value of null means "measured, but size could not be determined" (fail open).
const chartArchiveSizes = ref<Record<string, number | null>>({});

function archiveCacheKey(): string {
  return `${form.value.chartRepo}|${form.value.chartName}|${form.value.chartVersion}`;
}

// Measure the chart archive size once per repo/chart/version (install mode only).
async function ensureChartArchiveSize(): Promise<void> {
  if (isManageMode.value) return;
  if (!store || !form.value.chartRepo || !form.value.chartName || !form.value.chartVersion) return;

  const key = archiveCacheKey();
  if (key in chartArchiveSizes.value) return; // already measured

  const size = await fetchChartArchiveSize(
    store,
    REPO_CLUSTER,
    form.value.chartRepo,
    form.value.chartName,
    form.value.chartVersion
  );
  chartArchiveSizes.value = { ...chartArchiveSizes.value, [key]: size };
}

// True when the currently selected chart is too large for the Helm path.
const helmOversized = computed(() => {
  const key = archiveCacheKey();
  const size = key in chartArchiveSizes.value ? chartArchiveSizes.value[key] : null;
  return isChartArchiveOversized(size);
});

// Oversized charts cannot use the Helm path (1 MiB Secret limit). Force FleetBundle,
// mirroring the non-local cluster behavior above.
watch(helmOversized, (oversized) => {
  if (oversized && form.value.deployType === 'Helm') {
    form.value.deployType = 'FleetBundle';
  }
});

const { fleetGitConfigured, fetchFleetGitConfigured } = useFleetGitConfigured();

watch(fleetGitConfigured, (configured) => {
  if (!configured && form.value.deployType === 'GitOps') {
    form.value.deployType = 'FleetBundle';
  }
}, { immediate: true });

// Measure the archive once the user has committed to a chart and moved past Basic
// Info (Target step onward). Covers non-linear navigation: changing the chart on
// Basic Info and jumping straight to Configuration/Review still re-measures, so the
// auto-switch above can correct an oversized Helm selection. Step 0 (browsing chart
// versions) deliberately does not trigger a download. `immediate` covers resuming a
// persisted wizard that restores currentStep to the Target step or later before this
// watcher is registered. Errors are swallowed inside ensureChartArchiveSize, so
// fire-and-forget is safe.
watch(currentStep, (step) => {
  if (step >= 1) void ensureChartArchiveSize();
}, { immediate: true });

// Basic info form computed
const basicInfoForm = computed({
  get: () => ({
    release: form.value.release,
    namespace: form.value.namespace,
    chartRepo: form.value.chartRepo,
    chartName: form.value.chartName,
    chartVersion: form.value.chartVersion
  }),
  set: async (value) => {
    const oldRepo = form.value.chartRepo;
    const oldVersion = form.value.chartVersion;

    form.value.release = value.release;
    form.value.namespace = value.namespace;
    form.value.chartRepo = value.chartRepo;
    form.value.chartName = value.chartName;
    form.value.chartVersion = value.chartVersion;

    // Refresh versions if repo changed
    if (oldRepo !== value.chartRepo) {
      await refreshVersions();
    }

    // Load default values if version changed in install mode
    if (oldVersion !== value.chartVersion) {
      await onVersionChange();
    }
  }
});

onMounted(async () => {
  try {
    await initializeWizard();
    loadingNamespaces.value = true;
    await fetchAllNamespaces();
  } catch (e) {
    error.value = `Failed to initialize: ${e.message || 'Unknown error'}`;
  } finally {
    loadingNamespaces.value = false;
    loading.value = false;
  }
});

async function initializeWizard() {
  // Populate from URL parameters first
  populateFromUrlParams();

  if (isInstallMode.value) {
    await initializeInstallMode();
  } else {
    await initializeManageMode();
  }

  await refreshVersions();
  await fetchFleetGitConfigured();
}

function populateFromUrlParams() {
  const query = route?.query || {};

  if (isInstallMode.value) {
    // For install mode, use simple pre-population
    form.value.release = props.slug;
    form.value.namespace = `${props.slug}-system`;
    form.value.chartRepo = query.repo as string || '';
    form.value.chartName = props.slug;
  } else {
    // For manage mode, get from URL path parameters
    form.value.release = query.instanceName as string || props.slug;
    form.value.namespace = query.instanceNamespace as string || `${props.slug}-system`;
    const instanceCluster = query.instanceCluster as string || '';
    if (instanceCluster) {
      form.value.clusters = [instanceCluster];
    }
    const deployStrategy = query.deployStrategy as string || '';
    if (deployStrategy === 'FleetBundle' || deployStrategy === 'GitOps' || deployStrategy === 'Helm') {
      form.value.deployType = deployStrategy;
    }
  }
}

async function initializeInstallMode() {
  if (!store) return;

  // If repo is provided in query, use it
  if (form.value.chartRepo) {
    const guess = await findChartInRepo(store, REPO_CLUSTER, form.value.chartRepo, props.slug);
    if (guess) {
      form.value.chartName = guess.chartName;
      form.value.chartVersion = guess.version;
      return;
    }
  }

  // Try to find the app in SUSE AI apps collection
  const repo = await findRepoForApp(props.slug);
  if (repo) {
    form.value.chartRepo = repo;
    const guess = await findChartInRepo(store, REPO_CLUSTER, repo, props.slug);
    if (guess) {
      form.value.chartName = guess.chartName;
      form.value.chartVersion = guess.version;
    }
  }
}

async function findRepoForApp(slug: string): Promise<string | null> {
  if (!store) return null;

  try {
    const suseAiApps = await fetchSuseAiApps(store);
    const staticApp = suseAiApps.find(app => app.slug_name === slug);

    if (staticApp?.repository_url) {
      const clusterRepoName = await getClusterRepoNameFromUrl(store, staticApp.repository_url);
      if (clusterRepoName) return clusterRepoName;
    }

    return await inferClusterRepoForChart(store, slug);
  } catch (e) {
    console.warn('Failed to find repo for app:', e);
    return null;
  }
}

async function initializeManageMode() {
  if (!store) throw new Error('Store not available');

  const targetCluster = form.value.clusters[0];
  if (!targetCluster) throw new Error('No cluster specified for manage mode');

  // Load values and chart details from the AIWorkload CR first.
  await loadAIWorkloadDetails();

  // For Helm deployments, sync with the live Helm release to get the most current values.
  if (form.value.deployType === 'Helm') {
    await loadInstalledAppDetails(targetCluster);
  }
}

async function loadAIWorkloadDetails() {
  try {
    const { items } = await listAIWorkloads();
    const workload = items.find(
      w => w.metadata.name === form.value.release && w.metadata.namespace === form.value.namespace
    );
    if (!workload) return;

    // Sync chart identity fields from the AIWorkload CR.
    const app = workload.spec.source.app;
    if (app?.chartName)    form.value.chartName    = app.chartName;
    if (app?.chartVersion) form.value.chartVersion = app.chartVersion;
    if (app?.chartRepo)    form.value.chartRepo    = app.chartRepo;
    // Restore the user's original release name from the CR. The URL query
    // parameter (instanceName) contains the AIWorkload CR's metadata.name,
    // which includes cluster ID suffixes for downstream deployments (e.g.,
    // "qdrant-c-785f4"), but the Helm release name should stay stable across
    // upgrades (e.g., "qdrant"). Without this sync, an upgrade would write a
    // different releaseName to the HelmOp and Fleet would create a new Helm
    // install instead of upgrading the existing release.
    if (app?.release)      form.value.release      = app.release;

    // Authoritative deploy strategy from the CR.
    if (workload.spec.deployStrategy) {
      form.value.deployType = workload.spec.deployStrategy;
    }

    if (workload.spec.targetClusters?.length) {
      form.value.clusters = [...workload.spec.targetClusters];
    }

    managedFleetBundleNames.value = [...(workload.spec.fleetBundleNames || [])];

    // For non-Helm strategies, the live Helm release isn't accessible — use stored component values.
    if (form.value.deployType !== 'Helm') {
      const vals = workload.spec.componentValues?.[0]?.values;
      if (vals && Object.keys(vals).length > 0) {
        form.value.values = vals;
      }
    }
  } catch (e) {
    console.warn('[SUSE-AI] Could not load AIWorkload details (non-fatal):', e);
  }
}

async function loadInstalledAppDetails(clusterId: string) {
  let foundValues = false;
  if (!store) return;

  try {
    const helmDetails = await getInstalledHelmDetails(store, clusterId, form.value.namespace, form.value.release);

    // Verify we got valid data - if app doesn't exist, chartName would be missing
    if (!helmDetails.chartName) {
      throw new Error(`App ${form.value.release} not found in cluster ${clusterId}`);
    }

    if (helmDetails.chartName) form.value.chartName = helmDetails.chartName;
    if (helmDetails.chartVersion) {
      form.value.chartVersion = helmDetails.chartVersion;
    }

    if (helmDetails.values && Object.keys(helmDetails.values).length > 0) {
      form.value.values = helmDetails.values;
      foundValues = true;
    }
  } catch (helmError) {
    console.warn('Failed to load app details from Helm:', helmError);
    throw helmError; // Re-throw to fail fast if app doesn't exist
  }

  // Infer repo if still unknown
  if (!form.value.chartRepo && form.value.chartName) {
    try {
      const repo = await inferClusterRepoForChart(store, form.value.chartName);
      if (repo) form.value.chartRepo = repo;
    } catch (e) {
      console.warn('Failed to infer repository:', e);
    }
  }

  // Log final results for debugging
  if (!foundValues) {
    console.warn('No values found for installed app:', {
      cluster: clusterId,
      namespace: form.value.namespace,
      release: form.value.release
    });
  }
}

async function refreshVersions() {
  if (!store || !form.value.chartRepo || !form.value.chartName) return;

  loadingVersions.value = true;
  try {
    const vs = await listChartVersions(store, REPO_CLUSTER, form.value.chartRepo, form.value.chartName);
    versions.value = Array.from(new Set(vs));

    // Set initial version if not already set
    if (!form.value.chartVersion && vs.length > 0) {
      form.value.chartVersion = vs[0];
    }

    if (form.value.chartVersion) {
      await ensureVersionInfoLoaded();

      // Load default values for install mode, then resolve pull secret names
      if (isInstallMode.value) {
        await loadDefaultValues({ skipVersionInfoFetch: true });
        await resolvePullSecretNames();
      }
    }
  } finally {
    loadingVersions.value = false;
  }
}

async function loadDefaultValues(options: { skipVersionInfoFetch?: boolean } = {}) {
  if (!store || !form.value.chartRepo || !form.value.chartName || !form.value.chartVersion) {
    return;
  }

  loadingValues.value = true;
  error.value = null;

  try {
    if (!options.skipVersionInfoFetch) {
      await ensureVersionInfoLoaded();
    }

    let baseValues: Record<string, any> | null = defaultValuesSnapshot.value && Object.keys(defaultValuesSnapshot.value).length
      ? defaultValuesSnapshot.value
      : null;

    if (!baseValues && versionInfo.value?.values) {
      baseValues = JSON.parse(JSON.stringify(versionInfo.value.values || {}));
      defaultValuesSnapshot.value = baseValues;
    }

    if (baseValues) {
      form.value.values = JSON.parse(JSON.stringify(baseValues));
    } else {
      const valuesText = await fetchChartDefaultValues(
        store,
        REPO_CLUSTER,
        form.value.chartRepo,
        form.value.chartName,
        form.value.chartVersion
      );

      if (valuesText?.trim()) {
        const parsed = (yaml.load(valuesText) as any) || {};
        defaultValuesSnapshot.value = JSON.parse(JSON.stringify(parsed));
        form.value.values = parsed;
      } else {
        error.value = 'No default values found for the selected version.';
      }
    }
  } catch (e: any) {
    error.value = e?.message || 'Failed to fetch default values.';
  } finally {
    loadingValues.value = false;
  }
}

/**
 * Handler for the "Load defaults" button. Loading chart defaults replaces
 * form.values wholesale (dropping any injected imagePullSecrets), so we must
 * re-inject the registry pull secrets afterwards — the same sequence the
 * programmatic version-change flow uses. Without this, a Helm upgrade started
 * after clicking "Load defaults" ships values with no imagePullSecrets and the
 * upgraded pods hit ImagePullBackOff.
 */
async function onLoadDefaults() {
  await loadDefaultValues();
  await resolvePullSecretNames();
}

/**
 * Resolve pull secret names from Settings registry credentials,
 * then inject them into form.values so the user can see them in the Configuration step.
 */
async function resolvePullSecretNames() {
  if (!form.value.chartRepo || !form.value.chartName || !form.value.chartVersion) return;
  try {
    // Determine library type from repository URL
    const repos = await listClusterRepos(store);
    const repoObj = repos.find((r: any) => r?.metadata?.name === form.value.chartRepo);
    const chartRepoUrl = repoObj?.spec?.url || repoObj?.spec?.ociRepo || '';
    const library = getLibraryFromRepoUrl(chartRepoUrl);

    // NVIDIA charts don't have imagePullSecrets in their original values schema,
    // so we skip injecting them into the form values to avoid schema validation errors
    if (library === 'nvidia') {
      return;
    }

    const creds = await getRegistryCredentials(5000);
    const secrets = [creds.applicationCollection, creds.suseRegistry, creds.nvidia]
      .filter(Boolean)
      .map(cred => ({
        name: `suse-ai-pull-secret-${ cred!.registryHost.replace(/[^a-z0-9]/g, '-') }`,
      }));
    if (secrets.length > 0) {
      form.value.values.global = form.value.values.global || {};
      form.value.values.global.imagePullSecrets = secrets;
      form.value.values.imagePullSecrets = secrets;
    }
  } catch (e: any) {
    console.warn('[SUSE-AI] Failed to resolve pull secret names:', e?.message || e);
  }
}

async function ensureVersionInfoLoaded() {
  if (!store) {
    versionInfo.value = null;
    versionInfoKey.value = '';
    return null;
  }

  const repo = form.value.chartRepo;
  const chart = form.value.chartName;
  const version = form.value.chartVersion;

  if (!repo || !chart || !version) {
    versionInfo.value = null;
    versionInfoKey.value = '';
    return null;
  }

  const key = `${repo}:::${chart}:::${version}`;

  if (versionInfo.value && versionInfoKey.value === key) {
    return versionInfo.value;
  }

  questionsLoading.value = true;
  try {
    await store.dispatch('catalog/load');
    const info = await store.dispatch('catalog/getVersionInfo', {
      repoType:    'cluster',
      repoName:    repo,
      chartName:   chart,
      versionName: version
    });

    versionInfo.value = info;
    versionInfoKey.value = key;
    defaultValuesSnapshot.value = JSON.parse(JSON.stringify(info?.values || {}));

    return info;
  } catch (e) {
    console.warn('[SUSE-AI] Failed to load chart version info', e);
    versionInfo.value = null;
    versionInfoKey.value = '';
    defaultValuesSnapshot.value = {};

    return null;
  } finally {
    questionsLoading.value = false;
  }
}

// Handle version changes for install mode
async function onVersionChange() {
  if (!form.value.chartVersion) {
    versionInfo.value = null;
    versionInfoKey.value = '';
    return;
  }

  await ensureVersionInfoLoaded();

  if (isInstallMode.value) {
    await loadDefaultValues({ skipVersionInfoFetch: true });
    await resolvePullSecretNames();
  }

  // Persist form state
  persistSave(PKEY, { step: currentStep.value, form: form.value });
}

// Simplified event handlers
function onValuesEdited() {
  // Persist form state when values are edited
  persistSave(PKEY, { step: currentStep.value, form: form.value });
}

// Wizard event handlers
function onWizardNext({ step }) {
  currentStep.value = step;
  persistSave(PKEY, { step: currentStep.value, form: form.value });
}

async function onWizardFinish() {
  await submit();
}

function onWizardCancel() {
  persistClear(PKEY);
  router?.push({
    name:   isManageMode.value ? `c-cluster-suseai-workloads` : `c-cluster-suseai-apps`,
    params: { cluster: route?.params?.cluster },
  });
}

async function submit() {
  try {
    submitting.value = true;
    error.value = null;

    if (!form.value.chartRepo || !form.value.chartName || !form.value.chartVersion) {
      error.value = 'Please set repository, chart and version.'; submitting.value = false; return;
    }

    if (isInstallMode.value) {
      const instanceErr = instanceNameError(form.value.release);
      if (instanceErr) {
        error.value = instanceErr;
        submitting.value = false; return;
      }
    }

    if (form.value.clusters.length === 0) {
      error.value = 'Please select at least one cluster.'; submitting.value = false; return;
    }

    if (!store) { error.value = 'Store not available'; submitting.value = false; return; }

    const actionLabel = isInstallMode.value ? 'INSTALL' : 'UPGRADE';
    const targetClusters = form.value.clusters;

    console.log(`[SUSE-AI] ${actionLabel} start `, {
      clusters: targetClusters,
      ns: form.value.namespace,
      release: form.value.release
    });

    if (isInstallMode.value) {
      // Per-cluster collision check: one AIWorkload CR per (release, cluster),
      // so two installs of the same chart to different clusters are independent.
      // We block only the cluster(s) where a CR already exists.
      try {
        const { items } = await listAIWorkloads();
        const existingNames = new Set(
          (items || [])
            .filter((w: any) => w?.metadata?.namespace === form.value.namespace)
            .map((w: any) => w.metadata.name),
        );
        const collisions = form.value.clusters.filter(
          (c) => existingNames.has(crNameForCluster(form.value.release, c)),
        );
        if (collisions.length > 0) {
          const list = collisions.map((c) => `${form.value.release} on ${c}`).join(', ');
          error.value = `Already deployed: ${list}. Pick different clusters or manage the existing deployment(s) from the list.`;
          submitting.value = false;
          return;
        }
      } catch (e) {
        console.warn('[SUSE-AI] Could not check for existing deployments (proceeding):', e);
      }

      if (form.value.deployType === 'Helm') {
        await performMultiClusterInstall();
      } else if (form.value.deployType === 'FleetBundle') {
        await performFleetBundleInstall();
      } else {
        await performGitOpsInstall();
      }
    } else {
      if (form.value.deployType === 'Helm') {
        await performUpgrade();
      } else if (form.value.deployType === 'FleetBundle') {
        await performFleetBundleUpgrade();
      } else {
        await performGitOpsUpgrade();
      }
    }
  } catch (e: any) {
    error.value = e?.message || 'Operation failed';
    submitting.value = false;
  }
}

function navigateAfterSuccess() {
  persistClear(PKEY);
  router?.push({
    name:   isManageMode.value ? `c-cluster-suseai-workloads` : `c-cluster-suseai-apps`,
    params: { cluster: route?.params?.cluster },
  });
}

// Get cluster name for display (used in progress modal)
async function getClusterDisplayName(clusterId: string): Promise<string> {
  try {
    const clusters = await getClusters(store);
    const cluster = clusters.find((c: any) => c.id === clusterId);
    return cluster?.name || clusterId;
  } catch {
    return clusterId;
  }
}

// Concurrency limit for parallel installations
const INSTALL_CONCURRENCY = 3;

// Multi-cluster install orchestration with parallel execution
async function performMultiClusterInstall() {
  const targetClusters = form.value.clusters;

  // Initialize progress for all clusters
  installProgress.value = await Promise.all(
    targetClusters.map(async (clusterId) => ({
      clusterId,
      clusterName: await getClusterDisplayName(clusterId),
      status: 'pending' as const,
      progress: 0,
      message: 'Waiting to start...'
    }))
  );

  showProgressModal.value = true;

  // Install to clusters in parallel with concurrency limit
  await installWithConcurrencyLimit(targetClusters, INSTALL_CONCURRENCY);

  // Check final status
  const allSucceeded = installProgress.value.every(p => p.status === 'success');
  if (allSucceeded) {
    console.log('[SUSE-AI] Multi-cluster install completed successfully');
  } else {
    const failed = installProgress.value.filter(p => p.status === 'failed');
    console.warn(`[SUSE-AI] Multi-cluster install completed with ${failed.length} failure(s)`);
  }

  // One CR per cluster — Helm strategy has no fleet bundle name to record.
  await Promise.all(targetClusters.map(c => recordAIWorkload({}, 'Helm', c)));
  submitting.value = false;
}

// Map per-cluster settled results onto installProgress and aggregate the
// top-level error, mirroring BlueprintInstallWizard's per-cluster-independent
// reporting: one cluster's failure marks only that cluster failed and leaves
// the successful ones intact.
function applyPerClusterResults(results: PromiseSettledResult<unknown>[], successMessage: string) {
  installProgress.value = installProgress.value.map((p, i) => {
    const r = results[i];
    if (r && r.status === 'fulfilled') {
      return { ...p, progress: 100, status: 'success' as const, message: successMessage };
    }
    const e: any = (r as PromiseRejectedResult | undefined)?.reason;
    const errMsg = e?.status === 409
      ? `A deployment for "${ form.value.release }" already exists on ${ p.clusterId }.`
      : (e?.message || 'Unknown error');
    return { ...p, status: 'failed' as const, message: errMsg, error: errMsg };
  });

  const failed = results.filter(r => r.status === 'rejected');
  if (failed.length === results.length) {
    error.value = (failed[0] as PromiseRejectedResult | undefined)?.reason?.message || 'All cluster installs failed';
  } else if (failed.length > 0) {
    error.value = `${ failed.length } of ${ results.length } cluster installs failed — see progress for details`;
  }
}

async function performFleetBundleInstall() {
  installProgress.value = form.value.clusters.map(clusterId => ({
    clusterId,
    clusterName: clusterId,
    status:      'installing' as const,
    progress:    10,
    message:     'Creating imagePullSecrets on target clusters...',
  }));
  showProgressModal.value = true;

  try {
    // Per-cluster bundle name keeps two AIWorkloads installing the same chart
    // to different downstream clusters from clobbering each other in
    // fleet-default. Tracked here so the AIWorkload CR for each cluster
    // records its specific bundle name.
    const bundleNamesByCluster: Record<string, string> = {};
    for (const clusterId of form.value.clusters) {
      bundleNamesByCluster[clusterId] = buildBundleNameForCluster(
        form.value.release, form.value.namespace, clusterId,
      );
    }

    // Pre-create pull secrets for ALL configured registries so subchart images from a
    // different registry than the parent chart are also covered.
    let creds: { applicationCollection?: any; suseRegistry?: any; nvidia?: any } = {};
    try { creds = await getRegistryCredentials(5000); } catch (e) {
      console.warn('[SUSE-AI] FleetBundle: registry credentials unavailable:', e);
    }
    const activeCreds = [creds.applicationCollection, creds.suseRegistry, creds.nvidia].filter(Boolean);
    const secretResults = await Promise.all(
      form.value.clusters.flatMap(clusterId =>
        activeCreds.map(async cred => {
          try {
            const hostSlug = cred!.registryHost.replace(/[^a-z0-9]/g, '-');
            return await ensureRegistrySecretSimple(
              store, clusterId, form.value.namespace,
              cred!.registryHost, hostSlug, cred!.username, cred!.password,
            );
          } catch (e) { console.warn('[SUSE-AI] FleetBundle: pull-secret skipped:', e); return null; }
        })
      )
    );
    const extraPullSecretNames = [...new Set(secretResults.filter((n): n is string => !!n))];

    const repos = await listClusterRepos(store);
    const repoObj = repos.find((r: any) => r?.metadata?.name === form.value.chartRepo);
    const chartRepoUrl = repoObj?.spec?.url || repoObj?.spec?.ociRepo || '';

    updateAllProgress(50, 'Creating Fleet Bundles (one per cluster)...');
    // One bundle per cluster — Fleet's per-workspace name uniqueness means
    // sharing a name across clusters in fleet-default would silently overwrite.
    // Per-cluster independence: a failure on one cluster must not roll back or
    // mark failed the clusters that already succeeded (mirrors
    // BlueprintInstallWizard.onInstall).
    const results = await Promise.allSettled(form.value.clusters.map(async clusterId => {
      // Create the AIWorkload CR BEFORE the workload HelmOp. The operator's
      // pull-secret delivery (downstream secret bundle + SA-merge) is driven
      // off the AIWorkload CR, while createFleetBundle's HelmOp starts the
      // chart install immediately. Recording the CR first lets the operator
      // begin delivering imagePullSecrets ahead of (or concurrent with) the
      // chart install, instead of racing behind it — otherwise the chart's
      // pods/ServiceAccounts can come up before the SUSE-AI pull secret lands,
      // which surfaces as ImagePullBackOff on FleetBundle installs (the GitOps
      // path masks this race because its git→Fleet sync is inherently slower).
      await recordAIWorkload(bundleNamesByCluster, 'FleetBundle', clusterId, { phase: 'Pending', clusterStatuses: [] });
      await createFleetBundle(store, {
        bundleName:                bundleNamesByCluster[clusterId],
        release:                   form.value.release,
        chartRepo:                 form.value.chartRepo,
        chartRepoUrl,
        chartName:                 form.value.chartName,
        chartVersion:              form.value.chartVersion,
        values:                    form.value.values,
        targetNamespace:           form.value.namespace,
        targetClusterIds:          [clusterId],
        additionalPullSecretNames: extraPullSecretNames,
        library:                   getLibraryFromRepoUrl(chartRepoUrl),
      });
    }));

    applyPerClusterResults(results, 'Fleet Bundle created — Fleet will deploy to selected cluster');
  } catch (e: any) {
    installProgress.value = installProgress.value.map(p => ({
      ...p, status: 'failed' as const, error: e?.message || 'Unknown error',
    }));
    throw e;
  } finally {
    submitting.value = false;
  }
}

async function performGitOpsInstall() {
  installProgress.value = form.value.clusters.map(clusterId => ({
    clusterId,
    clusterName: clusterId,
    status:      'installing' as const,
    progress:    10,
    message:     'Creating imagePullSecrets on target clusters...',
  }));
  showProgressModal.value = true;

  try {
    // Per-cluster bundle name for the same reason as performFleetBundleInstall:
    // Fleet bundles are unique per (workspace, name) and the GitOps publish ends
    // up creating Fleet objects in fleet-default for downstream clusters.
    const bundleNamesByCluster: Record<string, string> = {};
    for (const clusterId of form.value.clusters) {
      bundleNamesByCluster[clusterId] = buildBundleNameForCluster(
        form.value.release, form.value.namespace, clusterId,
      );
    }

    const creds = await getRegistryCredentials(5000);
    const pullSecretNames: string[] = [];
    for (const clusterId of form.value.clusters) {
      for (const cred of [creds.applicationCollection, creds.suseRegistry, creds.nvidia]) {
        if (!cred) continue;
        try {
          const hostSlug = cred.registryHost.replace(/[^a-z0-9]/g, '-');
          const name = await ensureRegistrySecretSimple(
            store, clusterId, form.value.namespace,
            cred.registryHost, hostSlug, cred.username, cred.password,
          );
          if (name && !pullSecretNames.includes(name)) pullSecretNames.push(name);
        } catch (e) {
          console.warn('[SUSE-AI] pull-secret skipped for GitOps:', e);
        }
      }
    }

    updateAllProgress(60, 'Publishing Fleet Bundle YAML(s) to git...');

    const repos = await listClusterRepos(store);
    const repoObj = repos.find((r: any) => r?.metadata?.name === form.value.chartRepo);
    const chartRepoUrl = repoObj?.spec?.url || repoObj?.spec?.ociRepo || '';
    const helmSecretName = (() => { const cs = repoObj?.spec?.clientSecret; return typeof cs === 'object' ? (cs?.name || null) : (cs || null); })();

    // SUSE-registry charts can bundle subcharts whose images come from
    // AppCollection — wire those creds into the bundle values too (the SA's
    // imagePullSecrets are ignored once the chart sets pod-spec imagePullSecrets).
    if (getLibraryFromRepoUrl(chartRepoUrl) === 'suse-ai') {
      await ensureAppCollectionPullSecrets(store, form.value.namespace, form.value.clusters, pullSecretNames);
    }

    // One bundle YAML per cluster — committed sequentially to avoid racing on
    // the same git branch. Each cluster is independent: a failure records that
    // cluster as failed and continues with the rest, rather than aborting the
    // remaining clusters (per-cluster independence as in
    // BlueprintInstallWizard.onInstall, kept sequential for the shared branch).
    const results: PromiseSettledResult<void>[] = [];
    for (const clusterId of form.value.clusters) {
      try {
        await publishToFleetGit({
          bundleName:       bundleNamesByCluster[clusterId],
          release:          form.value.release,
          chartName:        form.value.chartName,
          chartVersion:     form.value.chartVersion,
          chartRepoUrl,
          helmSecretName,
          values:           form.value.values,
          pullSecretNames,
          targetClusterIds: [clusterId],
          targetNamespace:  form.value.namespace,
          library:          getLibraryFromRepoUrl(chartRepoUrl),
        });
        await recordAIWorkload(bundleNamesByCluster, 'GitOps', clusterId, { phase: 'Pending', clusterStatuses: [] });
        results.push({ status: 'fulfilled', value: undefined });
      } catch (e) {
        results.push({ status: 'rejected', reason: e });
      }
    }

    applyPerClusterResults(results, 'Fleet Bundle YAML committed — Fleet will deploy to selected cluster');
  } catch (e: any) {
    installProgress.value = installProgress.value.map(p => ({
      ...p, status: 'failed' as const, error: e?.message || 'Unknown error',
    }));
    throw e;
  } finally {
    submitting.value = false;
  }
}

function updateAllProgress(progress: number, message: string) {
  installProgress.value = installProgress.value.map(p => ({ ...p, progress, message }));
}

// recordAIWorkload writes ONE AIWorkload CR per (release, cluster). Each CR is
// uniquely identifiable on the management cluster as `<release>-<clusterId>`
// in the target namespace, so deploying the same chart to multiple clusters
// (or to a new cluster after an existing deployment) never collides on
// (namespace, name).
//
// The caller controls clusterId scoping:
//   - Install flows pass each clusterId individually (one CR per cluster)
//   - Manage flows pass the single cluster the user is editing
//
// fleetBundleNamesByCluster lets each per-cluster CR carry its own bundle
// name (since FleetBundle/GitOps strategies emit one Fleet HelmOp per cluster
// to avoid bundle-name collision in fleet-default).
async function recordAIWorkload(
  fleetBundleNamesByCluster: Record<string, string>,
  strategy: 'Helm' | 'FleetBundle' | 'GitOps',
  clusterId: string,
  initialStatus?: { phase: AIWorkloadPhase; clusterStatuses: AIWorkloadClusterStatus[] },
) {
  try {
    let phase: AIWorkloadPhase;
    let clusterStatuses: AIWorkloadClusterStatus[];

    if (initialStatus) {
      phase = initialStatus.phase;
      clusterStatuses = initialStatus.clusterStatuses;
    } else {
      const p = installProgress.value.find(x => x.clusterId === clusterId);
      const single: AIWorkloadClusterStatus = {
        clusterId,
        phase:   p?.status === 'success' ? 'Running' : 'Failed',
        message: p?.error || p?.message || '',
      };
      clusterStatuses = [single];
      phase = single.phase === 'Running' ? 'Running' : 'Failed';
    }

    const crName = crNameForCluster(form.value.release, clusterId);

    // Resolve the chart's repo URL to derive `vendor` for the operator's
    // pull-secret injector — mirrors BlueprintAppSelectorStep.vue's pattern.
    // Without this, the operator's reconcileAppPullSecrets falls back to the
    // CRD default "suse" and NVIDIA charts never get ngc-secret/ngc-api.
    const repos = await listClusterRepos(store);
    const repoObj = repos.find((r: any) => r?.metadata?.name === form.value.chartRepo);
    const chartRepoUrl = repoObj?.spec?.url || repoObj?.spec?.ociRepo || '';
    const vendor = getLibraryFromRepoUrl(chartRepoUrl) === 'nvidia' ? 'nvidia' : 'suse';

    const fleetBundleName = fleetBundleNamesByCluster[clusterId];
    const spec = {
      displayName:     (route.query.n as string) || props.slug,
      source: {
        sourceType: 'App' as const,
        app: {
          chartRepo:    form.value.chartRepo,
          chartName:    form.value.chartName,
          chartVersion: form.value.chartVersion,
          release:      form.value.release,
          vendor,
        },
      },
      targetNamespace:  form.value.namespace,
      targetClusters:   [clusterId],
      deployStrategy:   strategy,
      componentValues:  [{
        componentName: form.value.chartName,
        values:        form.value.values,
      }],
      fleetBundleNames: strategy !== 'Helm' && fleetBundleName ? [fleetBundleName] : undefined,
    };

    if (isManageMode.value) {
      await updateAIWorkload(form.value.namespace, crName, spec, { phase, clusterStatuses });
    } else {
      await createAIWorkload(form.value.namespace, crName, spec, { phase, clusterStatuses });
    }
  } catch (e) {
    console.warn('[SUSE-AI] Failed to record AIWorkload CR (non-fatal):', e);
  }
}

// Install to multiple clusters with concurrency limit
async function installWithConcurrencyLimit(clusterIds: string[], concurrency: number) {
  const queue = [...clusterIds];
  const executing: Promise<void>[] = [];

  while (queue.length > 0 || executing.length > 0) {
    // Start new installations up to concurrency limit
    while (queue.length > 0 && executing.length < concurrency) {
      const clusterId = queue.shift()!;
      const promise = installSingleCluster(clusterId).then(() => {
        // Remove from executing when done
        const index = executing.indexOf(promise);
        if (index > -1) executing.splice(index, 1);
      });
      executing.push(promise);
    }

    // Wait for at least one to complete before continuing
    if (executing.length > 0) {
      await Promise.race(executing);
    }
  }
}

// A large umbrella chart (e.g. nvidia-blueprint-rag) can exceed Kubernetes' 1 MiB
// Secret limit when installed via the Helm path, because Rancher packs the chart
// archive into a `helm-operation-*` Secret. The Fleet Bundle path pulls the chart
// in-cluster and isn't subject to this limit. Detect that specific failure and
// guide the user to switch deployment method instead of surfacing the raw k8s error.
function humanizeInstallError(message?: string): string {
  const m = message || 'Unknown error';
  if (/helm-operation/i.test(m) && /too long/i.test(m)) {
    return 'This chart is too large to install with the Helm deployment method '
      + '(it exceeds the 1 MiB Kubernetes Secret limit for Helm operation data). '
      + 'Go back to the Target Cluster step, change the Deployment Type to '
      + '"Fleet Bundle", and install again.';
  }
  return m;
}

// Install to a single cluster and update progress
async function installSingleCluster(clusterId: string): Promise<void> {
  updateClusterProgress(clusterId, {
    status: 'installing',
    progress: 10,
    message: 'Starting installation...'
  });

  try {
    await installToCluster(clusterId, (progress, message) => {
      updateClusterProgress(clusterId, { progress, message });
    });

    updateClusterProgress(clusterId, {
      status: 'success',
      progress: 100,
      message: 'Installation completed successfully'
    });
  } catch (e: any) {
    updateClusterProgress(clusterId, {
      status: 'failed',
      progress: 0,
      message: 'Installation failed',
      error: humanizeInstallError(e?.message)
    });
  }
}

// Update progress for a specific cluster
function updateClusterProgress(clusterId: string, updates: Partial<ClusterInstallProgress>) {
  const index = installProgress.value.findIndex(p => p.clusterId === clusterId);
  if (index !== -1) {
    installProgress.value[index] = { ...installProgress.value[index], ...updates };
  }
}

// Progress modal event handlers
function onProgressModalDone() {
  showProgressModal.value = false;
  navigateAfterSuccess();
}

function onProgressModalCancel() {
  showProgressModal.value = false;
  submitting.value = false;
}

async function onProgressModalRetryAll() {
  // Reset all to pending and retry
  installProgress.value = installProgress.value.map(p => ({
    ...p,
    status: 'pending' as const,
    progress: 0,
    message: 'Waiting to retry...',
    error: undefined
  }));

  submitting.value = true;

  const clusterIds = installProgress.value.map(p => p.clusterId);

  if (isManageMode.value) {
    await upgradeSingleCluster(clusterIds[0]);
  } else {
    // Re-run installation with parallelization
    await installWithConcurrencyLimit(clusterIds, INSTALL_CONCURRENCY);
  }

  submitting.value = false;
}

async function onProgressModalRetryFailed() {
  const failedClusters = installProgress.value.filter(p => p.status === 'failed');

  // Reset failed clusters to pending
  for (const item of failedClusters) {
    updateClusterProgress(item.clusterId, {
      status: 'pending',
      progress: 0,
      message: 'Waiting to retry...',
      error: undefined
    });
  }

  submitting.value = true;

  const failedIds = failedClusters.map(p => p.clusterId);

  if (isManageMode.value) {
    await upgradeSingleCluster(failedIds[0]);
  } else {
    // Re-run installation with parallelization
    await installWithConcurrencyLimit(failedIds, INSTALL_CONCURRENCY);
  }

  submitting.value = false;
}

function onProgressModalContinueAnyway() {
  showProgressModal.value = false;
  navigateAfterSuccess();
}

// Install to a single cluster with progress callback
async function installToCluster(
  clusterId: string,
  onProgress: (progress: number, message: string) => void
) {
  const allPullSecrets = new Set<string>();

  onProgress(15, 'Preparing namespace...');

  await ensureNamespace(store, clusterId, form.value.namespace);

  onProgress(25, 'Setting up registry credentials...');

  let creds: { applicationCollection?: any; suseRegistry?: any; nvidia?: any } = {};
  try {
    creds = await getRegistryCredentials(5000);
  } catch (e: any) {
    console.warn('[SUSE-AI] Registry credentials unavailable, skipping pull secret setup:', e?.message || e);
  }

  for (const cred of [creds.applicationCollection, creds.suseRegistry, creds.nvidia]) {
    if (!cred) continue;
    try {
      const hostSlug = cred.registryHost.replace(/[^a-z0-9]/g, '-');
      const secretName = await ensureRegistrySecretSimple(
        store, clusterId, form.value.namespace,
        cred.registryHost,
        hostSlug,
        cred.username,
        cred.password,
      );
      if (secretName) allPullSecrets.add(secretName);
    } catch (e: any) {
      console.error('[SUSE-AI] pull-secret creation skipped:', e?.message || e);
    }
  }

  onProgress(45, 'Configuring image pull secrets...');

  const v = JSON.parse(JSON.stringify(form.value.values || {}));
  const pullSecrets = Array.from(allPullSecrets);

  // Determine library type from repository URL
  const repos = await listClusterRepos(store);
  const repoObj = repos.find((r: any) => r?.metadata?.name === form.value.chartRepo);
  const chartRepoUrl = repoObj?.spec?.url || repoObj?.spec?.ociRepo || '';
  const library = getLibraryFromRepoUrl(chartRepoUrl);

  // Only add pull secrets to values for non-NVIDIA charts
  if (pullSecrets.length > 0 && library !== 'nvidia') {
    const secrets = pullSecrets.map(name => ({ name }));
    v.global = { ...(v.global || {}), imagePullSecrets: secrets };
    v.imagePullSecrets = secrets;
  }

  if (pullSecrets.length > 0) {
    const saCandidates = new Set<string>(['default']);
    const vs = (v as any).serviceAccount || {};
    if (typeof vs?.name === 'string' && vs.name.trim()) saCandidates.add(vs.name.trim());
    else if (vs.create === undefined || !!vs.create) saCandidates.add(form.value.release);
    for (const sa of saCandidates) {
      for (const secretName of pullSecrets) {
        try { await ensureServiceAccountPullSecret(store, clusterId, form.value.namespace, sa, secretName); }
        catch (e) { console.warn('[SUSE-AI] SA pull-secret attach (pre) failed', { sa, ns: form.value.namespace, e }); }
      }
    }
  }

  onProgress(55, 'Installing Helm chart...');

  console.log('[SUSE-AI] calling install ', {
    cluster: clusterId,
    repo: form.value.chartRepo,
    chart: form.value.chartName,
    version: form.value.chartVersion,
    ns: form.value.namespace,
    release: form.value.release,
    values: v
  });

  const { upgraded } = await createOrUpgradeApp(
    store, clusterId, form.value.namespace, form.value.release,
    { repoName: form.value.chartRepo, chartName: form.value.chartName, version: form.value.chartVersion },
    v,
    'install'
  );

  onProgress(75, 'Waiting for app deployment...');

  try {
    await waitForAppInstall(store, clusterId, form.value.namespace, form.value.release, 180_000, upgraded);
  } catch (e: any) {
    console.error('[SUSE-AI] post-install app status (peek): ', { error: e?.message || e });
    throw e; // propagate the specific message from waitForAppInstall
  }

  onProgress(90, 'Finalizing service accounts...');

  if (pullSecrets.length > 0) {
    for (let attempt = 1; attempt <= 5; attempt++) {
      try {
        for (const secretName of pullSecrets) {
          await ensurePullSecretOnAllSAs(store, clusterId, form.value.namespace, secretName);
        }
        break;
      } catch (e) {
        if (attempt === 5) break;
        await new Promise(r => setTimeout(r, 2000));
      }
    }
  }

  onProgress(100, 'Installation complete');
}

async function performUpgrade() {
  const clusterId = form.value.clusters[0];

  installProgress.value = [{
    clusterId,
    clusterName: await getClusterDisplayName(clusterId),
    status: 'pending' as const,
    progress: 0,
    message: 'Waiting to start...'
  }];

  showProgressModal.value = true;

  try {
    await upgradeSingleCluster(clusterId);
    // Manage mode operates on a single cluster instance (the AIWorkload CR
    // the user is editing). The Helm strategy has no fleet bundle.
    await recordAIWorkload({}, form.value.deployType, clusterId);
  } finally {
    submitting.value = false;
  }
}

function getManagedBundleName(): string {
  return managedFleetBundleNames.value[0] || buildBundleName(form.value.release, form.value.namespace);
}

async function performFleetBundleUpgrade() {
  const bundleName = getManagedBundleName();

  installProgress.value = form.value.clusters.map(clusterId => ({
    clusterId,
    clusterName: clusterId,
    status:      'installing' as const,
    progress:    10,
    message:     'Updating Fleet Bundle...',
  }));
  showProgressModal.value = true;

  try {
    let creds: { applicationCollection?: any; suseRegistry?: any; nvidia?: any } = {};
    try { creds = await getRegistryCredentials(5000); } catch (e) {
      console.warn('[SUSE-AI] FleetBundle upgrade: registry credentials unavailable:', e);
    }
    const activeCreds = [creds.applicationCollection, creds.suseRegistry, creds.nvidia].filter(Boolean);
    const secretResults = await Promise.all(
      form.value.clusters.flatMap(clusterId =>
        activeCreds.map(async cred => {
          try {
            const hostSlug = cred!.registryHost.replace(/[^a-z0-9]/g, '-');
            return await ensureRegistrySecretSimple(
              store, clusterId, form.value.namespace,
              cred!.registryHost, hostSlug, cred!.username, cred!.password,
            );
          } catch (e) { console.warn('[SUSE-AI] FleetBundle upgrade: pull-secret skipped:', e); return null; }
        })
      )
    );
    const extraPullSecretNames = [...new Set(secretResults.filter((n): n is string => !!n))];

    const repos = await listClusterRepos(store);
    const repoObj = repos.find((r: any) => r?.metadata?.name === form.value.chartRepo);
    const chartRepoUrl = repoObj?.spec?.url || repoObj?.spec?.ociRepo || '';

    updateAllProgress(60, 'Updating Fleet Bundle...');
    await createFleetBundle(store, {
      bundleName,
      release:                  form.value.release,
      chartRepo:                form.value.chartRepo,
      chartRepoUrl,
      chartName:                form.value.chartName,
      chartVersion:             form.value.chartVersion,
      values:                   form.value.values,
      targetNamespace:          form.value.namespace,
      targetClusterIds:         form.value.clusters,
      additionalPullSecretNames: extraPullSecretNames,
      library:                  getLibraryFromRepoUrl(chartRepoUrl),
    });

    updateAllProgress(100, 'Fleet Bundle updated — Fleet will reconcile selected clusters');
    installProgress.value = installProgress.value.map(p => ({ ...p, status: 'success' as const }));
    managedFleetBundleNames.value = [bundleName];

    // Upgrade flow operates on a single AIWorkload CR (form.value.clusters[0]
    // is the cluster the user is editing in manage mode).
    const clusterId = form.value.clusters[0];
    await recordAIWorkload({ [clusterId]: bundleName }, 'FleetBundle', clusterId, { phase: 'Pending', clusterStatuses: [] });
  } catch (e: any) {
    installProgress.value = installProgress.value.map(p => ({
      ...p, status: 'failed' as const, error: e?.message || 'Unknown error',
    }));
    throw e;
  } finally {
    submitting.value = false;
  }
}

async function performGitOpsUpgrade() {
  const bundleName = getManagedBundleName();

  installProgress.value = form.value.clusters.map(clusterId => ({
    clusterId,
    clusterName: clusterId,
    status:      'installing' as const,
    progress:    10,
    message:     'Publishing updated Fleet Bundle YAML to git...',
  }));
  showProgressModal.value = true;

  try {
    const creds = await getRegistryCredentials(5000);
    const pullSecretNames: string[] = [];
    for (const clusterId of form.value.clusters) {
      for (const cred of [creds.applicationCollection, creds.suseRegistry, creds.nvidia]) {
        if (!cred) continue;
        try {
          const hostSlug = cred.registryHost.replace(/[^a-z0-9]/g, '-');
          const name = await ensureRegistrySecretSimple(
            store, clusterId, form.value.namespace,
            cred.registryHost, hostSlug, cred.username, cred.password,
          );
          if (name && !pullSecretNames.includes(name)) pullSecretNames.push(name);
        } catch (e) {
          console.warn('[SUSE-AI] pull-secret skipped for GitOps upgrade:', e);
        }
      }
    }

    updateAllProgress(60, 'Publishing updated Fleet Bundle YAML to git...');

    const repos = await listClusterRepos(store);
    const repoObj = repos.find((r: any) => r?.metadata?.name === form.value.chartRepo);
    const chartRepoUrl = repoObj?.spec?.url || repoObj?.spec?.ociRepo || '';

    // SUSE-registry charts can bundle subcharts whose images come from
    // AppCollection — wire those creds in too (see performGitOpsInstall).
    if (getLibraryFromRepoUrl(chartRepoUrl) === 'suse-ai') {
      await ensureAppCollectionPullSecrets(store, form.value.namespace, form.value.clusters, pullSecretNames);
    }

    await publishToFleetGit({
      bundleName,
      release:          form.value.release,
      chartName:        form.value.chartName,
      chartVersion:     form.value.chartVersion,
      chartRepoUrl,
      helmSecretName:   (() => { const cs = repoObj?.spec?.clientSecret; return typeof cs === 'object' ? (cs?.name || null) : (cs || null); })(),
      values:           form.value.values,
      pullSecretNames,
      targetClusterIds: form.value.clusters,
      targetNamespace:  form.value.namespace,
      library:          getLibraryFromRepoUrl(chartRepoUrl),
    });

    updateAllProgress(100, 'Fleet Bundle YAML committed — Fleet will reconcile selected clusters');
    installProgress.value = installProgress.value.map(p => ({ ...p, status: 'success' as const }));
    managedFleetBundleNames.value = [bundleName];

    // Upgrade flow operates on a single AIWorkload CR (form.value.clusters[0]
    // is the cluster the user is editing in manage mode).
    const clusterId = form.value.clusters[0];
    await recordAIWorkload({ [clusterId]: bundleName }, 'GitOps', clusterId, { phase: 'Pending', clusterStatuses: [] });
  } catch (e: any) {
    installProgress.value = installProgress.value.map(p => ({
      ...p, status: 'failed' as const, error: e?.message || 'Unknown error',
    }));
    throw e;
  } finally {
    submitting.value = false;
  }
}

// Upgrade a single cluster and update progress
async function upgradeSingleCluster(clusterId: string): Promise<void> {
  updateClusterProgress(clusterId, {
    status: 'installing',
    progress: 10,
    message: 'Starting upgrade...'
  });

  try {
    await upgradeToCluster(clusterId, (progress, message) => {
      updateClusterProgress(clusterId, { progress, message });
    });

    updateClusterProgress(clusterId, {
      status: 'success',
      progress: 100,
      message: 'Upgrade completed successfully'
    });
  } catch (e: any) {
    updateClusterProgress(clusterId, {
      status: 'failed',
      progress: 0,
      message: 'Upgrade failed',
      error: e?.message || 'Unknown error'
    });
  }
}

// Upgrade a single cluster with progress callback
async function upgradeToCluster(
  clusterId: string,
  onProgress: (progress: number, message: string) => void
) {
  onProgress(15, 'Preparing namespace...');
  await ensureNamespace(store, clusterId, form.value.namespace);

  onProgress(40, 'Upgrading Helm chart...');

  const v = JSON.parse(JSON.stringify(form.value.values || {}));

  console.log('[SUSE-AI] calling upgrade ', {
    cluster: clusterId,
    repo: form.value.chartRepo,
    chart: form.value.chartName,
    version: form.value.chartVersion,
    ns: form.value.namespace,
    release: form.value.release,
    values: v
  });

  await createOrUpgradeApp(
    store, clusterId, form.value.namespace, form.value.release,
    { repoName: form.value.chartRepo, chartName: form.value.chartName, version: form.value.chartVersion },
    v,
    'upgrade'
  );

  onProgress(70, 'Waiting for app deployment...');

  try {
    await waitForAppInstall(store, clusterId, form.value.namespace, form.value.release, 180_000, true);
  } catch (e: any) {
    console.error('[SUSE-AI] post-upgrade app status (peek): ', { error: e?.message || e });
    throw new Error(e?.message || `App upgrade failed in namespace ${form.value.namespace}`);
  }

  onProgress(100, 'Upgrade complete');
}

// Custom wizard navigation methods
function goToStep(stepIndex: number) {
  // Backwards navigation is always allowed. Going forward requires both the current
  // step and the target step to be ready, so clicking a step tab can't bypass the
  // same gate that disables the Next button (e.g. an invalid instance name on step 0).
  const goingForward = stepIndex > currentStep.value;
  if (!goingForward || (wizardSteps.value[currentStep.value].ready && wizardSteps.value[stepIndex].ready)) {
    currentStep.value = stepIndex;
  }
}

function nextStep() {
  if (currentStep.value < wizardSteps.value.length - 1 && wizardSteps.value[currentStep.value + 1].ready) {
    currentStep.value++;
  }
}

function previousStep() {
  if (currentStep.value > 0) {
    currentStep.value--;
  }
}
</script>

<template>
  <div class="install-steps pt-20 outlet">
    <Loading v-if="loading" />
    
    <div v-else class="custom-wizard">
      <!-- Fixed Header -->
      <div class="wizard-header">
        <h1>{{ (route.query.n as string) || props.slug }}</h1>
        <p class="text-muted">{{ wizardTitle }}</p>
      </div>

      <!-- Fixed Step Navigation -->
      <div class="wizard-nav">
        <div class="steps-container">
          <div 
            v-for="(step, index) in wizardSteps" 
            :key="step.name"
            class="step-item"
            :class="{
              'active': index === currentStep,
              'completed': index < currentStep,
              'disabled': !step.ready && index > currentStep
            }"
            @click="goToStep(index)"
          >
            <div class="step-number">
              <i v-if="index < currentStep" class="icon icon-checkmark" />
              <span v-else>{{ index + 1 }}</span>
            </div>
            <div class="step-label">{{ step.label }}</div>
          </div>
        </div>
      </div>

      <!-- Scrollable Content Area -->
      <div class="wizard-content-wrapper">
        <!-- Error Banner -->
        <Banner v-if="error" color="error" class="mb-20">
          {{ error }}
        </Banner>

        <!-- Step Content -->
        <div class="wizard-content">
          <!-- Step: Basic Information -->
          <BasicInfoStep
            v-if="currentStep === 0"
            v-model:form="basicInfoForm"
            :version-options="versionOptions"
            :loading-versions="loadingVersions"
            :namespace-options="namespaceOptions"
            :loading-namespaces="loadingNamespaces"
            :release-disabled="isManageMode"
            :namespace-disabled="isManageMode"
          />

          <!-- Step: Target Cluster -->
          <TargetStep
            v-else-if="currentStep === 1"
            :mode="props.mode"
            v-model:clusters="form.clusters"
            v-model:deployType="form.deployType"
            :helm-oversized="helmOversized"
            :git-ops-unconfigured="!fleetGitConfigured"
          />

          <!-- Step: Configuration -->
          <ValuesStep
            v-else-if="currentStep === 2"
            v-model:values="form.values"
            :chart-repo="form.chartRepo"
            :chart-name="form.chartName"
            :chart-version="form.chartVersion"
            :loading-values="loadingValues"
            :version-dirty="false"
            :has-questions="hasQuestions"
            :questions-source="versionInfo"
            :questions-loading="questionsLoading"
            :ignore-variables="ignoreVariables"
            :target-namespace="form.namespace"
            :mode="props.mode"
            :in-store="inStore"
            @load-defaults="onLoadDefaults"
            @values-edited="onValuesEdited"
          />

          <!-- Step: Review -->
          <ReviewStep
            v-else-if="currentStep === 3"
            :mode="props.mode"
            :release="form.release"
            :namespace="form.namespace"
            :chart-repo="form.chartRepo"
            :chart-name="form.chartName"
            :chart-version="form.chartVersion"
            :clusters="form.clusters"
            v-model:values="form.values"
            @values-edited="onValuesEdited"
          />
        </div>
      </div>

      <!-- Fixed Bottom Navigation Buttons -->
      <div class="wizard-buttons-fixed">
        <button
          v-if="currentStep > 0"
          class="btn role-secondary"
          @click="previousStep"
        >
          Previous
        </button>

        <div class="flex-spacer" />

        <button
          class="btn role-secondary mr-10"
          @click="onWizardCancel"
        >
          Cancel
        </button>

        <button
          v-if="currentStep === 0 || currentStep === 1 || currentStep === 2"
          class="btn role-primary"
          :disabled="!wizardSteps[currentStep].ready"
          @click="nextStep"
        >
          Next
        </button>

        <button
          v-else-if="currentStep === 3"
          class="btn role-primary"
          :disabled="!wizardSteps[currentStep].ready || submitting"
          @click="onWizardFinish"
        >
          <i v-if="submitting" class="icon icon-spinner icon-spin mr-5" />
          <span v-if="submitting">
            {{ props.mode === 'install' ? 'Installing...' : 'Saving...' }}
          </span>
          <span v-else>
            {{ props.mode === 'install' ? 'Install' : 'Save' }}
          </span>
        </button>
      </div>
    </div>

    <!-- Multi-cluster Install Progress Modal -->
    <InstallProgressModal
      :show="showProgressModal"
      :progress="installProgress"
      :title="`${isInstallMode ? 'Installing' : 'Upgrading'} ${(route.query.n as string) || props.slug}`"
      @done="onProgressModalDone"
      @cancel="onProgressModalCancel"
      @retry-all="onProgressModalRetryAll"
      @retry-failed="onProgressModalRetryFailed"
      @continue-anyway="onProgressModalContinueAnyway"
    />
  </div>
</template>


<style scoped>
/* Ensure Loading overlay inherits Rancher's standard background */
.install-steps {
  --overlay-bg: var(--body-bg, #ffffff);
}

/* Button utilities */
.mr-5 {
  margin-right: 5px;
}

.icon-spin {
  animation: spin 1s linear infinite;
}

@keyframes spin {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}

/* Main wizard container - matches Rancher's main content areas */
.custom-wizard {
  background: var(--body-bg, #ffffff);
  max-width: 100%;
  width: 100%;
  height: calc(100vh - 140px);
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

/* Header styling to match Rancher's page headers */
.wizard-header {
  flex-shrink: 0;
  padding: 20px 24px 16px 24px;
  background: var(--body-bg, #ffffff);
}

.wizard-header h1 {
  margin: 0 0 4px 0;
  font-size: 20px;
  font-weight: 600;
  color: var(--body-text, #111827);
  line-height: 1.2;
}

.wizard-header p {
  margin: 0;
  font-size: 14px;
  color: var(--muted, #6b7280);
  font-weight: 400;
}

/* Step navigation - matches Rancher's wizard pattern */
.wizard-nav {
  flex-shrink: 0;
  width: 100%;
  padding: 20px 24px;
  background: var(--body-bg, #ffffff);
}

.steps-container {
  display: flex;
  justify-content: space-between;
  position: relative;
  max-width: 100%;
  align-items: center;
}

.steps-container::before {
  content: '';
  position: absolute;
  top: 20px;
  left: 50px;
  right: 50px;
  height: 1px;
  background: var(--border, #f3f4f6);
  z-index: 0;
}

.step-item {
  display: flex;
  flex-direction: column;
  align-items: center;
  cursor: pointer;
  flex: 1;
  max-width: 200px;
  position: relative;
  z-index: 1;
  transition: all 0.2s ease;
}

.step-item.disabled {
  cursor: not-allowed;
  opacity: 0.6;
}

.step-item:hover:not(.disabled) .step-number {
  transform: scale(1.05);
}

.step-number {
  width: 40px;
  height: 40px;
  border-radius: 50%;
  background: var(--body-bg, #ffffff);
  border: 1px solid var(--border, #f3f4f6);
  color: var(--muted, #9ca3af);
  display: flex;
  align-items: center;
  justify-content: center;
  font-weight: 500;
  font-size: 14px;
  margin-bottom: 8px;
  transition: all 0.2s ease;
}

.step-item.active .step-number {
  background: var(--primary, #2563eb);
  border-color: var(--primary, #2563eb);
  color: white;
}

.step-item.completed .step-number {
  background: var(--success, #16a34a);
  border-color: var(--success, #16a34a);
  color: white;
}

.step-label {
  font-size: 13px;
  text-align: center;
  color: var(--muted, #6b7280);
  font-weight: 400;
  line-height: 1.3;
}

.step-item.active .step-label {
  color: var(--primary, #2563eb);
  font-weight: 500;
}

.step-item.completed .step-label {
  color: var(--body-text, #111827);
}

/* Content area - matches Rancher's form containers */
.wizard-content-wrapper {
  flex: 1;
  overflow-y: auto;
  min-height: 0;
  background: var(--body-bg, #ffffff);
}

.wizard-content {
  padding: 24px;
  background: var(--body-bg, #ffffff);
  margin: 0;
  min-height: 100%;
}

/* Bottom button bar - matches Rancher's action bars */
.wizard-buttons-fixed {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 12px;
  width: 100%;
  padding: 16px 24px;
  background: var(--body-bg, #ffffff);
}

.flex-spacer {
  flex: 1;
}

/* Button overrides to match Rancher's button styling */
.wizard-buttons-fixed .btn {
  height: 36px;
  padding: 0 16px;
  font-size: 14px;
  font-weight: 500;
  border-radius: 4px;
  transition: all 0.2s ease;
}

.wizard-buttons-fixed .btn.role-secondary,
.wizard-buttons-fixed .btn.role-secondary:focus,
.wizard-buttons-fixed .btn.role-secondary:active {
  background: var(--body-bg, #ffffff);
  border: 1px solid var(--border, #d1d5db);
  color: var(--body-text, #111827) !important;
  box-shadow: none;
}

.wizard-buttons-fixed .btn.role-secondary:hover {
  background: var(--body-bg, #ffffff);
  border-color: var(--border-hover, #9ca3af);
  color: var(--body-text, #111827) !important;
}

.wizard-buttons-fixed .btn.role-secondary:focus-visible {
  outline: 2px solid var(--primary, #2563eb);
  outline-offset: 2px;
}

.wizard-buttons-fixed .btn.role-primary {
  background: var(--primary, #2563eb);
  border: 1px solid var(--primary, #2563eb);
  color: white;
}

.wizard-buttons-fixed .btn.role-primary:hover:not(:disabled) {
  background: var(--primary-hover, #1d4ed8);
  border-color: var(--primary-hover, #1d4ed8);
}

.wizard-buttons-fixed .btn:disabled {
  opacity: 0.6;
  cursor: not-allowed;
}

/* Utility classes */
.mb-20 {
  margin-bottom: 20px;
}
</style>
