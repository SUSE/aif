<template>
  <div class="step-content">
    <h2 class="step-title">Blueprint Details</h2>

    <div class="form-group">
      <label class="lbl required">Name</label>
      <input
        v-model="localForm.displayName"
        type="text"
        class="form-control"
        placeholder="e.g. My AI Stack"
        :disabled="props.nameDisabled"
        @input="emitForm"
      />
    </div>

    <div class="form-group">
      <label class="lbl required">Version</label>
      <input
        v-model="localForm.version"
        type="text"
        class="form-control"
        :class="{ 'error': versionError }"
        placeholder="e.g. 1.0.0"
        @blur="validateVersion"
        @input="emitForm"
      />
      <small v-if="versionError" class="text-error">{{ versionError }}</small>
      <small v-else class="text-muted">Semantic version (major.minor.patch)</small>
    </div>

    <div class="form-group">
      <label class="lbl">Description</label>
      <textarea
        v-model="localForm.description"
        class="form-control"
        rows="3"
        placeholder="Optional description"
        @input="emitForm"
      />
    </div>
  </div>
</template>

<script lang="ts" setup>
import { ref, watch } from 'vue';
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
.form-group { margin-bottom: 20px; }
.lbl {
  display: block; font-size: 13px; font-weight: 500;
  color: var(--body-text); margin-bottom: 6px;
  &.required::after { content: ' *'; color: var(--error); }
}
.form-control {
  width: 100%; padding: 8px 12px;
  border: 1px solid var(--border); border-radius: var(--border-radius);
  background: var(--input-bg); color: var(--body-text); font-size: 14px;
  &.error { border-color: var(--error); }
}
textarea.form-control { resize: vertical; }
.text-error { color: var(--error); font-size: 12px; }
.text-muted { color: var(--muted); font-size: 12px; }
</style>
