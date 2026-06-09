<template>
  <div class="step-content">
    <h2 class="step-title">Blueprint Details</h2>

    <LabeledInput
      v-model:value="localForm.displayName"
      label="Name"
      placeholder="e.g. My AI Stack"
      :disabled="props.nameDisabled"
      required
      class="mb-20"
      @update:value="emitForm"
    />

    <LabeledInput
      v-model:value="localForm.version"
      label="Version"
      placeholder="e.g. 1.0.0"
      :status="versionError ? 'error' : undefined"
      :sub-label="versionError || 'Semantic version (major.minor.patch)'"
      required
      class="mb-20"
      @blur="validateVersion"
      @update:value="emitForm"
    />

    <LabeledInput
      v-model:value="localForm.description"
      label="Description"
      placeholder="Optional description"
      type="multiline"
      @update:value="emitForm"
    />
  </div>
</template>

<script lang="ts" setup>
import { ref, watch } from 'vue';
import { LabeledInput } from '@components/Form/LabeledInput';
import { SEMVER_PATTERN } from '../../../types/blueprint-types';

interface BasicInfo {
  displayName: string;
  version:     string;
  description: string;
}

interface Props { form: BasicInfo; nameDisabled?: boolean }
interface Emits { (e: 'update:form', form: BasicInfo): void }

const props = defineProps<Props>();
const emit  = defineEmits<Emits>();

const localForm    = ref<BasicInfo>({ ...props.form });
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
