<script lang="ts" setup>
import { getCurrentInstance } from 'vue';
import { Banner } from '@components/Banner';
import { PRODUCT, PAGE_TYPES } from '../config/suseai';

defineProps<{ operatorError: string }>();
defineEmits<{ retry: [] }>();

// eslint-disable-next-line @typescript-eslint/no-explicit-any
const vm      = getCurrentInstance()?.proxy as any; // Vue component proxy has $route but no type declaration in this context
const cluster = (vm?.$route?.params?.cluster as string) || '_';

const settingsRoute = {
  name:   `c-cluster-${ PRODUCT }-${ PAGE_TYPES.SETTINGS }`,
  params: { cluster },
  query:  { section: 'advanced' },
};
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
          Update <strong>Operator Namespace</strong> under
          <em>Settings → Advanced → Operator Connection</em>.
          <RouterLink :to="settingsRoute">
            Go to Settings →
          </RouterLink>
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

  a { color: inherit; font-weight: 600; text-decoration: underline; }
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
