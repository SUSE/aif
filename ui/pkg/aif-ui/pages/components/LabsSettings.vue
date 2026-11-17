<script lang="ts" setup>
import { useStore } from 'vuex';
import { ref, computed, onMounted } from 'vue';
import AppModal from '@shell/components/AppModal';
import { Banner } from '@components/Banner';
import { BadgeState } from '@components/BadgeState';
import { Checkbox } from '@components/Form/Checkbox';
import { LabeledInput } from '@components/Form/LabeledInput';
import {
  loadLabs, loadLabCatalogs, buildLabRows, setLabEnabled, acceptLabUpdate, addLabCatalog, removeLabCatalog, removeLab, canEdit,
} from '../../services/labs';
import { useT } from '../../composables/useT';
import { getVersion } from '../../utils/operator-api';
import { LAB_TYPE, LAB_CATALOG_TYPE, BUILTIN_CATALOG } from '../../types/lab-types';
import type { Lab, LabRow, LabCatalog } from '../../types/lab-types';

const store = useStore();
const t = useT();

const expanded = ref(true);
const error = ref('');
const catError = ref('');
const catBusy = ref(false);
const busy = ref<Record<string, boolean>>({});
const newName = ref('');
const newUrl = ref('');
const loaded = ref(false);
// Catalog pending removal (confirmation modal), or null.
const removing = ref<LabCatalog | null>(null);
// Lab pending removal while Labs are turned off (confirmation modal), or null.
const removingLab = ref<Lab | null>(null);
const labBusy = ref(false);

// Watched lists (findAll) keep these getters live; no polling.
const allCatalogs = computed<LabCatalog[]>(() => store?.getters?.['management/all']?.(LAB_CATALOG_TYPE) || []);
const allLabs = computed(() => store?.getters?.['management/all']?.(LAB_TYPE) || []);
const labs = computed<LabRow[]>(() => buildLabRows(allCatalogs.value, allLabs.value));
const catalogs = computed(() => allCatalogs.value.filter((c) => c.metadata.name !== BUILTIN_CATALOG));
// Whether the operator runs the Lab controllers (Helm value labs.enabled), as
// reported by GET /api/v1/version. Cluster objects can't tell: Lab CRDs are always
// installed, and a builtin catalog can outlive a previous enable.
const labsEnabled = ref(false);
// Labs left installed when Labs were turned off: nothing reconciles them any more,
// so list them with a way to remove them.
const leftover = computed<Lab[]>(() => (labsEnabled.value ? [] : allLabs.value));
const labDeletable = computed(() => canEdit(store, LAB_TYPE, 'delete'));
const anyReady = computed(() => labs.value.some((l) => l.phase === 'Ready'));
const editable = computed(() => canEdit(store, LAB_TYPE));
const catEditable = computed(() => canEdit(store, LAB_CATALOG_TYPE));
const catDeletable = computed(() => canEdit(store, LAB_CATALOG_TYPE, 'delete'));

const PHASE_COLOR: Record<string, string> = {
  Ready: 'bg-success', Failed: 'bg-error', Pending: 'bg-warning', Installing: 'bg-info', Disabled: 'bg-default',
};

const errMsg = (e: any): string => e?._statusText || e?.message || e?.detail || String(e);

function sourceLabel(lab: LabRow): string {
  if (lab.source === 'catalog') return t('suseai.pages.settings.sections.labs.source.catalog', 'Catalog: {name}', { name: lab.catalog || '' });

  return lab.source === 'custom' ? t('suseai.pages.settings.sections.labs.source.custom', 'Custom') : t('suseai.pages.settings.sections.labs.source.builtin', 'Built-in');
}

function formatTime(iso?: string): string {
  const d = iso ? new Date(iso) : null;

  return d && !isNaN(d.getTime()) ? d.toLocaleString() : (iso || '');
}

