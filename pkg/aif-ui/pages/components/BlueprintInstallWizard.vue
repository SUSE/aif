<script lang="ts" setup>
import { ref, computed, watch, onMounted, getCurrentInstance } from 'vue';
import { useT } from '../../composables/useT';
import { Banner } from '@components/Banner';
import Loading from '@shell/components/Loading';
import BlueprintInstallBasicInfoStep from './wizard/BlueprintInstallBasicInfoStep.vue';
import TargetStep                    from './wizard/TargetStep.vue';
import BlueprintInstallReviewStep    from './wizard/BlueprintInstallReviewStep.vue';
import InstallProgressModal, { type ClusterInstallProgress } from './wizard/InstallProgressModal.vue';
import { getBlueprint, blueprintCRName, slugifyBlueprintName } from '../../utils/blueprint-api';
import { createAIWorkload, listAIWorkloads } from '../../utils/operator-api';
import { useFleetGitConfigured } from '../../composables/useFleetGitConfigured';
import type { Blueprint } from '../../types/blueprint-types';
import type { AIWorkloadDeployStrategy } from '../../types/aiworkload-types';
import { PRODUCT } from '../../config/suseai';
import logger from '../../utils/logger';

interface Props {
  blueprintName:    string;
  blueprintVersion: string;
}
const props   = defineProps<Props>();
// eslint-disable-next-line @typescript-eslint/no-explicit-any
const vm      = getCurrentInstance()?.proxy as any;
const router  = vm?.$router;
const route   = vm?.$route;

const t = useT();
const cluster = (route?.params?.cluster as string) || '_';

const loading     = ref(true);
const submitting  = ref(false);
const error       = ref<string | null>(null);
const blueprint   = ref<Blueprint | null>(null);
const currentStep = ref(0);

const workloadName = ref('');
const namespace    = ref('');
const clusters     = ref<string[]>([]);
const deployType   = ref<AIWorkloadDeployStrategy>('FleetBundle');
const { fleetGitConfigured, fetchFleetGitConfigured } = useFleetGitConfigured();

watch(fleetGitConfigured, (configured) => {
  if (!configured && deployType.value === 'GitOps') {
    deployType.value = 'FleetBundle';
  }
}, { immediate: true });

const showProgressModal = ref(false);
const installProgress   = ref<ClusterInstallProgress[]>([]);

const wizardSteps = computed(() => [
  { label: t('suseai.wizard.steps.basicInfo', 'Basic Information'),     ready: true },
  { label: t('suseai.wizard.steps.targetCluster', 'Target Cluster'),    ready: workloadName.value.trim() !== '' && namespace.value !== '' },
  { label: t('suseai.wizard.steps.review', 'Review'),                   ready: clusters.value.length > 0 },
]);

onMounted(async () => {
  try {
    const crName = blueprintCRName(props.blueprintName, props.blueprintVersion);
    blueprint.value = await getBlueprint(crName);
    const slug = slugifyBlueprintName(props.blueprintName);
    workloadName.value = slug;
    namespace.value    = `${ slug }-system`;
  } catch (e: unknown) {
    error.value = (e instanceof Error ? e.message : null) || 'Failed to load blueprint';
  } finally {
    loading.value = false;
  }

  await fetchFleetGitConfigured();
});

function nextStep() {
  if (currentStep.value < 2 && wizardSteps.value[currentStep.value + 1].ready) currentStep.value++;
}
function previousStep() {
  if (currentStep.value > 0) currentStep.value--;
}
function onCancel() {
  router.push({ name: `c-cluster-${ PRODUCT }-blueprints`, params: { cluster } });
}

const DNS_LABEL = /^[a-z0-9][a-z0-9-]{0,61}[a-z0-9]$|^[a-z0-9]$/;

