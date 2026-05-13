<template>
  <div class="settings-section mb-20">
    <h2>{{ t('aif.pages.settings.sections.fleet.title') }}</h2>

    <div class="row mb-20">
      <div class="col span-6">
        <LabeledInput
          :value="internalValue.repoURL"
          @input="updateField('repoURL', $event)"
          :mode="mode"
          :label="t('aif.pages.settings.sections.fleet.repoURL')"
          :placeholder="t('aif.pages.settings.sections.fleet.repoURLPlaceholder')"
        />
      </div>

      <div class="col span-6">
        <LabeledInput
          :value="internalValue.branch"
          @input="updateField('branch', $event)"
          :mode="mode"
          :label="t('aif.pages.settings.sections.fleet.branch')"
          :placeholder="t('aif.pages.settings.sections.fleet.branchPlaceholder')"
        />
      </div>
    </div>

    <div class="row mb-20">
      <div class="col span-6">
        <LabeledSelect
          :value="internalValue.authType"
          @input="updateField('authType', $event)"
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
  },

  methods: {
    updateField(field, value) {
      this.$emit('update:modelValue', {
        ...this.internalValue,
        [field]: value
      });
    }
  }
};
</script>

<style lang="scss" scoped>
.settings-section {
  border-bottom: 1px solid var(--border);
  padding-bottom: 20px;

  h2 {
    margin-bottom: 20px;
  }
}
</style>
