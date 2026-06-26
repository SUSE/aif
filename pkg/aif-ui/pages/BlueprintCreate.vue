<script lang="ts" setup>
import { ref, onMounted, getCurrentInstance } from 'vue';
import Loading from '@shell/components/Loading';
import BlueprintCreateWizard from './components/BlueprintCreateWizard.vue';
import { getBlueprint, blueprintCRName } from '../utils/blueprint-api';
import type { BlueprintSpec } from '../types/blueprint-types';
import logger from '../utils/logger';

// eslint-disable-next-line @typescript-eslint/no-explicit-any
const vm    = getCurrentInstance()?.proxy as any;
const route = vm?.$route;

const editName    = route.query.editName    as string | undefined;
const fromVersion = route.query.fromVersion as string | undefined;
const copyFrom    = route.query.copyFrom    as string | undefined;
const copyVersion = route.query.copyVersion as string | undefined;

const loading = ref(!!(editName || copyFrom));
const prefill = ref<BlueprintSpec | undefined>(undefined);

onMounted(async () => {
  if (editName && fromVersion) {
    try {
      const crName = blueprintCRName(editName, fromVersion);
      const bp     = await getBlueprint(crName);
      prefill.value = {
        ...bp.spec,
        version: suggestNextPatch(bp.spec.version),
      };
    } catch (e) {
      logger.warn('[SUSE-AI] Failed to load blueprint for edit', { data: e });
    } finally {
      loading.value = false;
    }
    return;
  }

  if (copyFrom && copyVersion) {
    try {
      const crName = blueprintCRName(copyFrom, copyVersion);
      const bp     = await getBlueprint(crName);
      prefill.value = {
        displayName: `Copy of ${ bp.spec.displayName }`,
        version:     '1.0.0',
        description: bp.spec.description,
        components:  bp.spec.components.map(c => ({
          ...c,
          values: c.values ? JSON.parse(JSON.stringify(c.values)) : undefined,
        })),
      };
    } catch (e) {
      logger.warn('[SUSE-AI] Failed to load blueprint for copy', { data: e });
    } finally {
      loading.value = false;
    }
    return;
  }

  loading.value = false;
});

function suggestNextPatch(version: string): string {
  const core  = version.split('-')[0].split('+')[0];
  const parts = core.split('.');
  const patch = parseInt(parts[2] || '0', 10);
  return `${ parts[0] || '0' }.${ parts[1] || '0' }.${ Number.isFinite(patch) ? patch + 1 : 1 }`;
}
</script>

<template>
  <div class="install-steps pt-20 outlet">
    <Loading v-if="loading" />
    <BlueprintCreateWizard
      v-else
      :edit-name="editName"
      :from-version="fromVersion"
      :prefill="prefill"
    />
  </div>
</template>
