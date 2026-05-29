<template>
  <div class="aif-wizard">
    <div v-if="!bpName || !bpVersion" class="aif-wizard__missing">
      <p>{{ t('aif.pages.wizards.install.missingBlueprint') }}</p>
      <button class="btn role-primary" @click="cancel">
        {{ t('aif.pages.wizards.install.backToBlueprints') }}
      </button>
    </div>
    <template v-else>
    <h1>{{ t('aif.pages.wizards.install.title', { name: bpName }) }}</h1>

    <div class="aif-wizard__bp-banner">
      <span class="badge badge--primary">{{ bpName }}</span>
      <span class="badge badge--secondary">v{{ bpVersion }}</span>
    </div>

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
        <input
          v-model="form.namespace"
          type="text"
          class="input"
          :placeholder="`${ form.name || 'workload' }-system`"
        />
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

    <!-- Step 2: Review -->
    <div v-if="currentStep === 2" class="aif-wizard__step aif-wizard__review">
      <dl>
        <dt>{{ t('aif.pages.wizards.install.blueprint') }}</dt>
        <dd>{{ bpName }} v{{ bpVersion }}</dd>
        <dt>{{ t('aif.pages.wizards.install.instanceName') }}</dt>
        <dd>{{ form.name }}</dd>
        <dt>{{ t('aif.pages.wizards.install.namespace') }}</dt>
        <dd>{{ form.namespace }}</dd>
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
      <button
        v-if="currentStep < steps.length - 1"
        class="btn role-primary"
        :disabled="!stepReady(currentStep + 1)"
        @click="next"
      >
        {{ t('aif.pages.wizards.install.next') }}
      </button>
      <button v-else class="btn role-primary" :disabled="installing || !canInstall" @click="install">
        {{ installing ? t('aif.pages.wizards.install.installing') : t('aif.pages.wizards.install.install') }}
      </button>
    </div>

    <div v-if="installError" class="aif-wizard__error">{{ installError.message || String(installError) }}</div>

    <InstallProgressModal
      :show="showProgressModal"
      :title="t('aif.pages.wizards.install.title', { name: bpName })"
      :progress="installProgress"
      @done="onProgressDone"
      @cancel="onProgressCancel"
    />
    </template>
  </div>
</template>

<script>
import { defineComponent } from 'vue';
import WizardStepIndicator from '../../components/wizards/WizardStepIndicator.vue';
import InstallProgressModal, { PROGRESS_STATUS } from '../../components/wizards/InstallProgressModal.vue';
import { createWorkload } from '../../utils/operator-api';
import { PRODUCT_NAME, MANAGEMENT_CLUSTER, PAGE_IDS } from '../../config/types';

// DNS-1123 label: lowercase alphanumeric + hyphens, 1-63 chars.
const DNS_LABEL = /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$/;

export default defineComponent({
  name: 'BlueprintInstallWizard',

  components: { WizardStepIndicator, InstallProgressModal },

  async fetch() {
    try {
      const clusters = await this.$store.dispatch('management/findAll', { type: 'management.cattle.io.cluster' });

      // Filter out the Rancher local (management) cluster — AIF workloads run
      // on downstream managed clusters, never on the Rancher control plane.
      this.availableClusters = (clusters || []).filter((c) => c.id !== MANAGEMENT_CLUSTER);
    } catch (_) {
      this.availableClusters = [];
    }
  },

  data() {
    return {
      availableClusters: [],
      currentStep:       0,
      installing:        false,
      installError:      null,
      showProgressModal: false,
      installProgress:   [],
      form:              {
        name:           '',
        namespace:      '',
        targetClusters: [],
        deployStrategy: 'helm',
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
    // Both router query and params are supported because the BlueprintCard
    // (Task 2-5) pushes via `query: { bpName, bpVersion }` while the route
    // path itself does not embed the version, so params will be empty in
    // the normal navigation flow. Query takes precedence; params are a fallback
    // for hand-crafted URLs.
    bpName() {
      return this.$route.query?.bpName || this.$route.params.bpName;
    },

    bpVersion() {
      return this.$route.query?.bpVersion || this.$route.params.bpVersion;
    },

    steps() {
      return [
        { label: this.t('aif.pages.wizards.steps.basicInfo') },
        { label: this.t('aif.pages.wizards.steps.target') },
        { label: this.t('aif.pages.wizards.steps.review') },
      ];
    },

    // Install is gated on (1) DNS-1123 valid name, (2) at least one target
    // cluster — empty list would leave the Workload Pending forever, (3) the
    // blueprint identity present in the route.
    canInstall() {
      return DNS_LABEL.test(this.form.name) &&
        this.form.targetClusters.length > 0 &&
        Boolean(this.bpName) &&
        Boolean(this.bpVersion);
    },
  },

  methods: {
    // Per-step readiness mirrors blueprint-create.vue so the user can't
    // tab forward into Review with an empty target-clusters list (which
    // would leave the resulting Workload Pending forever). Step 0 is
    // always entered fresh; Step 1 requires a DNS-1123 name + a
    // non-empty namespace; Step 2 (Review) additionally requires the
    // target-clusters list to be non-empty.
    stepReady(index) {
      switch (index) {
      case 0:  return true;
      case 1:  return DNS_LABEL.test(this.form.name) && this.form.namespace.trim() !== '';
      case 2:  return DNS_LABEL.test(this.form.name) &&
                 this.form.namespace.trim() !== '' &&
                 this.form.targetClusters.length > 0;
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
      this.$router.push({
        name:   `${ PRODUCT_NAME }-c-cluster-${ PAGE_IDS.BLUEPRINTS }`,
        params: { cluster: MANAGEMENT_CLUSTER },
      });
    },

    async install() {
      // canInstall already gates the button; re-check here so a keyboard
      // submission or programmatic call can't bypass the precondition.
      if (!DNS_LABEL.test(this.form.name)) {
        this.installError = new Error(this.t('aif.pages.wizards.install.nameInvalid'));

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
      // Mirrors the pattern in app-install.vue.
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
            source: {
              kind:      'Blueprint',
              blueprint: { name: this.bpName, version: this.bpVersion },
            },
            targetClusters: this.form.targetClusters,
            deployStrategy: this.form.deployStrategy,
          },
        });
        this.installProgress = this.installProgress.map((p) => ({
          ...p, status: PROGRESS_STATUS.SUCCESS, message: this.t('aif.pages.wizards.install.created'),
        }));
      } catch (e) {
        this.installError = e;
        // createWorkload is a single POST; a 4xx is a single rejection
        // of the whole submission, not per-cluster failures. Stamp every
        // row with a scope-neutral message so the modal doesn't imply
        // the request reached each cluster (it didn't).
        const failMsg = `${ this.t('aif.pages.wizards.install.createFailed') }: ${ e?.message || 'Error' }`;

        this.installProgress = this.installProgress.map((p) => ({
          ...p, status: PROGRESS_STATUS.FAILED, message: failMsg,
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

.aif-wizard__bp-banner {
  display: flex;
  gap: 8px;
  margin-bottom: 16px;
}

.aif-wizard__step {
  display: flex;
  flex-direction: column;
  gap: 16px;
  margin-bottom: 24px;
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
