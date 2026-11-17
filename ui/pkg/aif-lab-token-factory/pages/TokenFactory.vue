<script setup lang="ts">
import { useStore } from 'vuex';
import { useI18n } from '@shell/composables/useI18n';
import { Banner } from '@components/Banner';
import Tabbed from '@shell/components/Tabbed';
import Tab from '@shell/components/Tabbed/Tab';
import OverviewTab from '../components/OverviewTab.vue';
import EndpointsTab from '../components/EndpointsTab.vue';
import KeysTab from '../components/KeysTab.vue';
import QuotasTab from '../components/QuotasTab.vue';
import PlaygroundTab from '../components/PlaygroundTab.vue';

const { t } = useI18n(useStore());
</script>

<template>
  <div>
    <h1>{{ t('tokenFactory.title') }}</h1>
    <Banner
      color="warning"
      class="mb-20"
    >
      {{ t('tokenFactory.experimentalBanner') }}
    </Banner>
    <Tabbed :side-tabs="false">
      <Tab
        name="overview"
        label-key="tokenFactory.tabs.overview"
        :weight="5"
      >
        <OverviewTab />
      </Tab>
      <Tab
        name="endpoints"
        label-key="tokenFactory.tabs.endpoints"
        :weight="4"
      >
        <EndpointsTab />
      </Tab>
      <Tab
        name="keys"
        label-key="tokenFactory.tabs.keys"
        :weight="3"
      >
        <KeysTab />
      </Tab>
      <Tab
        name="quotas"
        label-key="tokenFactory.tabs.quotas"
        :weight="2"
      >
        <QuotasTab />
      </Tab>
      <Tab
        name="playground"
        label-key="tokenFactory.tabs.playground"
        :weight="1"
      >
        <PlaygroundTab />
      </Tab>
    </Tabbed>
  </div>
</template>
