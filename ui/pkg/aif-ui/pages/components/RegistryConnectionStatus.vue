<script>
import { Banner } from '@components/Banner';
import { BadgeState } from '@components/BadgeState';
import { LabeledInput } from '@components/Form/LabeledInput';
import {
  checkRegistryConnection, refreshChartRepository, registryConfigurationFingerprint,
} from '../../services/registry-connection';
import { requestErrorMessage } from '../../services/rancher-token';

export default {
  name: 'RegistryConnectionStatus',

  components: { Banner, BadgeState, LabeledInput },

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
    summaryColor() {
      if (this.verificationSummary === 'ready') return 'success';
      if (this.verificationSummary === 'failed') return 'error';
      if (['incomplete', 'changed', 'unsaved'].includes(this.verificationSummary)) return 'warning';
      return 'info';
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
    stateColor(state) {
      if (state === 'ok' || state === 'ready') return 'bg-success';
      if (state === 'failed' || state === 'error' || state === 'missing') return 'bg-error';
      if (state === 'skipped') return 'bg-warning';
      return 'bg-info';
    },
    chartStateLabel(check) {
      const label = this.t(`suseai.pages.settings.registryConnection.chartAccess.states.${check.status === 'ok' ? check.check : check.status}`);
      return `${label}${check.httpStatus ? ` (HTTP ${check.httpStatus})` : ''}`;
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
  <div class="registry-connection">
    <h3>{{ t('suseai.pages.settings.registryConnection.title') }}</h3>
    <div
      v-if="sampleSelectable"
      class="row mb-15"
    >
      <div class="col span-8">
        <LabeledInput
          :id="`${target}-test-chart`"
          v-model:value="chartName"
          :label="t('suseai.pages.settings.registryConnection.chartAccess.sampleLabel')"
          :aria-describedby="`${target}-test-chart-help`"
          mode="edit"
        />
        <p
          :id="`${target}-test-chart-help`"
          class="text-deemphasized mt-5"
        >
          {{ t('suseai.pages.settings.registryConnection.chartAccess.sampleHelp') }}
        </p>
      </div>
    </div>
    <div class="verification-actions">
      <button
        type="button"
        class="btn role-secondary"
        :disabled="busy"
        @click="runTest"
      >
        {{ checking ? t('suseai.pages.settings.registryConnection.checking') : t('suseai.pages.settings.test.button') }}
      </button>
      <p class="text-deemphasized">
        {{ t('suseai.pages.settings.registryConnection.description') }}
      </p>
    </div>
    <div
      role="status"
      aria-live="polite"
      :aria-busy="busy"
    >
      <template v-if="result">
        <Banner
          :color="summaryColor"
          class="verification-summary"
        >
          <span>{{ t(`suseai.pages.settings.registryConnection.summary.${verificationSummary}`) }}</span>
        </Banner>
        <dl class="verification-checks">
          <div class="verification-check">
            <dt>{{ t('suseai.pages.settings.registryConnection.authenticationLabel') }}</dt>
            <dd>
              <p v-if="formChanged">
                {{ t('suseai.pages.settings.registryConnection.formChanged') }}
              </p>
              <template v-else>
                <BadgeState
                  :color="stateColor(result.authentication.status)"
                  :label="t(`suseai.pages.settings.registryConnection.authentication.${result.authentication.status}`)"
                />
                <p
                  v-if="result.authentication.host || result.authentication.latencyMs != null"
                  class="text-deemphasized mt-5"
                >
                  {{ result.authentication.host }}<span v-if="result.authentication.latencyMs != null"> ({{ result.authentication.latencyMs }} ms)</span>
                </p>
                <p
                  v-if="result.authentication.status !== 'ok' && result.authentication.message"
                  class="mt-10"
                >
                  {{ result.authentication.message }}
                </p>
              </template>
            </dd>
          </div>
          <div class="verification-check">
            <dt>{{ t('suseai.pages.settings.registryConnection.chartAccess.label') }}</dt>
            <dd>
              <p v-if="formChanged">
                {{ t('suseai.pages.settings.registryConnection.formChanged') }}
              </p>
              <template v-else>
                <p class="text-deemphasized mb-10">
                  {{ t('suseai.pages.settings.registryConnection.chartAccess.scope') }}
                </p>
                <Banner
                  v-if="result.chartAccess.error"
                  color="error"
                >
                  {{ result.chartAccess.error }}
                </Banner>
                <ul>
                  <li
                    v-for="check in result.chartAccess.results"
                    :key="check.repositoryUrl"
                    class="repository-result"
                  >
                    <BadgeState
                      :color="stateColor(check.status)"
                      :label="chartStateLabel(check)"
                    />
                    <div class="text-deemphasized mt-5">
                      {{ check.repositoryUrl }}
                    </div>
                    <div v-if="check.chartName">
                      {{ check.chartName }}<span v-if="check.version"> — {{ check.version }}</span>
                    </div>
                    <p
                      v-if="check.reason"
                      class="mt-10"
                    >
                      {{ chartAdvice(check) }}
                    </p>
                  </li>
                </ul>
              </template>
            </dd>
          </div>
          <div class="verification-check">
            <dt>{{ t('suseai.pages.settings.registryConnection.repositoriesLabel') }}</dt>
            <dd>
              <Banner
                v-if="unsaved"
                color="warning"
              >
                {{ t('suseai.pages.settings.registryConnection.unsaved') }}
              </Banner>
              <Banner
                v-if="result.chartRepositories.settingsPending"
                color="info"
              >
                {{ t('suseai.pages.settings.registryConnection.settingsPending') }}
              </Banner>
              <Banner
                v-if="result.chartRepositories.settingsError"
                color="error"
              >
                {{ t('suseai.pages.settings.registryConnection.settingsError') }}: {{ result.chartRepositories.settingsError }}
              </Banner>
              <Banner
                v-if="result.chartRepositories.error"
                color="error"
              >
                {{ t('suseai.pages.settings.registryConnection.repositoriesError') }}: {{ result.chartRepositories.error }}
              </Banner>
              <ul>
                <li
                  v-for="repo in result.chartRepositories.repositories"
                  :key="repo.name"
                  class="repository-result"
                >
                  <div class="repository-heading">
                    <div class="repository-state">
                      <router-link :to="repo.link">
                        {{ repo.name }}
                      </router-link>
                      <span> — </span>
                      <BadgeState
                        :color="stateColor(repo.state)"
                        :label="t(`suseai.pages.settings.registryConnection.states.${repo.state}`)"
                      />
                    </div>
                    <button
                      v-if="repo.canRefresh"
                      type="button"
                      class="btn role-secondary"
                      :disabled="busy"
                      :aria-label="t('suseai.pages.settings.registryConnection.refreshLabel', { name: repo.name })"
                      @click="refresh(repo)"
                    >
                      {{ refreshing === repo.name ? t('suseai.pages.settings.registryConnection.refreshing') : t('suseai.pages.settings.registryConnection.refresh') }}
                    </button>
                  </div>
                  <div
                    v-if="repo.url"
                    class="text-deemphasized mt-5"
                  >
                    {{ repo.url }}
                  </div>
                  <p
                    v-if="repo.reason"
                    class="mt-10"
                  >
                    {{ t(`suseai.pages.settings.registryConnection.reasons.${repo.reason}`) }}
                  </p>
                  <p
                    v-if="repo.message"
                    class="repository-message mt-5"
                  >
                    {{ repo.message }}
                  </p>
                </li>
              </ul>
            </dd>
          </div>
        </dl>
        <Banner
          v-if="refreshError"
          color="error"
        >
          {{ t('suseai.pages.settings.registryConnection.refreshError') }}: {{ refreshError }}
        </Banner>
      </template>
    </div>
  </div>
</template>

<style lang="scss" scoped>
.registry-connection {
  margin-top: 20px;
  padding-top: 20px;
  border-top: 1px solid var(--border);
  overflow-wrap: anywhere;

  .verification-actions, .repository-heading {
    display: flex;
    align-items: center;
    gap: 15px;
  }

  .verification-actions .btn, .repository-heading .btn {
    flex-shrink: 0;
  }

  .verification-checks { margin: 0; }

  .verification-check {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(0, 3fr);
    gap: 20px;
    padding: 15px 0;
    border-bottom: 1px solid var(--border);

    &:last-child { border-bottom: 0; padding-bottom: 0; }
  }

  dt {
    font-weight: 600;
    line-height: 20px;
  }

  dd {
    margin: 0;
    min-width: 0;

    > .banner:first-child { margin-top: 0; }
  }

  ul {
    list-style: none;
    padding: 0;
    margin: 0;
  }

  .repository-result {
    + .repository-result {
      margin-top: 15px;
      padding-top: 15px;
      border-top: 1px solid var(--border);
    }
  }

  .repository-heading {
    justify-content: space-between;
    flex-wrap: wrap;
  }

  .repository-state { min-width: 0; }
  .repository-message { white-space: pre-wrap; }

  @media (max-width: 1100px) {
    .verification-check { grid-template-columns: minmax(0, 1fr); gap: 10px; }
    .verification-actions { flex-wrap: wrap; }
  }
}
</style>
