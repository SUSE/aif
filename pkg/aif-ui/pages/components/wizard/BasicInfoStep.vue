<template>
  <div class="basic-info-step">
    <div class="row">
      <div class="col span-6">
        <LabeledInput
          v-model:value="release"
          :label="t('suseai.wizard.form.release', 'Instance name')"
          :placeholder="t('suseai.wizard.form.releasePlaceholder', 'Enter instance name')"
          :disabled="props.releaseDisabled"
          required
        />
      </div>
      <div class="col span-6">
        <LabeledSelect
          v-model:value="namespace"
          :label="t('suseai.wizard.form.namespace', 'Namespace')"
          :options="namespaceOptions"
          :placeholder="t('suseai.wizard.form.namespacePlaceholder', 'Select or create a namespace')"
          :taggable="true"
          :searchable="true"
          :clearable="false"
          :required="true"
          :disabled="props.namespaceDisabled"
        />
      </div>
    </div>

    <div class="row mt-20">
      <div class="col span-6">
        <LabeledInput
          v-model:value="chartName"
          :label="t('suseai.wizard.form.chartName', 'Chart name')"
          :placeholder="t('suseai.wizard.form.chartNamePlaceholder', 'e.g. ollama')"
          :disabled="true"
        />
      </div>
      <div class="col span-6">
        <LabeledSelect
          v-model:value="chartVersion"
          :label="t('suseai.wizard.form.version', 'Version')"
          :options="versionOptions"
          :loading="loadingVersions"
          :disabled="!versionOptions.length && !props.form.chartVersion"
          required
        />
      </div>
    </div>
  </div>
</template>

<script lang="ts" setup>
import { computed } from 'vue';
import { LabeledInput } from '@components/Form/LabeledInput';
import LabeledSelect from '@shell/components/form/LabeledSelect';

export interface BasicInfoForm {
  release: string;
  namespace: string;
  chartRepo: string;
  chartName: string;
  chartVersion: string;
}

interface Props {
  form: BasicInfoForm;
  versionOptions: Array<{ label: string; value: string }>;
  loadingVersions: boolean;
  namespaceOptions: Array<{ label: string; value: string }>;
  releaseDisabled?: boolean;
  namespaceDisabled?: boolean;
}

interface Emits {
  (e: 'update:form', form: BasicInfoForm): void;
}

const props = defineProps<Props>();
const emit = defineEmits<Emits>();

// Simple fallback function for translations
const t = (key: string, fallback: string) => fallback;

// Individual field computeds for better reactivity
const release = computed({
  get: () => props.form.release,
  set: (value: string) => emit('update:form', { ...props.form, release: value })
});

const namespace = computed({
  get: () => props.form.namespace,
  set: (value: string | { label: string }) => {
    const namespaceName = typeof value === 'object' ? value.label : value;
    emit('update:form', { ...props.form, namespace: namespaceName });
  }
});

const chartName = computed({
  get: () => props.form.chartName,
  set: (value: string) => emit('update:form', { ...props.form, chartName: value })
});

const chartVersion = computed({
  get: () => props.form.chartVersion,
  set: (value: string) => emit('update:form', { ...props.form, chartVersion: value })
});
</script>

<style scoped>
.basic-info-step {
  width: 100%;
  max-width: 100%;
  box-sizing: border-box;
}

.mt-20 {
  margin-top: 20px;
}

/* Ensure form fields don't overflow */
.basic-info-step .row {
  margin-left: 0;
  margin-right: 0;
  width: 100%;
}

.basic-info-step .col {
  padding-left: 10px;
  padding-right: 10px;
}

/* Responsive adjustments */
@media (max-width: 768px) {
  .basic-info-step .row {
    flex-direction: column;
  }
  
  .basic-info-step .col {
    width: 100% !important;
    max-width: 100% !important;
    flex: none;
    padding-left: 0;
    padding-right: 0;
    margin-bottom: 15px;
  }
}
</style>