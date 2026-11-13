<script lang="ts" setup>
import { useStore } from 'vuex';
import { ref, computed, onMounted } from 'vue';
import { Banner } from '@components/Banner';
import { BadgeState } from '@components/BadgeState';
import { Checkbox } from '@components/Form/Checkbox';
import { LabeledInput } from '@components/Form/LabeledInput';
import {
  loadLabs, loadLabCatalogs, buildLabRows, setLabEnabled, addLabCatalog, removeLabCatalog, canEdit,
} from '../../services/labs';
import { LAB_TYPE, LAB_CATALOG_TYPE, BUILTIN_CATALOG } from '../../types/lab-types';
import type { LabRow, LabCatalog } from '../../types/lab-types';

const store = useStore();
const t = (key: string, args?: Record<string, unknown>): string => store?.getters['i18n/t']?.(key, args) ?? key;

const expanded = ref(true);
const error = ref('');
const catError = ref('');
const catBusy = ref(false);
const busy = ref<Record<string, boolean>>({});
const newName = ref('');
const newUrl = ref('');

// Watched lists (findAll) keep these getters live; no polling.
const allCatalogs = computed<LabCatalog[]>(() => store?.getters?.['management/all']?.(LAB_CATALOG_TYPE) || []);
const allLabs = computed(() => store?.getters?.['management/all']?.(LAB_TYPE) || []);
const labs = computed<LabRow[]>(() => buildLabRows(allCatalogs.value, allLabs.value));
const catalogs = computed(() => allCatalogs.value.filter((c) => c.metadata.name !== BUILTIN_CATALOG));
const anyReady = computed(() => labs.value.some((l) => l.phase === 'Ready'));
const editable = computed(() => canEdit(store, LAB_TYPE));
const catEditable = computed(() => canEdit(store, LAB_CATALOG_TYPE));
const catDeletable = computed(() => canEdit(store, LAB_CATALOG_TYPE, 'delete'));

const PHASE_COLOR: Record<string, string> = {
  Ready: 'bg-success', Failed: 'bg-error', Pending: 'bg-warning', Installing: 'bg-info', Disabled: 'bg-default',
};

const errMsg = (e: any): string => e?._statusText || e?.message || e?.detail || String(e);

function sourceLabel(lab: LabRow): string {
  if (lab.source === 'catalog') return t('suseai.pages.settings.sections.labs.source.catalog', { name: lab.catalog || '' });

  return t(`suseai.pages.settings.sections.labs.source.${ lab.source === 'custom' ? 'custom' : 'builtin' }`);
}

async function toggle(lab: LabRow, enabled: boolean) {
  busy.value[lab.name] = true;
  try {
    await setLabEnabled(store, lab, enabled);
    error.value = '';
  } catch (e: any) {
    error.value = errMsg(e);
  } finally {
    busy.value[lab.name] = false;
  }
}

async function addCatalog() {
  catBusy.value = true;
  try {
    await addLabCatalog(store, newName.value.trim(), newUrl.value.trim());
    newName.value = '';
    newUrl.value = '';
    catError.value = '';
  } catch (e: any) {
    catError.value = errMsg(e);
  } finally {
    catBusy.value = false;
  }
}

async function removeCatalog(c: LabCatalog) {
  catBusy.value = true;
  try {
    await removeLabCatalog(c);
    catError.value = '';
  } catch (e: any) {
    catError.value = errMsg(e);
  } finally {
    catBusy.value = false;
  }
}

onMounted(async() => {
  try {
    await Promise.all([loadLabCatalogs(store), loadLabs(store)]);
  } catch (e: any) {
    error.value = errMsg(e);
  }
});
</script>

