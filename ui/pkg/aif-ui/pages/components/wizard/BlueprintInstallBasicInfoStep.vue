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
        :label="t('suseai.wizard.form.installNamespace', 'Default Namespace')"
        :options="namespaceOptions"
        :required="true"
        :loading="loadingNamespaces"
        @update:value="onNamespaceChange"
      />
      <small class="text-muted">{{ t('suseai.wizard.form.installNamespaceHelp', 'Default namespace for components that don\'t pin their own. Individual components may deploy to fixed namespaces — see the Review step for the full list.') }}</small>
      <Banner
        v-if="fixedNamespaceCount > 0"
        color="info"
        class="mt-10"
        :label="fixedNamespaceWarning"
      />
    </div>
  </div>
</template>

<script lang="ts" setup>
import { ref, computed, onMounted, getCurrentInstance } from 'vue';
import { Banner } from '@components/Banner';
import { useT } from '../../../composables/useT';
import NamespaceAutocomplete from './NamespaceAutocomplete.vue';
import { fetchUserNamespaces } from '../../../services/rancher-apps';
import type { BlueprintComponent } from '../../../types/blueprint-types';

interface Props {
  displayName:    string;
  version:        string;
  componentCount: number;
  workloadName:   string;
  namespace:      string;
  components:     BlueprintComponent[];
}
interface Emits {
  (e: 'update:workloadName', v: string): void;
  (e: 'update:namespace',    v: string): void;
}

const props = defineProps<Props>();
const emit  = defineEmits<Emits>();
const vm    = getCurrentInstance()!.proxy as any;
const store = vm.$store;

const t = useT();

const localName         = ref(props.workloadName);
const localNs           = ref(props.namespace);
const namespaceOptions  = ref<Array<{ label: string; value: string }>>([]);
const loadingNamespaces = ref(false);

// Components with their own BlueprintComponent.targetNamespace ignore the
// wizard namespace entirely — surface that here so the user isn't surprised
// when resources land elsewhere (the Review step lists the exact targets).
const fixedNamespaceCount = computed(
  () => (props.components || []).filter((c) => !!c.targetNamespace).length,
);
const fixedNamespaceWarning = computed(
  () => store?.getters['i18n/t']?.(
    'suseai.wizard.form.installNamespaceFixedWarning',
    { count: fixedNamespaceCount.value },
  ) || (fixedNamespaceCount.value === 1
    ? '1 component deploys to its own fixed namespace and will ignore this value.'
    : `${ fixedNamespaceCount.value } components deploy to their own fixed namespaces and will ignore this value. It applies only to the remaining components.`),
);

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
.mt-10 { margin-top: 10px; }
</style>
