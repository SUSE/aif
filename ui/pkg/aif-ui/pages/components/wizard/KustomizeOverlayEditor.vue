<template>
  <div class="kustomize-overlay-editor">
    <template v-if="mode === 'view'">
      <Tabbed
        v-if="source === 'inline'"
        :key="resetCount"
        :use-hash="false"
        :show-extension-tabs="false"
        :hide-single-tab="true"
      >
        <Tab
          v-for="f in files"
          :key="f.name"
          :name="f.name"
          :label="f.name"
          :weight="isRoot(f.name) ? 1 : 0"
        >
          <YamlEditor
            :value="f.content"
            :editor-mode="EDITOR_MODES.VIEW_CODE"
            class="kustomize-editor"
          />
        </Tab>
      </Tabbed>
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
          {{ t('suseai.wizard.labels.kustomizeInlineHint', "Patches the chart's rendered manifests after Helm renders them. Write patches in kustomization.yaml. To add new resources or patch files, use Add file and list them under resources or patches in kustomization.yaml. An overlay embeds the chart in the Fleet bundle, so very large charts can't take one.") }}
        </Banner>
        <Banner
          v-if="unreferenced.length"
          color="warning"
        >
          {{ t('suseai.wizard.labels.kustomizeUnreferencedFiles', "Kustomize ignores files the kustomization file doesn't reference: {names}. List new objects under resources and patch files under patches.", { names: unreferenced.join(', ') }) }}
        </Banner>
        <Banner
          v-if="emptyReferenced.length"
          color="warning"
        >
          {{ t('suseai.wizard.labels.kustomizeEmptyReferencedFiles', 'These files are listed in the kustomization file but are empty or only comments, so they add nothing: {names}. Uncomment their content.', { names: emptyReferenced.join(', ') }) }}
        </Banner>
        <Banner
          v-if="outsideNames.length"
          color="warning"
        >
          {{ t('suseai.wizard.labels.kustomizeOutsideDirFiles', 'These files are outside the overlay directory "{dir}": {names}. They are kept unchanged, but the operator rejects the overlay until they are moved under it or removed.', { dir, names: outsideNames.join(', ') }) }}
        </Banner>
        <div
          v-if="adding"
          class="row add-file mb-10"
        >
          <div class="col span-6">
            <LabeledInput
              :value="newName"
              :label="t('suseai.wizard.labels.kustomizeFileName', 'File name')"
              placeholder="configmap.yaml"
              :sub-label="newNameError"
              :status="newNameError ? 'error' : undefined"
              @update:value="newName = $event"
              @keydown.enter.prevent="confirmAdd"
            />
          </div>
          <div class="col span-6 add-file-actions">
            <button
              type="button"
              class="btn role-primary"
              :disabled="!!validateInlineFileName(newName, files)"
              @click="confirmAdd"
            >
              {{ t('suseai.wizard.labels.kustomizeAddFileConfirm', 'Add') }}
            </button>
            <button
              type="button"
              class="btn role-secondary"
              @click="adding = false"
            >
              {{ t('suseai.wizard.labels.kustomizeAddFileCancel', 'Cancel') }}
            </button>
          </div>
        </div>
        <Tabbed
          ref="tabbed"
          :key="resetCount"
          :use-hash="false"
          :show-extension-tabs="false"
          :default-tab="activeName"
          @changed="onTabChanged($event.selectedName)"
        >
          <template #tab-row-extras>
            <div class="file-actions">
              <button
                type="button"
                class="btn btn-sm role-tertiary"
                :disabled="adding"
                @click="startAdd"
              >
                <i class="icon icon-plus" />
                {{ t('suseai.wizard.labels.kustomizeAddFile', 'Add file') }}
              </button>
              <button
                v-if="!isRoot(activeName)"
                type="button"
                class="btn btn-sm role-tertiary"
                @click="removeFile(activeName)"
              >
                <i class="icon icon-trash" />
                {{ t('suseai.wizard.labels.kustomizeRemoveFile', 'Remove {name}', { name: activeName }) }}
              </button>
            </div>
          </template>
          <Tab
            v-for="f in files"
            :key="f.name"
            :name="f.name"
            :label="f.name"
            :weight="isRoot(f.name) ? 1 : 0"
            :display-alert-icon="unreferenced.includes(f.name)"
            :error-icon-tooltip="t('suseai.wizard.labels.kustomizeUnreferencedFile', 'Not referenced from the kustomization file')"
          >
            <Banner
              v-if="isRoot(f.name) && looksLikeCrWrapper(f.content)"
              color="warning"
            >
              {{ t('suseai.wizard.labels.kustomizeWrapperWarning', 'This editor holds the contents of kustomization.yaml itself, not the kustomize/dir/files block of the resource. Put each file in its own tab with Add file instead.') }}
            </Banner>
            <YamlEditor
              :value="f.content"
              class="kustomize-editor"
              @update:value="onContentUpdate(f, $event)"
            />
          </Tab>
        </Tabbed>
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
import {
  computed, nextTick, ref, watch,
} from 'vue';
import { isEqual } from 'lodash';
import { useT } from '../../../composables/useT';
import { useKustomizeDrafts } from '../../../composables/useKustomizeDrafts';
import { Banner } from '@components/Banner';
import LabeledSelect from '@shell/components/form/LabeledSelect';
import { LabeledInput } from '@components/Form/LabeledInput';
import { RadioGroup } from '@components/Form/Radio';
import Tabbed from '@shell/components/Tabbed';
import Tab from '@shell/components/Tabbed/Tab';
import YamlEditor, { EDITOR_MODES } from '@shell/components/YamlEditor';
import {
  buildInlineKustomize, buildGitRefKustomize, initialInlineFiles, outsideDirFiles, unreferencedFiles, emptyReferencedFiles,
  validateInlineFileName, looksLikeCrWrapper, KUSTOMIZATION_FILE_NAMES,
} from './kustomize-authoring';
import type { InlineFile } from './kustomize-authoring';
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

