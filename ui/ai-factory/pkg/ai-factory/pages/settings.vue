<template>
  <div>
    <Banner
      v-if="loadError"
      color="error"
      :label="t('aif.pages.settings.errorLoading')"
    />
    <CruResource
      v-else
      :resource="value"
      :mode="mode"
      :errors="errors"
      :done-route="doneRoute"
      @finish="save"
    >
      <template #default>
        <div class="settings-page">
          <!-- Page Header -->
          <div class="page-header">
            <h1>{{ t('aif.pages.settings.title') }}</h1>
            <div class="header-controls">
              <Checkbox
                v-model="showAdvanced"
                :label="t('aif.pages.settings.showAdvanced')"
                class="advanced-toggle"
              />
              <Banner
                v-if="customEndpointsActive"
                color="info"
                :label="t('aif.pages.settings.customEndpointsActive')"
                class="custom-endpoints-chip"
              />
            </div>
          </div>

          <!-- Section 1: Catalog Refresh Interval -->
          <div class="settings-section">
            <h2>{{ t('aif.pages.settings.catalogRefresh.title') }}</h2>
            <CatalogRefreshInterval v-model="value.spec.catalogRefreshIntervalMinutes" />
          </div>

          <!-- Section 2: Fleet -->
          <div class="settings-section">
            <h2>{{ t('aif.pages.settings.fleet.title') }}</h2>
            <FleetSection
              v-model="value.spec.fleet"
              :mode="mode"
            />
          </div>

          <!-- Section 3: SUSE Application Collection -->
          <div class="settings-section">
            <h2>{{ t('aif.pages.settings.suseAppCollection.title') }}</h2>
            <SUSEAppCollectionSection v-model="value.spec.applicationCollection" />
          </div>

          <!-- Section 4: SUSE Registry -->
          <div class="settings-section">
            <h2>{{ t('aif.pages.settings.suseRegistry.title') }}</h2>
            <SUSERegistrySection v-model="value.spec.suseRegistry" />
          </div>

          <!-- Section 5: Image Pull Secrets (Read-only) -->
          <div class="settings-section">
            <h2>{{ t('aif.pages.settings.imagePullSecrets.title') }}</h2>
            <ImagePullSecretsSection
              :model-value="value.spec.imagePullSecrets || {}"
              :mode="mode"
            />
          </div>

          <!-- Section 6: Advanced Registry Endpoints (conditionally shown) -->
          <div
            v-if="showAdvanced"
            class="settings-section advanced-section"
          >
            <h2>{{ t('aif.pages.settings.advancedRegistry.title') }}</h2>
            <AdvancedRegistrySection
              v-model:registry-endpoints="value.spec.registryEndpoints"
              v-model:image-rewrite="value.spec.imageRewrite"
              v-model:catalog-discovery="value.spec.catalogDiscovery"
            />
          </div>
        </div>
      </template>
    </CruResource>
  </div>
</template>

<script>
import CruResource from '@shell/components/CruResource';
import Banner from '@components/Banner';
import Checkbox from '@components/Form/Checkbox';
import CatalogRefreshInterval from '@/components/settings/CatalogRefreshInterval';
import FleetSection from '@/components/settings/FleetSection';
import SUSEAppCollectionSection from '@/components/settings/SUSEAppCollectionSection';
import SUSERegistrySection from '@/components/settings/SUSERegistrySection';
import ImagePullSecretsSection from '@/components/settings/ImagePullSecretsSection';
import AdvancedRegistrySection from '@/components/settings/AdvancedRegistrySection';

export default {
  name: 'SettingsPage',

  components: {
    CruResource,
    Banner,
    Checkbox,
    CatalogRefreshInterval,
    FleetSection,
    SUSEAppCollectionSection,
    SUSERegistrySection,
    ImagePullSecretsSection,
    AdvancedRegistrySection
  },

  async fetch() {
    try {
      this.value = await this.$store.dispatch('ai-factory/find', {
        type: 'ai.suse.com.settings',
        id:   'aif/aif-settings'
      });
      this.loadError = false;
    } catch (e) {
      if (e?.status === 404) {
        this.loadError = true;
      } else {
        throw e;
      }
    }
  },

  data() {
    return {
      value:         null,
      showAdvanced:  false,
      loadError:     false,
      errors:        [],
      mode:          'edit',
      doneRoute:     { name: 'ai-factory-settings' }
    };
  },

  computed: {
    customEndpointsActive() {
      const defaults = {
        suseRegistry:              'registry.suse.com',
        applicationCollection:     'dp.apps.rancher.io',
        applicationCollectionAPI:  'https://api.apps.rancher.io',
        mode:                      'api'
      };

      const endpoints = this.value?.spec?.registryEndpoints || {};
      const imageRewrite = this.value?.spec?.imageRewrite || {};
      const catalogDiscovery = this.value?.spec?.catalogDiscovery || {};

      return (
        (endpoints.suseRegistry || defaults.suseRegistry) !== defaults.suseRegistry ||
        (endpoints.applicationCollection || defaults.applicationCollection) !== defaults.applicationCollection ||
        (endpoints.applicationCollectionAPI || defaults.applicationCollectionAPI) !== defaults.applicationCollectionAPI ||
        (catalogDiscovery.applicationCollectionMode || defaults.mode) !== defaults.mode ||
        (imageRewrite.enabled === true)
      );
    }
  },

  mounted() {
    const stored = localStorage.getItem('aif.settings.showAdvanced');

    this.showAdvanced = stored === 'true';
  },

  watch: {
    showAdvanced(val) {
      localStorage.setItem('aif.settings.showAdvanced', val);
    }
  },

  methods: {
    async save(buttonDone) {
      try {
        await this.value.save();
        buttonDone(true);
        this.$router.push(this.doneRoute);
      } catch (e) {
        this.errors = [e];
        buttonDone(false);
      }
    }
  }
};
</script>

<style lang="scss" scoped>
.settings-page {
  padding: 20px;

  .page-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 30px;

    h1 {
      margin: 0;
    }

    .header-controls {
      display: flex;
      align-items: center;
      gap: 15px;

      .advanced-toggle {
        margin: 0;
      }

      .custom-endpoints-chip {
        margin: 0;
        padding: 5px 12px;
        font-size: 13px;
        white-space: nowrap;
      }
    }
  }

  .settings-section {
    margin-bottom: 40px;
    padding: 20px;
    background: var(--nav-bg);
    border: 1px solid var(--border);
    border-radius: 4px;

    h2 {
      margin-top: 0;
      margin-bottom: 20px;
      font-size: 18px;
      font-weight: 600;
      border-bottom: 1px solid var(--border);
      padding-bottom: 10px;
    }

    &.advanced-section {
      border-color: var(--info);
      background: var(--info-banner-bg);
    }
  }
}
</style>
