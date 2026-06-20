<script lang="ts" setup>
import { computed } from 'vue';
import { useT } from '../../../composables/useT';
import { RcItemCard } from '@components/RcItemCard';
import ClusterResourceTable from '../ClusterResourceTable.vue';
import type { AIWorkloadDeployStrategy } from '../../../types/aiworkload-types';

interface Props {
  mode:            'install' | 'manage';
  clusters:        string[];
  deployType:      AIWorkloadDeployStrategy;
  appSlug:         string;
  appName:         string;
  helmOversized?:  boolean;
  helmUnsupported?: boolean;
}

interface Emits {
  (e: 'update:clusters',   clusters:   string[]): void;
  (e: 'update:deployType', deployType: AIWorkloadDeployStrategy): void;
}

const props = defineProps<Props>();
const emit = defineEmits<Emits>();

const t = useT();

const isManageMode = computed(() => props.mode === 'manage');
const hasNonLocalClusters = computed(() => props.clusters.some(c => c !== 'local'));

// The Helm card is blocked when the chart can't be installed via Helm: non-local
// clusters, an oversized chart archive, or a wizard that doesn't support Helm at all
// (e.g. blueprint installs).
const helmCardDisabled = computed(() =>
  hasNonLocalClusters.value || !!props.helmOversized || !!props.helmUnsupported
);

const deployTypeCards = [
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
  {
    id:      'Helm' as AIWorkloadDeployStrategy,
    header:  { title: { text: 'Helm' } },
    image:   { icon: 'helm' as any },
    content: { text: 'Deploy directly to each selected cluster via Helm install' },
  },
];

function onCardClick(id: AIWorkloadDeployStrategy) {
  if (isManageMode.value) return;
  if (id === 'Helm' && helmCardDisabled.value) return;
  emit('update:deployType', id);
}
</script>

<template>
  <div class="target-step">
    <label class="lbl">{{ t('suseai.wizard.form.deploymentType', 'Deployment Type') }}</label>
    <div class="deploy-type-grid">
      <RcItemCard
        v-for="card in deployTypeCards"
        :id="card.id"
        :key="card.id"
        :header="card.header"
        :image="card.image"
        :content="card.content"
        :selected="deployType === card.id"
        :clickable="!isManageMode && !(card.id === 'Helm' && helmCardDisabled)"
        variant="small"
        :class="{ 'card-disabled': isManageMode || (card.id === 'Helm' && helmCardDisabled) }"
        @card-click="onCardClick(card.id)"
      />
    </div>
    <p v-if="!isManageMode && helmUnsupported" class="hint">
      Helm is not available for this installation. Use Fleet Bundle or Fleet Git.
    </p>
    <p v-else-if="!isManageMode && hasNonLocalClusters" class="hint">
      Helm is only available for the local management cluster. Use Fleet Bundle or Fleet Git for multi-cluster deployments.
    </p>
    <p v-else-if="!isManageMode && helmOversized" class="hint">
      This chart is too large for the Helm deployment method (it exceeds Kubernetes' 1 MiB Secret limit). Use Fleet Bundle or Fleet Git.
    </p>

    <label class="lbl mt-16">{{ isManageMode ? t('suseai.wizard.labels.targetCluster', 'Target Cluster') : t('suseai.wizard.target.selectClusters', 'Select Target Cluster(s)') }}</label>
    <ClusterResourceTable
      :multi-select="!isManageMode"
      :selected-clusters="clusters"
      :app-slug="appSlug"
      :app-name="appName"
      :disabled="isManageMode"
      @update:selected-clusters="isManageMode ? undefined : $emit('update:clusters', $event)"
    />
    <p v-if="isManageMode" class="hint">
      {{ t('suseai.wizard.target.readOnly', 'Target cluster and deployment type are read-only in Manage mode.') }}
    </p>
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
