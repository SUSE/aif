<script lang="ts" setup>
import { computed } from 'vue';
import type { ClusterPodStatus } from '../types/workload-pods-types';

const props = defineProps<{ clusters: ClusterPodStatus[] }>();

// Only clusters that have something to say.
const withContent = computed(() =>
  props.clusters.filter(c => c.unavailable || c.pods.length > 0 || c.unattributed.length > 0),
);

// The row already has Cluster and State columns, so the cluster header and the
// per-pod name/phase are redundant in the common single-cluster/single-pod case.
// Show them only when there's more than one, to keep issues attributable.
const showClusterName = computed(() => withContent.value.length > 1);
</script>

<template>
  <div class="pod-status">
    <div v-for="c in withContent" :key="c.clusterId" class="pod-cluster">
      <div v-if="showClusterName" class="pod-cluster-name">cluster {{ c.clusterId || 'local' }}</div>

      <div v-if="c.unavailable" class="pod-note">(pod status unavailable)</div>

      <template v-else>
        <template v-for="p in c.pods" :key="p.name">
          <div v-if="c.pods.length > 1" class="pod-name">{{ p.name }}</div>
          <div v-for="(iss, idx) in p.issues" :key="idx" class="pod-issue">
            {{ iss.container }}<span v-if="iss.init"> (init)</span>: {{ iss.reason }}<span v-if="iss.message"> — {{ iss.message }}</span>
          </div>
          <div v-if="!p.issues.length" class="pod-issue">{{ p.name }}: {{ p.phase }}</div>
        </template>

        <template v-if="c.unattributed.length">
          <div class="pod-note">unlabeled Helm pods in namespace — attribution approximate</div>
          <template v-for="p in c.unattributed" :key="`u-${ p.name }`">
            <div v-if="c.unattributed.length > 1" class="pod-name">{{ p.name }}</div>
            <div v-for="(iss, idx) in p.issues" :key="idx" class="pod-issue">
              {{ iss.container }}<span v-if="iss.init"> (init)</span>: {{ iss.reason }}<span v-if="iss.message"> — {{ iss.message }}</span>
            </div>
            <div v-if="!p.issues.length" class="pod-issue">{{ p.name }}: {{ p.phase }}</div>
          </template>
        </template>
      </template>
    </div>
  </div>
</template>

<style scoped>
.pod-status      { margin-top: 4px; font-size: 12px; }
.pod-cluster     { margin-top: 4px; }
.pod-cluster-name{ font-weight: 600; opacity: 0.8; }
.pod-name        { font-weight: 600; padding-left: 8px; }
.pod-issue       { padding-left: 16px; opacity: 0.9; }
.pod-note        { padding-left: 8px; font-style: italic; opacity: 0.7; }
</style>
