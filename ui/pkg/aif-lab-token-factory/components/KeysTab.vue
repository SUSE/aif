<script setup lang="ts">
import { ref } from 'vue';
import SortableTable from '@shell/components/SortableTable';
import { apiKeys, endpoints, type ApiKey } from '../mock';

const rows = ref<ApiKey[]>([...apiKeys]);
const headers = [
  { name: 'team', label: 'Team', value: 'team', sort: 'team' },
  { name: 'prefix', label: 'Key', value: 'prefix' },
  { name: 'models', label: 'Models allowed', value: 'models' },
  { name: 'quota', label: 'Quota', value: 'quota' },
  { name: 'usedPct', label: 'Used', value: 'usedPct', sort: 'usedPct' },
  { name: 'expiry', label: 'Expires', value: 'expiry', sort: 'expiry' }
];
const barClass = (p: number) => (p >= 95 ? 'crit' : p >= 80 ? 'warn' : '');

const show = ref(false);
const team = ref('new-team');
const allowed = ref<string[]>([endpoints[0].model]);
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
    <div class="tf-toolbar"><button class="btn role-primary" @click="open">Create key</button></div>
    <SortableTable :headers="headers" :rows="rows" key-field="id" :table-actions="false" :row-actions="false" :search="false" :paging="false">
      <template #cell:prefix="{ row }"><span class="tf-mono">{{ row.prefix }}</span></template>
      <template #cell:models="{ row }">{{ row.models.join(', ') }}</template>
      <template #cell:usedPct="{ row }">
        <div style="display:flex;gap:8px;align-items:center">
          <div class="tf-bar"><div :class="barClass(row.usedPct)" :style="{ width: row.usedPct + '%' }" /></div>
          <span>{{ row.usedPct }}%</span>
        </div>
      </template>
    </SortableTable>

    <div v-if="show" class="tf-modal-bg" @click.self="show = false">
      <div class="tf-modal">
        <h3>Create API key</h3>
        <template v-if="!fakeKey">
          <label>Team / consumer</label>
          <input v-model="team">
          <label>Models allowed</label>
          <select v-model="allowed" multiple size="4">
            <option v-for="e in endpoints" :key="e.id" :value="e.model">{{ e.model }}</option>
          </select>
          <div class="tf-actions">
            <button class="btn role-secondary" @click="show = false">Cancel</button>
            <button class="btn role-primary" @click="create">Create</button>
          </div>
        </template>
        <template v-else>
          <p>Copy this key now. It will not be shown again.</p>
          <div class="tf-code">{{ fakeKey }}</div>
          <div class="tf-actions"><button class="btn role-primary" @click="show = false">Done</button></div>
        </template>
      </div>
    </div>
  </div>
</template>
