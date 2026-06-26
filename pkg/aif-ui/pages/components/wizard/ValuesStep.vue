<template>
  <div class="values-step">
    <div class="values-header">
      <h3>{{ t('suseai.wizard.form.valuesYaml', 'Configuration') }}</h3>
      <div class="header-actions">
        <div class="view-toggle" role="group" aria-label="Configuration view selector">
          <button
            type="button"
            class="btn btn-sm"
            :class="viewMode === 'form' ? 'role-primary' : 'role-secondary'"
            :disabled="!hasQuestions"
            @click="setViewMode('form')"
          >
            Form
          </button>
          <button
            type="button"
            class="btn btn-sm"
            :class="viewMode === 'yaml' ? 'role-primary' : 'role-secondary'"
            @click="setViewMode('yaml')"
          >
            YAML
          </button>
        </div>

        <button
          class="btn btn-sm role-secondary"
          type="button"
          :disabled="loadingValues"
          @click="emit('load-defaults')"
        >
          <i v-if="loadingValues" class="icon icon-spinner icon-spin" aria-hidden="true" />
          Load defaults
        </button>
      </div>
    </div>

    <div class="values-body">
      <div v-if="showFormView" class="form-container">
        <Loading v-if="questionsLoading" class="questions-loading" />
        <template v-else>
          <Tabbed
            v-if="questionsSource"
            ref="tabs"
            :side-tabs="true"
            :hide-single-tab="true"
            flat
            class="questions-tabs"
          >
            <Questions
              v-model:value="localValues"
              emit
              tabbed="multiple"
              :mode="normalizedMode"
              :in-store="inStoreValue"
              :target-namespace="targetNamespace"
              :source="questionsSource"
              :ignore-variables="ignoreVariables"
              @updated="handleQuestionsUpdated"
            />
          </Tabbed>
          <Banner v-else color="warning">
            {{ t('suseai.wizard.form.questionsUnavailable', 'Questions metadata is not available for this chart version. Use the YAML editor instead.') }}
          </Banner>
        </template>
      </div>

      <YamlEditor
        v-else
        v-model:value="localValues"
        :as-object="true"
        class="values-editor"
        @update:value="handleYamlUpdate"
      />
    </div>

    <Banner v-if="versionDirty" color="info" class="mt-10">
      {{ t('suseai.wizard.form.versionDirtyWarning', 'Version changed after you edited values. Click "Load defaults from chart" to apply defaults for the new version, or continue with your customizations.') }}
    </Banner>
  </div>
</template>

<script lang="ts" setup>
import { computed, ref, watch } from 'vue';
import YamlEditor from '@shell/components/YamlEditor';
import { useT } from '../../../composables/useT';
import Questions from '@shell/components/Questions';
import Loading from '@shell/components/Loading';
import Tabbed from '@shell/components/Tabbed';
import { Banner } from '@components/Banner';

interface Props {
  values: Record<string, any>;
  chartRepo: string;
  chartName: string;
  chartVersion: string;
  loadingValues: boolean;
  versionDirty: boolean;
  hasQuestions: boolean;
  questionsSource: Record<string, any> | null;
  questionsLoading: boolean;
  ignoreVariables: string[];
  targetNamespace: string;
  mode: string;
  inStore: string | null;
}

interface Emits {
  (e: 'update:values', values: Record<string, any>): void;
  (e: 'load-defaults'): void;
  (e: 'values-edited'): void;
}

const props = defineProps<Props>();
const emit = defineEmits<Emits>();

const t = useT();

const viewMode = ref<'form' | 'yaml'>(props.hasQuestions ? 'form' : 'yaml');
const normalizedMode = computed(() => props.mode === 'install' ? 'create' : 'edit');
const inStoreValue = computed(() => props.inStore || 'cluster');

const localValues = computed({
  get: () => props.values,
  set: (value: Record<string, any>) => {
    emit('update:values', value);
  }
});

const showFormView = computed(() => props.hasQuestions && viewMode.value === 'form');

watch(() => props.hasQuestions, (val) => {
  if (!val && viewMode.value === 'form') {
    viewMode.value = 'yaml';
  } else if (val && viewMode.value === 'yaml') {
    viewMode.value = 'form';
  }
});

function setViewMode(mode: 'form' | 'yaml') {
  if (mode === 'form' && !props.hasQuestions) {
    return;
  }
  viewMode.value = mode;
}

function handleYamlUpdate(value: Record<string, any>) {
  emit('update:values', value);
  emit('values-edited');
}

function handleQuestionsUpdated() {
  emit('values-edited');
}
</script>

<style scoped>
.values-step {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.values-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 16px;
}

.values-header h3 {
  margin: 0;
  font-size: 18px;
  font-weight: 600;
  color: var(--body-text);
}

.header-actions {
  display: flex;
  align-items: center;
  gap: 10px;
}

.view-toggle {
  display: inline-flex;
  border: 1px solid var(--border);
  border-radius: 6px;
  overflow: hidden;
}

.view-toggle .btn {
  border-radius: 0;
}

.values-body {
  min-height: 400px;
}

.values-editor {
  min-height: 400px;
}

.form-container {
  min-height: 400px;
  padding: 0 4px;
  background: transparent;
}

.questions-loading {
  min-height: 200px;
}

.questions-tabs :deep(.tabbed-container.side-tabs) {
  box-shadow: none;
  border-radius: 0;
  background: transparent;
}

.questions-tabs :deep(.tabbed-container.side-tabs .tabs) {
  border: none;
  background: transparent;
}

.questions-tabs :deep(.tabbed-container.side-tabs .tab-container) {
  background: transparent;
  border: none;
  box-shadow: none;
  padding: 0;
}

.mt-10 {
  margin-top: 10px;
}
</style>
