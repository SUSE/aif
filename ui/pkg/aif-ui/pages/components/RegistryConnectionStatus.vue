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
      active: true,
    };
  },

  computed: {
    fingerprint() {
      return registryConfigurationFingerprint(this.target, this.configuration);
    },
    formChanged() {
      return this.fingerprint !== this.testedFingerprint;
    },
    unsaved() {
      const applied = this.result?.chartRepositories.appliedConfiguration;
      return applied === null || (applied !== undefined &&
        this.fingerprint !== registryConfigurationFingerprint(this.target, applied));
    },
    busy() {
      return this.checking || !!this.refreshing;
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
      try {
        const result = await checkRegistryConnection(this.$store, this.target, JSON.parse(JSON.stringify(this.configuration)));
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
        <dl>
          <dt>{{ t('suseai.pages.settings.registryConnection.authenticationLabel') }}</dt>
          <dd>
            <span v-if="formChanged">{{ t('suseai.pages.settings.registryConnection.formChanged') }}</span>
            <span
              v-else
              :class="stateClass(result.authentication.status)"
            >{{ authenticationText }}</span>
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
