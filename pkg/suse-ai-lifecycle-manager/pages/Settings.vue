<script>
import AsyncButton      from '@shell/components/AsyncButton';
import { Banner }       from '@components/Banner';
import Loading          from '@shell/components/Loading';
import { LabeledInput } from '@components/Form/LabeledInput';
import LabeledSelect    from '@shell/components/form/LabeledSelect';
import { Checkbox }     from '@components/Form/Checkbox';
import SecretSelector   from '@shell/components/form/SecretSelector';
import { getSettings, putSettings } from '../utils/operator-api';
import { OPERATOR_NAMESPACE } from '../utils/constants';
import { ensureClusterRepo } from '../services/rancher-apps';
import { APP_COLLECTION_REPO_URL, SUSE_REGISTRY_REPO_URL } from '../services/app-collection';

function createEmptySpec() {
  return {
    fleet:                 { repoURL: '', branch: 'main', authType: '', credSecretRef: null },
    applicationCollection: { userSecretRef: null, tokenSecretRef: null, categories: [] },
    suseRegistry:          { userSecretRef: null, tokenSecretRef: null, refreshIntervalMinutes: 10 },
    registryEndpoints:     { suseRegistry: '', applicationCollection: '', applicationCollectionAPI: '' },
    catalogDiscovery:      { applicationCollectionMode: 'api' },
    imageRewrite:          { enabled: false, rules: [] },
  };
}

