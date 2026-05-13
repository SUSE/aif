<template>
  <div class="settings-section advanced">
    <h2>{{ t('aif.pages.settings.sections.advanced.title') }}</h2>
    <p class="mb-20">
      {{ t('aif.pages.settings.sections.advanced.description') }}
    </p>

    <Banner
      color="warning"
      class="mb-20"
    >
      {{ t('aif.pages.settings.sections.advanced.warning') }}
    </Banner>

    <!-- Registry Endpoints Override -->
    <div class="subsection mb-30">
      <h3 class="mb-10">
        {{ t('aif.pages.settings.sections.advanced.registryEndpoints.title') }}
      </h3>
      <p class="text-muted mb-15">
        {{ t('aif.pages.settings.sections.advanced.registryEndpoints.description') }}
      </p>

      <div class="row mb-10">
        <div class="col span-6">
          <LabeledInput
            :model-value="registryEndpoints.suseAppCollection"
            :label="t('aif.pages.settings.sections.advanced.registryEndpoints.suseAppCollection.label')"
            :placeholder="t('aif.pages.settings.sections.advanced.registryEndpoints.suseAppCollection.placeholder')"
            :tooltip="t('aif.pages.settings.sections.advanced.registryEndpoints.suseAppCollection.tooltip')"
            :mode="mode"
            @update:model-value="updateRegistryEndpoint('suseAppCollection', $event)"
          />
        </div>
      </div>

      <div class="row mb-10">
        <div class="col span-6">
          <LabeledInput
            :model-value="registryEndpoints.suseRegistry"
            :label="t('aif.pages.settings.sections.advanced.registryEndpoints.suseRegistry.label')"
            :placeholder="t('aif.pages.settings.sections.advanced.registryEndpoints.suseRegistry.placeholder')"
            :tooltip="t('aif.pages.settings.sections.advanced.registryEndpoints.suseRegistry.tooltip')"
            :mode="mode"
            @update:model-value="updateRegistryEndpoint('suseRegistry', $event)"
          />
        </div>
      </div>

      <div class="row">
        <div class="col span-6">
          <LabeledInput
            :model-value="registryEndpoints.nvidiaChartsProxy"
            :label="t('aif.pages.settings.sections.advanced.registryEndpoints.nvidiaChartsProxy.label')"
            :placeholder="t('aif.pages.settings.sections.advanced.registryEndpoints.nvidiaChartsProxy.placeholder')"
            :tooltip="t('aif.pages.settings.sections.advanced.registryEndpoints.nvidiaChartsProxy.tooltip')"
            :mode="mode"
            @update:model-value="updateRegistryEndpoint('nvidiaChartsProxy', $event)"
          />
        </div>
      </div>
    </div>

    <!-- Image Rewrite Rules -->
    <div class="subsection mb-30">
      <h3 class="mb-10">
        {{ t('aif.pages.settings.sections.advanced.imageRewrite.title') }}
      </h3>
      <p class="text-muted mb-15">
        {{ t('aif.pages.settings.sections.advanced.imageRewrite.description') }}
      </p>

      <div class="row mb-10">
        <div class="col span-6">
          <Checkbox
            :model-value="imageRewrite.enabled"
            :label="t('aif.pages.settings.sections.advanced.imageRewrite.enabled.label')"
            :mode="mode"
            @update:model-value="updateImageRewrite('enabled', $event)"
          />
          <p class="text-muted mt-5">
            {{ t('aif.pages.settings.sections.advanced.imageRewrite.enabled.detail') }}
          </p>
        </div>
      </div>

      <div
        v-if="imageRewrite.enabled"
        class="rewrite-rules"
      >
        <div class="row mb-10">
          <div class="col span-6">
            <LabeledInput
              :model-value="imageRewrite.defaultRegistry"
              :label="t('aif.pages.settings.sections.advanced.imageRewrite.defaultRegistry.label')"
              :placeholder="t('aif.pages.settings.sections.advanced.imageRewrite.defaultRegistry.placeholder')"
              :tooltip="t('aif.pages.settings.sections.advanced.imageRewrite.defaultRegistry.tooltip')"
              :mode="mode"
              @update:model-value="updateImageRewrite('defaultRegistry', $event)"
            />
          </div>
        </div>

        <div class="row">
          <div class="col span-6">
            <LabeledInput
              :model-value="imageRewrite.imagePrefix"
              :label="t('aif.pages.settings.sections.advanced.imageRewrite.imagePrefix.label')"
              :placeholder="t('aif.pages.settings.sections.advanced.imageRewrite.imagePrefix.placeholder')"
              :tooltip="t('aif.pages.settings.sections.advanced.imageRewrite.imagePrefix.tooltip')"
              :mode="mode"
              @update:model-value="updateImageRewrite('imagePrefix', $event)"
            />
            <p class="text-muted mt-5">
              {{ t('aif.pages.settings.sections.advanced.imageRewrite.imagePrefix.detail') }}
            </p>
          </div>
        </div>
      </div>
    </div>

    <!-- Catalog Discovery Mode -->
    <div class="subsection">
      <h3 class="mb-10">
        {{ t('aif.pages.settings.sections.advanced.catalogDiscovery.title') }}
      </h3>
      <p class="text-muted mb-15">
        {{ t('aif.pages.settings.sections.advanced.catalogDiscovery.description') }}
      </p>

      <div class="row mb-10">
        <div class="col span-6">
          <RadioGroup
            :model-value="catalogDiscovery.mode"
            :name="'catalogDiscoveryMode'"
            :options="catalogDiscoveryModeOptions"
            :mode="mode"
            @update:model-value="updateCatalogDiscovery('mode', $event)"
          />
        </div>
      </div>

      <div
        v-if="catalogDiscovery.mode === 'oci-fallback'"
        class="row"
      >
        <div class="col span-6">
          <Banner
            color="info"
            class="mt-10"
          >
            {{ t('aif.pages.settings.sections.advanced.catalogDiscovery.ociFallbackNote') }}
          </Banner>
        </div>
      </div>
    </div>
  </div>
