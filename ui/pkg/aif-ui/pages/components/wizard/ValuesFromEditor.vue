<template>
  <div>
    <h3>{{ t('suseai.wizard.labels.valuesFrom', 'Values from Secrets and ConfigMaps') }}</h3>
    <p class="text-muted mb-10">
      <template v-if="isView">
        {{ t('suseai.wizard.labels.valuesFromHelpView', "Set by the Blueprint. Read on the target cluster at deploy time and merged over this application's values (they win on conflicts).") }}
      </template>
      <template v-else>
        {{ t('suseai.wizard.labels.valuesFromHelp', "Read on the target cluster at deploy time and merged over this application's values (they win on conflicts). Leave Namespace empty to use the release namespace; Key defaults to values.yaml.") }}
      </template>
    </p>
    <ArrayList
      :value="rows"
      :default-add-value="emptyValuesFromRow()"
      :add-label="t('suseai.wizard.labels.valuesFromAdd', 'Add source')"
      :add-disabled="rows.length >= VALUES_FROM_MAX"
      :protip="false"
      :component-testid="`values-from-${name}`"
      class="mb-30"
      :mode="mode"
      @update:value="onRowsUpdate"
    >
      <template #columns="{ row, i }">
        <div class="row">
          <div class="col span-3">
            <LabeledSelect
              v-model:value="row.value.kind"
              :options="kindOptions"
              :label="t('suseai.wizard.labels.valuesFromKind', 'Kind')"
              :mode="mode"
              @update:value="emitValue"
            />
          </div>
          <div class="col span-3">
            <LabeledInput
              v-model:value="row.value.name"
              :label="t('suseai.wizard.labels.valuesFromName', 'Name')"
              :data-testid="`values-from-${name}-name-${i}`"
              :mode="mode"
              :required="!isView"
              @update:value="emitValue"
            />
          </div>
          <div class="col span-3">
            <LabeledInput
              v-model:value="row.value.namespace"
              :label="t('suseai.wizard.labels.valuesFromNamespace', 'Namespace')"
              :placeholder="t('suseai.wizard.labels.valuesFromNamespacePlaceholder', 'Release namespace')"
              :data-testid="`values-from-${name}-namespace-${i}`"
              :mode="mode"
              @update:value="emitValue"
            />
          </div>
          <div class="col span-3">
            <LabeledInput
              v-model:value="row.value.key"
              :label="t('suseai.wizard.labels.valuesFromKey', 'Key')"
              :placeholder="t('suseai.wizard.labels.valuesFromKeyPlaceholder', 'values.yaml')"
              :data-testid="`values-from-${name}-key-${i}`"
              :mode="mode"
              @update:value="emitValue"
            />
          </div>
        </div>
      </template>
    </ArrayList>
  </div>
</template>

<script lang="ts" setup>
import { ref, computed } from 'vue';
import { _EDIT, _VIEW } from '@shell/config/query-params';
import { LabeledInput } from '@components/Form/LabeledInput';
import LabeledSelect from '@shell/components/form/LabeledSelect';
import ArrayList from '@shell/components/form/ArrayList';
import { useT } from '../../../composables/useT';
import {
  emptyValuesFromRow, valuesFromToRows, rowsToValuesFrom, VALUES_FROM_MAX,
} from '../../../utils/values-from';
import type { ValuesFromRow } from '../../../utils/values-from';
import type { ComponentValuesFrom } from '../../../types/blueprint-types';

interface Props {
  value?: ComponentValuesFrom[];
  // Component name; scopes the test ids.
  name:   string;
  mode?:  typeof _EDIT | typeof _VIEW;
}
interface Emits { (e: 'update:value', v: ComponentValuesFrom[] | undefined): void }

const props = withDefaults(defineProps<Props>(), { value: undefined, mode: _EDIT });
const emit  = defineEmits<Emits>();

const t = useT();

const isView = computed(() => props.mode === _VIEW);

// Seeded once: ArrayList edits these rows in place, including blank ones that
// are not part of the emitted value yet.
const rows = ref<ValuesFromRow[]>(valuesFromToRows(props.value));

const kindOptions = [
  { label: t('suseai.wizard.labels.valuesFromSecret', 'Secret'), value: 'Secret' },
  { label: t('suseai.wizard.labels.valuesFromConfigMap', 'ConfigMap'), value: 'ConfigMap' },
];

function onRowsUpdate(next: ValuesFromRow[]) {
  rows.value = next;
  emitValue();
}

// Field edits emit right away rather than after ArrayList's debounced update,
// so an edit made just before the editor unmounts is kept.
function emitValue() {
  emit('update:value', rowsToValuesFrom(rows.value));
}
</script>

<style lang="scss" scoped>
.text-muted { color: var(--muted); font-size: 14px; }
.mb-10 { margin-bottom: 10px; }
.mb-30 { margin-bottom: 30px; }
</style>
