<template>
  <div class="settings-section">
    <h2>{{ t('aif.pages.settings.sections.imagePullSecrets.title') }}</h2>
    <p class="mb-20">
      {{ t('aif.pages.settings.sections.imagePullSecrets.description') }}
    </p>

    <Banner
      color="info"
      class="mb-20"
    >
      {{ t('aif.pages.settings.sections.imagePullSecrets.reconciliationNote') }}
    </Banner>

    <div class="row mb-20">
      <div class="col span-6">
        <LabeledInput
          :model-value="modelValue.secretName"
          :label="t('aif.pages.settings.sections.imagePullSecrets.secretName.label')"
          :tooltip="t('aif.pages.settings.sections.imagePullSecrets.secretName.tooltip')"
          :mode="'view'"
          :disabled="true"
        />
        <p class="text-muted mt-5">
          {{ t('aif.pages.settings.sections.imagePullSecrets.secretName.detail') }}
        </p>
      </div>
    </div>

    <div class="row">
      <div class="col span-12">
        <h3 class="mb-10">
          {{ t('aif.pages.settings.sections.imagePullSecrets.namespaces.title') }}
        </h3>
        <p class="text-muted mb-10">
          {{ t('aif.pages.settings.sections.imagePullSecrets.namespaces.description') }}
        </p>
        <div
          v-if="modelValue.namespaces && modelValue.namespaces.length > 0"
          class="namespace-list"
        >
          <div
            v-for="ns in modelValue.namespaces"
            :key="ns"
            class="namespace-item"
          >
            <i class="icon icon-folder" />
            <span>{{ ns }}</span>
          </div>
        </div>
        <div
          v-else
          class="text-muted"
        >
          {{ t('aif.pages.settings.sections.imagePullSecrets.namespaces.empty') }}
        </div>
      </div>
    </div>
  </div>
</template>

<script>
import { LabeledInput } from '@components/Form/LabeledInput';
import Banner from '@components/Banner';

export default {
  name: 'ImagePullSecretsSection',

  components: {
    LabeledInput,
    Banner
  },

  props: {
    modelValue: {
      type:     Object,
      required: true
    },
    mode: {
      type:    String,
      default: 'view'
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

  h3 {
    font-size: 15px;
    font-weight: 500;
  }

  .text-muted {
    font-size: 13px;
    color: var(--input-label);
  }

  .namespace-list {
    display: flex;
    flex-wrap: wrap;
    gap: 10px;

    .namespace-item {
      display: flex;
      align-items: center;
      gap: 5px;
      padding: 5px 10px;
      background-color: var(--input-bg);
      border: 1px solid var(--input-border);
      border-radius: 3px;
      font-size: 13px;

      i {
        font-size: 14px;
        color: var(--primary);
      }
    }
  }
}
</style>
