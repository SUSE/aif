<script lang="ts" setup>
import { computed } from 'vue';
import { RcItemCard } from '@components/RcItemCard';
import ClusterResourceTable from '../ClusterResourceTable.vue';
import ClusterSelect from '../ClusterSelect.vue';
import type { AIWorkloadDeployStrategy } from '../../../types/aiworkload-types';

interface Props {
  mode:       'install' | 'manage';
  clusters:   string[];
  deployType: AIWorkloadDeployStrategy;
  appSlug:    string;
  appName:    string;
}

interface Emits {
  (e: 'update:clusters',   clusters:   string[]): void;
  (e: 'update:deployType', deployType: AIWorkloadDeployStrategy): void;
}

const props = defineProps<Props>();
const emit = defineEmits<Emits>();

const isInstallMode = computed(() => props.mode === 'install');
const hasNonLocalClusters = computed(() => props.clusters.some(c => c !== 'local'));

const deployTypeCards = [
  {
    id:      'Helm' as AIWorkloadDeployStrategy,
    header:  { title: { text: 'Helm' } },
    image:   { icon: 'helm' as any },
    content: { text: 'Deploy directly to each selected cluster via Helm install' },
  },
  {
    id:      'FleetBundle' as AIWorkloadDeployStrategy,
    header:  { title: { text: 'Fleet Bundle' } },
    image:   { icon: 'fleet' as any },
    content: { text: 'Create a Fleet Bundle; Fleet deploys to selected clusters' },
  },
  {
    id:      'GitOps' as AIWorkloadDeployStrategy,
    header:  { title: { text: 'Publish to Fleet Git' } },
    image:   { icon: 'git' as any },
    content: { text: 'Commit Fleet Bundle YAML to the git repo configured in Settings' },
  },
];

function onCardClick(id: AIWorkloadDeployStrategy) {
  if (id === 'Helm' && hasNonLocalClusters.value) return;
  emit('update:deployType', id);
}
</script>

<template>
  <div class="target-step">
    <template v-if="isInstallMode">
      <label class="lbl">Deployment Type</label>
      <div class="deploy-type-grid">
        <RcItemCard
          v-for="card in deployTypeCards"
          :id="card.id"
          :key="card.id"
          :header="card.header"
          :image="card.image"
          :content="card.content"
          :selected="deployType === card.id"
          :clickable="!(card.id === 'Helm' && hasNonLocalClusters)"
          variant="small"
          :class="{ 'card-disabled': card.id === 'Helm' && hasNonLocalClusters }"
          @card-click="onCardClick(card.id)"
        />
      </div>
      <p v-if="hasNonLocalClusters" class="hint">
        Helm is only available for the local management cluster. Use Fleet Bundle or Fleet Git for multi-cluster deployments.
      </p>
      <label class="lbl mt-16">Select Target Cluster(s)</label>
      <ClusterResourceTable
        :multi-select="true"
        :selected-clusters="clusters"
        :app-slug="appSlug"
        :app-name="appName"
        :disabled="false"
        @update:selected-clusters="$emit('update:clusters', $event)"
      />
    </template>
    <template v-else>
      <label class="lbl">Target Cluster</label>
      <ClusterSelect
        :model-value="clusters[0] || ''"
        :disabled="true"
      />
      <p class="hint">
        Changes will be applied only to the cluster in the current context and cannot be changed in Manage mode.
      </p>
    </template>
  </div>
</template>

<style lang="scss" scoped>
.target-step {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.lbl {
  display: block;
  font-size: 12px;
  color: var(--body-text, #111827);
  margin-bottom: 6px;
}

.mt-16 {
  margin-top: 16px;
}

.hint {
  font-size: 12px;
  color: var(--muted, #64748b);
  margin-top: 8px;
}

.deploy-type-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
  gap: 12px;
}

.card-disabled {
  opacity: 0.45;
  cursor: not-allowed;
  pointer-events: none;
}
</style>
