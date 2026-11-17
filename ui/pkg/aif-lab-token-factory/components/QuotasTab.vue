<script setup lang="ts">
import { ref } from 'vue';
import { useStore } from 'vuex';
import { useI18n } from '@shell/composables/useI18n';
import PercentageBar from '@shell/components/PercentageBar';
import SortableTable from '@shell/components/SortableTable';
import { quotas } from '../mock';

const { t } = useI18n(useStore());

const pct = (q: { used: number; quota: number }) => Math.min(100, Math.round((q.used / q.quota) * 100));
// Theme colour per usage threshold: green below 80%, amber from 80%, red from 95%.
const colorStops = { 0: '--success', 80: '--warning', 95: '--error' };
const rows = quotas.map((q) => ({ ...q, id: q.team, cost: (q.used * q.pricePerM).toFixed(2) }));
const headers = [
  { name: 'team', labelKey: 'tokenFactory.quotas.headers.team', value: 'team', sort: 'team' },
  { name: 'used', labelKey: 'tokenFactory.quotas.headers.used', value: 'used', sort: 'used' },
  { name: 'price', labelKey: 'tokenFactory.quotas.headers.price', value: 'pricePerM', sort: 'pricePerM' },
  { name: 'cost', labelKey: 'tokenFactory.quotas.headers.cost', value: 'cost', sort: 'cost' }
];
const total = rows.reduce((a, r) => a + Number(r.cost), 0).toFixed(2);
const showExportMsg = ref(false);
function exportCsv() { showExportMsg.value = true; }
</script>

<template>
  <div>
    <h3>{{ t('tokenFactory.quotas.title') }}</h3>
    <div
      v-for="q in quotas"
      :key="q.team"
      class="qrow"
    >
      <span class="team">{{ q.team }}</span>
      <PercentageBar
        :model-value="pct(q)"
        :color-stops="colorStops"
      />
      <span class="nums">{{ t('tokenFactory.quotas.usage', { used: q.used, quota: q.quota, pct: pct(q) }) }}</span>
    </div>

    <h3 class="mt-20">
      {{ t('tokenFactory.quotas.chargeback') }}
    </h3>
    <div class="toolbar">
      <span
        v-if="showExportMsg"
        class="text-muted mr-20"
      >{{ t('tokenFactory.quotas.exportMessage') }}</span>
      <button
        class="btn role-secondary"
        @click="exportCsv"
      >
        {{ t('tokenFactory.quotas.exportCsv') }}
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
      <template #cell:price="{ row }">
        ${{ row.pricePerM.toFixed(2) }}
      </template>
      <template #cell:cost="{ row }">
        ${{ row.cost }}
      </template>
    </SortableTable>
    <p class="mt-10">
      <b>{{ t('tokenFactory.quotas.total', { total }) }}</b>
    </p>
  </div>
</template>

<style scoped lang="scss">
.toolbar { display: flex; justify-content: flex-end; align-items: center; margin-bottom: 12px; }
.qrow { display: grid; grid-template-columns: 140px 1fr 200px; gap: 12px; align-items: center; margin: 10px 0; }
.team { font-weight: 600; }
.nums { color: var(--muted); font-size: 13px; }
</style>
