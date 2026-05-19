<template>
  <ModalWithCard
    name="add-to-bundle"
    width="450"
    custom-class="add-to-bundle-modal"
    @close="$emit('cancel')"
  >
    <template #title>{{ t('aif.pages.apps.dialog.title') }}</template>
    <template #content>
      <div class="add-to-bundle-dialog">
        <div class="add-to-bundle-dialog__modes">
          <label class="add-to-bundle-dialog__mode">
            <input v-model="mode" type="radio" value="existing" />
            {{ t('aif.pages.apps.dialog.modeExisting') }}
          </label>
          <label class="add-to-bundle-dialog__mode">
            <input v-model="mode" type="radio" value="new" />
            {{ t('aif.pages.apps.dialog.modeNew') }}
          </label>
        </div>

        <div v-if="mode === 'existing'" class="add-to-bundle-dialog__existing">
          <label class="add-to-bundle-dialog__label">{{ t('aif.pages.apps.dialog.selectBundle') }}</label>
          <select
            :value="selectedBundle || ''"
            class="add-to-bundle-dialog__select"
            :disabled="!draftBundleOptions.length"
            @change="selectedBundle = $event.target.value"
          >
            <option value="" disabled>
              {{ draftBundleOptions.length ? t('aif.pages.apps.dialog.selectBundle') : t('aif.pages.apps.dialog.noDrafts') }}
            </option>
            <option v-for="opt in draftBundleOptions" :key="opt.value" :value="opt.value">
              {{ opt.label }}
            </option>
          </select>
        </div>

        <div v-if="mode === 'new'" class="add-to-bundle-dialog__new">
          <LabeledInput
            v-model:value="newBundleName"
            :label="t('aif.pages.apps.dialog.newBundleName')"
            required
          />
          <LabeledSelect
            v-model:value="newBundleNamespace"
            :label="t('aif.pages.apps.dialog.newBundleNamespace')"
            :options="namespaceOptions"
            required
          />
        </div>

        <Banner v-if="errorMsg" color="error" :label="errorMsg" />
      </div>
    </template>

    <template #footer>
      <div class="add-to-bundle-dialog__footer">
        <button class="btn role-secondary" @click="$emit('cancel')">
          {{ t('aif.pages.apps.dialog.cancel') }}
        </button>
        <button
          class="btn role-primary"
          :disabled="!canConfirm || saving"
          @click="onConfirm"
        >
          <i v-if="saving" class="icon icon-spinner icon-spin" />
          {{ t('aif.pages.apps.dialog.confirm') }}
        </button>
      </div>
    </template>
  </ModalWithCard>
</template>

<script>
import { defineComponent, ref, computed, getCurrentInstance, onMounted } from 'vue';
import ModalWithCard from '@shell/components/ModalWithCard';
import LabeledInput from '@components/Form/LabeledInput/LabeledInput.vue';
import LabeledSelect from '@shell/components/form/LabeledSelect';
import { Banner } from '@components/Banner';
import { NAMESPACE } from '@shell/config/types';
import { CRD_TYPES } from '../../config/types';

