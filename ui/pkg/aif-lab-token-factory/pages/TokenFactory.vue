<script setup lang="ts">
import { Banner } from '@components/Banner';
import Tabbed from '@shell/components/Tabbed';
import Tab from '@shell/components/Tabbed/Tab';
import OverviewTab from '../components/OverviewTab.vue';
import EndpointsTab from '../components/EndpointsTab.vue';
import KeysTab from '../components/KeysTab.vue';
import QuotasTab from '../components/QuotasTab.vue';
import PlaygroundTab from '../components/PlaygroundTab.vue';
</script>

<template>
  <div class="tf-page">
    <h1>Token Factory</h1>
    <Banner color="warning" class="mb-20">
      Labs · Experimental. This page shows mocked data only; no backend is connected.
    </Banner>
    <Tabbed :side-tabs="false">
      <Tab name="overview" label="Overview" :weight="5"><OverviewTab /></Tab>
      <Tab name="endpoints" label="Model endpoints" :weight="4"><EndpointsTab /></Tab>
      <Tab name="keys" label="API keys" :weight="3"><KeysTab /></Tab>
      <Tab name="quotas" label="Quotas &amp; billing" :weight="2"><QuotasTab /></Tab>
      <Tab name="playground" label="Playground" :weight="1"><PlaygroundTab /></Tab>
    </Tabbed>
  </div>
</template>

<style lang="scss">
.tf-page {
  .tf-toolbar { display: flex; justify-content: flex-end; margin-bottom: 12px; }
  .tf-modal-bg { position: fixed; inset: 0; background: rgba(0,0,0,.5); z-index: 1000; display: flex; align-items: center; justify-content: center; }
  .tf-modal { background: var(--body-bg); border: 1px solid var(--border); border-radius: 6px; padding: 24px; width: 480px; max-width: 90vw; }
  .tf-modal h3 { margin-top: 0; }
  .tf-modal .tf-actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 20px; }
  .tf-modal label { display: block; margin: 12px 0 4px; }
  .tf-modal input, .tf-modal select { width: 100%; }
  .tf-code { background: var(--box-bg, #1e1e1e); color: var(--body-text); border: 1px solid var(--border); border-radius: 4px; padding: 12px; font-family: monospace; font-size: 12px; white-space: pre-wrap; word-break: break-all; }
  .tf-bar { height: 10px; background: var(--border); border-radius: 5px; overflow: hidden; min-width: 100px; }
  .tf-bar > div { height: 100%; background: #30ba78; }
  .tf-bar > div.warn { background: #f5a623; }
  .tf-bar > div.crit { background: #dc3545; }
  .tf-mono { font-family: monospace; font-size: 12px; }
}
</style>
