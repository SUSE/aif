<template>
  <main class="main-layout">
    <div class="outlet">
      <header class="fixed-header">
        <h1>Blueprints</h1>
        <div class="actions-container" role="toolbar">
          <div class="search-box">
            <input
              v-model="search"
              type="search"
              placeholder="Search blueprints"
              class="input-sm"
            />
          </div>

          <select v-model="sortBy" class="sort-select form-control-sm">
            <option value="name-asc">Name (A → Z)</option>
            <option value="name-desc">Name (Z → A)</option>
            <option value="newest">Newest first</option>
            <option value="oldest">Oldest first</option>
          </select>

          <Checkbox v-model:value="showDeprecated" label="Show deprecated" />

          <button class="btn role-primary ml-auto" @click="navigateCreate" type="button">
            Create
          </button>
          <button class="btn role-secondary" @click="refresh" :disabled="loading" type="button">
            <i v-if="loading" class="icon icon-spinner icon-spin" />
            <i v-else class="icon icon-refresh" />
            Refresh
          </button>
        </div>
      </header>

      <OperatorErrorBanner v-if="operatorError" :operator-error="operatorError" @retry="retryConnection" />

      <Banner v-if="error" color="error">{{ error }}</Banner>

      <div class="main-content">
        <div v-if="!loading && !sortedFamilies.length && !error && !operatorError" class="empty-state-content">
          <i class="icon icon-folder-open icon-4x text-muted" />
          <h3>No blueprints found</h3>
          <p class="text-muted">Click Create to define your first blueprint.</p>
        </div>

        <div class="tiles-grid" role="grid">
          <div
            v-for="[family, versions] in sortedFamilies"
            :key="family"
            class="app-tile"
          >
            <div class="tile-header">
              <div class="tile-info">
                <div class="tile-title-row">
                  <h3 class="tile-title">{{ toTitleCase(latestFor(versions).spec.displayName) }}</h3>
                  <select
                    v-model="selectedVersions[family]"
                    class="version-select form-control-sm"
                    @click.stop
                  >
                    <option
                      v-for="bp in visibleVersionsFor(versions)"
                      :key="bp.spec.version"
                      :value="bp.spec.version"
                    >
                      {{ versionLabel(bp) }}
                    </option>
                  </select>
                </div>
                <div class="tile-meta">
                  <span class="tile-meta-item">{{ componentCount(versions, family) }} apps</span>
                </div>
              </div>
            </div>

            <div class="tile-content">
              <p class="tile-description">{{ descriptionFor(versions, family) || '—' }}</p>
            </div>

            <div class="tile-footer">
              <button class="btn role-primary btn-sm" @click="navigateInstall(family, versions)" type="button">
                Install
              </button>
              <div style="margin-left: auto">
                <ActionMenuShell
                  button-variant="tertiary"
                  button-aria-label="More options"
                  :custom-actions="tileActions(family, versions)"
                  @action-invoked="onTileAction($event, family, versions)"
                />
              </div>
            </div>
          </div>
          <div v-for="n in 5" :key="`filler-${ n }`" class="app-tile app-tile-filler" />
        </div>
      </div>

      <!-- Delete confirmation modal -->
      <AppModal v-if="deleteModal.show" :click-to-close="true" :width="480" @close="deleteModal.show = false">
        <div class="modal-body">
          <h3>Delete Blueprint</h3>
          <p>
            Delete <strong>{{ deleteModal.displayName }}</strong>
            v{{ deleteModal.version }}?
          </p>
          <Banner v-if="deleteModal.activeWorkloads.length" color="warning" class="mb-10">
            <strong>Warning:</strong> The following AIWorkloads use this blueprint version and will lose their source reference:
            <ul class="mt-5">
              <li v-for="wl in deleteModal.activeWorkloads" :key="wl.metadata.name">
                {{ wl.metadata.namespace }}/{{ wl.metadata.name }}
              </li>
            </ul>
          </Banner>
          <div class="modal-buttons">
            <button class="btn role-secondary" @click="deleteModal.show = false">Cancel</button>
            <button class="btn role-primary" @click="executeDelete" :disabled="deleteModal.deleting">
              <i v-if="deleteModal.deleting" class="icon icon-spinner icon-spin" />
              Delete
            </button>
          </div>
        </div>
      </AppModal>

      <!-- Deprecate / Undeprecate confirmation modal -->
      <AppModal v-if="deprecateModal.show" :click-to-close="true" :width="480" @close="deprecateModal.show = false">
        <div class="modal-body">
          <h3>{{ deprecateModal.currentlyDeprecated ? 'Undeprecate' : 'Deprecate' }} Blueprint</h3>
          <p>
            {{ deprecateModal.currentlyDeprecated ? 'Undeprecate' : 'Deprecate' }}
            <strong>{{ deprecateModal.displayName }}</strong> v{{ deprecateModal.version }}?
          </p>
          <p v-if="!deprecateModal.currentlyDeprecated" class="text-muted modal-hint">
            Deprecated blueprints are hidden from the Blueprints page by default.
            Users with existing deployments are not affected.
          </p>
          <Banner
            v-if="!deprecateModal.currentlyDeprecated && deprecateModal.activeWorkloads.length"
            color="warning"
            class="mb-10"
          >
            <strong>Warning:</strong> The following deployments are currently using this blueprint version:
            <ul class="mt-5">
              <li v-for="wl in deprecateModal.activeWorkloads" :key="wl.metadata.name">
                {{ wl.metadata.namespace }}/{{ wl.metadata.name }}
              </li>
            </ul>
          </Banner>
          <div class="modal-buttons">
            <button class="btn role-secondary" @click="deprecateModal.show = false">Cancel</button>
            <button class="btn role-primary" @click="executeDeprecate" :disabled="deprecateModal.saving">
              <i v-if="deprecateModal.saving" class="icon icon-spinner icon-spin" />
              {{ deprecateModal.currentlyDeprecated ? 'Undeprecate' : 'Deprecate' }}
            </button>
          </div>
        </div>
      </AppModal>
    </div>
  </main>
