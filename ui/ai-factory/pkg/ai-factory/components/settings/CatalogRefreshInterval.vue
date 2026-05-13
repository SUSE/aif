<template>
  <div class="catalog-refresh-interval mb-20">
    <h3>{{ t('aif.pages.settings.catalogRefresh.label') }}</h3>
    <p class="text-muted mb-10">
      {{ t('aif.pages.settings.catalogRefresh.description') }}
    </p>

    <UnitInput
      v-model="internalValue"
      :mode="mode"
      :label="t('aif.pages.settings.catalogRefresh.label')"
      :placeholder="t('aif.pages.settings.catalogRefresh.placeholder')"
      suffix="m"
      :input-exponent="0"
    />
  </div>
</template>

<script>
import { UnitInput } from '@shell/components/form/UnitInput';

export default {
  name: 'CatalogRefreshInterval',

  components: {
    UnitInput
  },

  props: {
    modelValue: {
      type: Number,
      default: 10
    },

    mode: {
      type: String,
      default: 'edit'
    }
  },

  computed: {
    internalValue: {
      get() {
        return this.modelValue || 10;
      },
      set(val) {
        this.$emit('update:modelValue', val);
      }
    }
  }
};
</script>

<style lang="scss" scoped>
.catalog-refresh-interval {
  border-bottom: 1px solid var(--border);
  padding-bottom: 20px;

  h3 {
    margin-bottom: 10px;
  }
}
</style>
