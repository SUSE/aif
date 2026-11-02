<template>
  <div class="kustomize-overlay-editor">
    <template v-if="mode === 'view'">
      <YamlEditor
        v-if="source === 'inline'"
        :value="body"
        :editor-mode="EDITOR_MODES.VIEW_CODE"
        class="kustomize-editor"
      />
      <div
        v-else-if="source === 'git'"
        class="row"
      >
        <div class="col span-6">
          <LabeledInput
            :value="chartRepo"
            :label="t('suseai.wizard.labels.kustomizeChartRepo', 'Git ClusterRepo')"
            mode="view"
          />
        </div>
        <div class="col span-6">
          <LabeledInput
            :value="path"
            :label="t('suseai.wizard.labels.kustomizePath', 'Path in repo')"
            mode="view"
          />
        </div>
      </div>
    </template>

    <template v-else>
      <RadioGroup
        :name="`kustomize-source-${name}`"
        :options="sourceOptions"
        :value="source"
        :row="true"
        class="mb-20"
        @update:value="onSourceUpdate"
      />

      <template v-if="source === 'inline'">
        <Banner color="info">
          {{ t('suseai.wizard.labels.kustomizeInlineHint', "Patches the chart's rendered manifests after Helm renders them. Write a single kustomization.yaml with inline patches. For overlays made of several files, use the Git source. An overlay embeds the chart in the Fleet bundle, so very large charts can't take one.") }}
        </Banner>
        <YamlEditor
          :value="body"
          class="kustomize-editor"
          @update:value="onBodyUpdate"
        />
      </template>

      <template v-else-if="source === 'git'">
        <Banner color="info">
          {{ t('suseai.wizard.labels.kustomizeGitHint', "Fetches a directory containing a kustomization.yaml and its patch files from a Git-type repository added in AI Factory Settings. The repository must use an HTTPS URL (SSH repositories are not supported and are not listed) and doesn't need to contain any Helm charts. An overlay embeds the chart in the Fleet bundle, so very large charts can't take one.") }}
        </Banner>
        <Banner
          v-if="gitRepoOptions && !gitRepoOptions.length"
          color="warning"
        >
          {{ t('suseai.wizard.labels.kustomizeNoGitRepos', "No Git-type repositories with an HTTPS URL found. Add a repository of type Git with an HTTPS URL in AI Factory Settings to use it as an overlay source; SSH repositories are not supported. It doesn't need to contain any Helm charts.") }}
        </Banner>
        <div class="row">
          <div class="col span-6">
            <LabeledSelect
              :value="chartRepo"
              :options="gitRepoOptions || []"
              :loading="!gitRepoOptions"
              :label="t('suseai.wizard.labels.kustomizeChartRepo', 'Git ClusterRepo')"
              :mode="'edit'"
              :taggable="true"
              @update:value="onChartRepoUpdate"
            />
          </div>
          <div class="col span-6">
            <LabeledInput
              :value="path"
              :label="t('suseai.wizard.labels.kustomizePath', 'Path in repo')"
              placeholder="overlays/prod"
              @update:value="onPathUpdate"
            />
          </div>
        </div>
      </template>
    </template>
  </div>
</template>

<script lang="ts" setup>
import { ref } from 'vue';
import { useT } from '../../../composables/useT';
import { Banner } from '@components/Banner';
import LabeledSelect from '@shell/components/form/LabeledSelect';
import { LabeledInput } from '@components/Form/LabeledInput';
import { RadioGroup } from '@components/Form/Radio';
import YamlEditor, { EDITOR_MODES } from '@shell/components/YamlEditor';
import { buildInlineKustomize, buildGitRefKustomize, initialInlineBody } from './kustomize-authoring';
import type { ComponentKustomize } from '../../../types/blueprint-types';

interface Props {
  value:          ComponentKustomize | undefined;
  // Unique per editor on the page: names the source radio group.
  name:           string;
  // Custom git ClusterRepo names; null while they are still loading.
  gitRepoOptions: string[] | null;
  mode?:          'edit' | 'view';
}
interface Emits { (e: 'update:value', v: ComponentKustomize | undefined): void }

const props = withDefaults(defineProps<Props>(), { mode: 'edit' });
const emit  = defineEmits<Emits>();
const t     = useT();

// Authoring state is seeded from value once. The overlay dir is not
// user-facing (it only names the folder inside the Fleet bundle); an existing
// value is kept, otherwise the builders default it to "overlay".
type KustomizeSource = 'none' | 'inline' | 'git';
const source    = ref<KustomizeSource>(props.value?.gitRef ? 'git' : props.value ? 'inline' : 'none');
const dir       = props.value?.dir || '';
const body      = ref(initialInlineBody(props.value));
const chartRepo = ref(props.value?.gitRef?.chartRepo || '');
const path      = ref(props.value?.gitRef?.path || '');

const sourceOptions = [
  { value: 'none', label: t('suseai.wizard.labels.kustomizeNone', 'None') },
  { value: 'inline', label: t('suseai.wizard.labels.kustomizeInline', 'Inline') },
  { value: 'git', label: t('suseai.wizard.labels.kustomizeGit', 'Git') },
];

function emitValue() {
  emit('update:value', source.value === 'inline' ? buildInlineKustomize(dir, body.value) : source.value === 'git' ? buildGitRefKustomize(dir, chartRepo.value, path.value) : undefined);
}
function onSourceUpdate(v: KustomizeSource) {
  source.value = v;
  emitValue();
}
function onBodyUpdate(v: string) {
  body.value = v;
  emitValue();
}
function onChartRepoUpdate(v: string) {
  chartRepo.value = v;
  emitValue();
}
function onPathUpdate(v: string) {
  path.value = v;
  emitValue();
}
</script>

<style lang="scss" scoped>
.mb-20 { margin-bottom: 20px; }
// CodeMirror 6 sizes itself from its content, so the min-height has to sit on
// the content/gutter, not the wrapper (which left a one-line editor in a tall box).
.kustomize-editor :deep(.cm-content),
.kustomize-editor :deep(.cm-gutter) { min-height: 320px; }
</style>