export default {
  name: 'SettingsPage',

  components: {
    AsyncButton,
    Banner,
    Loading,
    LabeledInput,
    LabeledSelect,
    Checkbox,
    SecretSelector,
  },

  async fetch() {
    try {
      const data = await getSettings();

      this.spec   = this.buildSpec(data.spec);
      this.loaded = true;
    } catch (e) {
      if (e?.status === 404) {
        this.notFound = true;
        this.loaded   = true;
      } else {
        this.fetchErrorMessage = e?.message || String(e);
        this.loaded            = true;
      }
    }
  },

  data() {
    return {
      loaded:            false,
      notFound:          false,
      spec:              createEmptySpec(),
      fetchErrorMessage: null,
      errors:            [],
      mode:              'edit',
      expanded:          {
        fleet:         false,
        appCollection: true,
        suseRegistry:  false,
        advanced:      false,
      },
    };
  },

  computed: {
    settingsNamespace() {
      return OPERATOR_NAMESPACE;
    },

    authTypeOptions() {
      return [
        { label: this.t('suseai.pages.settings.sections.fleet.authType.options.none'), value: '' },
        { label: this.t('suseai.pages.settings.sections.fleet.authType.options.ssh'), value: 'ssh' },
        { label: this.t('suseai.pages.settings.sections.fleet.authType.options.token'), value: 'token' },
        { label: this.t('suseai.pages.settings.sections.fleet.authType.options.basic'), value: 'basic' },
      ];
    },

    catalogDiscoveryOptions() {
      return [
        { label: this.t('suseai.pages.settings.sections.advanced.catalogDiscovery.applicationCollectionMode.options.api'), value: 'api' },
        { label: this.t('suseai.pages.settings.sections.advanced.catalogDiscovery.applicationCollectionMode.options.registryFallback'), value: 'registry-fallback' },
        { label: this.t('suseai.pages.settings.sections.advanced.catalogDiscovery.applicationCollectionMode.options.disabled'), value: 'disabled' },
      ];
    },

    categoriesString: {
      get() {
        return (this.spec.applicationCollection.categories || []).join(', ');
      },
      set(val) {
        this.spec.applicationCollection.categories = val
          ? val.split(',').map((s) => s.trim()).filter(Boolean)
          : [];
      },
    },
  },

  methods: {
    emptySpec() {
      return createEmptySpec();
    },

    buildSpec(crdSpec = {}) {
      const s = this.emptySpec();

      if (crdSpec.fleet) {
        s.fleet = {
          repoURL:       crdSpec.fleet.repoURL || '',
          branch:        crdSpec.fleet.branch || 'main',
          authType:      crdSpec.fleet.authType || '',
          credSecretRef: crdSpec.fleet.credSecretRef || null,
        };
      }
      if (crdSpec.applicationCollection) {
        s.applicationCollection = {
          userSecretRef:  crdSpec.applicationCollection.userSecretRef || null,
          tokenSecretRef: crdSpec.applicationCollection.tokenSecretRef || null,
          categories:     crdSpec.applicationCollection.categories || [],
        };
      }
      if (crdSpec.suseRegistry) {
        s.suseRegistry = {
          userSecretRef:          crdSpec.suseRegistry.userSecretRef || null,
          tokenSecretRef:         crdSpec.suseRegistry.tokenSecretRef || null,
          refreshIntervalMinutes: crdSpec.suseRegistry.refreshIntervalMinutes ?? 10,
        };
      }
      if (crdSpec.registryEndpoints) {
        s.registryEndpoints = { ...s.registryEndpoints, ...crdSpec.registryEndpoints };
      }
      if (crdSpec.catalogDiscovery) {
        s.catalogDiscovery.applicationCollectionMode =
          crdSpec.catalogDiscovery.applicationCollectionMode || 'api';
      }
      if (crdSpec.imageRewrite) {
        s.imageRewrite = {
          enabled: !!crdSpec.imageRewrite.enabled,
          rules:   (crdSpec.imageRewrite.rules || []).map((r) => ({ match: r.match, replace: r.replace })),
        };
      }

      return s;
    },

    buildCrdSpec(spec) {
      const out = {};

      if (spec.fleet.repoURL || spec.fleet.credSecretRef?.name) {
        out.fleet = {};
        if (spec.fleet.repoURL) out.fleet.repoURL = spec.fleet.repoURL;
        if (spec.fleet.branch) out.fleet.branch = spec.fleet.branch;
        if (spec.fleet.authType) out.fleet.authType = spec.fleet.authType;
        if (spec.fleet.credSecretRef?.name) out.fleet.credSecretRef = spec.fleet.credSecretRef;
      }

      const ac = spec.applicationCollection;

      if (ac.userSecretRef?.name || ac.tokenSecretRef?.name || ac.categories.length) {
        out.applicationCollection = {};
        if (ac.userSecretRef?.name) out.applicationCollection.userSecretRef = ac.userSecretRef;
        if (ac.tokenSecretRef?.name) out.applicationCollection.tokenSecretRef = ac.tokenSecretRef;
        if (ac.categories.length) out.applicationCollection.categories = ac.categories;
      }

      const sr = spec.suseRegistry;

      if (sr.userSecretRef?.name || sr.tokenSecretRef?.name || sr.refreshIntervalMinutes !== 10) {
        out.suseRegistry = { refreshIntervalMinutes: sr.refreshIntervalMinutes };
        if (sr.userSecretRef?.name) out.suseRegistry.userSecretRef = sr.userSecretRef;
        if (sr.tokenSecretRef?.name) out.suseRegistry.tokenSecretRef = sr.tokenSecretRef;
      }

      const re = spec.registryEndpoints;

      if (re.suseRegistry || re.applicationCollection || re.applicationCollectionAPI) {
        out.registryEndpoints = {};
        if (re.suseRegistry) out.registryEndpoints.suseRegistry = re.suseRegistry;
        if (re.applicationCollection) out.registryEndpoints.applicationCollection = re.applicationCollection;
        if (re.applicationCollectionAPI) out.registryEndpoints.applicationCollectionAPI = re.applicationCollectionAPI;
      }

      if (spec.catalogDiscovery.applicationCollectionMode !== 'api') {
        out.catalogDiscovery = { applicationCollectionMode: spec.catalogDiscovery.applicationCollectionMode };
      }

      if (spec.imageRewrite.enabled || spec.imageRewrite.rules.length) {
        out.imageRewrite = {
          enabled: spec.imageRewrite.enabled,
          rules:   spec.imageRewrite.rules.filter((r) => r.match && r.replace),
        };
      }

      return out;
    },

    toggle(section) {
      this.expanded[section] = !this.expanded[section];
    },

    toSelectorValue(ref) {
      if (!ref?.name) return undefined;

      return { valueFrom: { secretKeyRef: ref } };
    },

    fromSelectorValue(val) {
      return val?.valueFrom?.secretKeyRef || null;
    },

    addRewriteRule() {
      this.spec.imageRewrite.rules.push({ match: '', replace: '' });
    },

    removeRewriteRule(index) {
      this.spec.imageRewrite.rules.splice(index, 1);
    },

    async readSecretData(name) {
      if (!name) return {};
      try {
        const res = await this.$store.dispatch('rancher/request', {
          url:     `/k8s/clusters/local/api/v1/namespaces/${OPERATOR_NAMESPACE}/secrets/${name}`,
          timeout: 10000,
        });
        // rancher/request returns the raw K8s object: res.data is the base64 data map.
        // If ever wrapped (Axios-style), res.data is the K8s Secret and res.data.data is the map.
        const secretObj = res?.kind === 'Secret' ? res : (res?.data?.kind === 'Secret' ? res.data : res);
        const dataMap = secretObj?.data || {};
        return Object.fromEntries(
          Object.entries(dataMap).map(([k, v]) => [k, atob(String(v))])
        );
      } catch { return {}; }
    },

    async ensureClusterReposWithCredentials() {
      const store = this.$store;
      const ac = this.spec.applicationCollection;
      const sr = this.spec.suseRegistry;

      // Read all unique secrets referenced in settings (deduped)
      const secretNames = [...new Set([
        ac.userSecretRef?.name,
        ac.tokenSecretRef?.name,
        sr.userSecretRef?.name,
        sr.tokenSecretRef?.name,
      ].filter(Boolean))];

      const secretCache = {};
      await Promise.all(secretNames.map(async (name) => {
        secretCache[name] = await this.readSecretData(name);
      }));

      const getVal = (ref) => ref?.name && ref?.key ? secretCache[ref.name]?.[ref.key] : null;

      // Extract credentials for a registry: prefer explicit refs, fall back to common key names
      const buildCreds = (userRef, tokenRef) => {
        const user  = getVal(userRef);
        const token = getVal(tokenRef);
        const data  = secretCache[tokenRef?.name] || secretCache[userRef?.name] || {};
        const username = user  || data.username || data.user  || data.login || data.email || null;
        const password = token || data.password || data.token || null;
        return username && password ? { username, password } : null;
      };

      const re = this.spec.registryEndpoints;
      const acUrl = re.applicationCollection || APP_COLLECTION_REPO_URL;
      const srUrl = re.suseRegistry         || SUSE_REGISTRY_REPO_URL;

      const tasks = [];
      const acCreds = buildCreds(ac.userSecretRef, ac.tokenSecretRef);
      const srCreds = buildCreds(sr.userSecretRef, sr.tokenSecretRef);
      if (acCreds) tasks.push(ensureClusterRepo(store, acUrl, acCreds));
      if (srCreds) tasks.push(ensureClusterRepo(store, srUrl, srCreds));
      await Promise.all(tasks);
    },

    async save(buttonDone) {
      try {
        this.errors = [];
        const data = await putSettings(this.buildCrdSpec(this.spec));

        this.spec = this.buildSpec(data.spec);
        buttonDone(true);

        // Fire-and-forget: ensure ClusterRepos exist on the local cluster with
        // credentials so the install wizard can list chart versions.
        this.ensureClusterReposWithCredentials()
          .catch(e => console.warn('[SUSE-AI] ClusterRepo setup (non-fatal):', e));
      } catch (e) {
        this.errors = [e?.message || String(e)];
        buttonDone(false);
      }
    },
  },
};
</script>