async function acceptUpdate(lab: LabRow) {
  busy.value[lab.name] = true;
  try {
    await acceptLabUpdate(lab);
    error.value = '';
  } catch (e: any) {
    error.value = errMsg(e);
  } finally {
    busy.value[lab.name] = false;
  }
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

async function confirmRemoveLab() {
  const l = removingLab.value;

  removingLab.value = null;
  if (!l) return;
  labBusy.value = true;
  try {
    await removeLab(l);
    error.value = '';
  } catch (e: any) {
    error.value = errMsg(e);
  } finally {
    labBusy.value = false;
  }
}

async function confirmRemoveCatalog() {
  const c = removing.value;

  removing.value = null;
  if (!c) return;
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
    labsEnabled.value = (await getVersion()).labsEnabled === true;
    if (labsEnabled.value) {
      await Promise.all([loadLabCatalogs(store), loadLabs(store)]);
    } else {
      // Best effort: the Lab API may be missing, or the user may lack access.
      await loadLabs(store).catch(() => undefined);
    }
  } catch (e: any) {
    error.value = errMsg(e);
  } finally {
    loaded.value = true;
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
      <h2>{{ t('suseai.pages.settings.sections.labs.title', 'Labs') }}</h2>
      <BadgeState
        color="bg-warning"
        :label="t('suseai.pages.settings.sections.labs.experimentalBadge', 'experimental')"
      />
    </div>

    <div
      v-if="expanded && loaded && !labsEnabled && !error"
      class="mt-15"
      data-testid="labs-disabled"
    >
      <Banner color="info">
        <span>
          {{ t('suseai.pages.settings.sections.labs.disabled', 'Labs are turned off. Labs are experimental, unsupported previews that may never become product features; do not use them in production. To try them, an administrator sets the aif-operator Helm value') }}
          <code>labs.enabled=true</code>.
        </span>
      </Banner>
      <div
        v-if="leftover.length"
        data-testid="labs-leftover"
      >
        <Banner
          color="warning"
          :label="t('suseai.pages.settings.sections.labs.leftover', 'These labs are still installed. With Labs turned off nothing updates or disables them. Remove them to uninstall their extensions.')"
        />
        <div
          v-for="l in leftover"
          :key="l.metadata.name"
          class="lab-row"
          :data-testid="`lab-leftover-${ l.metadata.name }`"
        >
          <div class="lab-info">
            <h3>{{ l.spec?.displayName || l.spec?.catalogRef?.entry || l.metadata.name }}</h3>
            <p class="text-muted">
              {{ l.metadata.name }}<span v-if="l.status?.resolvedVersion"> · v{{ l.status.resolvedVersion }}</span>
            </p>
          </div>
          <button
            type="button"
            class="btn role-secondary"
            :disabled="labBusy || !labDeletable"
            :data-testid="`lab-leftover-remove-${ l.metadata.name }`"
            @click="removingLab = l"
          >
            {{ t('suseai.pages.settings.sections.labs.removeLab.button', 'Remove') }}
          </button>
        </div>
      </div>
    </div>

    <div
      v-if="expanded && (labsEnabled || error)"
      class="mt-15"
    >
      <Banner
        color="warning"
        :label="t('suseai.pages.settings.sections.labs.experimentalBanner', 'Labs are experimental, unsupported previews. They may change or be removed without notice. Do not use them in production.')"
      />
      <Banner
        color="info"
        :label="t('suseai.pages.settings.sections.labs.appliesImmediately', 'Changes in this section apply immediately. Apply and Cancel at the bottom of the page do not affect them.')"
        data-testid="labs-immediate"
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
        :label="t('suseai.pages.settings.sections.labs.reloadHint', 'After a lab becomes Ready, reload the browser to load its UI.')"
      />
      <p
        v-if="!editable"
        class="text-muted"
        data-testid="labs-readonly"
      >
        {{ t('suseai.pages.settings.sections.labs.readOnly', 'You do not have permission to change labs.') }}
      </p>
      <p
        v-if="!labs.length && !error"
        class="text-muted"
      >
        {{ t('suseai.pages.settings.sections.labs.empty', 'No labs available.') }}
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
            v-if="lab.maturity"
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
          <button
            v-if="lab.enabled && lab.updateAvailable"
            type="button"
            class="btn btn-sm role-secondary"
            :disabled="busy[lab.name] || !editable"
            :data-testid="`lab-update-${ lab.name }`"
            @click="acceptUpdate(lab)"
          >
            {{ t('suseai.pages.settings.sections.labs.update', 'Update to v{version}', { version: lab.updateAvailable }) }}
          </button>
          <Checkbox
            :value="lab.enabled"
            :disabled="busy[lab.name] || !editable"
            :label="t('suseai.pages.settings.sections.labs.enable', 'Enabled')"
            @update:value="toggle(lab, $event)"
          />
        </div>
      </div>

      <div
        class="catalogs mt-20"
        data-testid="lab-catalogs"
      >
        <h3>{{ t('suseai.pages.settings.sections.labs.catalogs.title', 'Lab catalogs') }}</h3>
        <p class="text-muted">
          {{ t('suseai.pages.settings.sections.labs.catalogs.help', 'Catalog URL must be public https and point at a JSON array in the same format as the built-in catalog. External catalogs need the operator registry host allowlist (manager.allowedRegistryHosts); only Helm entries from listed hosts are accepted.') }}
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
          {{ t('suseai.pages.settings.sections.labs.catalogs.readOnly', 'You do not have permission to change lab catalogs.') }}
        </p>
        <p
          v-if="!catalogs.length"
          class="text-muted"
        >
          {{ t('suseai.pages.settings.sections.labs.catalogs.empty', 'No external lab catalogs configured.') }}
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
              :label="c.status?.phase || t('suseai.pages.settings.sections.labs.catalogs.pending', 'Pending')"
            />
            <span class="text-muted">
              {{ t('suseai.pages.settings.sections.labs.catalogs.labCount', 'Labs') }}: {{ c.status?.entries?.length ?? 0 }}
            </span>
            <span
              v-if="c.status?.skipped"
              class="text-muted"
            >
              {{ t('suseai.pages.settings.sections.labs.catalogs.skipped', '{count} skipped (duplicate or rejected entries)', { count: String(c.status?.skipped) }) }}
            </span>
            <span
              v-if="c.status?.lastFetched"
              class="text-muted"
            >
              {{ t('suseai.pages.settings.sections.labs.catalogs.lastFetched', 'Last fetched') }}: {{ formatTime(c.status?.lastFetched) }}
            </span>
            <span
              v-if="c.status?.message"
              class="text-muted"
            >{{ c.status?.message }}</span>
            <button
              type="button"
              class="btn role-secondary"
              :disabled="catBusy || !catDeletable"
              :aria-label="t('suseai.pages.settings.sections.labs.catalogs.remove', 'Remove')"
              :data-testid="`lab-catalog-remove-${ c.metadata.name }`"
              @click="removing = c"
            >
              <i class="icon icon-trash" />
            </button>
          </div>
        </div>
        <div class="row mb-5">
          <div class="col span-4">
            <LabeledInput
              v-model:value="newName"
              :label="t('suseai.pages.settings.sections.labs.catalogs.name', 'Name')"
              :disabled="!catEditable"
            />
          </div>
          <div class="col span-6">
            <LabeledInput
              v-model:value="newUrl"
              :label="t('suseai.pages.settings.sections.labs.catalogs.url', 'URL')"
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
              {{ t('suseai.pages.settings.sections.labs.catalogs.add', 'Add catalog') }}
            </button>
          </div>
        </div>
      </div>
    </div>

    <AppModal
      v-if="removingLab"
      :click-to-close="true"
      :width="480"
      @close="removingLab = null"
    >
      <div class="modal-body">
        <h3>{{ t('suseai.pages.settings.sections.labs.removeLab.title', 'Remove lab') }}</h3>
        <p>
          {{ t('suseai.pages.settings.sections.labs.removeLab.prompt', 'Remove lab') }}
          <strong>{{ removingLab.spec?.displayName || removingLab.metadata.name }}</strong>?
        </p>
        <p class="text-muted">
          {{ t('suseai.pages.settings.sections.labs.removeLab.note', 'Its UI extension and server are uninstalled. Reload the browser afterwards.') }}
        </p>
        <div class="modal-buttons">
          <button
            type="button"
            class="btn role-secondary"
            data-testid="lab-remove-cancel"
            @click="removingLab = null"
          >
            {{ t('suseai.pages.settings.sections.labs.catalogs.deleteModal.cancel', 'Cancel') }}
          </button>
          <button
            type="button"
            class="btn role-primary bg-error"
            data-testid="lab-remove-confirm"
            @click="confirmRemoveLab"
          >
            {{ t('suseai.pages.settings.sections.labs.removeLab.button', 'Remove') }}
          </button>
        </div>
      </div>
    </AppModal>

    <AppModal
      v-if="removing"
      :click-to-close="true"
      :width="480"
      @close="removing = null"
    >
      <div class="modal-body">
        <h3>{{ t('suseai.pages.settings.sections.labs.catalogs.deleteModal.title', 'Remove lab catalog') }}</h3>
        <p>
          {{ t('suseai.pages.settings.sections.labs.catalogs.deleteModal.prompt', 'Remove lab catalog') }}
          <strong>{{ removing.spec.displayName || removing.metadata.name }}</strong>?
        </p>
        <p class="text-muted">
          {{ t('suseai.pages.settings.sections.labs.catalogs.deleteModal.note', 'Labs from this catalog stop receiving updates and cannot be re-enabled. Installed labs keep running until you disable them.') }}
        </p>
        <div class="modal-buttons">
          <button
            type="button"
            class="btn role-secondary"
            data-testid="lab-catalog-remove-cancel"
            @click="removing = null"
          >
            {{ t('suseai.pages.settings.sections.labs.catalogs.deleteModal.cancel', 'Cancel') }}
          </button>
          <button
            type="button"
            class="btn role-primary bg-error"
            data-testid="lab-catalog-remove-confirm"
            @click="confirmRemoveCatalog"
          >
            {{ t('suseai.pages.settings.sections.labs.catalogs.deleteModal.confirm', 'Remove') }}
          </button>
        </div>
      </div>
    </AppModal>
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
.modal-body { padding: 20px; }
.modal-buttons { display: flex; justify-content: flex-end; gap: 10px; margin-top: 20px; }
</style>
