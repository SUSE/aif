<script setup lang="ts">
import { ref } from 'vue';
import { useStore } from 'vuex';
import { useI18n } from '@shell/composables/useI18n';
import AppModal from '@shell/components/AppModal';
import PercentageBar from '@shell/components/PercentageBar';
import SortableTable from '@shell/components/SortableTable';
import LabeledSelect from '@shell/components/form/LabeledSelect';
import { LabeledInput } from '@components/Form/LabeledInput';
import { apiKeys, endpoints, type ApiKey } from '../mock';

const { t } = useI18n(useStore());

const rows = ref<ApiKey[]>([...apiKeys]);
const headers = [
  { name: 'team', labelKey: 'tokenFactory.keys.headers.team', value: 'team', sort: 'team' },
  { name: 'prefix', labelKey: 'tokenFactory.keys.headers.key', value: 'prefix' },
  { name: 'models', labelKey: 'tokenFactory.keys.headers.models', value: 'models' },
  { name: 'quota', labelKey: 'tokenFactory.keys.headers.quota', value: 'quota' },
  { name: 'usedPct', labelKey: 'tokenFactory.keys.headers.used', value: 'usedPct', sort: 'usedPct' },
  { name: 'expiry', labelKey: 'tokenFactory.keys.headers.expiry', value: 'expiry', sort: 'expiry' }
];
// Theme colour per usage threshold: green below 80%, amber from 80%, red from 95%.
const colorStops = { 0: '--success', 80: '--warning', 95: '--error' };

const show = ref(false);
const team = ref('new-team');
const allowed = ref<string[]>([endpoints[0].model]);
const modelOptions = endpoints.map((e) => e.model);
const fakeKey = ref('');

function randHex(n: number) {
  let s = '';

  for (let i = 0; i < n; i++) s += Math.floor(Math.random() * 16).toString(16);

  return s;
}
function open() { fakeKey.value = ''; show.value = true; }
function create() {
  const secret = randHex(40);

  fakeKey.value = `sk-${ secret }`;
  rows.value.push({
    id: `k${ rows.value.length + 1 }`, team: team.value, prefix: `sk-...${ secret.slice(-4) }`,
    models: [...allowed.value], quota: '10M / month', usedPct: 0, expiry: '2027-11-11'
  });
}
</script>

<template>
  <div>
    <div class="toolbar">
      <button
        class="btn role-primary"
        @click="open"
      >
        {{ t('tokenFactory.keys.create') }}
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
      <template #cell:prefix="{ row }">
        <span class="mono">{{ row.prefix }}</span>
      </template>
      <template #cell:models="{ row }">
        {{ row.models.join(', ') }}
      </template>
      <template #cell:usedPct="{ row }">
        <PercentageBar
          class="usage"
          :model-value="row.usedPct"
          :color-stops="colorStops"
          :show-percentage="true"
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
        <h3>{{ t('tokenFactory.keys.modal.title') }}</h3>
        <template v-if="!fakeKey">
          <LabeledInput
            v-model:value="team"
            class="mb-10"
            :label="t('tokenFactory.keys.modal.team')"
          />
          <LabeledSelect
            v-model:value="allowed"
            :label="t('tokenFactory.keys.modal.models')"
            :options="modelOptions"
            :multiple="true"
          />
          <div class="modal-buttons">
            <button
              type="button"
              class="btn role-secondary"
              @click="show = false"
            >
              {{ t('tokenFactory.keys.modal.cancel') }}
            </button>
            <button
              type="button"
              class="btn role-primary"
              @click="create"
            >
              {{ t('tokenFactory.keys.modal.confirm') }}
            </button>
          </div>
        </template>
        <template v-else>
          <p class="mb-10">
            {{ t('tokenFactory.keys.modal.copyNotice') }}
          </p>
          <div class="code">
            {{ fakeKey }}
          </div>
          <div class="modal-buttons">
            <button
              type="button"
              class="btn role-primary"
              @click="show = false"
            >
              {{ t('tokenFactory.keys.modal.done') }}
            </button>
          </div>
        </template>
      </div>
    </AppModal>
  </div>
</template>

<style scoped lang="scss">
.toolbar { display: flex; justify-content: flex-end; margin-bottom: 12px; }
.mono { font-family: monospace; font-size: 12px; }
.usage { min-width: 140px; }
.code { background: var(--box-bg); color: var(--body-text); border: 1px solid var(--border); border-radius: var(--border-radius); padding: 12px; font-family: monospace; font-size: 12px; white-space: pre-wrap; word-break: break-all; }
.modal-body {
  padding: 20px;

  h3 { margin: 0 0 10px; }

  .modal-buttons { display: flex; gap: 12px; justify-content: flex-end; margin-top: 20px; }
}
</style>
