<template>
  <div class="app-health-indicator">
    <div
      class="health-status"
      :class="`health-${healthStatus}`"
      :title="healthTooltip"
    >
      <i :class="healthIcon"></i>
      <span v-if="showText" class="health-text">{{ healthText }}</span>
    </div>
    <div v-if="details && showDetails" class="health-details">
      <div v-for="detail in details" :key="detail.name" class="health-detail">
        <span class="detail-name">{{ detail.name }}:</span>
        <span :class="`detail-status detail-${detail.status}`">
          {{ detail.value }}
        </span>
      </div>
    </div>
  </div>
</template>

<script lang="ts">
import { defineComponent, computed } from 'vue';
import type { PropType } from 'vue';

interface HealthDetail {
  name: string;
  value: string;
  status: 'healthy' | 'warning' | 'unhealthy' | 'unknown';
}

export default defineComponent({
  name: 'AppHealthIndicator',
  props: {
    health: {
      type: String as PropType<'healthy' | 'warning' | 'unhealthy' | 'unknown'>,
      default: 'unknown'
    },
    details: {
      type: Array as PropType<HealthDetail[]>,
      default: () => []
    },
    showText: {
      type: Boolean,
      default: true
    },
    showDetails: {
      type: Boolean,
      default: false
    },
    customText: {
      type: String,
      default: ''
    }
  },
  setup(props) {
    const healthStatus = computed(() => props.health);

    const healthIcon = computed(() => {
      switch (props.health) {
        case 'healthy':
          return 'icon icon-checkmark';
        case 'warning':
          return 'icon icon-warning';
        case 'unhealthy':
          return 'icon icon-error';
        default:
          return 'icon icon-info';
      }
    });

    const healthText = computed(() => {
      if (props.customText) {
        return props.customText;
      }
      switch (props.health) {
        case 'healthy':
          return 'Healthy';
        case 'warning':
          return 'Warning';
        case 'unhealthy':
          return 'Unhealthy';
        default:
          return 'Unknown';
      }
    });

    const healthTooltip = computed(() => {
      const baseText = healthText.value;
      if (props.details.length > 0) {
        const detailsText = props.details
          .map(d => `${d.name}: ${d.value}`)
          .join('\n');
        return `${baseText}\n\n${detailsText}`;
      }
      return baseText;
    });

    return {
      healthStatus,
      healthIcon,
      healthText,
      healthTooltip
    };
  }
});
</script>

<style scoped>
.app-health-indicator {
  display: inline-block;
}

.health-status {
  display: flex;
  align-items: center;
  gap: 0.25rem;
  padding: 0.25rem 0.5rem;
  border-radius: 4px;
  font-size: 0.875rem;
}

.health-status.health-healthy {
  color: var(--success);
  background-color: var(--success-bg, rgba(76, 175, 80, 0.1));
}

.health-status.health-warning {
  color: var(--warning);
  background-color: var(--warning-bg, rgba(255, 152, 0, 0.1));
}

.health-status.health-unhealthy {
  color: var(--error);
  background-color: var(--error-bg, rgba(244, 67, 54, 0.1));
}

.health-status.health-unknown {
  color: var(--text-muted);
  background-color: var(--muted-bg, rgba(158, 158, 158, 0.1));
}

.health-text {
  font-weight: 500;
}

.health-details {
  margin-top: 0.5rem;
  padding: 0.5rem;
  background-color: var(--body-bg);
  border: 1px solid var(--border);
  border-radius: 4px;
  font-size: 0.75rem;
}

.health-detail {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 0.125rem 0;
}

.detail-name {
  color: var(--text-muted);
}

.detail-status {
  font-weight: 500;
}

.detail-status.detail-healthy {
  color: var(--success);
}

.detail-status.detail-warning {
  color: var(--warning);
}

.detail-status.detail-unhealthy {
  color: var(--error);
}

.detail-status.detail-unknown {
  color: var(--text-muted);
}
</style>