<template>
  <div class="aif-wizard">
    <h1>{{ t('aif.pages.wizards.create.title') }}</h1>

    <WizardStepIndicator
      :steps="steps"
      :current-step="currentStep"
      @go-to-step="goToStep"
    />

    <div v-if="currentStep === 0" class="aif-wizard__step">
      <label>
        {{ t('aif.pages.wizards.create.blueprintName') }}
        <input v-model="form.blueprintName" type="text" class="input" />
      </label>
      <label>
        {{ t('aif.pages.wizards.create.version') }}
        <input v-model="form.version" type="text" class="input" placeholder="1.0.0" />
        <span v-if="form.version && !versionValid" class="aif-wizard__field-error">
          {{ t('aif.pages.wizards.create.versionInvalid') }}
        </span>
      </label>
      <label>
        {{ t('aif.pages.wizards.create.useCase') }}
        <select v-model="form.useCase" class="select">
          <option value="rag">{{ t('aif.pages.wizards.create.useCaseOptions.rag') }}</option>
          <option value="vision">{{ t('aif.pages.wizards.create.useCaseOptions.vision') }}</option>
          <option value="fine-tuning">{{ t('aif.pages.wizards.create.useCaseOptions.fineTuning') }}</option>
          <option value="inference">{{ t('aif.pages.wizards.create.useCaseOptions.inference') }}</option>
          <option value="other">{{ t('aif.pages.wizards.create.useCaseOptions.other') }}</option>
        </select>
      </label>
      <label>
        {{ t('aif.pages.wizards.create.description') }}
        <textarea v-model="form.description" class="input" rows="3" />
      </label>
    </div>

    <div v-if="currentStep === 1" class="aif-wizard__step">
      <input v-model="appSearch" type="search" class="input" :placeholder="t('aif.pages.wizards.create.selectApps.search')" />
      <ul class="aif-wizard__catalog">
        <li v-for="app in catalogResults" :key="app.id" class="aif-wizard__catalog-row">
          <span>{{ app.displayName || app.name }}</span>
          <button class="btn btn-sm role-secondary" :disabled="isSelected(app)" @click="addApp(app)">
            {{ t('aif.pages.wizards.create.selectApps.add') }}
          </button>
        </li>
      </ul>

      <p v-if="!form.components.length" class="aif-wizard__hint">
        {{ t('aif.pages.wizards.create.selectApps.empty') }}
      </p>
      <div v-for="(comp, idx) in form.components" :key="comp.name" class="aif-wizard__comp-row">
        <span class="aif-wizard__comp-name">{{ comp.name }}</span>
        <span class="aif-wizard__comp-chart">{{ comp.repo }}/{{ comp.chart }}</span>
        <span>{{ t('aif.pages.wizards.create.selectApps.version') }}: {{ comp.version }}</span>
        <button class="btn btn-sm role-danger" @click="removeComponent(idx)">
          {{ t('aif.pages.wizards.create.selectApps.remove') }}
        </button>
      </div>
    </div>

    <div v-if="currentStep === 2" class="aif-wizard__step">
      <p class="aif-wizard__hint">{{ t('aif.pages.wizards.create.config.intro') }}</p>
      <div v-for="comp in form.components" :key="comp.name" class="aif-wizard__config-panel">
        <div class="aif-wizard__config-head">
          <strong>{{ comp.name }}</strong>
          <button class="btn btn-sm role-secondary" :disabled="loadingDefaults[comp.name]" @click="loadComponentDefaults(comp)">
            {{ t('aif.pages.wizards.create.config.loadDefaults') }}
          </button>
        </div>
        <textarea
          v-model="form.valueOverrides[comp.name]"
          class="aif-wizard__yaml-editor"
          rows="10"
          :placeholder="t('aif.pages.wizards.create.config.valuesPlaceholder')"
        />
      </div>
    </div>

    <div v-if="currentStep === 3" class="aif-wizard__step aif-wizard__review">
      <dl>
        <dt>{{ t('aif.pages.wizards.create.blueprintName') }}</dt><dd>{{ form.blueprintName }}</dd>
        <dt>{{ t('aif.pages.wizards.create.version') }}</dt><dd>{{ form.version }}</dd>
        <dt>{{ t('aif.pages.wizards.create.useCase') }}</dt><dd>{{ form.useCase }}</dd>
        <dt>{{ t('aif.pages.wizards.create.description') }}</dt><dd>{{ form.description || '—' }}</dd>
      </dl>
      <ul>
        <li v-for="comp in form.components" :key="comp.name">
          {{ comp.name }} — {{ comp.repo }}/{{ comp.chart }}@{{ comp.version }}
          <em v-if="form.valueOverrides[comp.name]">({{ t('aif.pages.wizards.create.config.loadDefaults') }})</em>
        </li>
      </ul>
    </div>

    <div class="aif-wizard__nav">
      <button v-if="currentStep > 0" class="btn role-secondary" @click="back">
        {{ t('aif.pages.wizards.create.back') }}
      </button>
      <button class="btn role-secondary" @click="cancel">
        {{ t('aif.pages.wizards.create.cancel') }}
      </button>
      <button v-if="currentStep < steps.length - 1" class="btn role-primary" :disabled="!stepReady(currentStep + 1)" @click="next">
        {{ t('aif.pages.wizards.create.next') }}
      </button>
      <button v-else class="btn role-primary" :disabled="publishing || !stepReady(3)" @click="publish">
        {{ publishing ? t('aif.pages.wizards.create.publishing') : t('aif.pages.wizards.create.publish') }}
      </button>
    </div>

    <div v-if="publishError" class="aif-wizard__error">{{ publishError.message }}</div>
  </div>
