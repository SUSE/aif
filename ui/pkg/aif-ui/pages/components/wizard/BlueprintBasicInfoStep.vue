<template>
  <div class="step-content">
    <h2 class="step-title">{{ t('suseai.wizard.sections.blueprintDetails', 'Blueprint Details') }}</h2>

    <LabeledInput
      v-model:value="localForm.displayName"
      :label="t('suseai.wizard.form.blueprintName', 'Name')"
      :placeholder="t('suseai.wizard.form.blueprintNamePlaceholder', 'e.g. My AI Stack')"
      :disabled="props.nameDisabled"
      required
      class="mb-20"
      @update:value="emitForm"
    />

    <LabeledInput
      v-model:value="localForm.version"
      :label="t('suseai.wizard.form.version', 'Version')"
      :placeholder="t('suseai.wizard.form.versionPlaceholder', 'e.g. 1.0.0')"
      :status="versionError ? 'error' : undefined"
      :sub-label="versionError || 'Semantic version (major.minor.patch)'"
      required
      class="mb-20"
      @blur="validateVersion"
      @update:value="emitForm"
    />

    <LabeledInput
      v-model:value="localForm.description"
      :label="t('suseai.wizard.form.description', 'Description')"
      :placeholder="t('suseai.wizard.form.descriptionPlaceholder', 'Optional description')"
      type="multiline"
      class="mb-20"
      @update:value="emitForm"
    />

    <Checkbox
      v-model:value="localForm.allowRollback"
      :label="t('suseai.wizard.form.allowRollback', 'Allow rollback')"
      :description="t('suseai.wizard.form.allowRollbackHelp', 'Turn off when upgrades migrate data that older versions can\'t read (for example database schema changes). Workloads of this Blueprint then can\'t be rolled back.')"
      @update:value="emitForm"
    />
  </div>
</template>

<script lang="ts" setup>
import { ref, watch } from 'vue';
import { LabeledInput } from '@components/Form/LabeledInput';
import { Checkbox } from '@components/Form/Checkbox';
import { useT } from '../../../composables/useT';
import { SEMVER_PATTERN } from '../../../types/blueprint-types';
import type { BlueprintBasicInfo } from '../../../types/blueprint-types';

interface Props { form: BlueprintBasicInfo; nameDisabled?: boolean }
interface Emits { (e: 'update:form', form: BlueprintBasicInfo): void }

const props = defineProps<Props>();
const emit  = defineEmits<Emits>();

const t = useT();

const localForm    = ref<BlueprintBasicInfo>({ ...props.form });
const versionError = ref('');

watch(() => props.form, (v) => { localForm.value = { ...v }; }, { deep: true });

function emitForm() {
  emit('update:form', { ...localForm.value });
}

function validateVersion() {
  if (!localForm.value.version) {
    versionError.value = 'Version is required';
  } else if (!SEMVER_PATTERN.test(localForm.value.version)) {
    versionError.value = 'Must be a valid semantic version (e.g. 1.0.0)';
  } else {
    versionError.value = '';
  }
}
</script>

<style lang="scss" scoped>
.step-content { max-width: 600px; }
.step-title { margin: 0 0 24px; font-size: 18px; font-weight: 600; }
.mb-20 { margin-bottom: 20px; }
</style>
