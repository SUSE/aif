<template>
  <div class="step-content">
    <div class="info-banner mb-20">
      Installing: <strong>{{ displayName }}</strong> v{{ version }} · {{ componentCount }} component{{ componentCount !== 1 ? 's' : '' }}
    </div>

    <div class="form-group">
      <label class="lbl required">Workload Name</label>
      <input
        v-model="localName"
        type="text"
        class="form-control"
        placeholder="e.g. my-ai-deployment"
        @input="emit('update:workloadName', localName)"
      />
      <small class="text-muted">Used as prefix for Fleet Bundle names</small>
    </div>

    <div class="form-group">
      <label class="lbl required">Target Namespace</label>
      <select v-model="localNs" class="form-control" @change="emit('update:namespace', localNs)">
        <option v-for="ns in namespaceOptions" :key="ns" :value="ns">{{ ns }}</option>
      </select>
    </div>
  </div>
</template>

<script lang="ts" setup>
import { ref, onMounted, getCurrentInstance } from 'vue';
import { getClusters, listNamespaces } from '../../../services/rancher-apps';

interface Props {
  displayName:    string;
  version:        string;
  componentCount: number;
  workloadName:   string;
  namespace:      string;
}
interface Emits {
  (e: 'update:workloadName', v: string): void;
  (e: 'update:namespace',    v: string): void;
}

const props = defineProps<Props>();
const emit  = defineEmits<Emits>();
const vm    = getCurrentInstance()!.proxy as any;
const store = vm.$store;

const localName = ref(props.workloadName);
const localNs   = ref(props.namespace);
const namespaceOptions = ref<string[]>([]);

const SYSTEM_PREFIXES = ['c-', 'p-', 'kube-', 'cattle-', 'rancher', 'longhorn-', 'fleet-', 'cluster-fleet-', 'system-', 'istio-'];

onMounted(async () => {
  try {
    const clusters = await getClusters(store);
    const allNs = new Set<string>();
    for (const cl of clusters) {
      try {
        const nsList = await listNamespaces(store, cl.id);
        nsList.forEach(ns => allNs.add(ns));
      } catch {}
    }
    const filtered = [...allNs]
      .filter(ns => !SYSTEM_PREFIXES.some(p => ns.startsWith(p)))
      .sort();
    const suggested = `${ props.workloadName }-system`;
    if (!filtered.includes(suggested)) filtered.unshift(suggested);
    namespaceOptions.value = filtered;
  } catch {
    namespaceOptions.value = [`${ props.workloadName }-system`];
  }
  if (!localNs.value && namespaceOptions.value.length) {
    localNs.value = namespaceOptions.value[0];
    emit('update:namespace', localNs.value);
  }
});
</script>

<style lang="scss" scoped>
.step-content { max-width: 600px; }
.info-banner {
  padding: 12px 16px; background: var(--accent-btn);
  border: 1px solid var(--border); border-radius: 6px; font-size: 14px;
}
.mb-20 { margin-bottom: 20px; }
.form-group { margin-bottom: 20px; }
.lbl {
  display: block; font-size: 13px; font-weight: 500; margin-bottom: 6px;
  &.required::after { content: ' *'; color: var(--error); }
}
.form-control {
  width: 100%; padding: 8px 12px;
  border: 1px solid var(--border); border-radius: var(--border-radius);
  background: var(--input-bg); color: var(--body-text); font-size: 14px;
}
.text-muted { color: var(--muted); font-size: 12px; }
</style>
