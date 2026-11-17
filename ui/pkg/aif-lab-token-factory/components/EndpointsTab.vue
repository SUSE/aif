<script setup lang="ts">
import { ref, computed } from 'vue';
import { useStore } from 'vuex';
import { useI18n } from '@shell/composables/useI18n';
import { _VIEW } from '@shell/config/query-params';
import AppModal from '@shell/components/AppModal';
import SortableTable from '@shell/components/SortableTable';
import LabeledSelect from '@shell/components/form/LabeledSelect';
import { LabeledInput } from '@components/Form/LabeledInput';
import { BadgeState } from '@components/BadgeState';
import { endpoints as seed, workloads, type ModelEndpoint } from '../mock';

const { t } = useI18n(useStore());

const rows = ref<ModelEndpoint[]>([...seed]);
const headers = [
  { name: 'model', labelKey: 'tokenFactory.endpoints.headers.model', value: 'model', sort: 'model' },
  { name: 'workload', labelKey: 'tokenFactory.endpoints.headers.workload', value: 'workload', sort: 'workload' },
  { name: 'cluster', labelKey: 'tokenFactory.endpoints.headers.cluster', value: 'cluster', sort: 'cluster' },
  { name: 'url', labelKey: 'tokenFactory.endpoints.headers.url', value: 'url' },
  { name: 'status', labelKey: 'tokenFactory.endpoints.headers.status', value: 'status', sort: 'status' },
  { name: 'rateLimit', labelKey: 'tokenFactory.endpoints.headers.rateLimit', value: 'rateLimit' }
];
const stateColor = (s: string) => (s === 'Ready' ? 'bg-success' : s === 'Degraded' ? 'bg-warning' : 'bg-info');

const show = ref(false);
const pick = ref(workloads[0].name);
const workloadOptions = workloads.map((w) => ({ label: `${ w.name } (${ w.cluster })`, value: w.name }));
const picked = computed(() => workloads.find((w) => w.name === pick.value)!);
function publish() {
  const w = picked.value;

  rows.value.push({
    id: `${ w.name }-${ rows.value.length }`, model: w.model, workload: w.name, cluster: w.cluster,
    url: 'https://tokens.ai-factory.example.com/v1/chat/completions', status: 'Pending', rateLimit: '300 req/min', kind: 'chat'
  });
  show.value = false;
}
</script>

<template>
  <div>
    <div class="toolbar">
      <button
        class="btn role-primary"
        @click="show = true"
      >
        {{ t('tokenFactory.endpoints.publish') }}
      </button>
    </div>
    <SortableTable
      :headers="headers"
      :rows="rows"
      key-field="id"
      :table-actions="false"
      :row-actions="false"
      :search="false"
      :paging="false"
    >
      <template #cell:url="{ row }">
        <span class="mono">{{ row.url }}</span>
      </template>
      <template #cell:status="{ row }">
        <BadgeState
          :label="row.status"
          :color="stateColor(row.status)"
        />
      </template>
    </SortableTable>

    <AppModal
      v-if="show"
      :click-to-close="true"
      :width="480"
      @close="show = false"
    >
      <div class="modal-body">
        <h3>{{ t('tokenFactory.endpoints.modal.title') }}</h3>
        <p class="text-muted mb-10">
          {{ t('tokenFactory.endpoints.modal.description') }}
        </p>
        <LabeledSelect
          v-model:value="pick"
          class="mb-10"
          :label="t('tokenFactory.endpoints.modal.workload')"
          :options="workloadOptions"
          :clearable="false"
        />
        <LabeledInput
          :value="picked.model"
          :label="t('tokenFactory.endpoints.modal.modelName')"
          :mode="_VIEW"
        />
        <div class="modal-buttons">
          <button
            type="button"
            class="btn role-secondary"
            @click="show = false"
          >
            {{ t('tokenFactory.endpoints.modal.cancel') }}
          </button>
          <button
            type="button"
            class="btn role-primary"
            @click="publish"
          >
            {{ t('tokenFactory.endpoints.modal.confirm') }}
          </button>
        </div>
      </div>
    </AppModal>
  </div>
</template>

<style scoped lang="scss">
.toolbar { display: flex; justify-content: flex-end; margin-bottom: 12px; }
.mono { font-family: monospace; font-size: 12px; }
.modal-body {
  padding: 20px;

  h3 { margin: 0 0 10px; }

  .modal-buttons { display: flex; gap: 12px; justify-content: flex-end; margin-top: 20px; }
}
</style>
