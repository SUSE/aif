<script lang="ts" setup>
import { Banner } from '@components/Banner';

defineProps<{ operatorError: string }>();
defineEmits<{ retry: [] }>();
</script>

<template>
  <Banner
    color="error"
    class="mb-20"
  >
    <div class="operator-error-body">
      <div class="operator-error-text">
        <div>{{ operatorError }}</div>
        <div>
          Verify the operator deployment and the <code>aif-ui-config</code> ConfigMap, then retry.
        </div>
      </div>
      <button
        class="btn-retry"
        type="button"
        @click="$emit('retry')"
      >
        Retry Connection
      </button>
    </div>
  </Banner>
</template>

<style lang="scss" scoped>
.mb-20 { margin-bottom: 20px; }

.operator-error-body {
  display: flex;
  align-items: center;
  gap: 16px;
  width: 100%;
}

.operator-error-text {
  display: flex;
  flex-direction: column;
  gap: 4px;
  flex: 1;
}

.btn-retry {
  flex-shrink: 0;
  background: none;
  border: 1px solid currentColor;
  border-radius: 4px;
  color: inherit;
  cursor: pointer;
  font-size: 14px;
  padding: 6px 16px;
  white-space: nowrap;

  &:hover { opacity: 1; }
}
</style>
