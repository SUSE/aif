<template>
  <span data-testid="review-rollback">
    {{ rollback === 'Disabled' ? t('suseai.wizard.form.rollbackDisabled', 'Disabled') : t('suseai.wizard.form.rollbackAllowed', 'Allowed') }}
  </span>
</template>

<script lang="ts" setup>
import { useT } from '../../../composables/useT';
import type { BlueprintSpec } from '../../../types/blueprint-types';

interface Props { rollback?: BlueprintSpec['rollback'] }
withDefaults(defineProps<Props>(), { rollback: undefined });

const t = useT();
</script>