<template>
  <div
    class="box mt-10"
    data-testid="section-labs"
  >
    <div
      class="accordion-header"
      role="button"
      tabindex="0"
      @click="expanded = !expanded"
      @keydown.space.enter.prevent="expanded = !expanded"
    >
      <i :class="expanded ? 'icon icon-chevron-down' : 'icon icon-chevron-right'" />
      <h2>{{ t('suseai.pages.settings.sections.labs.title') }}</h2>
      <BadgeState
        color="bg-warning"
        :label="t('suseai.pages.settings.sections.labs.experimentalBadge')"
      />
    </div>

    <div
      v-if="expanded"
      class="mt-15"
    >
      <Banner
        color="warning"
        :label="t('suseai.pages.settings.sections.labs.experimentalBanner')"
      />
      <Banner
        v-if="error"
        color="error"
        :label="error"
        data-testid="labs-error"
      />
      <Banner
        v-if="anyReady"
        color="info"
        :label="t('suseai.pages.settings.sections.labs.reloadHint')"
      />
      <p
        v-if="!editable"
        class="text-muted"
        data-testid="labs-readonly"
      >
        {{ t('suseai.pages.settings.sections.labs.readOnly') }}
      </p>
      <p
        v-if="!labs.length && !error"
        class="text-muted"
      >
        {{ t('suseai.pages.settings.sections.labs.empty') }}
      </p>

      <div
        v-for="lab in labs"
        :key="lab.name"
        class="lab-row"
        :data-testid="`lab-row-${ lab.name }`"
      >
        <div class="lab-info">
          <h3>{{ lab.displayName }}</h3>
          <p class="text-muted">
            {{ lab.description }}
          </p>
          <p
            v-if="lab.message"
            class="text-muted"
          >
            {{ lab.message }}
          </p>
        </div>
        <div class="lab-meta">
          <BadgeState
            color="bg-info"
            :label="sourceLabel(lab)"
            :data-testid="`lab-source-${ lab.name }`"
          />
          <BadgeState
            color="bg-warning"
            :label="lab.maturity"
          />
          <BadgeState
            :color="PHASE_COLOR[lab.phase] || 'bg-default'"
            :label="lab.phase"
          />
          <span
            v-if="lab.version"
            class="text-muted"
          >v{{ lab.version }}</span>
          <Checkbox
            :value="lab.enabled"
            :disabled="busy[lab.name] || !editable"
            :label="t('suseai.pages.settings.sections.labs.enable')"
            @update:value="toggle(lab, $event)"
          />
        </div>
      </div>

      <div
        class="catalogs mt-20"
        data-testid="lab-catalogs"
      >
        <h3>{{ t('suseai.pages.settings.sections.labs.catalogs.title') }}</h3>
        <p class="text-muted">
          {{ t('suseai.pages.settings.sections.labs.catalogs.help') }}
        </p>
        <Banner
          v-if="catError"
          color="error"
          :label="catError"
          data-testid="lab-catalogs-error"
        />
        <p
          v-if="!catEditable"
          class="text-muted"
        >
          {{ t('suseai.pages.settings.sections.labs.catalogs.readOnly') }}
        </p>
        <p
          v-if="!catalogs.length"
          class="text-muted"
        >
          {{ t('suseai.pages.settings.sections.labs.catalogs.empty') }}
        </p>
        <div
          v-for="c in catalogs"
          :key="c.metadata.name"
          class="cat-row mb-10"
          :data-testid="`lab-catalog-row-${ c.metadata.name }`"
        >
          <div class="cat-info">
            <strong>{{ c.spec.displayName || c.metadata.name }}</strong>
            <span class="text-muted">{{ c.spec.url }}</span>
          </div>
          <div class="cat-status">
            <BadgeState
              :color="c.status?.phase === 'Ready' ? 'bg-success' : (c.status?.phase ? 'bg-error' : 'bg-default')"
              :label="c.status?.phase || t('suseai.pages.settings.sections.labs.catalogs.pending')"
            />
            <span class="text-muted">
              {{ t('suseai.pages.settings.sections.labs.catalogs.labCount') }}: {{ c.status?.entries?.length ?? 0 }}
            </span>
            <span
              v-if="c.status?.skipped"
              class="text-muted"
            >
              {{ t('suseai.pages.settings.sections.labs.catalogs.skipped', { count: c.status?.skipped }) }}
            </span>
            <span
              v-if="c.status?.lastFetched"
              class="text-muted"
            >
              {{ t('suseai.pages.settings.sections.labs.catalogs.lastFetched') }}: {{ c.status?.lastFetched }}
            </span>
            <span
              v-if="c.status?.message"
              class="text-muted"
            >{{ c.status?.message }}</span>
            <button
              type="button"
              class="btn role-secondary"
              :disabled="catBusy || !catDeletable"
              :aria-label="t('suseai.pages.settings.sections.labs.catalogs.remove')"
              :data-testid="`lab-catalog-remove-${ c.metadata.name }`"
              @click="removeCatalog(c)"
            >
              <i class="icon icon-trash" />
            </button>
          </div>
        </div>
        <div class="row mb-5">
          <div class="col span-4">
            <LabeledInput
              v-model:value="newName"
              :label="t('suseai.pages.settings.sections.labs.catalogs.name')"
              :disabled="!catEditable"
            />
          </div>
          <div class="col span-6">
            <LabeledInput
              v-model:value="newUrl"
              :label="t('suseai.pages.settings.sections.labs.catalogs.url')"
              :disabled="!catEditable"
            />
          </div>
          <div class="col span-2 trash-col">
            <button
              type="button"
              class="btn role-primary"
              :disabled="catBusy || !catEditable || !newName.trim() || !newUrl.trim()"
              data-testid="lab-catalog-add"
              @click="addCatalog"
            >
              {{ t('suseai.pages.settings.sections.labs.catalogs.add') }}
            </button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style lang="scss" scoped>
/* Copied from Settings.vue: its scoped styles don't reach child components. */
.accordion-header {
  display: flex;
  align-items: center;
  gap: 10px;
  cursor: pointer;
  width: fit-content;

  h2 {
    margin: 0;
  }

  &:focus-visible {
    outline: var(--outline-width) solid var(--outline);
  }
}
.box {
  border-radius: var(--border-radius);
  border: 1px solid var(--border);
  padding: 15px;
}
.lab-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 16px;
  padding: 12px 0;
  border-top: 1px solid var(--border);
}
.cat-row { display: flex; justify-content: space-between; align-items: center; gap: 16px; flex-wrap: wrap; }
.cat-info { display: flex; flex-direction: column; }
.cat-status { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; }
.trash-col { display: flex; align-items: center; }
.lab-meta { display: flex; align-items: center; gap: 12px; }
</style>