</template>

<script lang="ts">
import { defineComponent, ref, computed, watch, onMounted, onUnmounted, getCurrentInstance, reactive } from 'vue';
import { Banner } from '@components/Banner';
import { Checkbox } from '@components/Form/Checkbox';
import ActionMenuShell from '@shell/components/ActionMenuShell';
import AppModal from '@shell/components/AppModal';
import { isAdminUser } from '@shell/store/type-map';
import {
  listBlueprints, deleteBlueprint, updateBlueprintDeprecated, groupBlueprintsByFamily, latestVersion,
} from '../utils/blueprint-api';
import { listAIWorkloads } from '../utils/operator-api';
import { checkOperatorConnection, getConnectionError } from '../utils/operator-config';
import OperatorErrorBanner from '../components/OperatorErrorBanner.vue';
import type { Blueprint } from '../types/blueprint-types';
import { PRODUCT } from '../config/suseai';

export default defineComponent({
  name: 'SuseAIBlueprints',
  components: { Banner, Checkbox, ActionMenuShell, AppModal, OperatorErrorBanner },
  setup() {
    const vm        = getCurrentInstance()!.proxy as any;
    const $router   = vm.$router;
    const $route    = vm.$route;
    const cluster   = ($route?.params?.cluster as string) || '_';

    const loading         = ref(true);
    const error           = ref<string | null>(null);
    const operatorError   = ref<string | null>(null);
    const search          = ref('');
    const sortBy          = ref('name-asc');
    const blueprints      = ref<Blueprint[]>([]);
    const selectedVersions = ref<Record<string, string>>({});
    const showDeprecated  = ref(false);

    // Global Administrator check — true only when the current user has globalRoleName === 'admin'.
    const isAdmin = ref(false);

    // ── Helpers ────────────────────────────────────────────────────────────────
    function isDeprecated(bp: Blueprint): boolean {
      return bp.spec.deprecated === true;
    }

    function visibleVersionsFor(versions: Blueprint[]): Blueprint[] {
      if (showDeprecated.value) return versions;
      return versions.filter(bp => !isDeprecated(bp));
    }

    // ── Computed ───────────────────────────────────────────────────────────────
    const families = computed(() => groupBlueprintsByFamily(blueprints.value));

    const filteredFamilies = computed(() => {
      const q = search.value.toLowerCase();
      return [...families.value.entries()].filter(([, versions]) => {
        // When not showing deprecated, hide families that have no visible versions.
        if (!showDeprecated.value && visibleVersionsFor(versions).length === 0) return false;
        if (!q) return true;
        const bp = versions[0];
        return (
          bp.spec.displayName.toLowerCase().includes(q) ||
          bp.spec.description?.toLowerCase().includes(q) ||
          bp.metadata.name.includes(q)
        );
      });
    });

    const sortedFamilies = computed(() => {
      const entries = [...filteredFamilies.value];
      const key = sortBy.value;
      entries.sort((a, b) => {
        const bpA = latestVersion(a[1]);
        const bpB = latestVersion(b[1]);
        switch (key) {
          case 'name-desc':
            return bpB.spec.displayName.localeCompare(bpA.spec.displayName);
          case 'newest':
            return (bpB.metadata.creationTimestamp || '').localeCompare(bpA.metadata.creationTimestamp || '');
          case 'oldest':
            return (bpA.metadata.creationTimestamp || '').localeCompare(bpB.metadata.creationTimestamp || '');
          default:
            return bpA.spec.displayName.localeCompare(bpB.spec.displayName);
        }
      });
      return entries;
    });

    // When the user hides deprecated, bump any selected-version that is deprecated
    // to the latest non-deprecated version for that family.
    watch(showDeprecated, (showNow) => {
      if (showNow) return;
      const updates: Record<string, string> = {};
      for (const [family, versions] of families.value.entries()) {
        const cur = selectedVersions.value[family];
        const bp  = versions.find(v => v.spec.version === cur);
        if (bp && isDeprecated(bp)) {
          const fallback = versions.find(v => !isDeprecated(v));
          if (fallback) updates[family] = fallback.spec.version;
        }
      }
      if (Object.keys(updates).length) {
        selectedVersions.value = { ...selectedVersions.value, ...updates };
      }
    });

    function selectedVersion(family: string, versions: Blueprint[]): Blueprint {
      const v = selectedVersions.value[family];
      // If the stored version is deprecated and deprecated are hidden, fall back.
      const candidate = versions.find(b => b.spec.version === v);
      if (candidate && (!isDeprecated(candidate) || showDeprecated.value)) return candidate;
      return visibleVersionsFor(versions)[0] || latestVersion(versions);
    }

    function componentCount(versions: Blueprint[], family: string): number {
      return selectedVersion(family, versions).spec.components.length;
    }

    function descriptionFor(versions: Blueprint[], family: string): string {
      return selectedVersion(family, versions).spec.description || '';
    }

    function versionLabel(bp: Blueprint): string {
      return isDeprecated(bp) ? `v${ bp.spec.version } (deprecated)` : `v${ bp.spec.version }`;
    }

    // ── Data loading ───────────────────────────────────────────────────────────
    async function refresh() {
      loading.value = true;
      error.value = null;
      await checkOperatorConnection();
      operatorError.value = getConnectionError();
      if (operatorError.value) {
        loading.value = false;
        return;
      }
      try {
        const list = await listBlueprints();
        blueprints.value = list.items || [];
        const updates: Record<string, string> = {};
        for (const [family, versions] of groupBlueprintsByFamily(blueprints.value).entries()) {
          const current = selectedVersions.value[family];
          const visible = visibleVersionsFor(versions);
          const stillVisible = current && visible.some(v => v.spec.version === current);
          if (!stillVisible) {
            const pick = visible[0] || latestVersion(versions);
            updates[family] = pick.spec.version;
          }
        }
        if (Object.keys(updates).length) {
          selectedVersions.value = { ...selectedVersions.value, ...updates };
        }
      } catch (e: any) {
        error.value = e?.message || 'Failed to load blueprints';
      } finally {
        loading.value = false;
      }
    }

    async function retryConnection() {
      loading.value = true;
      await checkOperatorConnection(true);
      operatorError.value = getConnectionError();
      if (!operatorError.value) await refresh();
      else loading.value = false;
    }

    async function silentRefresh() {
      if (loading.value) return;
      try {
        const list = await listBlueprints();
        blueprints.value = list.items || [];
        const updates: Record<string, string> = {};
        for (const [family, versions] of groupBlueprintsByFamily(blueprints.value).entries()) {
          const current = selectedVersions.value[family];
          const visible = visibleVersionsFor(versions);
          const stillVisible = current && visible.some(v => v.spec.version === current);
          if (!stillVisible) {
            const pick = visible[0] || latestVersion(versions);
            updates[family] = pick.spec.version;
          }
        }
        if (Object.keys(updates).length) {
          selectedVersions.value = { ...selectedVersions.value, ...updates };
        }
      } catch { /* ignore during background poll */ }
    }

    let pollTimer: ReturnType<typeof setInterval> | null = null;

    // ── Navigation ─────────────────────────────────────────────────────────────
    function navigateCreate() {
      $router.push({ name: `c-cluster-${ PRODUCT }-blueprint-create`, params: { cluster } });
    }

    function navigateEdit(family: string, versions: Blueprint[]) {
      const bp = selectedVersion(family, versions);
      $router.push({
        name:   `c-cluster-${ PRODUCT }-blueprint-create`,
        params: { cluster },
        query:  { editName: family, fromVersion: bp.spec.version },
      });
    }

    function navigateCopy(family: string, versions: Blueprint[]) {
      const bp = selectedVersion(family, versions);
      $router.push({
        name:   `c-cluster-${ PRODUCT }-blueprint-create`,
        params: { cluster },
        query:  { copyFrom: family, copyVersion: bp.spec.version },
      });
    }

    function navigateInstall(family: string, versions: Blueprint[]) {
      const bp = selectedVersion(family, versions);
      $router.push({
        name:   `c-cluster-${ PRODUCT }-blueprint-install`,
        params: { cluster },
        query:  { name: family, version: bp.spec.version },
      });
    }

    // ── Shared active-workloads lookup ──────────────────────────────────────────
    async function fetchActiveWorkloads(family: string, version: string) {
      try {
        const wls = await listAIWorkloads();
        return (wls.items || []).filter(wl => {
          const src = wl.spec.source.blueprint;
          return src?.name === family && src?.version === version;
        });
      } catch {
        return [];
      }
    }

    // ── Delete modal ───────────────────────────────────────────────────────────
    const deleteModal = reactive({
      show:            false,
      family:          '',
      displayName:     '',
      version:         '',
      crName:          '',
      activeWorkloads: [] as any[],
      deleting:        false,
    });

    async function confirmDelete(family: string, versions: Blueprint[]) {
      const bp = selectedVersion(family, versions);
      deleteModal.family      = family;
      deleteModal.displayName = bp.spec.displayName;
      deleteModal.version     = bp.spec.version;
      deleteModal.crName      = bp.metadata.name;
      deleteModal.activeWorkloads = await fetchActiveWorkloads(family, bp.spec.version);
      deleteModal.show = true;
    }

    async function executeDelete() {
      deleteModal.deleting = true;
      try {
        await deleteBlueprint(deleteModal.crName);
        deleteModal.show = false;
        await refresh();
      } catch (e: any) {
        error.value = e?.message || 'Failed to delete blueprint';
        deleteModal.show = false;
      } finally {
        deleteModal.deleting = false;
      }
    }

    // ── Deprecate modal ────────────────────────────────────────────────────────
    const deprecateModal = reactive({
      show:            false,
      family:          '',
      displayName:     '',
      version:         '',
      crName:          '',
      currentlyDeprecated: false,
      activeWorkloads: [] as any[],
      saving:          false,
    });

    async function confirmDeprecate(family: string, versions: Blueprint[]) {
      const bp = selectedVersion(family, versions);
      deprecateModal.family             = family;
      deprecateModal.displayName        = bp.spec.displayName;
      deprecateModal.version            = bp.spec.version;
      deprecateModal.crName             = bp.metadata.name;
      deprecateModal.currentlyDeprecated = isDeprecated(bp);
      deprecateModal.activeWorkloads    = deprecateModal.currentlyDeprecated
        ? []
        : await fetchActiveWorkloads(family, bp.spec.version);
      deprecateModal.show = true;
    }

    async function executeDeprecate() {
      deprecateModal.saving = true;
      try {
        await updateBlueprintDeprecated(deprecateModal.crName, !deprecateModal.currentlyDeprecated);
        deprecateModal.show = false;
        await refresh();
      } catch (e: any) {
        error.value = e?.message || 'Failed to update blueprint';
        deprecateModal.show = false;
      } finally {
        deprecateModal.saving = false;
      }
    }

    function isSelectedDeprecated(family: string, versions: Blueprint[]): boolean {
      return isDeprecated(selectedVersion(family, versions));
    }

    function latestFor(versions: Blueprint[]) {
      return latestVersion(versions);
    }

    function toTitleCase(str: string): string {
      return str.replace(/\b\w/g, c => c.toUpperCase());
    }

    function checkAdminRole() {
      try {
        // Use Rancher's canonical admin detection (RBAC capability check) instead of
        // matching user.id against a GlobalRoleBinding's userName. The latter breaks in
        // production where user.id is a principal ID (e.g. "u-xxxxx") rather than the
        // login username, so the global admin was only ever seen as a non-admin.
        isAdmin.value = isAdminUser(vm.$store.getters);
      } catch (e) {
        console.warn('[SUSE-AI] checkAdminRole failed — admin actions will be hidden:', e);
        isAdmin.value = false;
      }
    }

    // ── Three-dot tile menu ────────────────────────────────────────────────────
    function tileActions(family: string, versions: Blueprint[]): any[] {
      const actions: any[] = [
        { action: 'copy', label: 'Copy', enabled: true },
      ];
      if (isAdmin.value) {
        actions.push(
          { action: 'edit',      label: 'Edit',      enabled: true },
          { action: 'deprecate', label: isSelectedDeprecated(family, versions) ? 'Undeprecate' : 'Deprecate', enabled: true },
          { divider: true, label: '', enabled: true },
          { action: 'delete',    label: 'Delete',    enabled: true },
        );
      }
      return actions;
    }

    function onTileAction(payload: { action: string }, family: string, versions: Blueprint[]) {
      switch (payload.action) {
        case 'copy':      navigateCopy(family, versions);      break;
        case 'edit':      navigateEdit(family, versions);      break;
        case 'deprecate': confirmDeprecate(family, versions);  break;
        case 'delete':    confirmDelete(family, versions);     break;
      }
    }

    onMounted(() => {
      refresh();
      checkAdminRole();
      pollTimer = setInterval(silentRefresh, 10_000);
    });

    onUnmounted(() => {
      if (pollTimer) clearInterval(pollTimer);
    });

    return {
      loading, error, operatorError, retryConnection,
      search, sortBy, sortedFamilies, selectedVersions,
      showDeprecated, isAdmin,
      deleteModal, deprecateModal,
      latestFor, isDeprecated, isSelectedDeprecated, visibleVersionsFor, versionLabel, componentCount, descriptionFor,
      toTitleCase, tileActions, onTileAction,
      refresh, navigateCreate, navigateEdit, navigateCopy, navigateInstall,
      confirmDelete, executeDelete, confirmDeprecate, executeDeprecate,
    };
  },
});
</script>

