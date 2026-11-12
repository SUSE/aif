<script setup lang="ts">
import { ref } from 'vue';
import SortableTable from '@shell/components/SortableTable';
import { quotas } from '../mock';

const pct = (q: { used: number; quota: number }) => Math.min(100, Math.round((q.used / q.quota) * 100));
const barClass = (p: number) => (p >= 95 ? 'crit' : p >= 80 ? 'warn' : '');
const rows = quotas.map((q) => ({ ...q, id: q.team, cost: (q.used * q.pricePerM).toFixed(2) }));
const headers = [
  { name: 'team', label: 'Team', value: 'team', sort: 'team' },
  { name: 'used', label: 'Tokens used (M)', value: 'used', sort: 'used' },
  { name: 'price', label: 'Price / 1M tokens', value: 'pricePerM', sort: 'pricePerM' },
  { name: 'cost', label: 'Chargeback (USD, month to date)', value: 'cost', sort: 'cost' }
];
const total = rows.reduce((a, r) => a + Number(r.cost), 0).toFixed(2);
const msg = ref('');
function exportCsv() { msg.value = 'Mock export: usage-2026-11.csv would be downloaded here.'; }
</script>

<template>
  <div>
    <h3>Per-team token quotas (monthly)</h3>
    <div v-for="q in quotas" :key="q.team" class="qrow">
      <span class="team">{{ q.team }}</span>
      <div class="tf-bar"><div :class="barClass(pct(q))" :style="{ width: pct(q) + '%' }" /></div>
      <span class="nums">{{ q.used }}M / {{ q.quota }}M ({{ pct(q) }}%)</span>
    </div>

    <h3 class="mt-20">Chargeback</h3>
    <div class="tf-toolbar">
      <span v-if="msg" class="text-muted mr-20">{{ msg }}</span>
      <button class="btn role-secondary" @click="exportCsv">Export CSV</button>
    </div>
    <SortableTable :headers="headers" :rows="rows" key-field="id" :table-actions="false" :row-actions="false" :search="false" :paging="false">
      <template #cell:price="{ row }">${{ row.pricePerM.toFixed(2) }}</template>
      <template #cell:cost="{ row }">${{ row.cost }}</template>
    </SortableTable>
    <p class="mt-10"><b>Total: ${{ total }}</b></p>
  </div>
</template>

<style scoped lang="scss">
.qrow { display: grid; grid-template-columns: 140px 1fr 200px; gap: 12px; align-items: center; margin: 10px 0; }
.team { font-weight: 600; }
.nums { color: var(--muted); font-size: 13px; }
</style>
