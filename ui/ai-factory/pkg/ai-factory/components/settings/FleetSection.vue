<template>
  <div class="fleet-section mb-20">
    <h3>{{ t('aif.pages.settings.sections.fleet.title') }}</h3>

    <div class="row mb-20">
      <div class="col span-6">
        <LabeledInput
          v-model="internalValue.repoURL"
          :mode="mode"
          :label="t('aif.pages.settings.sections.fleet.repoURL')"
          :placeholder="t('aif.pages.settings.sections.fleet.repoURLPlaceholder')"
        />
      </div>

      <div class="col span-6">
        <LabeledInput
          v-model="internalValue.branch"
          :mode="mode"
          :label="t('aif.pages.settings.sections.fleet.branch')"
          :placeholder="t('aif.pages.settings.sections.fleet.branchPlaceholder')"
        />
      </div>
    </div>

    <div class="row mb-20">
      <div class="col span-6">
        <LabeledSelect
          v-model="internalValue.authType"
          :mode="mode"
          :label="t('aif.pages.settings.sections.fleet.authType')"
          :options="authTypeOptions"
        />
      </div>
    </div>
  </div>
</template>

<script>
import LabeledInput from '@components/Form/LabeledInput';
import LabeledSelect from '@shell/components/form/LabeledSelect';

export default {
  name: 'FleetSection',

  components: {
    LabeledInput,
    LabeledSelect
  },

  props: {
    modelValue: {
      type: Object,
      default: () => ({})
    },

    mode: {
      type: String,
      default: 'edit'
    }
  },

  computed: {
    internalValue: {
      get() {
        return this.modelValue || {};
      },
      set(val) {
        this.$emit('update:modelValue', val);
      }
    },

    authTypeOptions() {
      return [
        { label: this.t('aif.pages.settings.sections.fleet.authTypeToken'), value: 'token' },
        { label: this.t('aif.pages.settings.sections.fleet.authTypeSSH'), value: 'ssh' },
        { label: this.t('aif.pages.settings.sections.fleet.authTypeBasic'), value: 'basic' }
      ];
    }
  }
};
</script>

<style lang="scss" scoped>
.fleet-section {
  border-bottom: 1px solid var(--border);
  padding-bottom: 20px;

  h3 {
    margin-bottom: 20px;
  }
}
</style>