</template>

<script>
import { defineComponent } from 'vue';
import WizardStepIndicator from '../../components/wizards/WizardStepIndicator.vue';
import { createBlueprint, listApps, getAppValues } from '../../utils/operator-api';
import { PRODUCT_NAME, MANAGEMENT_CLUSTER } from '../../config/types';
import yaml from 'js-yaml';

// Mirror api/v1alpha1/blueprint_types.go BlueprintSpec.Version
// (+kubebuilder:validation:Pattern=`^\d+\.\d+\.\d+$`); pre-release /
// build suffixes are rejected by the CRD, so reject them here too rather
// than letting Publish hit a generic 400.
const SEMVER = /^\d+\.\d+\.\d+$/;

export default defineComponent({
  name: 'BlueprintCreateWizard',

  components: { WizardStepIndicator },

  async fetch() {
    try {
      this.catalogApps = await listApps({ source: 'suse' });
    } catch (e) {
      this.catalogApps = [];
    }
  },

  data() {
    return {
      currentStep:     0,
      publishing:      false,
      publishError:    null,
      catalogApps:     [],
      appSearch:       '',
      loadingDefaults: {},
      form:            {
        blueprintName:  '',
        version:        '',
        useCase:        'inference',
        description:    '',
        components:     [],
        valueOverrides: {},
      },
    };
  },

  computed: {
    steps() {
      return [
        { label: this.t('aif.pages.wizards.create.steps.basicInfo') },
        { label: this.t('aif.pages.wizards.create.steps.selectApps') },
        { label: this.t('aif.pages.wizards.create.steps.configuration') },
        { label: this.t('aif.pages.wizards.create.steps.review') },
      ];
    },

    versionValid() {
      return SEMVER.test(this.form.version);
    },

    catalogResults() {
      const q = this.appSearch.trim().toLowerCase();
      const list = this.catalogApps || [];

      if (!q) {
        return list.slice(0, 20);
      }

      return list
        .filter((a) => (a.name || '').toLowerCase().includes(q) || (a.displayName || '').toLowerCase().includes(q))
        .slice(0, 20);
    },
  },

  methods: {
    stepReady(index) {
      switch (index) {
      case 0:  return true;
      case 1:  return this.form.blueprintName.trim() !== '' && this.versionValid;
      case 2:  return this.form.components.length > 0;
      case 3:  return this.form.components.length > 0;
      default: return false;
      }
    },

    goToStep(index) {
      if (index <= this.currentStep || this.stepReady(index)) {
        this.currentStep = index;
      }
    },

    next() {
      if (this.stepReady(this.currentStep + 1)) {
        this.currentStep++;
      }
    },

    back() {
      this.currentStep--;
    },

    cancel() {
      this.$router.push({ name: `${ PRODUCT_NAME }-c-cluster-blueprints`, params: { cluster: MANAGEMENT_CLUSTER } });
    },

    isSelected(app) {
      return this.form.components.some((c) => c.appId === app.id);
    },

    addApp(app) {
      if (this.isSelected(app)) {
        return;
      }
      const name = app.chartRef?.chart || app.name || app.id;

      this.form.components.push({
        name,
        appId:   app.id,
        repo:    app.chartRef?.repo || '',
        chart:   app.chartRef?.chart || name,
        version: app.version || '',
      });
    },

    removeComponent(idx) {
      const [removed] = this.form.components.splice(idx, 1);

      if (removed) {
        delete this.form.valueOverrides[removed.name];
      }
    },

    async loadComponentDefaults(comp) {
      this.loadingDefaults = { ...this.loadingDefaults, [comp.name]: true };
      try {
        const { values } = await getAppValues(comp.appId, comp.version);

        this.form.valueOverrides[comp.name] = yaml.dump(values || {});
      } catch (e) {
        this.publishError = e;
      } finally {
        this.loadingDefaults = { ...this.loadingDefaults, [comp.name]: false };
      }
    },

    async publish() {
      if (!this.stepReady(1)) {
        this.publishError = new Error(this.t('aif.pages.wizards.create.versionInvalid'));

        return;
      }
      this.publishing   = true;
      this.publishError = null;
      try {
        const valueOverrides = {};

        for (const [k, v] of Object.entries(this.form.valueOverrides)) {
          if (v && v.trim()) {
            valueOverrides[k] = v;
          }
        }
        await createBlueprint({
          blueprintName: this.form.blueprintName,
          version:       this.form.version,
          useCase:       this.form.useCase,
          description:   this.form.description,
          components:    this.form.components.map((c) => ({
            name: c.name,
            kind: 'App',
            app:  { repo: c.repo, chart: c.chart, version: c.version },
          })),
          valueOverrides,
        });
        this.$router.push({ name: `${ PRODUCT_NAME }-c-cluster-blueprints`, params: { cluster: MANAGEMENT_CLUSTER } });
      } catch (e) {
        this.publishError = e;
      } finally {
        this.publishing = false;
      }
    },
  },
});
</script>

