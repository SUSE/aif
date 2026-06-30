<template>
  <div class="installation-progress">
    <div v-if="progress !== undefined" class="progress-container">
      <div class="progress-bar">
        <div
          class="progress-fill"
          :style="{ width: `${progress}%` }"
          :class="{
            'progress-success': status === 'success',
            'progress-error': status === 'error',
            'progress-warning': status === 'warning'
          }"
        ></div>
      </div>
      <span class="progress-text">{{ progressText }}</span>
    </div>
    <div v-else class="progress-indeterminate">
      <i class="icon icon-spinner icon-spin"></i>
      <span>{{ statusText || 'Installing...' }}</span>
    </div>
  </div>
</template>

<script lang="ts">
import { defineComponent, computed } from 'vue';
import type { PropType } from 'vue';

export default defineComponent({
  name: 'InstallationProgress',
  props: {
    progress: {
      type: Number as PropType<number | undefined>,
      default: undefined
    },
    status: {
      type: String as PropType<'success' | 'error' | 'warning' | 'info'>,
      default: 'info'
    },
    statusText: {
      type: String,
      default: ''
    },
    showPercentage: {
      type: Boolean,
      default: true
    }
  },
  setup(props) {
    const progressText = computed(() => {
      if (props.progress !== undefined && props.showPercentage) {
        return `${props.progress}%`;
      }
      return props.statusText || '';
    });

    return {
      progressText
    };
  }
});
</script>

<style scoped>
.installation-progress {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  min-width: 120px;
}

.progress-container {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  width: 100%;
}

.progress-bar {
  flex: 1;
  height: 8px;
  background-color: var(--border);
  border-radius: 4px;
  overflow: hidden;
}

.progress-fill {
  height: 100%;
  background-color: var(--primary);
  transition: width 0.3s ease;
  border-radius: 4px;
}

.progress-fill.progress-success {
  background-color: var(--success);
}

.progress-fill.progress-error {
  background-color: var(--error);
}

.progress-fill.progress-warning {
  background-color: var(--warning);
}

.progress-text {
  font-size: 0.875rem;
  color: var(--text-muted);
  white-space: nowrap;
}

.progress-indeterminate {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}

.progress-indeterminate .icon {
  font-size: 1rem;
}
</style>