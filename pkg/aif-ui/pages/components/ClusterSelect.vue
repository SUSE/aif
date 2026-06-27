<script lang="ts">
import { defineComponent, PropType } from 'vue';
import { getAllClusters } from '../../services/rancher-apps';

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
      options: [] as { id: string; name: string; ready: boolean }[]
    };
  },
  computed: {
    selected(): string {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const mv = (this as any).modelValue as string | undefined;
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const legacy = (this as any).value as string | undefined;
      return (mv !== null && mv !== undefined ? mv : (legacy || '')) as string;
    }
  },
  async mounted() {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const store: unknown = (this as any).$store;
    try {
      const rows = await getAllClusters(store);
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      (this as any).options = (rows || []).map((c: { id: string; name?: string; ready?: boolean }) => ({
        id:    c.id,
        name:  c.name || c.id,
        ready: c.ready !== false
      }));

      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const readyOptions = ((this as any).options as { id: string; name: string; ready: boolean }[]).filter(o => o.ready);

      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      if ((this as any).options.length === 0) {
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        (this as any).error = 'No clusters found';
      } else if (readyOptions.length === 0) {
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        (this as any).error = 'All clusters are currently unavailable';
      } else {
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        (this as any).error = null;
      }

      // Auto-select if only one ready cluster exists
      if (readyOptions.length === 1 && !this.selected) {
        this.$emit('update:modelValue', readyOptions[0].id);
        this.$emit('input', readyOptions[0].id);
      }
    } catch (e: unknown) {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      (this as any).error = (e instanceof Error ? e.message : null) || 'Failed to list clusters';
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      (this as any).options = [];
    } finally {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
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
    <div
      v-if="loading"
      class="hint"
    >
      Loading clusters…
    </div>
    <div
      v-else-if="error"
      class="hint"
    >
      {{ error }}
    </div>
    <select
      v-else 
      class="control" 
      :value="selected" 
      :disabled="disabled"
      @change="onSelect"
    >
      <option value="">
        — Select a cluster —
      </option>
      <option
        v-for="o in options"
        :key="o.id"
        :value="o.id"
        :disabled="!o.ready"
      >
        {{ o.ready ? o.name : `${o.name} (Unavailable)` }}
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