</template>

<script>
import { LabeledInput } from '@components/Form/LabeledInput';
import Checkbox from '@components/Form/Checkbox';
import RadioGroup from '@components/Form/Radio';
import Banner from '@components/Banner';

export default {
  name: 'AdvancedRegistrySection',

  components: {
    LabeledInput,
    Checkbox,
    RadioGroup,
    Banner
  },

  props: {
    registryEndpoints: {
      type:     Object,
      required: true
    },
    imageRewrite: {
      type:     Object,
      required: true
    },
    catalogDiscovery: {
      type:     Object,
      required: true
    },
    mode: {
      type:    String,
      default: 'edit'
    }
  },

  computed: {
    catalogDiscoveryModeOptions() {
      return [
        {
          label: this.t('aif.pages.settings.sections.advanced.catalogDiscovery.modes.api.label'),
          value: 'api'
        },
        {
          label: this.t('aif.pages.settings.sections.advanced.catalogDiscovery.modes.ociFallback.label'),
          value: 'oci-fallback'
        }
      ];
    }
  },

  methods: {
    updateRegistryEndpoint(field, value) {
      this.$emit('update:registryEndpoints', {
        ...this.registryEndpoints,
        [field]: value
      });
    },

    updateImageRewrite(field, value) {
      this.$emit('update:imageRewrite', {
        ...this.imageRewrite,
        [field]: value
      });
    },

    updateCatalogDiscovery(field, value) {
      this.$emit('update:catalogDiscovery', {
        ...this.catalogDiscovery,
        [field]: value
      });
    }
  }
};
</script>

<style lang="scss" scoped>
.settings-section.advanced {
  margin-bottom: 40px;
  padding: 20px;
  background-color: var(--box-bg);
  border: 1px solid var(--border);
  border-radius: 4px;

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

  .subsection {
    padding: 15px;
    background-color: var(--body-bg);
    border-radius: 3px;

    .rewrite-rules {
      margin-top: 15px;
      padding-top: 15px;
      border-top: 1px solid var(--border);
    }
  }
}
</style>
