<template>
  <div v-if="show" class="install-progress-overlay">
    <div class="install-progress-modal">
      <div class="modal-header">
        <h2>{{ title }}</h2>
        <p class="modal-subtitle">{{ subtitle }}</p>
      </div>

      <!-- Overall Progress -->
      <div class="overall-progress">
        <div class="progress-stats">
          <span>{{ completedCount }} of {{ totalCount }} clusters</span>
          <span class="progress-percentage">{{ overallPercentage }}%</span>
        </div>
        <div class="progress-bar-track">
          <div
            class="progress-bar-fill"
            :class="overallProgressClass"
            :style="{ width: `${overallPercentage}%` }"
          ></div>
        </div>
      </div>

      <!-- Per-cluster Progress List -->
      <div class="clusters-progress-list">
        <div
          v-for="item in progress"
          :key="item.clusterId"
          class="cluster-progress-item"
          :class="`status-${item.status}`"
        >
          <div class="cluster-status-icon">
            <i v-if="item.status === 'pending'" class="icon icon-clock" />
            <i v-else-if="item.status === 'installing'" class="icon icon-spinner icon-spin" />
            <i v-else-if="item.status === 'success'" class="icon icon-checkmark" />
            <i v-else-if="item.status === 'failed'" class="icon icon-close" />
          </div>
          <div class="cluster-info">
            <div class="cluster-name">{{ item.clusterName }}</div>
            <div class="cluster-message">{{ item.message }}</div>
            <div v-if="item.error" class="cluster-error">{{ item.error }}</div>
          </div>
          <div class="cluster-progress-bar" v-if="item.status === 'installing'">
            <div class="mini-progress-track">
              <div
                class="mini-progress-fill"
                :style="{ width: `${item.progress}%` }"
              ></div>
            </div>
          </div>
        </div>
      </div>

      <!-- Action Buttons -->
      <div class="modal-actions">
        <!-- All succeeded -->
        <template v-if="allSucceeded">
          <button class="btn role-primary" @click="$emit('done')">
            Done
          </button>
        </template>

        <!-- All failed -->
        <template v-else-if="allFailed">
          <button class="btn role-secondary" @click="$emit('cancel')">
            Cancel
          </button>
          <button class="btn role-primary" @click="$emit('retry-all')">
            Retry All
          </button>
        </template>

        <!-- Partial failure -->
        <template v-else-if="hasFailures && hasSuccesses">
          <button class="btn role-secondary" @click="$emit('retry-failed')">
            Retry Failed
          </button>
          <button class="btn role-primary" @click="$emit('continue-anyway')">
            Continue Anyway
          </button>
        </template>

        <!-- Still in progress -->
        <template v-else-if="isInProgress">
          <button class="btn role-secondary" :disabled="true">
            Installing...
          </button>
        </template>
      </div>
    </div>
  </div>
</template>

<script lang="ts" setup>
import { computed } from 'vue';

export interface ClusterInstallProgress {
  clusterId: string;
  clusterName: string;
  status: 'pending' | 'installing' | 'success' | 'failed';
  progress: number;
  message: string;
  error?: string;
}

interface Props {
  show: boolean;
  progress: ClusterInstallProgress[];
  title?: string;
}

interface Emits {
  (e: 'done'): void;
  (e: 'cancel'): void;
  (e: 'retry-all'): void;
  (e: 'retry-failed'): void;
  (e: 'continue-anyway'): void;
}

const props = withDefaults(defineProps<Props>(), {
  title: 'Installing Application'
});

defineEmits<Emits>();

const totalCount = computed(() => props.progress.length);

const completedCount = computed(() =>
  props.progress.filter(p => p.status === 'success' || p.status === 'failed').length
);

const successCount = computed(() =>
  props.progress.filter(p => p.status === 'success').length
);

const failedCount = computed(() =>
  props.progress.filter(p => p.status === 'failed').length
);

const overallPercentage = computed(() => {
  if (totalCount.value === 0) return 0;
  return Math.round((completedCount.value / totalCount.value) * 100);
});

const overallProgressClass = computed(() => {
  if (failedCount.value > 0 && successCount.value === 0) return 'progress-error';
  if (failedCount.value > 0) return 'progress-warning';
  if (completedCount.value === totalCount.value) return 'progress-success';
  return 'progress-active';
});

const isInProgress = computed(() =>
  props.progress.some(p => p.status === 'pending' || p.status === 'installing')
);

const allSucceeded = computed(() =>
  props.progress.length > 0 && props.progress.every(p => p.status === 'success')
);

const allFailed = computed(() =>
  props.progress.length > 0 && props.progress.every(p => p.status === 'failed')
);

const hasFailures = computed(() => failedCount.value > 0);
const hasSuccesses = computed(() => successCount.value > 0);

const subtitle = computed(() => {
  if (isInProgress.value) {
    const installing = props.progress.find(p => p.status === 'installing');
    return installing ? `Installing on ${installing.clusterName}...` : 'Preparing installation...';
  }
  if (allSucceeded.value) {
    return `Successfully installed on ${successCount.value} cluster${successCount.value !== 1 ? 's' : ''}`;
  }
  if (allFailed.value) {
    return `Installation failed on all ${failedCount.value} cluster${failedCount.value !== 1 ? 's' : ''}`;
  }
  if (hasFailures.value) {
    return `${successCount.value} succeeded, ${failedCount.value} failed`;
  }
  return '';
});
</script>