// Authoring state is seeded from value, and re-seeded when value changes to
// something this editor did not emit. Inside a wizard it is restored from the
// wizard's draft store when value is still what this editor last emitted, so
// edits that emit no overlay (an empty new file, comment-only content) survive
// a step change. The overlay dir is not user-facing (it only names the folder
// inside the Fleet bundle); an existing value is kept, otherwise the builders
// default it to "overlay".
type KustomizeSource = 'none' | 'inline' | 'git';
const drafts      = props.mode === 'edit' ? useKustomizeDrafts() : null;
const source      = ref<KustomizeSource>('none');
const dir         = ref('');
const files       = ref<InlineFile[]>([]);
const passthrough = ref<Record<string, string>>({});
const activeName  = ref('');
const chartRepo   = ref('');
const path        = ref('');
// Bumped on re-seed so the Tabbed instances remount with the new files.
const resetCount  = ref(0);
let lastEmitted: ComponentKustomize | undefined;

function seed(v: ComponentKustomize | undefined) {
  const draft = drafts?.get(props.name);

  lastEmitted = v;
  if (draft && isEqual(draft.emitted, v)) {
    source.value = draft.source;
    dir.value = draft.dir;
    files.value = draft.files.map((f) => ({ ...f }));
    passthrough.value = { ...draft.passthrough };
    activeName.value = draft.activeName;
    chartRepo.value = draft.chartRepo;
    path.value = draft.path;
  } else {
    source.value = v?.gitRef ? 'git' : v ? 'inline' : 'none';
    dir.value = v?.dir || '';
    files.value = initialInlineFiles(v);
    passthrough.value = outsideDirFiles(v);
    activeName.value = files.value[0].name;
    chartRepo.value = v?.gitRef?.chartRepo || '';
    path.value = v?.gitRef?.path || '';
  }
  adding.value = false;
  resetCount.value++;
}

function saveDraft() {
  drafts?.set(props.name, {
    emitted:     lastEmitted,
    source:      source.value,
    dir:         dir.value,
    files:       files.value.map((f) => ({ ...f })),
    passthrough: { ...passthrough.value },
    chartRepo:   chartRepo.value,
    path:        path.value,
    activeName:  activeName.value,
  });
}
function onTabChanged(name: string) {
  activeName.value = name;
  saveDraft();
}

const tabbed       = ref<{ select: (name: string) => void } | null>(null);
const unreferenced = computed(() => unreferencedFiles(files.value));
const emptyReferenced = computed(() => emptyReferencedFiles(files.value));
const outsideNames = computed(() => Object.keys(passthrough.value));

const sourceOptions = [
  { value: 'none', label: t('suseai.wizard.labels.kustomizeNone', 'None') },
  { value: 'inline', label: t('suseai.wizard.labels.kustomizeInline', 'Inline') },
  { value: 'git', label: t('suseai.wizard.labels.kustomizeGit', 'Git') },
];

function emitValue() {
  lastEmitted = source.value === 'inline' ? buildInlineKustomize(dir.value, files.value, passthrough.value) : source.value === 'git' ? buildGitRefKustomize(dir.value, chartRepo.value, path.value) : undefined;
  saveDraft();
  emit('update:value', lastEmitted);
}
function onSourceUpdate(v: KustomizeSource) {
  source.value = v;
  emitValue();
}
function isRoot(name: string): boolean {
  return KUSTOMIZATION_FILE_NAMES.includes(name);
}
function onContentUpdate(f: InlineFile, v: string) {
  f.content = v;
  emitValue();
}

// Add-file form. The error only shows once something is typed.
const adding  = ref(false);
const newName = ref('');

seed(props.value);
watch(() => props.value, (v) => {
  if (!isEqual(v, lastEmitted)) seed(v);
});
const newNameError = computed(() => {
  if (!newName.value.trim()) return '';
  switch (validateInlineFileName(newName.value, files.value)) {
  case 'invalid':   return t('suseai.wizard.labels.kustomizeFileNameInvalid', 'Use a relative path without empty, "." or ".." segments.');
  case 'reserved':  return t('suseai.wizard.labels.kustomizeFileNameReserved', 'The kustomization file already exists.');
  case 'duplicate': return t('suseai.wizard.labels.kustomizeFileNameDuplicate', 'A file with this name already exists.');
  default:          return '';
  }
});
function startAdd() {
  newName.value = '';
  adding.value = true;
}
function confirmAdd() {
  if (validateInlineFileName(newName.value, files.value)) return;
  const name = newName.value.trim();

  files.value.push({ name, content: '' });
  adding.value = false;
  emitValue();
  // The new Tab registers with Tabbed on mount.
  nextTick(() => tabbed.value?.select(name));
}
function removeFile(name: string) {
  if (isRoot(name)) return;
  files.value = files.value.filter((f) => f.name !== name);
  activeName.value = files.value[0].name;
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
.mb-10 { margin-bottom: 10px; }
.file-actions {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-left: auto;
  padding: 0 6px;
}
.add-file-actions {
  display: flex;
  align-items: center;
  gap: 10px;
}
// CodeMirror 6 sizes itself from its content, so the min-height has to sit on
// the content/gutter, not the wrapper (which left a one-line editor in a tall box).
.kustomize-editor :deep(.cm-content),
.kustomize-editor :deep(.cm-gutter) { min-height: 320px; }
</style>
