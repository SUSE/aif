<script lang="ts" setup>
import { Banner } from '@components/Banner';

// operatorError is the code from getConnectionError() (OPERATOR_UNREACHABLE);
// the text comes from l10n.
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
        <div>{{ t('suseai.operator.unreachable') }}</div>
        <div>{{ t('suseai.operator.verifyHint') }}</div>
      </div>
      <button
        class="btn-retry"
        type="button"
        @click="$emit('retry')"
      >
        {{ t('suseai.operator.retry') }}
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
