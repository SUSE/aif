<script setup lang="ts">
import { ref, computed } from 'vue';
import SortableTable from '@shell/components/SortableTable';
import { BadgeState } from '@components/BadgeState';
import { endpoints as seed, workloads, type ModelEndpoint } from '../mock';

const rows = ref<ModelEndpoint[]>([...seed]);
const headers = [
  { name: 'model', labelKey: 'model', label: 'Model', value: 'model', sort: 'model' },
  { name: 'workload', label: 'AIWorkload', value: 'workload', sort: 'workload' },
  { name: 'cluster', label: 'Cluster', value: 'cluster', sort: 'cluster' },
  { name: 'url', label: 'OpenAI-compatible URL', value: 'url' },
  { name: 'status', label: 'Status', value: 'status', sort: 'status' },
  { name: 'rateLimit', label: 'Rate limit', value: 'rateLimit' }
];
const stateColor = (s: string) => (s === 'Ready' ? 'bg-success' : s === 'Degraded' ? 'bg-warning' : 'bg-info');

const show = ref(false);
const pick = ref(workloads[0].name);
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
    <div class="tf-toolbar"><button class="btn role-primary" @click="show = true">Publish model</button></div>
    <SortableTable :headers="headers" :rows="rows" key-field="id" :table-actions="false" :row-actions="false" :search="false" :paging="false">
      <template #cell:url="{ row }"><span class="tf-mono">{{ row.url }}</span></template>
      <template #cell:status="{ row }"><BadgeState :label="row.status" :color="stateColor(row.status)" /></template>
    </SortableTable>

    <div v-if="show" class="tf-modal-bg" @click.self="show = false">
      <div class="tf-modal">
        <h3>Publish model</h3>
        <p class="text-muted">Expose an existing AIWorkload as an OpenAI-compatible endpoint.</p>
        <label>AIWorkload</label>
        <select v-model="pick">
          <option v-for="w in workloads" :key="w.name" :value="w.name">{{ w.name }} ({{ w.cluster }})</option>
        </select>
        <label>Published model name</label>
        <input :value="picked.model" readonly>
        <div class="tf-actions">
          <button class="btn role-secondary" @click="show = false">Cancel</button>
          <button class="btn role-primary" @click="publish">Publish</button>
        </div>
      </div>
    </div>
  </div>
</template>
