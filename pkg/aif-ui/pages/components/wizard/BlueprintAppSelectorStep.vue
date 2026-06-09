<template>
  <div class="step-content">
    <h2 class="step-title">Select Applications</h2>

    <div class="search-row">
      <input
        v-model="searchQuery"
        type="search"
        class="form-control search-input"
        placeholder="Search applications..."
        @input="onSearch"
      />
      <div v-if="searchResults.length" class="search-dropdown">
        <div
          v-for="app in searchResults"
          :key="app.slug_name"
          class="search-result"
          @click="addApp(app)"
        >
          <img :src="app.logo_url || genericIcon" alt="" class="result-logo" @error="onImgError" />
          <div>
            <div class="result-name">{{ app.name }}</div>
            <div class="result-meta text-muted">{{ app.description?.slice(0, 60) }}</div>
          </div>
        </div>
      </div>
    </div>

    <div v-if="!components.length" class="empty-hint text-muted mt-20">
      Search and select at least one application.
    </div>

    <div class="selected-apps mt-20">
      <div
        v-for="(comp, idx) in components"
        :key="comp.chartName"
        class="selected-tile"
      >
        <img :src="logoFor(comp.chartName)" alt="" class="tile-logo" @error="onImgError" />
        <div class="tile-body">
          <div class="tile-name">{{ comp.chartName }}</div>
          <select
            :value="comp.chartVersion"
            class="version-select"
            @change="onVersionChange(idx, ($event.target as HTMLSelectElement).value)"
          >
            <option
              v-for="v in versionsFor(comp.chartName)"
              :key="v"
              :value="v"
            >
              {{ v }}
            </option>
          </select>
        </div>
        <button class="btn-remove" @click="removeApp(idx)" type="button">✕</button>
      </div>
    </div>
  </div>
</template>

<script lang="ts" setup>
import { ref, onMounted, getCurrentInstance } from 'vue';
import type { BlueprintComponent } from '../../../types/blueprint-types';
import type { AppCollectionItem } from '../../../services/app-collection';
import { fetchSuseAiApps, fetchNvidiaApps, fetchSettingsOrNull, getClusterRepoNameFromUrl } from '../../../services/app-collection';
import { listChartVersions, inferClusterRepoForChart } from '../../../services/rancher-apps';

const genericIcon = require('../../../assets/generic-app.svg');

interface Props { components: BlueprintComponent[] }
interface Emits { (e: 'update:components', v: BlueprintComponent[]): void }

const props  = defineProps<Props>();
const emit   = defineEmits<Emits>();
const vm     = getCurrentInstance()!.proxy as any;
const store  = vm.$store;

const searchQuery   = ref('');
const searchResults = ref<AppCollectionItem[]>([]);
const allApps       = ref<AppCollectionItem[]>([]);
const versionMap    = ref<Record<string, string[]>>({});
const logoMap       = ref<Record<string, string>>({});

// Combined catalog: SUSE AI Library + Nvidia Library (mirrors the Apps catalog selector).
async function loadAllApps(): Promise<AppCollectionItem[]> {
  const settings = await fetchSettingsOrNull();
  const [suseApps, nvidiaApps] = await Promise.all([
    fetchSuseAiApps(store, settings),
    fetchNvidiaApps(store, settings),
  ]);
  return [...suseApps, ...nvidiaApps];
}

// Backfill logos and versions for components selected before this mount (navigate-back / edit flow).
onMounted(async () => {
  if (!props.components.length) return;

  // Logos — one fetch covers all components.
  try {
    const apps = await loadAllApps();
    allApps.value = apps;
    const logoUpdates: Record<string, string> = {};
    for (const comp of props.components) {
      const app = apps.find(a => a.slug_name === comp.chartName);
      if (app?.logo_url) logoUpdates[comp.chartName] = app.logo_url;
    }
    if (Object.keys(logoUpdates).length) {
      logoMap.value = { ...logoMap.value, ...logoUpdates };
    }
  } catch { /* generic icon fallback is fine */ }

  // Versions — fetch per component in parallel.
  await Promise.allSettled(
    props.components.map(async (comp) => {
      try {
        const versions = await listChartVersions(store, 'local', comp.chartRepo, comp.chartName);
        if (versions.length) {
          versionMap.value = { ...versionMap.value, [comp.chartName]: versions };
        }
      } catch { /* single-version fallback is fine */ }
    })
  );
});

