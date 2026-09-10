<script>
import {
  checkRegistryConnection, refreshChartRepository, registryConfigurationFingerprint,
} from '../../services/registry-connection';
import { requestErrorMessage } from '../../services/rancher-token';

export default {
  name: 'RegistryConnectionStatus',

  props: {
    target: { type: String, required: true },
    configuration: { type: Object, required: true },
  },

  data() {
    return {
      checking: false,
      refreshing: '',
      refreshError: '',
      result: null,
      testedFingerprint: '',
      chartName: { applicationCollection: 'ollama', suseRegistry: 'qdrant', nvidia: 'aiq-aira' }[this.target] || '',
      testedChartName: '',
      active: true,
    };
  },

  computed: {
    fingerprint() {
      return registryConfigurationFingerprint(this.target, this.configuration);
    },
    formChanged() {
      return this.fingerprint !== this.testedFingerprint || this.chartName !== this.testedChartName;
    },
    unsaved() {
      const applied = this.result?.chartRepositories.appliedConfiguration;
      return applied === null || (applied !== undefined &&
        this.fingerprint !== registryConfigurationFingerprint(this.target, applied));
    },
    busy() {
      return this.checking || !!this.refreshing;
    },
    sampleSelectable() {
      return this.target !== 'nvidia' || !!this.configuration.url?.trim();
    },
    verificationSummary() {
      if (this.formChanged) return 'changed';
      const { authentication, chartAccess, chartRepositories } = this.result;
      if (authentication.status === 'failed' || chartAccess.results.some(check => check.status === 'failed') ||
          chartRepositories.repositories.some(repo => ['failed', 'missing'].includes(repo.state))) return 'failed';
      if (authentication.status === 'error' || chartAccess.error || !chartAccess.results.length ||
          chartAccess.results.some(check => check.status !== 'ok') || chartRepositories.error ||
          chartRepositories.settingsError || !chartRepositories.repositories.length) return 'incomplete';
      if (this.unsaved) return 'unsaved';
      if (chartRepositories.settingsPending || chartRepositories.repositories.some(repo => repo.state !== 'ready')) return 'pending';
      return 'ready';
    },
    authenticationText() {
      const result = this.result.authentication;
      const label = this.t(`suseai.pages.settings.registryConnection.authentication.${result.status}`);
      const host = result.host ? ` — ${result.host}` : '';
      const latency = result.latencyMs != null ? ` (${result.latencyMs} ms)` : '';
      return `${label}${host}${latency}${result.status !== 'ok' && result.message ? `: ${result.message}` : ''}`;
    },
  },

  beforeUnmount() {
    this.active = false;
  },

  methods: {
    chartAdvice(check) {
      const reason = check.reason === 'accessDenied' && check.repositoryUrl === 'oci://registry.suse.com/ai/charts'
        ? 'suseAccessDenied' : check.reason;
      return this.t(`suseai.pages.settings.registryConnection.chartAccess.reasons.${reason}`);
    },
    stateClass(state) {
      if (state === 'ok' || state === 'ready') return 'text-success';
      if (state === 'failed' || state === 'error' || state === 'missing') return 'text-error';
      return 'text-muted';
    },
    async runTest() {
      if (this.busy) return;
      this.checking = true;
      this.result = null;
      this.refreshError = '';
      this.testedFingerprint = this.fingerprint;
      this.testedChartName = this.chartName;
      try {
        const result = await checkRegistryConnection(this.$store, this.target, JSON.parse(JSON.stringify(this.configuration)), this.sampleSelectable ? this.chartName : '');
        if (this.active) this.result = result;
      } finally {
        if (this.active) this.checking = false;
      }
    },
    async refresh(repo) {
      if (this.busy) return;
      this.refreshing = repo.name;
      this.refreshError = '';
      try {
        const updated = await refreshChartRepository(this.$store, this.target, repo.name);
        if (this.active) {
          this.result.chartRepositories.repositories = this.result.chartRepositories.repositories.map(
            item => item.name === repo.name ? updated : item,
          );
        }
      } catch (e) {
        if (this.active) this.refreshError = `${repo.name}: ${requestErrorMessage(e)}`;
      } finally {
        if (this.active) this.refreshing = '';
      }
    },
  },
};
</script>

