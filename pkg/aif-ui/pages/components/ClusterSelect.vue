<script lang="ts">
import { defineComponent, PropType } from 'vue';
import { getClusters } from '../../services/rancher-apps';

export default defineComponent({
  name: 'ClusterSelect',
  // Vue 3 v-model support (+ legacy "value"/"input" for safety)
  props: {
    modelValue: { type: String as PropType<string>, default: '' },
    value:      { type: String as PropType<string>, default: undefined },
    disabled:   { type: Boolean, default: false }
  },
  emits: ['update:modelValue', 'input'],
  data() {
    return {
      loading: true as boolean,
      error:   null as string | null,
      options: [] as { id: string; name: string }[]
    };
  },
  computed: {
    selected(): string {
      const mv = (this as any).modelValue as string | undefined;
      const legacy = (this as any).value as string | undefined;
      return (mv != null ? mv : (legacy || '')) as string;
    }
  },
  async mounted() {
    const store: any = (this as any).$store;
    try {
      const rows = await getClusters(store);
      (this as any).options = (rows || []).map((c: any) => ({ id: c.id, name: c.name || c.id }));
      (this as any).error = (this as any).options.length ? null : 'No clusters found';
      
      // Auto-select if only one cluster exists
      if ((this as any).options.length === 1 && !this.selected) {
        const clusterId = (this as any).options[0].id;
        this.$emit('update:modelValue', clusterId);
        this.$emit('input', clusterId);
      }
    } catch (e: any) {
      (this as any).error = e?.message || 'Failed to list clusters';
      (this as any).options = [];
    } finally {
      (this as any).loading = false;
    }
  },
  methods: {
    onSelect(event: Event) {
      const target = event.target as HTMLSelectElement;
      const value = target.value;
      this.$emit('update:modelValue', value);
      this.$emit('input', value);
    }
  }
});
</script>

<template>
  <div>
    <div v-if="loading" class="hint">Loading clusters…</div>
    <div v-else-if="error" class="hint">{{ error }}</div>
    <select v-else 
            class="control" 
            :value="selected" 
            :disabled="disabled"
            @change="onSelect">
      <option value="">— Select a cluster —</option>
      <option v-for="o in options" :key="o.id" :value="o.id">
        {{ o.name }}
      </option>
    </select>
  </div>
</template>

<style scoped>
.hint { font-size:12px; color:#64748b; }
.control { 
  height:36px; 
  padding:0 10px; 
  border:1px solid #cbd5e1; 
  border-radius:8px; 
  line-height:1; 
  background:#fff; 
  color:#111827; 
  width:100%; 
}
.control:disabled {
  opacity: 0.6;
  cursor: not-allowed;
  background: #f9fafb;
}
</style>