async function onInstall() {
  if (!blueprint.value) return;

  if (!DNS_LABEL.test(workloadName.value)) {
    error.value = 'Deployment name must be lowercase alphanumeric and hyphens only, 1–63 characters, and must start and end with a letter or digit.';
    return;
  }

  submitting.value = true;
  error.value      = null;

  try {
    const { items } = await listAIWorkloads();
    const exists = items.some(
      w => w.metadata?.namespace === namespace.value && w.metadata?.name === workloadName.value,
    );
    if (exists) {
      error.value = `A deployment named "${workloadName.value}" already exists in namespace "${namespace.value}". Choose a different deployment name.`;
      submitting.value = false;
      return;
    }
  } catch (e) {
    logger.warn('[SUSE-AI] Could not check for existing deployments (proceeding)', { data: e });
  }

  installProgress.value = clusters.value.map(c => ({
    clusterId:   c,
    clusterName: c,
    status:      'installing' as const,
    progress:    10,
    message:     'Creating AIWorkload CR...',
  }));
  showProgressModal.value = true;

  try {
    await createAIWorkload(
      namespace.value,
      workloadName.value,
      {
        displayName:     blueprint.value.spec.displayName,
        source: {
          sourceType: 'Blueprint',
          blueprint: {
            name:    props.blueprintName,
            version: props.blueprintVersion,
          },
        },
        targetNamespace: namespace.value,
        targetClusters:  clusters.value,
        deployStrategy:  deployType.value,
      },
      { phase: 'Pending', clusterStatuses: [] },
    );

    installProgress.value = installProgress.value.map(p => ({
      ...p,
      status:   'success' as const,
      progress: 100,
      message:  'AIWorkload created — controller will deploy bundles',
    }));
  } catch (e: unknown) {
    const errMsg = (e as Record<string, unknown>)?.status === 409
      ? `A deployment named "${workloadName.value}" already exists in namespace "${namespace.value}". Choose a different deployment name.`
      : ((e instanceof Error ? e.message : null) || 'Unknown error');
    installProgress.value = installProgress.value.map(p => ({
      ...p,
      status:  'failed' as const,
      message: errMsg,
      error:   errMsg,
    }));
    error.value = errMsg;
  } finally {
    submitting.value = false;
  }
}

function onProgressDone() {
  showProgressModal.value = false;
  router.push({ name: `c-cluster-${ PRODUCT }-blueprints`, params: { cluster } });
}
function onProgressCancel() { showProgressModal.value = false; }
</script>

<template>
  <div class="install-steps pt-20 outlet">
    <Loading v-if="loading" />
    <div
      v-else
      class="custom-wizard"
    >
      <div class="wizard-header">
        <h1>Install Blueprint</h1>
        <p class="text-muted">
          {{ blueprint?.spec.displayName }} v{{ props.blueprintVersion }}
        </p>
      </div>

      <div class="wizard-nav">
        <div class="steps-container">
          <div
            v-for="(step, idx) in wizardSteps"
            :key="step.label"
            class="step-item"
            :class="{ active: idx === currentStep, completed: idx < currentStep }"
          >
            <div class="step-number">
              <i
                v-if="idx < currentStep"
                class="icon icon-checkmark"
              />
              <span v-else>{{ idx + 1 }}</span>
            </div>
            <div class="step-label">
              {{ step.label }}
            </div>
          </div>
        </div>
      </div>

      <div class="wizard-content-wrapper">
        <Banner
          v-if="error"
          color="error"
          class="mb-20"
        >
          {{ error }}
        </Banner>
        <div class="wizard-content">
          <BlueprintInstallBasicInfoStep
            v-if="currentStep === 0"
            :display-name="blueprint?.spec.displayName || ''"
            :version="props.blueprintVersion"
            :component-count="blueprint?.spec.components.length || 0"
            :workload-name="workloadName"
            :namespace="namespace"
            @update:workload-name="workloadName = $event"
            @update:namespace="namespace = $event"
          />
          <TargetStep
            v-else-if="currentStep === 1"
            mode="install"
            :clusters="clusters"
            :deploy-type="deployType"
            :helm-unsupported="true"
            :git-ops-unconfigured="!fleetGitConfigured"
            @update:clusters="clusters = $event"
            @update:deploy-type="deployType = $event"
          />
          <BlueprintInstallReviewStep
            v-else-if="currentStep === 2"
            :workload-name="workloadName"
            :namespace="namespace"
            :display-name="blueprint?.spec.displayName || ''"
            :version="props.blueprintVersion"
            :component-count="blueprint?.spec.components.length || 0"
            :deploy-type="deployType"
            :clusters="clusters"
            :components="blueprint?.spec.components || []"
          />
        </div>
      </div>

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
          @click="onCancel"
        >
          Cancel
        </button>
        <button
          v-if="currentStep < 2"
          class="btn role-primary"
          :disabled="!wizardSteps[currentStep + 1]?.ready"
          @click="nextStep"
        >
          Next
        </button>
        <button
          v-else
          class="btn role-primary"
          :disabled="submitting || clusters.length === 0"
          @click="onInstall"
        >
          <i
            v-if="submitting"
            class="icon icon-spinner icon-spin mr-5"
          />
          Install
        </button>
      </div>
    </div>

    <InstallProgressModal
      :show="showProgressModal"
      :progress="installProgress"
      :title="`Installing ${ blueprint?.spec.displayName || '' }`"
      @done="onProgressDone"
      @cancel="onProgressCancel"
      @retry-all="onProgressCancel"
      @retry-failed="onProgressCancel"
      @continue-anyway="onProgressDone"
    />
  </div>
