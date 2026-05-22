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

      <Banner v-if="error" color="error">{{ error }}</Banner>

      <div class="main-content">
        <div v-if="!loading && !filteredFamilies.length && !error" class="empty-state-content">
          <i class="icon icon-folder-open icon-4x text-muted" />
          <h3>No blueprints found</h3>
          <p class="text-muted">Click Create to define your first blueprint.</p>
        </div>

        <div class="tiles-grid" role="grid">
          <div
            v-for="[family, versions] in filteredFamilies"
            :key="family"
            class="app-tile"
          >
            <div class="tile-header">
              <div class="tile-info">
                <div class="tile-title-row">
                  <h3 class="tile-title">{{ latestFor(versions).spec.displayName }}</h3>
                  <select
                    v-model="selectedVersions[family]"
                    class="version-select form-control-sm"
                    @click.stop
                  >
                    <option
                      v-for="bp in versions"
                      :key="bp.spec.version"
                      :value="bp.spec.version"
                    >
                      v{{ bp.spec.version }}
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
              <button class="btn role-secondary btn-sm" @click="navigateEdit(family, versions)" type="button">
                Edit
              </button>
              <button class="btn role-secondary btn-sm" @click="confirmDelete(family, versions)" type="button">
                Delete
              </button>
            </div>
          </div>
          <div v-for="n in 5" :key="`filler-${ n }`" class="app-tile app-tile-filler" />
        </div>
      </div>

      <!-- Delete confirmation modal -->
      <div v-if="deleteModal.show" class="modal-overlay" @click.self="deleteModal.show = false">
        <div class="modal-content">
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
      </div>
    </div>
  </main>
</template>

<script lang="ts">
import { defineComponent, ref, computed, onMounted, getCurrentInstance, reactive } from 'vue';
import { Banner } from '@components/Banner';
import {
  listBlueprints, deleteBlueprint, groupBlueprintsByFamily, latestVersion,
} from '../utils/blueprint-api';
import { listAIWorkloads } from '../utils/operator-api';
import type { Blueprint } from '../types/blueprint-types';
import { PRODUCT } from '../config/suseai';

export default defineComponent({
  name: 'SuseAIBlueprints',
  components: { Banner },
  setup() {
    const vm        = getCurrentInstance()!.proxy as any;
    const $router   = vm.$router;
    const $route    = vm.$route;
    const cluster   = ($route?.params?.cluster as string) || '_';

    const loading    = ref(true);
    const error      = ref<string | null>(null);
    const search     = ref('');
    const blueprints = ref<Blueprint[]>([]);
    const selectedVersions = ref<Record<string, string>>({});

    const deleteModal = reactive({
      show:          false,
      family:        '',
      displayName:   '',
      version:       '',
      crName:        '',
      activeWorkloads: [] as any[],
      deleting:      false,
    });

    const families = computed(() => groupBlueprintsByFamily(blueprints.value));

    const filteredFamilies = computed(() => {
      const q = search.value.toLowerCase();
      return [...families.value.entries()].filter(([, versions]) => {
        if (!q) return true;
        const bp = versions[0];
        return (
          bp.spec.displayName.toLowerCase().includes(q) ||
          bp.spec.description?.toLowerCase().includes(q) ||
          bp.metadata.name.includes(q)
        );
      });
    });

    function latestFor(versions: Blueprint[]) {
      return latestVersion(versions);
    }

    function selectedVersion(family: string, versions: Blueprint[]): Blueprint {
      const v = selectedVersions.value[family];
      return versions.find(b => b.spec.version === v) || latestVersion(versions);
    }

    function componentCount(versions: Blueprint[], family: string): number {
      return selectedVersion(family, versions).spec.components.length;
    }

    function descriptionFor(versions: Blueprint[], family: string): string {
      return selectedVersion(family, versions).spec.description || '';
    }

    async function refresh() {
      loading.value = true;
      error.value = null;
      try {
        const list = await listBlueprints();
        blueprints.value = list.items || [];
        const updates: Record<string, string> = {};
        for (const [family, versions] of groupBlueprintsByFamily(blueprints.value).entries()) {
          const current = selectedVersions.value[family];
          const stillExists = current && versions.some(v => v.spec.version === current);
          if (!stillExists) {
            updates[family] = latestVersion(versions).spec.version;
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

    function navigateInstall(family: string, versions: Blueprint[]) {
      const bp = selectedVersion(family, versions);
      $router.push({
        name:   `c-cluster-${ PRODUCT }-blueprint-install`,
        params: { cluster },
        query:  { name: family, version: bp.spec.version },
      });
    }

    async function confirmDelete(family: string, versions: Blueprint[]) {
      const bp = selectedVersion(family, versions);
      deleteModal.family      = family;
      deleteModal.displayName = bp.spec.displayName;
      deleteModal.version     = bp.spec.version;
      deleteModal.crName      = bp.metadata.name;
      deleteModal.activeWorkloads = [];

      try {
        const wls = await listAIWorkloads();
        deleteModal.activeWorkloads = (wls.items || []).filter(wl => {
          const src = wl.spec.source.blueprint;
          return src?.name === family && src?.version === bp.spec.version;
        });
      } catch (e) {
        console.warn('[SUSE-AI] Could not verify active workloads:', e);
      }
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

    onMounted(refresh);

    return {
      loading, error, search, filteredFamilies, selectedVersions, deleteModal,
      latestFor, componentCount, descriptionFor,
      refresh, navigateCreate, navigateEdit, navigateInstall, confirmDelete, executeDelete,
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
.modal-overlay {
  position: fixed; inset: 0; background: rgba(0,0,0,0.5);
  display: flex; align-items: center; justify-content: center; z-index: 1000;
}
.modal-content {
  background: var(--body-bg); padding: 24px; border-radius: 8px;
  max-width: 480px; width: 100%;
  h3 { margin: 0 0 16px; }
  .modal-buttons { display: flex; gap: 12px; justify-content: flex-end; margin-top: 20px; }
}
.mb-10 { margin-bottom: 10px; }
.mt-5 { margin-top: 5px; }
.btn {
  display: inline-flex; align-items: center; gap: 6px;
  padding: 0 14px; height: 32px; border-radius: 6px;
  font-weight: 500; font-size: 13px; cursor: pointer;
  border: 1px solid; transition: all 0.15s ease;
  &.btn-sm { height: 28px; padding: 0 12px; font-size: 12px; }
  &.role-primary { background: var(--primary); border-color: var(--primary); color: var(--primary-text); }
  &.role-secondary { background: var(--body-bg); border-color: var(--border); color: var(--body-text); }
  &:disabled { opacity: 0.6; cursor: not-allowed; }
  .icon-spin { animation: spin 1s linear infinite; }
}
@keyframes spin { from { transform: rotate(0deg); } to { transform: rotate(360deg); } }
</style>