<style lang="scss" scoped>
.fixed-header {
  margin-bottom: 30px;
  .actions-container {
    display: flex;
    align-items: center;
    gap: 12px;
    flex-wrap: wrap;
    .search-box .input-sm {
      width: 200px;
      height: 32px;
      padding: 0 12px;
      border: 1px solid var(--border);
      border-radius: var(--border-radius);
      background: var(--input-bg);
      color: var(--body-text);
      font-size: 14px;
    }
    .sort-select {
      height: 30px;
      padding: 0 6px 0 8px;
      border: 1px solid var(--border);
      border-radius: var(--border-radius);
      background: var(--input-bg);
      color: var(--body-text);
      font-size: 13px;
      width: auto;
    }
    .ml-auto { margin-left: auto; }
  }
}

.tiles-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(320px, 1fr));
  gap: 20px;
}
.app-tile {
  display: flex;
  flex-direction: column;
  border: 1px solid var(--border);
  border-radius: 14px;
  padding: 20px;
  gap: 12px;
  min-height: 200px;
  background: transparent;
  transition: border-color 0.2s ease, background 0.2s ease;
  &:hover { border-color: var(--primary); }
  .tile-header { display: flex; align-items: flex-start; }
  .tile-info { flex: 1; }
  .tile-title-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
  }
  .tile-title { margin: 0; font-size: 14px; font-weight: 600; }
  .tile-meta { font-size: 12px; color: var(--muted); margin-top: 4px; }
  .tile-content { flex: 1; }
  .tile-description {
    margin: 0;
    font-size: 14px;
    color: var(--body-text);
    overflow: hidden;
    display: -webkit-box;
    -webkit-line-clamp: 3;
    -webkit-box-orient: vertical;
  }
  .tile-footer {
    display: flex;
    align-items: center;
    gap: 8px;
    padding-top: 12px;
    border-top: 1px solid var(--border);
  }
}
.app-tile-filler { visibility: hidden; }
.version-select {
  font-size: 12px;
  height: 26px;
  padding: 0 4px 0 8px;
  border: 1px solid var(--border);
  border-radius: var(--border-radius);
  background: var(--input-bg);
  color: var(--body-text);
  width: auto;
  flex-shrink: 0;
  max-width: 120px;
}

