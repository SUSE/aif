<template>
  <div class="settings-section">
    <h2>{{ t('settings.suseAppCollection.title') }}</h2>
    <p class="mb-20">
      {{ t('settings.suseAppCollection.description') }}
    </p>

    <div class="row mb-20">
      <div class="col span-6">
        <LabeledInput
          :value="value.endpoint"
          :label="t('settings.suseAppCollection.endpoint.label')"
          :placeholder="t('settings.suseAppCollection.endpoint.placeholder')"
          :tooltip="t('settings.suseAppCollection.endpoint.tooltip')"
          :mode="mode"
          @input="updateField('endpoint', $event)"
        />
      </div>
    </div>

    <div class="row mb-20">
      <div class="col span-6">
        <Checkbox
          :value="value.enabled"
          :label="t('settings.suseAppCollection.enabled.label')"
          :mode="mode"
          @input="updateField('enabled', $event)"
        />
        <p class="text-muted mt-5">
          {{ t('settings.suseAppCollection.enabled.detail') }}
        </p>
      </div>
    </div>

    <div class="row mb-20">
      <div class="col span-6">
        <UnitInput
          v-model:value="refreshInterval"
          :label="t('settings.suseAppCollection.refreshInterval.label')"
          :placeholder="t('settings.suseAppCollection.refreshInterval.placeholder')"
          :tooltip="t('settings.suseAppCollection.refreshInterval.tooltip')"
          :mode="mode"
          :input-exponent="0"
          :increment="1"
          :output-modifier="true"
          suffix="m"
        />
        <p class="text-muted mt-5">
          {{ t('settings.suseAppCollection.refreshInterval.detail') }}
        </p>
      </div>
    </div>

    <div class="row">
      <div class="col span-6">
        <Checkbox
          :value="value.skipTLSVerify"
          :label="t('settings.suseAppCollection.skipTLSVerify.label')"
          :mode="mode"
          @input="updateField('skipTLSVerify', $event)"
        />
        <Banner
          v-if="value.skipTLSVerify"
          color="warning"
          class="mt-10"
        >
          {{ t('settings.suseAppCollection.skipTLSVerify.warning') }}
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
    value: {
      type:     Object,
      required: true
    },
    mode: {
      type:    String,
      default: 'edit'
    }
  },

  computed: {
    refreshInterval: {
      get() {
        return this.value.refreshInterval || 30;
      },
      set(val) {
        this.updateField('refreshInterval', val);
      }
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
