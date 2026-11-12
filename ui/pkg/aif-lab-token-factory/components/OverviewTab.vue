<script setup lang="ts">
import { kpis, usageSeries } from '../mock';

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
      <div v-for="k in kpis" :key="k.label" class="tile">
        <div class="label">{{ k.label }}</div>
        <div class="value">{{ k.value }}</div>
        <div class="delta">{{ k.delta }} vs yesterday</div>
      </div>
    </div>
    <h3 class="mt-20">Tokens per hour (millions, last 24h)</h3>
    <svg :viewBox="`0 0 ${ W } ${ H }`" class="chart" preserveAspectRatio="none" role="img" aria-label="Token usage chart">
      <polygon :points="area" fill="#30ba78" fill-opacity="0.18" />
      <polyline :points="line" fill="none" stroke="#30ba78" stroke-width="2" vector-effect="non-scaling-stroke" />
    </svg>
    <div class="axis"><span>-24h</span><span>-12h</span><span>now</span></div>
  </div>
</template>

<style scoped lang="scss">
.tiles { display: grid; grid-template-columns: repeat(auto-fit, minmax(170px, 1fr)); gap: 12px; }
.tile { border: 1px solid var(--border); border-radius: 6px; padding: 14px; background: var(--box-bg, transparent); }
.label { color: var(--muted); font-size: 12px; }
.value { font-size: 26px; font-weight: 600; margin: 6px 0; }
.delta { font-size: 12px; color: #30ba78; }
.chart { width: 100%; height: 180px; border: 1px solid var(--border); border-radius: 6px; }
.axis { display: flex; justify-content: space-between; color: var(--muted); font-size: 12px; margin-top: 4px; }
</style>
