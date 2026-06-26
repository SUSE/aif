<script lang="ts" setup>
import { ref, computed, getCurrentInstance, defineAsyncComponent } from 'vue';
import { useT } from '../../composables/useT';
import { Banner } from '@components/Banner';
import BlueprintBasicInfoStep   from './wizard/BlueprintBasicInfoStep.vue';
import BlueprintAppSelectorStep from './wizard/BlueprintAppSelectorStep.vue';
// Steps 3 & 4 are loaded asynchronously so that Steps 1-2 render even before Task 4 files exist.
const BlueprintConfigStep       = defineAsyncComponent(() => import('./wizard/BlueprintConfigStep.vue'));
const BlueprintReviewCreateStep = defineAsyncComponent(() => import('./wizard/BlueprintReviewCreateStep.vue'));
import type { BlueprintSpec } from '../../types/blueprint-types';
import { SEMVER_PATTERN, DNS_LABEL_PATTERN } from '../../types/blueprint-types';
import { createBlueprint } from '../../utils/blueprint-api';
import { PRODUCT } from '../../config/suseai';

interface BasicInfo {
  displayName: string;
  version:     string;
  description: string;
}

interface Props {
  editName?:    string;
  fromVersion?: string;
  prefill?:     BlueprintSpec;
}

const props  = defineProps<Props>();
// eslint-disable-next-line @typescript-eslint/no-explicit-any
const vm     = getCurrentInstance()?.proxy as any;
const router = vm?.$router;
const route  = vm?.$route;

const t = useT();
const cluster = (route?.params?.cluster as string) || '_';

const error      = ref<string | null>(null);
const submitting = ref(false);
const currentStep = ref(0);

const basicInfo = ref<BasicInfo>({
  displayName: props.prefill?.displayName || '',
  version:     props.prefill?.version || '',
  description: props.prefill?.description || '',
});

const components = ref(
  props.prefill?.components?.map(c => ({ ...c })) || []
);

const namespacesValid = computed(() => components.value.every((c) => {
  const ns = c.targetNamespace?.trim();
  return !ns || (ns.length <= 63 && DNS_LABEL_PATTERN.test(ns));
}));

const wizardSteps = computed(() => [
  { label: t('suseai.wizard.steps.basicInfo', 'Basic Information'),      ready: true },
  {
    label: t('suseai.wizard.steps.selectApps', 'Select Applications'),
    ready: basicInfo.value.displayName.trim() !== '' && SEMVER_PATTERN.test(basicInfo.value.version),
  },
  { label: t('suseai.wizard.steps.configuration', 'Configuration'),   ready: components.value.length > 0 && namespacesValid.value },
  { label: t('suseai.wizard.steps.review', 'Review'),                 ready: components.value.length > 0 && namespacesValid.value },
]);

function nextStep() {
  if (currentStep.value < 3 && wizardSteps.value[currentStep.value + 1]?.ready) currentStep.value++;
}

function previousStep() {
  if (currentStep.value > 0) currentStep.value--;
}

function goToStep(i: number) {
  if (i <= currentStep.value || wizardSteps.value[i].ready) currentStep.value = i;
}

function onCancel() {
  router.push({ name: `c-cluster-${ PRODUCT }-blueprints`, params: { cluster } });
}

async function onCreate() {
  submitting.value = true;
  error.value = null;
  if (!namespacesValid.value) {
    error.value = t('suseai.wizard.form.componentNamespacesInvalid', 'One or more components have an invalid target namespace.');
    submitting.value = false;
    return;
  }
  try {
    const spec: BlueprintSpec = {
      displayName: basicInfo.value.displayName,
      version:     basicInfo.value.version,
      description: basicInfo.value.description || undefined,
      source:      props.prefill?.source ?? 'Custom',
      components:  components.value,
    };
    await createBlueprint(spec);
    router.push({ name: `c-cluster-${ PRODUCT }-blueprints`, params: { cluster } });
  } catch (e: unknown) {
    error.value = (e instanceof Error ? e.message : null) || 'Failed to create blueprint';
  } finally {
    submitting.value = false;
  }
}

const reviewForm = computed<BlueprintSpec>(() => ({
  displayName: basicInfo.value.displayName,
  version:     basicInfo.value.version,
  description: basicInfo.value.description || undefined,
  source:      props.prefill?.source ?? 'Custom',
  components:  components.value,
}));
</script>

<template>
  <div class="custom-wizard">
    <div class="wizard-header">
      <h1>{{ props.editName ? 'Edit Blueprint' : 'Create Blueprint' }}</h1>
      <p class="text-muted">
        {{ props.editName ? 'Save as a new version' : 'Define a reusable multi-app template' }}
      </p>
    </div>

    <div class="wizard-nav">
      <div class="steps-container">
        <div
          v-for="(step, idx) in wizardSteps"
          :key="step.label"
          class="step-item"
          :class="{ active: idx === currentStep, completed: idx < currentStep, disabled: !step.ready && idx > currentStep }"
          @click="goToStep(idx)"
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
        <BlueprintBasicInfoStep
          v-if="currentStep === 0"
          :form="basicInfo"
          :name-disabled="!!props.editName"
          @update:form="basicInfo = $event"
        />
        <BlueprintAppSelectorStep
          v-else-if="currentStep === 1"
          :components="components"
          @update:components="components = $event"
        />
        <BlueprintConfigStep
          v-else-if="currentStep === 2"
          :components="components"
          @update:components="components = $event"
        />
        <BlueprintReviewCreateStep
          v-else-if="currentStep === 3"
          :form="reviewForm"
          :is-edit="!!props.editName"
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
        v-if="currentStep < 3"
        class="btn role-primary"
        :disabled="!wizardSteps[currentStep + 1]?.ready"
        @click="nextStep"
      >
        Next
      </button>
      <button
        v-else
        class="btn role-primary"
        :disabled="submitting || !wizardSteps[3].ready"
        @click="onCreate"
      >
        <i
          v-if="submitting"
          class="icon icon-spinner icon-spin mr-5"
        />
        {{ props.editName ? 'Save as New Version' : 'Create' }}
      </button>
    </div>
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
  cursor: pointer; flex: 1; max-width: 200px; position: relative; z-index: 1;
}
.step-number {
  width: 40px; height: 40px; border-radius: 50%; border: 1px solid var(--border);
  display: flex; align-items: center; justify-content: center;
  font-weight: 500; font-size: 14px; margin-bottom: 8px;
  background: var(--body-bg); color: var(--muted);
}
.step-item.active .step-number { background: var(--primary); border-color: var(--primary); color: white; }
.step-item.completed .step-number { background: var(--success); border-color: var(--success); color: white; }
.step-label { font-size: 13px; color: var(--muted); text-align: center; }
.step-item.active .step-label { color: var(--primary); font-weight: 500; }
.wizard-content-wrapper { flex: 1; overflow-y: auto; }
.wizard-content { padding: 24px; }
.wizard-buttons-fixed {
  flex-shrink: 0; display: flex; align-items: center; gap: 12px;
  padding: 16px 24px; background: var(--body-bg);
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
