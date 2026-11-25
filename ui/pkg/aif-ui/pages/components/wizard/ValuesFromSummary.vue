<template>
  <div v-if="sources.length">
    <div
      v-for="(src, i) in sources"
      :key="i"
      class="comp-values-from"
    >
      <template v-if="src.key">
        {{ t('suseai.wizard.labels.valuesFromReviewKey', 'Values from {kind} {ref} (key {key})', { kind: src.kind, ref: sourceRef(src), key: src.key }) }}
      </template>
      <template v-else>
        {{ t('suseai.wizard.labels.valuesFromReview', 'Values from {kind} {ref}', { kind: src.kind, ref: sourceRef(src) }) }}
      </template>
    </div>
  </div>
</template>

<script lang="ts" setup>
import { computed } from 'vue';
import { useT } from '../../../composables/useT';
import { describeValuesFrom } from '../../../utils/values-from';
import type { ValuesFromSource } from '../../../utils/values-from';
import type { ComponentValuesFrom } from '../../../types/blueprint-types';

interface Props { valuesFrom?: ComponentValuesFrom[] }
const props = withDefaults(defineProps<Props>(), { valuesFrom: undefined });

const t = useT();

const sources = computed(() => describeValuesFrom(props.valuesFrom));

function sourceRef(src: ValuesFromSource): string {
  return src.namespace ? `${ src.namespace }/${ src.name }` : src.name;
}
</script>

<style lang="scss" scoped>
.comp-values-from { color: var(--muted); font-size: 12px; }
</style>