<template>
  <div>
    <Banner
      v-if="fetchErrorMessage"
      color="error"
      :label="fetchErrorMessage"
    />

    <Loading v-else-if="!loaded" />

    <div v-else>
      <h1>{{ t('suseai.pages.settings.title') }}</h1>

      <Banner
        v-if="notFound"
        color="info"
        :label="t('suseai.pages.settings.notConfigured')"
        class="mb-10"
      />

      <Banner
        v-for="(err, i) in errors"
        :key="i"
        color="error"
        :label="err"
      />

      <!-- SUSE Application Collection -->
      <div class="box mt-10">
        <div
          class="accordion-header"
          role="button"
          tabindex="0"
          @click="toggle('appCollection')"
          @keydown.space.enter.prevent="toggle('appCollection')"
        >
          <i :class="expanded.appCollection ? 'icon icon-chevron-down' : 'icon icon-chevron-right'" />
          <h2>{{ t('suseai.pages.settings.sections.appCollection.title') }}</h2>
        </div>

        <div
          v-if="expanded.appCollection"
          class="mt-15"
        >
          <p class="text-label mb-5">
            {{ t('suseai.pages.settings.sections.appCollection.userSecretRef.label') }}
          </p>
          <div class="row mb-15">
            <div class="col span-8">
              <SecretSelector
                :value="toSelectorValue(spec.applicationCollection.userSecretRef)"
                :namespace="settingsNamespace"
                :show-key-selector="true"
                :secret-name-label="t('suseai.pages.settings.sections.appCollection.userSecretRef.secretNameLabel')"
                :key-name-label="t('suseai.pages.settings.sections.appCollection.userSecretRef.keyNameLabel')"
                :mode="mode"
                @update:value="spec.applicationCollection.userSecretRef = fromSelectorValue($event)"
              />
            </div>
          </div>

          <p class="text-label mb-5">
            {{ t('suseai.pages.settings.sections.appCollection.tokenSecretRef.label') }}
          </p>
          <div class="row mb-15">
            <div class="col span-8">
              <SecretSelector
                :value="toSelectorValue(spec.applicationCollection.tokenSecretRef)"
                :namespace="settingsNamespace"
                :show-key-selector="true"
                :secret-name-label="t('suseai.pages.settings.sections.appCollection.tokenSecretRef.secretNameLabel')"
                :key-name-label="t('suseai.pages.settings.sections.appCollection.tokenSecretRef.keyNameLabel')"
                :mode="mode"
                @update:value="spec.applicationCollection.tokenSecretRef = fromSelectorValue($event)"
              />
            </div>
          </div>

          <div class="row">
            <div class="col span-8">
              <LabeledInput
                v-model:value="categoriesString"
                :label="t('suseai.pages.settings.sections.appCollection.categories.label')"
                :placeholder="t('suseai.pages.settings.sections.appCollection.categories.placeholder')"
                :mode="mode"
              />
            </div>
          </div>
        </div>
      </div>

      <!-- SUSE Registry -->
      <div class="box mt-10">
        <div
          class="accordion-header"
          role="button"
          tabindex="0"
          @click="toggle('suseRegistry')"
          @keydown.space.enter.prevent="toggle('suseRegistry')"
        >
          <i :class="expanded.suseRegistry ? 'icon icon-chevron-down' : 'icon icon-chevron-right'" />
          <h2>{{ t('suseai.pages.settings.sections.suseRegistry.title') }}</h2>
        </div>

        <div
          v-if="expanded.suseRegistry"
          class="mt-15"
        >
          <p class="text-label mb-5">
            {{ t('suseai.pages.settings.sections.suseRegistry.userSecretRef.label') }}
          </p>
          <div class="row mb-15">
            <div class="col span-8">
              <SecretSelector
                :value="toSelectorValue(spec.suseRegistry.userSecretRef)"
                :namespace="settingsNamespace"
                :show-key-selector="true"
                :secret-name-label="t('suseai.pages.settings.sections.suseRegistry.userSecretRef.secretNameLabel')"
                :key-name-label="t('suseai.pages.settings.sections.suseRegistry.userSecretRef.keyNameLabel')"
                :mode="mode"
                @update:value="spec.suseRegistry.userSecretRef = fromSelectorValue($event)"
              />
            </div>
          </div>

          <p class="text-label mb-5">
            {{ t('suseai.pages.settings.sections.suseRegistry.tokenSecretRef.label') }}
          </p>
          <div class="row mb-15">
            <div class="col span-8">
              <SecretSelector
                :value="toSelectorValue(spec.suseRegistry.tokenSecretRef)"
                :namespace="settingsNamespace"
                :show-key-selector="true"
                :secret-name-label="t('suseai.pages.settings.sections.suseRegistry.tokenSecretRef.secretNameLabel')"
                :key-name-label="t('suseai.pages.settings.sections.suseRegistry.tokenSecretRef.keyNameLabel')"
                :mode="mode"
                @update:value="spec.suseRegistry.tokenSecretRef = fromSelectorValue($event)"
              />
            </div>
          </div>

          <div class="row">
            <div class="col span-3">
              <LabeledInput
                :value="spec.suseRegistry.refreshIntervalMinutes"
                :label="t('suseai.pages.settings.sections.suseRegistry.refreshIntervalMinutes.label')"
                type="number"
                :min="1"
                :mode="mode"
                @update:value="spec.suseRegistry.refreshIntervalMinutes = Number($event) || 10"
              />
            </div>
          </div>
        </div>
      </div>

      <!-- Fleet / GitOps -->
      <div class="box mt-10">
        <div
          class="accordion-header"
          role="button"
          tabindex="0"
          @click="toggle('fleet')"
          @keydown.space.enter.prevent="toggle('fleet')"
        >
          <i :class="expanded.fleet ? 'icon icon-chevron-down' : 'icon icon-chevron-right'" />
          <h2>{{ t('suseai.pages.settings.sections.fleet.title') }}</h2>
        </div>

        <div
          v-if="expanded.fleet"
          class="mt-15"
        >
          <div class="row mb-10">
            <div class="col span-6">
              <LabeledInput
                v-model:value="spec.fleet.repoURL"
                :label="t('suseai.pages.settings.sections.fleet.repoURL.label')"
                :placeholder="t('suseai.pages.settings.sections.fleet.repoURL.placeholder')"
                :mode="mode"
              />
            </div>
            <div class="col span-6">
              <LabeledInput
                v-model:value="spec.fleet.branch"
                :label="t('suseai.pages.settings.sections.fleet.branch.label')"
                :placeholder="t('suseai.pages.settings.sections.fleet.branch.placeholder')"
                :mode="mode"
              />
            </div>
          </div>
          <div class="row mb-15">
            <div class="col span-4">
              <LabeledSelect
                v-model:value="spec.fleet.authType"
                :label="t('suseai.pages.settings.sections.fleet.authType.label')"
                :options="authTypeOptions"
                :mode="mode"
              />
            </div>
          </div>
          <div
            v-if="spec.fleet.authType"
            class="row"
          >
            <div class="col span-8">
              <p class="text-label mb-5">
                {{ t('suseai.pages.settings.sections.fleet.credSecretRef.label') }}
              </p>
              <SecretSelector
                :value="toSelectorValue(spec.fleet.credSecretRef)"
                :namespace="settingsNamespace"
                :show-key-selector="true"
                :secret-name-label="t('suseai.pages.settings.sections.fleet.credSecretRef.secretNameLabel')"
                :key-name-label="t('suseai.pages.settings.sections.fleet.credSecretRef.keyNameLabel')"
                :mode="mode"
                @update:value="spec.fleet.credSecretRef = fromSelectorValue($event)"
              />
            </div>
          </div>
        </div>
      </div>

      <!-- Advanced -->
      <div class="box mt-10">
        <div
          class="accordion-header"
          role="button"
          tabindex="0"
          @click="toggle('advanced')"
          @keydown.space.enter.prevent="toggle('advanced')"
        >
          <i :class="expanded.advanced ? 'icon icon-chevron-down' : 'icon icon-chevron-right'" />
          <h2>{{ t('suseai.pages.settings.sections.advanced.title') }}</h2>
        </div>

        <div
          v-if="expanded.advanced"
          class="mt-15"
        >
          <Banner
            color="warning"
            :label="t('suseai.pages.settings.sections.advanced.warning')"
            class="mb-15"
          />

          <h3 class="mb-10">
            {{ t('suseai.pages.settings.sections.advanced.registryEndpoints.title') }}
          </h3>
          <div class="row mb-10">
            <div class="col span-6">
              <LabeledInput
                v-model:value="spec.registryEndpoints.suseRegistry"
                :label="t('suseai.pages.settings.sections.advanced.registryEndpoints.suseRegistry.label')"
                :placeholder="t('suseai.pages.settings.sections.advanced.registryEndpoints.suseRegistry.placeholder')"
                :mode="mode"
              />
            </div>
          </div>
          <div class="row mb-10">
            <div class="col span-6">
              <LabeledInput
                v-model:value="spec.registryEndpoints.applicationCollection"
                :label="t('suseai.pages.settings.sections.advanced.registryEndpoints.applicationCollection.label')"
                :placeholder="t('suseai.pages.settings.sections.advanced.registryEndpoints.applicationCollection.placeholder')"
                :mode="mode"
              />
            </div>
          </div>
          <div class="row mb-20">
            <div class="col span-6">
              <LabeledInput
                v-model:value="spec.registryEndpoints.applicationCollectionAPI"
                :label="t('suseai.pages.settings.sections.advanced.registryEndpoints.applicationCollectionAPI.label')"
                :placeholder="t('suseai.pages.settings.sections.advanced.registryEndpoints.applicationCollectionAPI.placeholder')"
                :mode="mode"
              />
            </div>
          </div>

          <h3 class="mb-10">
            {{ t('suseai.pages.settings.sections.advanced.catalogDiscovery.title') }}
          </h3>
          <div class="row mb-20">
            <div class="col span-4">
              <LabeledSelect
                v-model:value="spec.catalogDiscovery.applicationCollectionMode"
                :label="t('suseai.pages.settings.sections.advanced.catalogDiscovery.applicationCollectionMode.label')"
                :options="catalogDiscoveryOptions"
                :mode="mode"
              />
            </div>
          </div>

          <h3 class="mb-10">
            {{ t('suseai.pages.settings.sections.advanced.imageRewrite.title') }}
          </h3>
          <div class="row mb-10">
            <div class="col span-12">
              <Checkbox
                v-model:value="spec.imageRewrite.enabled"
                :label="t('suseai.pages.settings.sections.advanced.imageRewrite.enabled.label')"
                :mode="mode"
              />
            </div>
          </div>
          <template v-if="spec.imageRewrite.enabled">
            <div
              v-for="(rule, i) in spec.imageRewrite.rules"
              :key="i"
              class="row mb-5"
            >
              <div class="col span-5">
                <LabeledInput
                  v-model:value="rule.match"
                  :label="i === 0 ? t('suseai.pages.settings.sections.advanced.imageRewrite.rules.match.label') : ''"
                  :placeholder="t('suseai.pages.settings.sections.advanced.imageRewrite.rules.match.placeholder')"
                  :mode="mode"
                />
              </div>
              <div class="col span-5">
                <LabeledInput
                  v-model:value="rule.replace"
                  :label="i === 0 ? t('suseai.pages.settings.sections.advanced.imageRewrite.rules.replace.label') : ''"
                  :placeholder="t('suseai.pages.settings.sections.advanced.imageRewrite.rules.replace.placeholder')"
                  :mode="mode"
                />
              </div>
              <div class="col span-2 trash-col">
                <button
                  type="button"
                  class="btn btn-sm role-link"
                  @click="removeRewriteRule(i)"
                >
                  <i class="icon icon-trash" />
                </button>
              </div>
            </div>
            <button
              type="button"
              class="btn btn-sm role-secondary mt-5"
              @click="addRewriteRule"
            >
              {{ t('suseai.pages.settings.sections.advanced.imageRewrite.rules.add') }}
            </button>
          </template>
        </div>
      </div>

      <div class="footer-bar">
        <AsyncButton
          :action-label="t('suseai.pages.settings.apply')"
          @click="save"
        />
      </div>
    </div>
  </div>
</template>

<style lang="scss" scoped>
.footer-bar {
  display: flex;
  justify-content: flex-end;
  margin-top: 20px;
}

.accordion-header {
  display: flex;
  align-items: center;
  gap: 10px;
  cursor: pointer;
  width: fit-content;

  h2 {
    margin: 0;
  }

  &:focus-visible {
    outline: var(--outline-width) solid var(--outline);
  }
}

.box {
  border-radius: var(--border-radius);
  border: 1px solid var(--border);
  padding: 15px;
}

.trash-col {
  display: flex;
  align-items: flex-end;
  padding-bottom: 4px;
}
</style>
