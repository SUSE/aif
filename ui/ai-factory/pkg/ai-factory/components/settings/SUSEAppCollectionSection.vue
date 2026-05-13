<template>
  <div class="settings-section">
    <h2>{{ t('aif.pages.settings.sections.appCollection.title') }}</h2>
    <p class="mb-20">
      {{ t('aif.pages.settings.sections.appCollection.description') }}
    </p>

    <div class="row mb-20">
      <div class="col span-6">
        <LabeledInput
          :model-value="modelValue.endpoint"
          :label="t('aif.pages.settings.sections.appCollection.endpoint.label')"
          :placeholder="t('aif.pages.settings.sections.appCollection.endpoint.placeholder')"
          :tooltip="t('aif.pages.settings.sections.appCollection.endpoint.tooltip')"
          :mode="mode"
          @update:model-value="updateField('endpoint', $event)"
        />
      </div>
    </div>

    <div class="row mb-20">
      <div class="col span-6">
        <Checkbox
          :model-value="modelValue.enabled"
          :label="t('aif.pages.settings.sections.appCollection.enabled.label')"
          :mode="mode"
          @update:model-value="updateField('enabled', $event)"
        />
        <p class="text-muted mt-5">
          {{ t('aif.pages.settings.sections.appCollection.enabled.detail') }}
        </p>
      </div>
    </div>

    <div class="row mb-20">
      <div class="col span-6">
        <UnitInput
          v-model:value="refreshInterval"
          :label="t('aif.pages.settings.sections.appCollection.refreshInterval.label')"
          :placeholder="t('aif.pages.settings.sections.appCollection.refreshInterval.placeholder')"
          :tooltip="t('aif.pages.settings.sections.appCollection.refreshInterval.tooltip')"
          :mode="mode"
          :input-exponent="0"
          :increment="1"
          :output-modifier="true"
          suffix="m"
        />
        <p class="text-muted mt-5">
          {{ t('aif.pages.settings.sections.appCollection.refreshInterval.detail') }}
        </p>
      </div>
    </div>

    <div class="row">
      <div class="col span-6">
        <Checkbox
          :model-value="modelValue.skipTLSVerify"
          :label="t('aif.pages.settings.sections.appCollection.skipTLSVerify.label')"
          :mode="mode"
          @update:model-value="updateField('skipTLSVerify', $event)"
        />
        <Banner
          v-if="modelValue.skipTLSVerify"
          color="warning"
          class="mt-10"
        >
          {{ t('aif.pages.settings.sections.appCollection.skipTLSVerify.warning') }}
        </Banner>
      </div>
    </div>
  </div>
</template>

<script>
import { LabeledInput } from '@components/Form/LabeledInput';
import UnitInput from '@shell/components/form/UnitInput';
import Checkbox from '@components/Form/Checkbox';
import Banner from '@components/Banner';

export default {
  name: 'SUSEAppCollectionSection',

  components: {
    LabeledInput,
    UnitInput,
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

  computed: {
    refreshInterval: {
      get() {
        return this.modelValue.refreshInterval || 30;
      },
      set(val) {
        this.updateField('refreshInterval', val);
      }
    }
  },

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