</template>

<style scoped>
.custom-wizard {
  background: var(--body-bg, #fff); max-width: 100%; width: 100%;
  height: calc(100vh - 140px); display: flex; flex-direction: column; overflow: hidden;
}
.wizard-header { flex-shrink: 0; padding: 20px 24px 16px; }
.wizard-header h1 { margin: 0 0 4px; font-size: 20px; font-weight: 600; }
.wizard-nav { flex-shrink: 0; padding: 20px 24px; }
.steps-container {
  display: flex; justify-content: space-between; align-items: center; position: relative;
}
.steps-container::before {
  content: ''; position: absolute; top: 20px; left: 50px; right: 50px;
  height: 1px; background: var(--border); z-index: 0;
}
.step-item {
  display: flex; flex-direction: column; align-items: center;
  flex: 1; max-width: 200px; position: relative; z-index: 1;
}
.step-number {
  width: 40px; height: 40px; border-radius: 50%; border: 1px solid var(--border);
  display: flex; align-items: center; justify-content: center;
  background: var(--body-bg); color: var(--muted); font-size: 14px; margin-bottom: 8px;
}
.step-item.active .step-number { background: var(--primary); border-color: var(--primary); color: white; }
.step-item.completed .step-number { background: var(--success); border-color: var(--success); color: white; }
.step-label { font-size: 13px; color: var(--muted); text-align: center; }
.step-item.active .step-label { color: var(--primary); font-weight: 500; }
.wizard-content-wrapper { flex: 1; overflow-y: auto; }
.wizard-content { padding: 24px; }
.wizard-buttons-fixed {
  flex-shrink: 0; display: flex; align-items: center; gap: 12px; padding: 16px 24px;
}
.flex-spacer { flex: 1; }
.btn {
  display: inline-flex; align-items: center; gap: 6px; height: 36px; padding: 0 16px;
  font-size: 14px; font-weight: 500; border-radius: 4px; border: 1px solid; cursor: pointer;
}
.btn.role-primary { background: var(--primary); border-color: var(--primary); color: white; }
.btn.role-secondary { background: var(--body-bg); border-color: var(--border); color: var(--body-text); }
.btn:disabled { opacity: 0.6; cursor: not-allowed; }
.mb-20 { margin-bottom: 20px; }
.mr-10 { margin-right: 10px; }
.mr-5 { margin-right: 5px; }
.text-muted { color: var(--muted); font-size: 14px; }
.icon-spin { animation: spin 1s linear infinite; }
@keyframes spin { from { transform: rotate(0deg); } to { transform: rotate(360deg); } }
</style>
