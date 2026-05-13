<template>
  <div class="settings-section">
    <h2>{{ t('aif.pages.settings.sections.suseRegistry.title') }}</h2>
    <p class="mb-20">
      {{ t('aif.pages.settings.sections.suseRegistry.description') }}
    </p>

    <div class="row mb-20">
      <div class="col span-6">
        <LabeledInput
          :model-value="modelValue.endpoint"
          :label="t('aif.pages.settings.sections.suseRegistry.endpoint.label')"
          :placeholder="t('aif.pages.settings.sections.suseRegistry.endpoint.placeholder')"
          :tooltip="t('aif.pages.settings.sections.suseRegistry.endpoint.tooltip')"
          :mode="mode"
          @update:model-value="updateField('endpoint', $event)"
        />
      </div>
    </div>

    <div class="row">
      <div class="col span-6">
        <Checkbox
          :model-value="modelValue.skipTLSVerify"
          :label="t('aif.pages.settings.sections.suseRegistry.skipTLSVerify.label')"
          :mode="mode"
          @update:model-value="updateField('skipTLSVerify', $event)"
        />
        <Banner
          v-if="modelValue.skipTLSVerify"
          color="warning"
          class="mt-10"
        >
          {{ t('aif.pages.settings.sections.suseRegistry.skipTLSVerify.warning') }}
        </Banner>
      </div>
    </div>
  </div>
</template>

<script>
import { LabeledInput } from '@components/Form/LabeledInput';
import Checkbox from '@components/Form/Checkbox';
import Banner from '@components/Banner';

export default {
  name: 'SUSERegistrySection',

  components: {
    LabeledInput,
    Checkbox,
    Banner
  },

  props: {
    modelValue: {
      type:     Object,
      required: true
    },
    mode: {
      type:    String,
      default: 'edit'
    }
  },

  emits: ['update:modelValue'],

  methods: {
    updateField(field, value) {
      this.$emit('update:modelValue', { ...this.modelValue, [field]: value });
    }
  }
};
</script>

<style lang="scss" scoped>
.settings-section {
  margin-bottom: 40px;

  h2 {
    font-size: 18px;
    margin-bottom: 10px;
  }

  .text-muted {
    font-size: 13px;
    color: var(--input-label);
  }
}
</style>
