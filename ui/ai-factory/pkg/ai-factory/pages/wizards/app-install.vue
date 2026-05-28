<template>
  <div class="aif-wizard">
    <h1>{{ t('aif.pages.wizards.install.title', { name: appId }) }}</h1>

    <WizardStepIndicator
      :steps="steps"
      :current-step="currentStep"
      @go-to-step="goToStep"
    />

    <!-- Step 0: Basic Info -->
    <div v-if="currentStep === 0" class="aif-wizard__step">
      <label>
        {{ t('aif.pages.wizards.install.instanceName') }}
        <input v-model="form.name" type="text" class="input" />
      </label>
      <label>
        {{ t('aif.pages.wizards.install.namespace') }}
        <input v-model="form.namespace" type="text" class="input" />
      </label>
      <label>
        {{ t('aif.pages.wizards.install.chartVersion') }}
        <select v-model="form.chartVersion" class="select">
          <option v-for="v in availableVersions" :key="v" :value="v">{{ v }}</option>
        </select>
      </label>
    </div>

    <!-- Step 1: Target -->
    <div v-if="currentStep === 1" class="aif-wizard__step">
      <label>{{ t('aif.pages.wizards.install.targetClusters') }}</label>
      <div v-for="cluster in availableClusters" :key="cluster.id" class="aif-wizard__cluster-row">
        <input
          :id="cluster.id"
          v-model="form.targetClusters"
          type="checkbox"
          :value="cluster.id"
        />
        <label :for="cluster.id">{{ cluster.nameDisplay || cluster.id }}</label>
      </div>
      <fieldset class="aif-wizard__strategy">
        <legend>{{ t('aif.pages.wizards.install.deliveryStrategy') }}</legend>
        <label>
          <input v-model="form.deployStrategy" type="radio" value="helm" />
          {{ t('aif.pages.wizards.install.strategyHelm') }}
        </label>
        <label>
          <input v-model="form.deployStrategy" type="radio" value="gitops" />
          {{ t('aif.pages.wizards.install.strategyGitops') }}
        </label>
      </fieldset>
    </div>

    <!-- Step 2: Configuration (pre-filled with the chart's default values from getAppValues) -->
    <div v-if="currentStep === 2" class="aif-wizard__step">
      <label>{{ t('aif.pages.wizards.install.helmValues') }}</label>
      <div v-if="loadingValues" class="aif-wizard__loading">
        <Loading />
      </div>
      <textarea v-else v-model="form.valuesYaml" class="aif-wizard__yaml-editor" rows="16" />
      <div v-if="valuesError" class="aif-wizard__error">
        {{ t('aif.pages.wizards.install.valuesError', { message: valuesError.message || String(valuesError) }) }}
      </div>
      <button class="btn btn-sm role-secondary" :disabled="loadingValues" @click="resetValues">
        {{ t('aif.pages.wizards.install.resetDefaults') }}
      </button>
    </div>

    <!-- Step 3: Review -->
    <div v-if="currentStep === 3" class="aif-wizard__step aif-wizard__review">
      <dl>
        <dt>{{ t('aif.pages.wizards.install.instanceName') }}</dt>
        <dd>{{ form.name }}</dd>
        <dt>{{ t('aif.pages.wizards.install.namespace') }}</dt>
        <dd>{{ form.namespace }}</dd>
        <dt>{{ t('aif.pages.wizards.install.chartVersion') }}</dt>
        <dd>{{ form.chartVersion }}</dd>
        <dt>{{ t('aif.pages.wizards.install.targetClusters') }}</dt>
        <dd>{{ form.targetClusters.join(', ') }}</dd>
        <dt>{{ t('aif.pages.wizards.install.deliveryStrategy') }}</dt>
        <dd>{{ form.deployStrategy }}</dd>
      </dl>
    </div>

    <div class="aif-wizard__nav">
      <button v-if="currentStep > 0" class="btn role-secondary" @click="back">
        {{ t('aif.pages.wizards.install.back') }}
      </button>
      <button class="btn role-secondary" @click="cancel">
        {{ t('aif.pages.wizards.install.cancel') }}
      </button>
      <button v-if="currentStep < steps.length - 1" class="btn role-primary" @click="next">
        {{ t('aif.pages.wizards.install.next') }}
      </button>
      <button v-else class="btn role-primary" :disabled="installing || !canInstall" @click="install">
        {{ installing ? t('aif.pages.wizards.install.installing') : t('aif.pages.wizards.install.install') }}
      </button>
    </div>

    <div v-if="installError" class="aif-wizard__error">{{ installError.message || String(installError) }}</div>

    <InstallProgressModal
      :show="showProgressModal"
      :title="t('aif.pages.wizards.install.title', { name: appId })"
      :progress="installProgress"
      @done="onProgressDone"
      @cancel="onProgressCancel"
    />
  </div>
</template>

<script>
import { defineComponent } from 'vue';
import Loading from '@shell/components/Loading';
import yaml from 'js-yaml';
import WizardStepIndicator from '../../components/wizards/WizardStepIndicator.vue';
import InstallProgressModal, { PROGRESS_STATUS } from '../../components/wizards/InstallProgressModal.vue';
import { getApp, getAppValues, createWorkload } from '../../utils/operator-api';
import { PRODUCT_NAME, MANAGEMENT_CLUSTER, PAGE_IDS } from '../../config/types';

const STORAGE_KEY = 'aif-app-install-wizard';
// DNS-1123 label: lowercase alphanumeric + hyphens, 1-63 chars.
const DNS_LABEL = /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$/;

export default defineComponent({
  name: 'AppInstallWizard',

  components: {
    WizardStepIndicator, InstallProgressModal, Loading,
  },

  async fetch() {
    const id = this.$route.params.id;

    try {
      this.app = await getApp(id);
    } catch (e) {
      this.installError = e;
    }

    try {
      const clusters = await this.$store.dispatch('management/findAll', { type: 'management.cattle.io.cluster' });

      // Filter out the Rancher local (management) cluster — AIF workloads run
      // on downstream managed clusters, never on the Rancher control plane.
      this.availableClusters = (clusters || []).filter((c) => c.id !== 'local');
    } catch (_) {
      this.availableClusters = [];
    }

    // Default chart version before restoring saved state.
    if (this.app?.version) {
      this.form.chartVersion = this.app.version;
    }

    const saved = localStorage.getItem(`${ STORAGE_KEY }:${ id }`);

    if (saved) {
      try {
        Object.assign(this.form, JSON.parse(saved));
      } catch (_) { /* ignore corrupt storage */ }
    }
  },

  data() {
    return {
      app:               null,
      availableClusters: [],
      currentStep:       0,
      installing:        false,
      installError:      null,
      // Separate from installError so a failed chart-defaults fetch surfaces
      // inside the Configuration step, not next to the Install button on Review.
      valuesError:       null,
      loadingValues:     false,
      valuesLoaded:      false,
      showProgressModal: false,
      installProgress:   [],
      form:              {
        name:           '',
        namespace:      '',
        chartVersion:   '',
        targetClusters: [],
        deployStrategy: 'helm',
        valuesYaml:     '',
      },
    };
  },

  watch: {
    // Suggest "<name>-system" only while the namespace field is still empty,
    // so we never clobber a namespace the user has typed.
    'form.name'(name) {
      if (!this.form.namespace) {
        this.form.namespace = name ? `${ name }-system` : '';
      }
    },
  },

  computed: {
    appId() {
      return this.$route.params.id;
    },

    availableVersions() {
      return this.app ? [this.app.version] : [];
    },

    steps() {
      return [
        { label: this.t('aif.pages.wizards.steps.basicInfo') },
        { label: this.t('aif.pages.wizards.steps.target') },
        { label: this.t('aif.pages.wizards.steps.configuration') },
        { label: this.t('aif.pages.wizards.steps.review') },
      ];
    },

    // Install is gated on (1) App loaded — required for repo/chart fields the
    // CRD enforces non-empty, (2) at least one target cluster — empty list
    // would leave the Workload Pending forever, (3) DNS-1123 valid name.
    canInstall() {
      return Boolean(this.app) &&
        this.form.targetClusters.length > 0 &&
        DNS_LABEL.test(this.form.name);
    },
  },

  methods: {
    async goToStep(index) {
      this.currentStep = index;
      if (index === 2) {
        await this.ensureDefaultValues();
      }
    },

    async next() {
      this.saveToStorage();
      this.currentStep++;
      if (this.currentStep === 2) {
        await this.ensureDefaultValues();
      }
    },

    back() {
      this.currentStep--;
    },

    cancel() {
      this.clearStorage();
      this.$router.push({
        name:   `${ PRODUCT_NAME }-c-cluster-${ PAGE_IDS.APPS }`,
        params: { cluster: MANAGEMENT_CLUSTER },
      });
    },

    saveToStorage() {
      try {
        localStorage.setItem(`${ STORAGE_KEY }:${ this.appId }`, JSON.stringify(this.form));
      } catch (_) { /* localStorage may be unavailable / quota exceeded */ }
    },

    clearStorage() {
      try {
        localStorage.removeItem(`${ STORAGE_KEY }:${ this.appId }`);
      } catch (_) { /* ignore */ }
    },

    // Load the chart's default values (Task 3-0 endpoint) the first time the
    // Configuration step is shown, unless saved/edited values already exist.
    async ensureDefaultValues() {
      if (this.valuesLoaded || this.form.valuesYaml) {
        return;
      }
      await this.loadDefaultValues();
    },

    async loadDefaultValues() {
      this.loadingValues = true;
      this.valuesError = null;
      try {
        const { values } = await getAppValues(this.appId, this.form.chartVersion);

        this.form.valuesYaml = yaml.dump(values || {});
        this.valuesLoaded = true;
      } catch (e) {
        // Scope to Configuration step — installError is reserved for the
        // createWorkload POST so the Review/Install error band stays meaningful.
        this.valuesError = e;
      } finally {
        this.loadingValues = false;
      }
    },

    resetValues() {
      // "Reset to defaults" reloads the chart defaults rather than clearing.
      this.valuesLoaded = false;
      this.form.valuesYaml = '';

      return this.loadDefaultValues();
    },

    async install() {
      // canInstall already gates the button; re-check here so a keyboard
      // submission or programmatic call can't bypass the precondition.
      if (!DNS_LABEL.test(this.form.name)) {
        this.installError = new Error(this.t('aif.pages.wizards.install.nameInvalid'));

        return;
      }
      if (!this.app) {
        this.installError = new Error(this.t('aif.pages.wizards.install.appNotLoaded'));

        return;
      }
      if (this.form.targetClusters.length === 0) {
        this.installError = new Error(this.t('aif.pages.wizards.install.targetClustersRequired'));

        return;
      }

      this.installing      = true;
      this.installError    = null;
      // AIDEV-NOTE: createWorkload is a single POST that creates one Workload
      // CR carrying spec.targetClusters; per-cluster Fleet reconciliation status
      // is not surfaced through this endpoint. We seed one progress row per
      // selected cluster so the modal scales with the user's selection, but all
      // rows are stamped SUCCESS/FAILED together based on the create result.
      // Per-cluster status will arrive in a later wave when the Workloads list
      // polls bundleDeployments for each target. See PR #59 round-1 review.
      this.installProgress = this.form.targetClusters.map((c) => ({
        clusterId:   c,
        clusterName: c,
        status:      PROGRESS_STATUS.INSTALLING,
        message:     this.t('aif.pages.wizards.install.creating'),
      }));
      this.showProgressModal = true;
      try {
        await createWorkload({
          metadata: { name: this.form.name, namespace: this.form.namespace },
          spec:     {
            // Explicit spec.name matches the CRD's required field rather than
            // relying on the handler's metadata.name → spec.name default; keeps
            // the wizard resilient if validation tightens server-side.
            name:   this.form.name,
            source: {
              kind: 'App',
              app:  {
                repo:    this.app.chartRef.repo,
                chart:   this.app.chartRef.chart,
                version: this.form.chartVersion,
              },
            },
            targetClusters: this.form.targetClusters,
            deployStrategy: this.form.deployStrategy,
            // App sources use ONE component keyed by the workload name
            // (deployer.go: desiredComponent.name = req.SpecName for SourceKindApp).
            valueOverrides: { [this.form.name]: this.form.valuesYaml },
          },
        });
        this.installProgress = this.installProgress.map((p) => ({
          ...p, status: PROGRESS_STATUS.SUCCESS, message: this.t('aif.pages.wizards.install.created'),
        }));
        this.clearStorage();
      } catch (e) {
        this.installError    = e;
        this.installProgress = this.installProgress.map((p) => ({
          ...p, status: PROGRESS_STATUS.FAILED, message: e?.message || 'Error',
        }));
      } finally {
        this.installing = false;
      }
    },

    onProgressDone() {
      this.showProgressModal = false;
      this.$router.push({
        name:   `${ PRODUCT_NAME }-c-cluster-${ PAGE_IDS.WORKLOADS }`,
        params: { cluster: MANAGEMENT_CLUSTER },
      });
    },

    onProgressCancel() {
      this.showProgressModal = false;
    },
  },
});
</script>

<style scoped>
.aif-wizard {
  max-width: 720px;
  padding: 24px;
}

.aif-wizard__step {
  display: flex;
  flex-direction: column;
  gap: 16px;
  margin-bottom: 24px;
}

.aif-wizard__yaml-editor {
  width: 100%;
  font-family: monospace;
  font-size: 0.85rem;
}

.aif-wizard__review dt {
  font-weight: 600;
}

.aif-wizard__nav {
  display: flex;
  gap: 8px;
  justify-content: flex-end;
  margin-top: 24px;
}

.aif-wizard__error {
  color: var(--error);
  margin-top: 12px;
}

.aif-wizard__strategy {
  border: 1px solid var(--border);
  border-radius: 4px;
  padding: 12px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.aif-wizard__cluster-row {
  display: flex;
  align-items: center;
  gap: 8px;
}
</style>
