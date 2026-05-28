<template>
  <LabeledSelect
    :value="modelValue"
    :options="options"
    :label="t('aif.pages.blueprints.card.version')"
    :clearable="false"
    :disabled="!options.length"
    class="version-picker"
    @update:value="$emit('update:modelValue', $event)"
  />
</template>

<script>
import { defineComponent, computed } from 'vue';
import LabeledSelect from '@shell/components/form/LabeledSelect';

export default defineComponent({
  name: 'BlueprintVersionPicker',

  components: { LabeledSelect },

  props: {
    versions: {
      type:     Array,
      required: true
    },
    modelValue: {
      type:     String,
      required: true
    },
    showWithdrawn: {
      type:    Boolean,
      default: false
    }
  },

  emits: ['update:modelValue'],

  setup(props) {
    // Option labels are built as plain strings here because LabeledSelect renders
    // options from a string `label` prop with no slot/render-function escape hatch
    // — calling Rancher's `t()` from setup proved unreliable across component
    // contexts. The phase suffix stays in raw CR enum form ('Active' / 'Deprecated'
    // / 'Withdrawn'); the user-facing phase pill (BlueprintPhasePill.vue) handles
    // the localized rendering. Follow-up: translate option labels when LabeledSelect
    // exposes an option slot, or when we move to a custom dropdown component.
    const options = computed(() =>
      (props.versions ?? [])
        .filter((v) => props.showWithdrawn || v.phase !== 'Withdrawn')
        .map((v) => ({
          label: `v${ v.version } — ${ v.phase }`,
          value: v.id
        }))
    );

    return { options };
  }
});
</script>

<style lang="scss" scoped>
.version-picker {
  min-width: 200px;
}
</style>
