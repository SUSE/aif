<script setup lang="ts">
import { useStore } from 'vuex';
import { useI18n } from '@shell/composables/useI18n';
import { kpis, usageSeries } from '../mock';

const { t } = useI18n(useStore());

const W = 720;
const H = 180;
const max = Math.max(...usageSeries) * 1.1;
const pts = usageSeries.map((v, i) => `${ (i / (usageSeries.length - 1)) * W },${ H - (v / max) * H }`);
const line = pts.join(' ');
const area = `0,${ H } ${ line } ${ W },${ H }`;
</script>

<template>
  <div>
    <div class="tiles">
      <div
        v-for="k in kpis"
        :key="k.label"
        class="tile"
      >
        <div class="label">
          {{ k.label }}
        </div>
        <div class="value">
          {{ k.value }}
        </div>
        <div class="delta">
          {{ t('tokenFactory.overview.vsYesterday', { delta: k.delta }) }}
        </div>
      </div>
    </div>
    <h3 class="mt-20">
      {{ t('tokenFactory.overview.chartTitle') }}
    </h3>
    <svg
      :viewBox="`0 0 ${ W } ${ H }`"
      class="chart"
      preserveAspectRatio="none"
      role="img"
      :aria-label="t('tokenFactory.overview.chartAria')"
    >
      <polygon
        :points="area"
        class="chart-area"
      />
      <polyline
        :points="line"
        class="chart-line"
        vector-effect="non-scaling-stroke"
      />
    </svg>
    <div class="axis">
      <span>{{ t('tokenFactory.overview.axis.start') }}</span>
      <span>{{ t('tokenFactory.overview.axis.middle') }}</span>
      <span>{{ t('tokenFactory.overview.axis.end') }}</span>
    </div>
  </div>
</template>

<style scoped lang="scss">
.tiles { display: grid; grid-template-columns: repeat(auto-fit, minmax(170px, 1fr)); gap: 12px; }
.tile { border: 1px solid var(--border); border-radius: var(--border-radius); padding: 14px; background: var(--box-bg); }
.label { color: var(--muted); font-size: 12px; }
.value { font-size: 26px; font-weight: 600; margin: 6px 0; }
.delta { font-size: 12px; color: var(--success); }
.chart { width: 100%; height: 180px; border: 1px solid var(--border); border-radius: var(--border-radius); }
.chart-area { fill: var(--success); fill-opacity: 0.18; }
.chart-line { fill: none; stroke: var(--success); stroke-width: 2; }
.axis { display: flex; justify-content: space-between; color: var(--muted); font-size: 12px; margin-top: 4px; }
</style>