export default defineComponent({
  name: 'AddToBundleDialog',

  components: {
    ModalWithCard,
    LabeledInput,
    LabeledSelect,
    Banner
  },

  props: {
    app: {
      type:     Object,
      required: true
    }
  },

  emits: ['added', 'cancel'],

  setup(props, { emit }) {
    const instance = getCurrentInstance();
    const store = instance?.proxy?.$store;
    const t = instance?.proxy?.t?.bind(instance.proxy) || ((key) => key);

    const mode = ref('existing');
    const selectedBundle = ref(null);
    const newBundleName = ref(`${props.app.name}-bundle`);
    const newBundleNamespace = ref('default');
    const saving = ref(false);
    const errorMsg = ref('');
    const draftBundles = ref([]);
    const namespaces = ref([]);

    const namespaceOptions = computed(() => {
      const names = namespaces.value
        .filter((n) => !n.isSystem)
        .map((n) => n.metadata?.name)
        .filter(Boolean);

      if (!names.includes('default')) {
        names.unshift('default');
      }

      return names;
    });

    const draftBundleOptions = computed(() => {
      return draftBundles.value.map((b) => ({
        label: `${b.metadata.namespace}/${b.metadata.name}`,
        value: `${b.metadata.namespace}/${b.metadata.name}`
      }));
    });

    const selectedBundleObj = computed(() => {
      if (!selectedBundle.value) {
        return null;
      }

      return draftBundles.value.find(
        (b) => `${b.metadata.namespace}/${b.metadata.name}` === selectedBundle.value
      );
    });

    const canConfirm = computed(() => {
      if (mode.value === 'existing') {
        return !!selectedBundle.value;
      }

      return newBundleName.value.trim().length > 0 && !!newBundleNamespace.value;
    });

    const buildComponentRef = () => ({
      name: props.app.name,
      kind: 'App',
      app:  {
        repo:    props.app.chartRef.repo,
        chart:   props.app.chartRef.chart,
        version: props.app.chartRef.version
      }
    });

    const addToExistingBundle = async () => {
      const bundle = selectedBundleObj.value;

      if (!bundle) {
        throw new Error('No bundle selected');
      }
      if (!bundle.spec.components) {
        bundle.spec.components = [];
      }
      bundle.spec.components.push(buildComponentRef());
      await bundle.save();

      return bundle.metadata.name;
    };

    const createNewBundle = async () => {
      const name = newBundleName.value.trim();
      const namespace = newBundleNamespace.value || 'default';
      const bundleData = {
        type:       CRD_TYPES.BUNDLE,
        apiVersion: 'ai.suse.com/v1alpha1',
        kind:       'Bundle',
        metadata:   { name, namespace },
        spec:       {
          title:           name,
          targetBlueprint: name,
          useCase:         'other',
          components:      [buildComponentRef()]
        }
      };

      const created = await store.dispatch('management/create', bundleData);
      await created.save();

      return name;
    };

    const onConfirm = async () => {
      saving.value = true;
      errorMsg.value = '';

      try {
        const bundleName = mode.value === 'existing'
          ? await addToExistingBundle()
          : await createNewBundle();

        emit('added', { bundle: bundleName, mode: mode.value });
      } catch (err) {
        errorMsg.value = err.message || t('aif.pages.apps.dialog.errorAdd');
      } finally {
        saving.value = false;
      }
    };

    onMounted(async() => {
      try {
        const allBundles = await store.dispatch('management/findAll', { type: CRD_TYPES.BUNDLE });

        draftBundles.value = (allBundles || []).filter(
          (b) => !b.status?.phase || b.status.phase === 'Draft'
        );
      } catch (err) {
        console.error('AddToBundleDialog: failed to load bundles', err); // eslint-disable-line no-console
        draftBundles.value = [];
      }

      try {
        namespaces.value = await store.dispatch('management/findAll', { type: NAMESPACE }) || [];
      } catch (err) {
        console.error('AddToBundleDialog: failed to load namespaces', err); // eslint-disable-line no-console
        namespaces.value = [];
      }
    });

    return {
      mode,
      selectedBundle,
      newBundleName,
      newBundleNamespace,
      saving,
      errorMsg,
      draftBundleOptions,
      namespaceOptions,
      canConfirm,
      onConfirm,
      t
    };
  }
});
</script>

<style lang="scss">
.add-to-bundle-modal.modal-container {
  overflow: visible;
}
</style>

<style lang="scss" scoped>
.add-to-bundle-dialog {
  display: flex;
  flex-direction: column;
  gap: 16px;
  min-width: 400px;
  min-height: 140px;
}

.add-to-bundle-dialog__modes {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.add-to-bundle-dialog__mode {
  display: flex;
  align-items: center;
  gap: 8px;
  cursor: pointer;
}

.add-to-bundle-dialog__new {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.add-to-bundle-dialog__label {
  font-size: 13px;
  color: var(--input-label);
  margin-bottom: 4px;
  display: block;
}

.add-to-bundle-dialog__select {
  width: 100%;
  height: 40px;
  padding: 0 12px;
  border: 1px solid var(--border);
  border-radius: var(--border-radius);
  background: var(--input-bg);
  color: var(--body-text);
  font-size: 14px;
}

.add-to-bundle-dialog__footer {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  width: 100%;
}
</style>