<style scoped>
.install-progress-overlay {
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
  background: rgba(0, 0, 0, 0.5);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 10000;
}

.install-progress-modal {
  background: var(--body-bg, #ffffff);
  border-radius: 8px;
  box-shadow: 0 4px 24px rgba(0, 0, 0, 0.2);
  width: 100%;
  max-width: 560px;
  max-height: 80vh;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.modal-header {
  padding: 20px 24px 16px;
  border-bottom: 1px solid var(--border, #e2e8f0);
}

.modal-header h2 {
  margin: 0 0 4px 0;
  font-size: 18px;
  font-weight: 600;
  color: var(--body-text, #111827);
}

.modal-subtitle {
  margin: 0;
  font-size: 14px;
  color: var(--muted, #6b7280);
}

.overall-progress {
  padding: 16px 24px;
  border-bottom: 1px solid var(--border, #e2e8f0);
}

.progress-stats {
  display: flex;
  justify-content: space-between;
  margin-bottom: 8px;
  font-size: 13px;
  color: var(--body-text, #374151);
}

.progress-percentage {
  font-weight: 600;
}

.progress-bar-track {
  height: 8px;
  background: var(--progress-bg, #e5e7eb);
  border-radius: 4px;
  overflow: hidden;
}

.progress-bar-fill {
  height: 100%;
  border-radius: 4px;
  transition: width 0.3s ease;
}

.progress-bar-fill.progress-active {
  background: var(--primary, #2563eb);
}

.progress-bar-fill.progress-success {
  background: var(--success, #10b981);
}

.progress-bar-fill.progress-warning {
  background: var(--warning, #f59e0b);
}

.progress-bar-fill.progress-error {
  background: var(--error, #ef4444);
}

.clusters-progress-list {
  flex: 1;
  overflow-y: auto;
  padding: 12px 24px;
  max-height: 300px;
}

.cluster-progress-item {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  padding: 12px;
  border-radius: 6px;
  margin-bottom: 8px;
  background: var(--box-bg, #f9fafb);
  border: 1px solid var(--border, #e5e7eb);
}

.cluster-progress-item:last-child {
  margin-bottom: 0;
}

.cluster-progress-item.status-installing {
  border-color: var(--primary, #2563eb);
  background: var(--primary-banner-bg, rgba(37, 99, 235, 0.05));
}

.cluster-progress-item.status-success {
  border-color: var(--success, #10b981);
  background: var(--success-banner-bg, rgba(16, 185, 129, 0.05));
}

.cluster-progress-item.status-failed {
  border-color: var(--error, #ef4444);
  background: var(--error-banner-bg, rgba(239, 68, 68, 0.05));
}

.cluster-status-icon {
  width: 24px;
  height: 24px;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}

.cluster-status-icon .icon {
  font-size: 16px;
}

.status-pending .cluster-status-icon {
  color: var(--muted, #9ca3af);
}

.status-installing .cluster-status-icon {
  color: var(--primary, #2563eb);
}

.status-success .cluster-status-icon {
  color: var(--success, #10b981);
}

.status-failed .cluster-status-icon {
  color: var(--error, #ef4444);
}

.cluster-info {
  flex: 1;
  min-width: 0;
}

.cluster-name {
  font-weight: 600;
  font-size: 14px;
  color: var(--body-text, #111827);
  margin-bottom: 2px;
}

.cluster-message {
  font-size: 13px;
  color: var(--muted, #6b7280);
}

.cluster-error {
  font-size: 12px;
  color: var(--error, #dc2626);
  margin-top: 4px;
  padding: 6px 8px;
  background: var(--error-banner-bg, rgba(220, 38, 38, 0.1));
  border-radius: 4px;
  word-break: break-word;
}

.cluster-progress-bar {
  width: 60px;
  flex-shrink: 0;
  align-self: center;
}

.mini-progress-track {
  height: 4px;
  background: var(--progress-bg, #e5e7eb);
  border-radius: 2px;
  overflow: hidden;
}

.mini-progress-fill {
  height: 100%;
  background: var(--primary, #2563eb);
  border-radius: 2px;
  transition: width 0.3s ease;
}

.modal-actions {
  display: flex;
  justify-content: flex-end;
  gap: 12px;
  padding: 16px 24px;
  border-top: 1px solid var(--border, #e2e8f0);
  background: var(--body-bg, #ffffff);
}

.modal-actions .btn {
  height: 36px;
  padding: 0 16px;
  font-size: 14px;
  font-weight: 500;
  border-radius: 4px;
}

.modal-actions .btn.role-secondary {
  background: var(--body-bg, #ffffff);
  border: 1px solid var(--border, #d1d5db);
  color: var(--body-text, #111827);
}

.modal-actions .btn.role-secondary:hover:not(:disabled) {
  border-color: var(--border-hover, #9ca3af);
}

.modal-actions .btn.role-primary {
  background: var(--primary, #2563eb);
  border: 1px solid var(--primary, #2563eb);
  color: white;
}

.modal-actions .btn.role-primary:hover:not(:disabled) {
  background: var(--primary-hover, #1d4ed8);
  border-color: var(--primary-hover, #1d4ed8);
}

.modal-actions .btn:disabled {
  opacity: 0.6;
  cursor: not-allowed;
}

.icon-spin {
  animation: spin 1s linear infinite;
}

@keyframes spin {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}
</style>