<template>
  <div class="registry-connection mt-10">
    <div v-if="sampleSelectable" class="mb-10">
      <label :for="`${target}-test-chart`">{{ t('suseai.pages.settings.registryConnection.chartAccess.sampleLabel') }}</label>
      <input :id="`${target}-test-chart`" v-model="chartName" type="text" :aria-describedby="`${target}-test-chart-help`" />
      <p :id="`${target}-test-chart-help`" class="text-muted">{{ t('suseai.pages.settings.registryConnection.chartAccess.sampleHelp') }}</p>
    </div>
    <button
      type="button"
      class="btn role-secondary"
      :disabled="busy"
      @click="runTest"
    >
      {{ checking ? t('suseai.pages.settings.registryConnection.checking') : t('suseai.pages.settings.test.button') }}
    </button>
    <p class="text-muted mt-10">
      {{ t('suseai.pages.settings.registryConnection.description') }}
    </p>
    <div
      role="status"
      aria-live="polite"
      :aria-busy="busy"
    >
      <template v-if="result">
        <p :class="stateClass(verificationSummary)"><strong>{{ t(`suseai.pages.settings.registryConnection.summary.${verificationSummary}`) }}</strong></p>
        <dl>
          <dt>{{ t('suseai.pages.settings.registryConnection.authenticationLabel') }}</dt>
          <dd>
            <span v-if="formChanged">{{ t('suseai.pages.settings.registryConnection.formChanged') }}</span>
            <span
              v-else
              :class="stateClass(result.authentication.status)"
            >{{ authenticationText }}</span>
          </dd>
          <dt>{{ t('suseai.pages.settings.registryConnection.chartAccess.label') }}</dt>
          <dd>
            <p v-if="formChanged">{{ t('suseai.pages.settings.registryConnection.formChanged') }}</p>
            <template v-else>
              <p class="text-muted">{{ t('suseai.pages.settings.registryConnection.chartAccess.scope') }}</p>
              <p v-if="result.chartAccess.error" class="text-error">{{ result.chartAccess.error }}</p>
              <ul>
                <li v-for="check in result.chartAccess.results" :key="check.repositoryUrl" class="repository-result">
                  <strong :class="stateClass(check.status)">{{ t(`suseai.pages.settings.registryConnection.chartAccess.states.${check.status === 'ok' ? check.check : check.status}`) }}</strong>
                  <span v-if="check.httpStatus"> (HTTP {{ check.httpStatus }})</span>
                  <div>{{ check.repositoryUrl }}</div>
                  <div v-if="check.chartName">{{ check.chartName }}<span v-if="check.version"> — {{ check.version }}</span></div>
                  <p v-if="check.reason">{{ chartAdvice(check) }}</p>
                </li>
              </ul>
            </template>
          </dd>
          <dt>{{ t('suseai.pages.settings.registryConnection.repositoriesLabel') }}</dt>
          <dd>
            <p
              v-if="unsaved"
              class="text-warning"
            >
              {{ t('suseai.pages.settings.registryConnection.unsaved') }}
            </p>
            <p
              v-if="result.chartRepositories.settingsPending"
              class="text-muted"
            >
              {{ t('suseai.pages.settings.registryConnection.settingsPending') }}
            </p>
            <p
              v-if="result.chartRepositories.settingsError"
              class="text-error"
            >
              {{ t('suseai.pages.settings.registryConnection.settingsError') }}: {{ result.chartRepositories.settingsError }}
            </p>
            <p
              v-if="result.chartRepositories.error"
              class="text-error"
            >
              {{ t('suseai.pages.settings.registryConnection.repositoriesError') }}: {{ result.chartRepositories.error }}
            </p>
            <ul>
              <li
                v-for="repo in result.chartRepositories.repositories"
                :key="repo.name"
                class="repository-result"
              >
                <router-link :to="repo.link">{{ repo.name }}</router-link>
                <span :class="stateClass(repo.state)"> — {{ t(`suseai.pages.settings.registryConnection.states.${repo.state}`) }}</span>
                <button
                  v-if="repo.canRefresh"
                  type="button"
                  class="btn role-secondary ml-10"
                  :disabled="busy"
                  :aria-label="t('suseai.pages.settings.registryConnection.refreshLabel', { name: repo.name })"
                  @click="refresh(repo)"
                >
                  {{ refreshing === repo.name ? t('suseai.pages.settings.registryConnection.refreshing') : t('suseai.pages.settings.registryConnection.refresh') }}
                </button>
                <div
                  v-if="repo.url"
                  class="text-muted"
                >{{ repo.url }}</div>
                <p v-if="repo.reason">{{ t(`suseai.pages.settings.registryConnection.reasons.${repo.reason}`) }}</p>
                <p v-if="repo.message">{{ repo.message }}</p>
              </li>
            </ul>
          </dd>
        </dl>
        <p
          v-if="refreshError"
          class="text-error"
        >
          {{ t('suseai.pages.settings.registryConnection.refreshError') }}: {{ refreshError }}
        </p>
      </template>
    </div>
  </div>
</template>

<style lang="scss" scoped>
.registry-connection {
  overflow-wrap: anywhere;

  label { display: block; margin-bottom: 6px; }
  input { max-width: 400px; width: 100%; }

  dt {
    font-weight: bold;
    margin-top: 10px;
  }

  dd {
    margin: 5px 0 15px;
  }

  ul {
    list-style: none;
    padding: 0;
  }

  .repository-result {
    margin-bottom: 10px;
  }
}
</style>