async function onSearch() {
  const q = searchQuery.value.toLowerCase();
  if (!q) { searchResults.value = []; return; }
  if (!allApps.value.length) {
    allApps.value = await loadAllApps();
  }
  const existing = new Set(props.components.map(c => c.chartName));
  searchResults.value = allApps.value
    .filter(a => !existing.has(a.slug_name))
    .filter(a =>
      a.name.toLowerCase().includes(q) ||
      a.slug_name.toLowerCase().includes(q)
    )
    .slice(0, 8);
}

async function addApp(app: AppCollectionItem) {
  searchQuery.value   = '';
  searchResults.value = [];

  if (props.components.find(c => c.chartName === app.slug_name)) return;

  // Resolve the cluster repo name from the app's repository URL
  let chartRepo = 'suse-ai';
  try {
    if (app.repository_url) {
      const repoName = await getClusterRepoNameFromUrl(store, app.repository_url);
      if (repoName) chartRepo = repoName;
    } else {
      const inferred = await inferClusterRepoForChart(store, app.slug_name);
      if (inferred) chartRepo = inferred;
    }
  } catch {
    // fallback to 'suse-ai'
  }

  // Fetch available chart versions
  let versions: string[] = [];
  try {
    versions = await listChartVersions(store, 'local', chartRepo, app.slug_name);
  } catch {
    versions = [];
  }
  versionMap.value = { ...versionMap.value, [app.slug_name]: versions };
  if (app.logo_url) {
    logoMap.value = { ...logoMap.value, [app.slug_name]: app.logo_url };
  }

  emit('update:components', [
    ...props.components,
    {
      chartRepo,
      chartName:    app.slug_name,
      chartVersion: versions[0] || '1.0.0',
    },
  ]);
}

function removeApp(idx: number) {
  emit('update:components', props.components.filter((_, i) => i !== idx));
}

function onVersionChange(idx: number, newVersion: string) {
  const updated = props.components.map((c, i) =>
    i === idx ? { ...c, chartVersion: newVersion } : c
  );
  emit('update:components', updated);
}

function versionsFor(chartName: string): string[] {
  const v = versionMap.value[chartName];
  if (v?.length) return v;
  const current = props.components.find(c => c.chartName === chartName)?.chartVersion;
  return current ? [current] : ['1.0.0'];
}

function logoFor(chartName: string): string {
  return logoMap.value[chartName] || genericIcon;
}

function onImgError(e: Event) {
  (e.target as HTMLImageElement).src = genericIcon;
}
</script>

<style lang="scss" scoped>
.step-content { max-width: 700px; }
.step-title { margin: 0 0 24px; font-size: 18px; font-weight: 600; }
.search-row { position: relative; }
.search-input { width: 100%; }
.search-dropdown {
  position: absolute; z-index: 100; background: var(--body-bg);
  border: 1px solid var(--border); border-radius: 6px; max-height: 260px;
  overflow-y: auto; width: 100%;
  .search-result {
    display: flex; align-items: center; gap: 12px;
    padding: 10px 14px; cursor: pointer;
    &:hover { background: var(--hover-bg); }
    .result-logo { width: 28px; height: 28px; object-fit: contain; }
    .result-name { font-weight: 500; font-size: 14px; }
    .result-meta { font-size: 12px; }
  }
}
.empty-hint { color: var(--muted); font-size: 14px; }
.mt-20 { margin-top: 20px; }
.selected-apps { display: flex; flex-direction: column; gap: 12px; }
.selected-tile {
  display: flex; align-items: center; gap: 12px;
  padding: 12px 16px; border: 1px solid var(--border); border-radius: 8px;
  .tile-logo { width: 36px; height: 36px; object-fit: contain; }
  .tile-body { flex: 1; display: flex; align-items: center; gap: 12px; }
  .tile-name { font-weight: 500; font-size: 14px; min-width: 120px; }
  .version-select {
    font-size: 13px; height: 30px; padding: 0 8px;
    border: 1px solid var(--border); border-radius: var(--border-radius);
    background: var(--input-bg); color: var(--body-text);
  }
  .btn-remove {
    background: none; border: none; cursor: pointer;
    color: var(--muted); font-size: 16px;
    &:hover { color: var(--error); }
  }
}
.form-control {
  width: 100%; padding: 8px 12px;
  border: 1px solid var(--border); border-radius: var(--border-radius);
  background: var(--input-bg); color: var(--body-text); font-size: 14px;
}
.text-muted { color: var(--muted); font-size: 12px; }
</style>
