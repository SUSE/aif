<script lang="ts">
import { defineComponent, PropType } from 'vue';
import yaml from 'js-yaml';
import logger from '../../utils/logger';

export default defineComponent({
  name: 'ValuesYaml',
  props: {
    modelValue: { type: Object as PropType<Record<string, unknown>>, default: undefined }, // Vue 3 v-model
    value:      { type: Object as PropType<Record<string, unknown>>, default: undefined }  // legacy v-model
  },
  emits: ['update:modelValue', 'input'],
  data() {
    return { 
      text: '# values.yaml' as string,
      isEditing: false
    };
  },
  mounted() { this.syncFromProps(); },
  beforeUnmount() {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    clearTimeout((this as any).editTimeout);
  },
  watch: {
    modelValue: { handler() { this.syncFromProps(); }, deep: true },
    value:      { handler() { this.syncFromProps(); }, deep: true }
  },
  methods: {
    currentVal(): Record<string, unknown> {
      return (this.modelValue !== undefined ? this.modelValue : (this.value || {})) as Record<string, unknown>;
    },
    syncFromProps() {
      // Don't overwrite text while user is actively editing
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      if ((this as any).isEditing) return;
      logger.debug('[SUSE-AI DEBUG] ValuesYaml syncFromProps called with', { data: Object.keys(this.currentVal() || {}) });
      try {
        const val = this.currentVal() || {};
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        (this as any).text = yaml.dump(val);
        logger.debug(`[SUSE-AI DEBUG] ValuesYaml set text to YAML with ${Object.keys(val).length} keys`);
      }
      catch {
        logger.debug('[SUSE-AI DEBUG] ValuesYaml failed to dump YAML, using default');
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        (this as any).text = '# values.yaml';
      }
    },
    onInput(e: Event) {
      const v = (e.target as HTMLTextAreaElement).value;
      // Update local text immediately for smooth typing
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      (this as any).text = v;
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      (this as any).isEditing = true;

      // Clear editing flag after a short delay to allow external updates
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      clearTimeout((this as any).editTimeout);
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      (this as any).editTimeout = setTimeout(() => {
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        (this as any).isEditing = false;
      }, 1000);

      try {
        const obj = (yaml.load(v) as Record<string, unknown>) || {};
        this.$emit('update:modelValue', obj); // Vue 3
        this.$emit('input', obj);             // legacy
      } catch {
        // ignore parse errors while typing; user sees raw text
      }
    }
  }
});
</script>

<template>
  <textarea
    :value="text"
    spellcheck="false"
    class="yaml"
    @input="onInput"
  />
</template>

<style scoped>
.yaml{
  width:100%; min-height:260px; border-radius:12px;
  background:#0e1312; border:1px solid #333; color:#e5fff2; padding:12px;
  font-family: ui-monospace, Menlo, Monaco, Consolas, 'Liberation Mono', 'Courier New', monospace;
  font-size: 14px;
  line-height: 1.5;
  resize: vertical;
}
</style>