<style scoped>
.aif-wizard { max-width: 760px; padding: 24px; }
.aif-wizard__step { display: flex; flex-direction: column; gap: 16px; margin-bottom: 24px; }
.aif-wizard__catalog { list-style: none; padding: 0; margin: 0; max-height: 220px; overflow-y: auto; border: 1px solid var(--border); border-radius: 4px; }
.aif-wizard__catalog-row { display: flex; justify-content: space-between; align-items: center; padding: 6px 10px; border-bottom: 1px solid var(--border); }
.aif-wizard__catalog-row:last-child { border-bottom: none; }
.aif-wizard__comp-row { display: grid; grid-template-columns: 1fr 2fr auto auto; gap: 8px; align-items: center; }
.aif-wizard__comp-name { font-weight: 600; }
.aif-wizard__config-panel { border: 1px solid var(--border); border-radius: 4px; padding: 12px; display: flex; flex-direction: column; gap: 8px; }
.aif-wizard__config-head { display: flex; justify-content: space-between; align-items: center; }
.aif-wizard__yaml-editor { width: 100%; font-family: monospace; font-size: 0.85rem; }
.aif-wizard__review dt { font-weight: 600; }
.aif-wizard__nav { display: flex; gap: 8px; justify-content: flex-end; margin-top: 24px; }
.aif-wizard__error { color: var(--error); margin-top: 12px; }
.aif-wizard__field-error { color: var(--error); font-size: 0.8rem; }
.aif-wizard__hint { color: var(--muted); }
</style>
