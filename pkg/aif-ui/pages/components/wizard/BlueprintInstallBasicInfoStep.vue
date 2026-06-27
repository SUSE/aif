<template>
  <div class="step-content">
    <div class="info-banner mb-20">
      Installing: <strong>{{ displayName }}</strong> v{{ version }} · {{ componentCount }} component{{ componentCount !== 1 ? 's' : '' }}
    </div>

    <div class="form-group">
      <label class="lbl required">{{ t('suseai.wizard.form.workloadName', 'Instance Name') }}</label>
      <input
        v-model="localName"
        type="text"
        class="form-control"
        :placeholder="t('suseai.wizard.form.workloadNamePlaceholder', 'e.g. my-ai-deployment')"
        @input="emit('update:workloadName', localName)"
      />
      <small class="text-muted">{{ t('suseai.wizard.form.workloadNameHelp', 'Used as prefix for Fleet Bundle names') }}</small>
    </div>

    <div class="form-group">
      <NamespaceAutocomplete
        :value="localNs"
        :label="t('suseai.wizard.form.namespace', 'Namespace')"
        :options="namespaceOptions"
        :required="true"
        :loading="loadingNamespaces"
        @update:value="onNamespaceChange"
      />
    </div>
  </div>
</template>

<script lang="ts" setup>
import { ref, onMounted, getCurrentInstance } from 'vue';
import { useT } from '../../../composables/useT';
import NamespaceAutocomplete from './NamespaceAutocomplete.vue';
import { fetchUserNamespaces } from '../../../services/rancher-apps';

interface Props {
  displayName:    string;
  version:        string;
  componentCount: number;
  workloadName:   string;
  namespace:      string;
}
interface Emits {
  (e: 'update:workloadName', v: string): void;
  (e: 'update:namespace',    v: string): void;
}

const props = defineProps<Props>();
const emit  = defineEmits<Emits>();
// eslint-disable-next-line @typescript-eslint/no-explicit-any
const vm    = getCurrentInstance()?.proxy as any;
const store = vm?.$store;

const t = useT();

const localName         = ref(props.workloadName);
const localNs           = ref(props.namespace);
const namespaceOptions  = ref<Array<{ label: string; value: string }>>([]);
const loadingNamespaces = ref(false);

onMounted(async () => {
  loadingNamespaces.value = true;
  try {
    namespaceOptions.value = await fetchUserNamespaces(store, `${props.workloadName}-system`);
  } finally {
    loadingNamespaces.value = false;
  }
  if (!localNs.value && namespaceOptions.value.length) {
    localNs.value = namespaceOptions.value[0].value;
    emit('update:namespace', localNs.value);
  }
});

function onNamespaceChange(v: string) {
  localNs.value = v;
  emit('update:namespace', v);
}
</script>

<style lang="scss" scoped>
.step-content { max-width: 600px; }
.info-banner {
  padding: 12px 16px; background: var(--accent-btn);
  border: 1px solid var(--border); border-radius: 6px; font-size: 14px;
}
.mb-20 { margin-bottom: 20px; }
.form-group { margin-bottom: 20px; }
.lbl {
  display: block; font-size: 13px; font-weight: 500; margin-bottom: 6px;
  &.required::after { content: ' *'; color: var(--error); }
}
.form-control {
  width: 100%; padding: 8px 12px;
  border: 1px solid var(--border); border-radius: var(--border-radius);
  background: var(--input-bg); color: var(--body-text); font-size: 14px;
}
.text-muted { color: var(--muted); font-size: 12px; }
</style>