.empty-state-content {
  display: flex; flex-direction: column; align-items: center;
  text-align: center; padding: 60px 20px;
  .icon-4x { font-size: 64px; opacity: 0.5; margin-bottom: 20px; }
  h3 { margin: 0 0 12px; font-size: 20px; }
  p { color: var(--muted); }
}
.modal-body {
  padding: 24px;
  h3 { margin: 0 0 16px; }
  .modal-buttons { display: flex; gap: 12px; justify-content: flex-end; margin-top: 20px; }
}
.mb-10 { margin-bottom: 10px; }
.mb-20 { margin-bottom: 20px; }
.ml-5  { margin-left: 5px; }
.mt-5  { margin-top: 5px; }

.btn {
  display: inline-flex; align-items: center; gap: 6px;
  padding: 0 14px; height: 32px; border-radius: 6px;
  font-weight: 500; font-size: 13px; cursor: pointer;
  border: 1px solid; transition: all 0.15s ease;
  &.btn-sm { height: 28px; padding: 0 12px; font-size: 12px; }
  &.role-primary { background: var(--primary); border-color: var(--primary); color: var(--primary-text); }
  &.role-secondary { background: var(--body-bg); border-color: var(--border); color: var(--body-text); }
  &.role-secondary.btn-warn { color: var(--warning); border-color: var(--warning); }
  &:disabled { opacity: 0.6; cursor: not-allowed; }
  .icon-spin { animation: spin 1s linear infinite; }
}

.modal-hint {
  font-size: 13px;
  color: var(--muted);
  margin-bottom: 12px;
}
@keyframes spin { from { transform: rotate(0deg); } to { transform: rotate(360deg); } }
</style>
