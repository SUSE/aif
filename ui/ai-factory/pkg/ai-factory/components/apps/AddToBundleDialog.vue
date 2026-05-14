<template>
  <ModalWithCard
    :title="t('aif.pages.apps.dialog.title')"
    @close="$emit('cancel')"
  >
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
          <LabeledSelect
            v-model="selectedBundle"
            :label="t('aif.pages.apps.dialog.selectBundle')"
            :options="draftBundleOptions"
            :disabled="!draftBundleOptions.length"
            :placeholder="draftBundleOptions.length ? '' : t('aif.pages.apps.dialog.noDrafts')"
          />
        </div>

        <div v-if="mode === 'new'" class="add-to-bundle-dialog__new">
          <LabeledInput
            v-model="newBundleName"
            :label="t('aif.pages.apps.dialog.newBundleName')"
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
          disabled
          :title="t('aif.pages.apps.dialog.notYetAvailable')"
        >
          <i v-if="saving" class="icon icon-spinner icon-spin" />
          {{ t('aif.pages.apps.dialog.confirm') }}
        </button>
      </div>
    </template>
  </ModalWithCard>
</template>

<script>
import { defineComponent, ref, computed, inject, getCurrentInstance } from 'vue';
import ModalWithCard from '@shell/components/ModalWithCard';
import LabeledSelect from '@shell/components/form/LabeledSelect';
import LabeledInput from '@components/Form/LabeledInput/LabeledInput.vue';
import Banner from '@components/Banner/Banner.vue';
import { CRD_TYPES } from '../../config/types';

export default defineComponent({
  name: 'AddToBundleDialog',

  components: {
    ModalWithCard,
    LabeledSelect,
    LabeledInput,
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
    const store = inject('$store') || instance?.proxy?.$store;
    const t = instance?.proxy?.t || ((key) => key);

    const mode = ref('existing');
    const selectedBundle = ref(null);
    const newBundleName = ref(`${props.app.name}-bundle`);
    const saving = ref(false);
    const errorMsg = ref('');
    const draftBundles = ref([]);

    const draftBundleOptions = computed(() => {
      return draftBundles.value.map((b) => ({
        label: `${b.metadata.namespace}/${b.metadata.name} — ${b.spec.title}`,
        value: b
      }));
    });

    const canConfirm = computed(() => {
      if (mode.value === 'existing') {
        return !!selectedBundle.value;
      }

      return newBundleName.value.trim().length > 0;
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
      const bundle = selectedBundle.value;

      if (!bundle.spec.components) {
        bundle.spec.components = [];
      }
      bundle.spec.components.push(buildComponentRef());
      await bundle.save();

      return bundle.metadata.name;
    };

    const createNewBundle = async () => {
      const name = newBundleName.value.trim();
      const bundleData = {
        apiVersion: 'ai.suse.com/v1alpha1',
        kind:       'Bundle',
        metadata:   { name, namespace: 'default' },
        spec:       {
          title:           name,
          targetBlueprint: name,
          useCase:         'other',
          components:      [buildComponentRef()]
        }
      };

      const created = await store.dispatch('aif/create', bundleData);
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

    // Bundle store queries disabled until Bundle CRD integration is wired (P3-x stories)

    return {
      mode,
      selectedBundle,
      newBundleName,
      saving,
      errorMsg,
      draftBundleOptions,
      canConfirm,
      onConfirm,
      t
    };
  }
});
</script>

<style lang="scss" scoped>
.add-to-bundle-dialog {
  display: flex;
  flex-direction: column;
  gap: 16px;
  min-width: 400px;
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

.add-to-bundle-dialog__footer {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}
</style>
