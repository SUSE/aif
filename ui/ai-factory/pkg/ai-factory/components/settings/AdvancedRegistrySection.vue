<template>
  <div class="settings-section advanced">
    <h2>{{ t('settings.advancedRegistry.title') }}</h2>
    <p class="mb-20">
      {{ t('settings.advancedRegistry.description') }}
    </p>

    <Banner
      color="warning"
      class="mb-20"
    >
      {{ t('settings.advancedRegistry.warning') }}
    </Banner>

    <!-- Registry Endpoints Override -->
    <div class="subsection mb-30">
      <h3 class="mb-10">
        {{ t('settings.advancedRegistry.registryEndpoints.title') }}
      </h3>
      <p class="text-muted mb-15">
        {{ t('settings.advancedRegistry.registryEndpoints.description') }}
      </p>

      <div class="row mb-10">
        <div class="col span-6">
          <LabeledInput
            :value="registryEndpoints.suseAppCollection"
            :label="t('settings.advancedRegistry.registryEndpoints.suseAppCollection.label')"
            :placeholder="t('settings.advancedRegistry.registryEndpoints.suseAppCollection.placeholder')"
            :tooltip="t('settings.advancedRegistry.registryEndpoints.suseAppCollection.tooltip')"
            :mode="mode"
            @input="updateRegistryEndpoint('suseAppCollection', $event)"
          />
        </div>
      </div>

      <div class="row mb-10">
        <div class="col span-6">
          <LabeledInput
            :value="registryEndpoints.suseRegistry"
            :label="t('settings.advancedRegistry.registryEndpoints.suseRegistry.label')"
            :placeholder="t('settings.advancedRegistry.registryEndpoints.suseRegistry.placeholder')"
            :tooltip="t('settings.advancedRegistry.registryEndpoints.suseRegistry.tooltip')"
            :mode="mode"
            @input="updateRegistryEndpoint('suseRegistry', $event)"
          />
        </div>
      </div>

      <div class="row">
        <div class="col span-6">
          <LabeledInput
            :value="registryEndpoints.nvidiaChartsProxy"
            :label="t('settings.advancedRegistry.registryEndpoints.nvidiaChartsProxy.label')"
            :placeholder="t('settings.advancedRegistry.registryEndpoints.nvidiaChartsProxy.placeholder')"
            :tooltip="t('settings.advancedRegistry.registryEndpoints.nvidiaChartsProxy.tooltip')"
            :mode="mode"
            @input="updateRegistryEndpoint('nvidiaChartsProxy', $event)"
          />
        </div>
      </div>
    </div>

    <!-- Image Rewrite Rules -->
    <div class="subsection mb-30">
      <h3 class="mb-10">
        {{ t('settings.advancedRegistry.imageRewrite.title') }}
      </h3>
      <p class="text-muted mb-15">
        {{ t('settings.advancedRegistry.imageRewrite.description') }}
      </p>

      <div class="row mb-10">
        <div class="col span-6">
          <Checkbox
            :value="imageRewrite.enabled"
            :label="t('settings.advancedRegistry.imageRewrite.enabled.label')"
            :mode="mode"
            @input="updateImageRewrite('enabled', $event)"
          />
          <p class="text-muted mt-5">
            {{ t('settings.advancedRegistry.imageRewrite.enabled.detail') }}
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
              :value="imageRewrite.defaultRegistry"
              :label="t('settings.advancedRegistry.imageRewrite.defaultRegistry.label')"
              :placeholder="t('settings.advancedRegistry.imageRewrite.defaultRegistry.placeholder')"
              :tooltip="t('settings.advancedRegistry.imageRewrite.defaultRegistry.tooltip')"
              :mode="mode"
              @input="updateImageRewrite('defaultRegistry', $event)"
            />
          </div>
        </div>

        <div class="row">
          <div class="col span-6">
            <LabeledInput
              :value="imageRewrite.imagePrefix"
              :label="t('settings.advancedRegistry.imageRewrite.imagePrefix.label')"
              :placeholder="t('settings.advancedRegistry.imageRewrite.imagePrefix.placeholder')"
              :tooltip="t('settings.advancedRegistry.imageRewrite.imagePrefix.tooltip')"
              :mode="mode"
              @input="updateImageRewrite('imagePrefix', $event)"
            />
            <p class="text-muted mt-5">
              {{ t('settings.advancedRegistry.imageRewrite.imagePrefix.detail') }}
            </p>
          </div>
        </div>
      </div>
    </div>

    <!-- Catalog Discovery Mode -->
    <div class="subsection">
      <h3 class="mb-10">
        {{ t('settings.advancedRegistry.catalogDiscovery.title') }}
      </h3>
      <p class="text-muted mb-15">
        {{ t('settings.advancedRegistry.catalogDiscovery.description') }}
      </p>

      <div class="row mb-10">
        <div class="col span-6">
          <RadioGroup
            :value="catalogDiscovery.mode"
            :name="'catalogDiscoveryMode'"
            :options="catalogDiscoveryModeOptions"
            :mode="mode"
            @input="updateCatalogDiscovery('mode', $event)"
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
            {{ t('settings.advancedRegistry.catalogDiscovery.ociFallbackNote') }}
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
          label: this.t('settings.advancedRegistry.catalogDiscovery.modes.api.label'),
          value: 'api'
        },
        {
          label: this.t('settings.advancedRegistry.catalogDiscovery.modes.ociFallback.label'),
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
