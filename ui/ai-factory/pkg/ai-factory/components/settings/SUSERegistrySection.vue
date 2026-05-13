<template>
  <div class="settings-section">
    <h2>{{ t('settings.suseRegistry.title') }}</h2>
    <p class="mb-20">
      {{ t('settings.suseRegistry.description') }}
    </p>

    <div class="row mb-20">
      <div class="col span-6">
        <LabeledInput
          :value="value.endpoint"
          :label="t('settings.suseRegistry.endpoint.label')"
          :placeholder="t('settings.suseRegistry.endpoint.placeholder')"
          :tooltip="t('settings.suseRegistry.endpoint.tooltip')"
          :mode="mode"
          @input="updateField('endpoint', $event)"
        />
      </div>
    </div>

    <div class="row">
      <div class="col span-6">
        <Checkbox
          :value="value.skipTLSVerify"
          :label="t('settings.suseRegistry.skipTLSVerify.label')"
          :mode="mode"
          @input="updateField('skipTLSVerify', $event)"
        />
        <Banner
          v-if="value.skipTLSVerify"
          color="warning"
          class="mt-10"
        >
          {{ t('settings.suseRegistry.skipTLSVerify.warning') }}
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
    value: {
      type:     Object,
      required: true
    },
    mode: {
      type:    String,
      default: 'edit'
    }
  },

  methods: {
    updateField(field, value) {
      this.$emit('input', { ...this.value, [field]: value });
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
