<script>
import AsyncButton      from '@shell/components/AsyncButton';
import { Banner }       from '@components/Banner';
import Loading          from '@shell/components/Loading';
import { LabeledInput } from '@components/Form/LabeledInput';
import LabeledSelect    from '@shell/components/form/LabeledSelect';
import { Checkbox }     from '@components/Form/Checkbox';
import SecretSelector   from '@shell/components/form/SecretSelector';
import { getSettings, putSettings, validateCredentials } from '../utils/operator-api';
import { TIMEOUT_VALUES } from '../utils/constants';
import { loadOperatorConfig, getOperatorNamespace } from '../utils/operator-config';
import { ensureClusterRepo } from '../services/rancher-apps';
import {
  APP_COLLECTION_REPO_URL,
  SUSE_REGISTRY_REPO_URL,
  NVIDIA_REPO_URL,
  NVIDIA_BLUEPRINT_REPO_URL,
  resolveRegistryEndpoints,
  registryEndpointOverrides,
} from '../services/registry-endpoints';
import {
  mintOperatorToken, ensureTokenSecret, deleteToken, requestErrorMessage,
  TOKEN_EXPIRES_ANNOTATION, TOKEN_NAME_ANNOTATION,
  DEFAULT_TOKEN_SECRET_NAME, DEFAULT_TOKEN_SECRET_KEY,
} from '../services/rancher-token';

function createEmptySpec() {
  return {
    fleet:                 { repoURL: '', branch: 'main', authType: '', username: '', credSecretRef: null, caBundleSecretRef: null },
    applicationCollection: { userSecretRef: null, tokenSecretRef: null, caBundleSecretRef: null, categories: [] },
    suseRegistry:          { userSecretRef: null, tokenSecretRef: null, caBundleSecretRef: null, refreshIntervalMinutes: 10 },
    nvidia:                { userSecretRef: null, tokenSecretRef: null, caBundleSecretRef: null },
    rancherCatalog:        { url: '', tokenSecretRef: null, caBundleSecretRef: null, insecureSkipVerify: false },
    registryEndpoints:     resolveRegistryEndpoints(),
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
    await loadOperatorConfig();
    try {
      const data = await getSettings();

      this.spec   = this.buildSpec(data.spec);
      this.loaded = true;
      await this.loadTokenState();
    } catch (e) {
      if (e?.status === 404) {
        this.notFound = true;
        this.loaded   = true;
        await this.loadTokenState();
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
      tokenState:      { expiresAt: '', tokenName: '', configured: false, loaded: false },
      authorizeError:  '',
      showAdvanced:    { rancherCatalog: false },
      expanded:          {
        fleet:          false,
        appCollection:  true,
        suseRegistry:   false,
        nvidia:         false,
        rancherCatalog: false,
      },
      testResults: {
        applicationCollection: null,
        suseRegistry:          null,
        nvidia:                null,
        gitops:                null,
        rancherCatalog:        null,
      },
    };
  },

  computed: {
    settingsNamespace() {
      return getOperatorNamespace();
    },

    authTypeOptions() {
      return [
        { label: this.t('suseai.pages.settings.sections.fleet.authType.options.none'), value: '' },
        { label: this.t('suseai.pages.settings.sections.fleet.authType.options.token'), value: 'token' },
        { label: this.t('suseai.pages.settings.sections.fleet.authType.options.basic'), value: 'basic' },
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

    // 'expired' | 'expiring' | '' — drives the banner. Fourteen days of notice,
    // because Rancher clamps token TTL to auth-token-max-ttl-minutes (90 days by
    // default) and every token therefore reaches this point.
    tokenExpiryStatus() {
      const raw = this.tokenState.expiresAt;
      if (!raw) return '';
      const expires = new Date(raw).getTime();
      if (Number.isNaN(expires)) return '';
      const remainingDays = (expires - Date.now()) / 86400000;
      if (remainingDays <= 0) return 'expired';
      if (remainingDays <= 14) return 'expiring';
      return '';
    },
  },

  watch: {
    loaded(val) {
      if (!val) return;
      const section = this.$route?.query?.section;

      if (section && this.expanded[section] !== undefined) {
        this.openSection(section);
      }
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
          username:      crdSpec.fleet.username || '',
          credSecretRef: crdSpec.fleet.credSecretRef || null,
          caBundleSecretRef: crdSpec.fleet.caBundleSecretRef || null,
        };
      }
      if (crdSpec.applicationCollection) {
        s.applicationCollection = {
          userSecretRef:     crdSpec.applicationCollection.userSecretRef || null,
          tokenSecretRef:    crdSpec.applicationCollection.tokenSecretRef || null,
          caBundleSecretRef: crdSpec.applicationCollection.caBundleSecretRef || null,
          categories:        crdSpec.applicationCollection.categories || [],
        };
      }
      if (crdSpec.suseRegistry) {
        s.suseRegistry = {
          userSecretRef:          crdSpec.suseRegistry.userSecretRef || null,
          tokenSecretRef:         crdSpec.suseRegistry.tokenSecretRef || null,
          caBundleSecretRef:      crdSpec.suseRegistry.caBundleSecretRef || null,
          refreshIntervalMinutes: crdSpec.suseRegistry.refreshIntervalMinutes ?? 10,
        };
      }
      if (crdSpec.nvidia) {
        s.nvidia = {
          userSecretRef:     crdSpec.nvidia.userSecretRef || null,
          tokenSecretRef:    crdSpec.nvidia.tokenSecretRef || null,
          caBundleSecretRef: crdSpec.nvidia.caBundleSecretRef || null,
        };
      }
      if (crdSpec.rancherCatalog) {
        s.rancherCatalog = {
          url:                crdSpec.rancherCatalog.url || '',
          tokenSecretRef:     crdSpec.rancherCatalog.tokenSecretRef || null,
          caBundleSecretRef:  crdSpec.rancherCatalog.caBundleSecretRef || null,
          insecureSkipVerify: !!crdSpec.rancherCatalog.insecureSkipVerify,
        };
      }
      s.registryEndpoints = resolveRegistryEndpoints(crdSpec.registryEndpoints);
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

      if (spec.fleet.repoURL || spec.fleet.credSecretRef?.name || spec.fleet.caBundleSecretRef?.name) {
        out.fleet = {};
        if (spec.fleet.repoURL) out.fleet.repoURL = spec.fleet.repoURL;
        if (spec.fleet.branch) out.fleet.branch = spec.fleet.branch;
        if (spec.fleet.authType) out.fleet.authType = spec.fleet.authType;
        if (spec.fleet.username) out.fleet.username = spec.fleet.username;
        if (spec.fleet.credSecretRef?.name) out.fleet.credSecretRef = spec.fleet.credSecretRef;
        if (spec.fleet.caBundleSecretRef?.name) out.fleet.caBundleSecretRef = spec.fleet.caBundleSecretRef;
      }

      const ac = spec.applicationCollection;

      if (ac.userSecretRef?.name || ac.tokenSecretRef?.name || ac.caBundleSecretRef?.name || ac.categories.length) {
        out.applicationCollection = {};
        if (ac.userSecretRef?.name) out.applicationCollection.userSecretRef = ac.userSecretRef;
        if (ac.tokenSecretRef?.name) out.applicationCollection.tokenSecretRef = ac.tokenSecretRef;
        if (ac.caBundleSecretRef?.name) out.applicationCollection.caBundleSecretRef = ac.caBundleSecretRef;
        if (ac.categories.length) out.applicationCollection.categories = ac.categories;
      }

      const sr = spec.suseRegistry;

      if (sr.userSecretRef?.name || sr.tokenSecretRef?.name || sr.caBundleSecretRef?.name || sr.refreshIntervalMinutes !== 10) {
        out.suseRegistry = { refreshIntervalMinutes: sr.refreshIntervalMinutes };
        if (sr.userSecretRef?.name) out.suseRegistry.userSecretRef = sr.userSecretRef;
        if (sr.tokenSecretRef?.name) out.suseRegistry.tokenSecretRef = sr.tokenSecretRef;
        if (sr.caBundleSecretRef?.name) out.suseRegistry.caBundleSecretRef = sr.caBundleSecretRef;
      }

      const nv = spec.nvidia;

      if (nv.userSecretRef?.name || nv.tokenSecretRef?.name || nv.caBundleSecretRef?.name) {
        out.nvidia = {};
        if (nv.userSecretRef?.name) out.nvidia.userSecretRef = nv.userSecretRef;
        if (nv.tokenSecretRef?.name) out.nvidia.tokenSecretRef = nv.tokenSecretRef;
        if (nv.caBundleSecretRef?.name) out.nvidia.caBundleSecretRef = nv.caBundleSecretRef;
      }

      const rc = spec.rancherCatalog;

      if (rc.url || rc.tokenSecretRef?.name || rc.caBundleSecretRef?.name || rc.insecureSkipVerify) {
        out.rancherCatalog = {};
        if (rc.url) out.rancherCatalog.url = rc.url;
        if (rc.tokenSecretRef?.name) out.rancherCatalog.tokenSecretRef = rc.tokenSecretRef;
        if (rc.caBundleSecretRef?.name) out.rancherCatalog.caBundleSecretRef = rc.caBundleSecretRef;
        if (rc.insecureSkipVerify) out.rancherCatalog.insecureSkipVerify = true;
      }

      const re = spec.registryEndpoints;

      const endpointOverrides = registryEndpointOverrides(re);

      if (Object.keys(endpointOverrides).length) {
        out.registryEndpoints = endpointOverrides;
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

    openSection(section) {
      this.expanded[section] = true;
      this.$nextTick(() => {
        document.getElementById(section)?.scrollIntoView({ behavior: 'smooth', block: 'start' });
      });
    },

    toSelectorValue(ref) {
      if (!ref?.name) return undefined;

      return { valueFrom: { secretKeyRef: ref } };
    },

    fromSelectorValue(val) {
      return val?.valueFrom?.secretKeyRef || null;
    },

    async readSecretData(name) {
      if (!name) return {};
      try {
        const res = await this.$store.dispatch('rancher/request', {
          url:     `/k8s/clusters/local/api/v1/namespaces/${ getOperatorNamespace() }/secrets/${ name }`,
          timeout: TIMEOUT_VALUES.CLUSTER,
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
      const nv = this.spec.nvidia;

      // Read all unique secrets referenced in settings (deduped)
      const secretNames = [...new Set([
        ac.userSecretRef?.name,
        ac.tokenSecretRef?.name,
        ac.caBundleSecretRef?.name,
        sr.userSecretRef?.name,
        sr.tokenSecretRef?.name,
        sr.caBundleSecretRef?.name,
        nv.userSecretRef?.name,
        nv.tokenSecretRef?.name,
        nv.caBundleSecretRef?.name,
      ].filter(Boolean))];

      const secretCache = {};
      await Promise.all(secretNames.map(async (name) => {
        secretCache[name] = await this.readSecretData(name);
      }));

      const getVal = (ref) => ref?.name && ref?.key ? secretCache[ref.name]?.[ref.key] : null;

      // Extract credentials for a registry: prefer explicit refs, fall back to common key names
      const buildCreds = (userRef, tokenRef, caBundleRef) => {
        const user  = getVal(userRef);
        const token = getVal(tokenRef);
        const data  = secretCache[tokenRef?.name] || secretCache[userRef?.name] || {};
        const username = user  || data.username || data.user  || data.login || data.email || null;
        const password = token || data.password || data.token || null;
        const cacerts = getVal(caBundleRef);
        if (this.secretRefComplete(caBundleRef) && !cacerts) {
          throw new Error(
            `CA bundle secret ${ caBundleRef.name } does not contain key ${ caBundleRef.key }`
          );
        }
        return username && password ? { username, password, ...(cacerts ? { cacerts } : {}) } : null;
      };

      const re = this.spec.registryEndpoints;
      const acUrl = re.applicationCollection || APP_COLLECTION_REPO_URL;
      const srUrl = re.suseRegistry         || SUSE_REGISTRY_REPO_URL;

      const tasks = [];
      const acCreds = buildCreds(ac.userSecretRef, ac.tokenSecretRef, ac.caBundleSecretRef);
      const srCreds = buildCreds(sr.userSecretRef, sr.tokenSecretRef, sr.caBundleSecretRef);
      if (acCreds) tasks.push(ensureClusterRepo(store, acUrl, acCreds));
      if (srCreds) tasks.push(ensureClusterRepo(store, srUrl, srCreds));

      // NVIDIA chart repos.
      //  - Air-gapped (registryEndpoints.nvidia set): stable nvidia and
      //    nvidia-blueprints identities at the mirrored OCI URL. Credentials are
      //    attached when configured; an unauthenticated mirror is also supported.
      //  - Connected (registryEndpoints.nvidia empty): the two PUBLIC HTTPS NGC repos, created
      //    when NVIDIA credentials are configured (the creds signal that NVIDIA is in use).
      const nvHasRefs = !!(nv.userSecretRef?.name && nv.tokenSecretRef?.name);
      if (re.nvidia) {
        const nvCreds = buildCreds(nv.userSecretRef, nv.tokenSecretRef, nv.caBundleSecretRef);
        // If secret refs are set but no usable credentials could be read, surface it rather
        // than silently creating an unauthenticated repo against a gated mirror.
        if (nvHasRefs && !nvCreds) {
          throw new Error(
            'NVIDIA secret references are set but no usable username/token could be read from them. ' +
            'Check the selected secret and key names.'
          );
        }
        tasks.push(
          ensureClusterRepo(store, re.nvidia, nvCreds || undefined, 'nvidia'),
          ensureClusterRepo(store, re.nvidia, nvCreds || undefined, 'nvidia-blueprints'),
        );
      } else if (nvHasRefs) {
        tasks.push(
          ensureClusterRepo(store, NVIDIA_REPO_URL),
          ensureClusterRepo(store, NVIDIA_BLUEPRINT_REPO_URL),
        );
      }

      await Promise.all(tasks);
    },

    // Returns whether the save reached the operator. authorizeRancher needs to
    // know: it must not revoke the token it replaced unless the reference to the
    // new one was actually persisted.
    async save(buttonDone) {
      try {
        this.errors = [];
        const data = await putSettings(this.buildCrdSpec(this.spec));

        this.spec = this.buildSpec(data.spec);
        buttonDone(true);

        // Settings are saved; now ensure ClusterRepos exist on the local cluster with
        // credentials so the install wizard can list chart versions. This runs after the
        // save succeeds, so on failure we surface a banner rather than failing the save.
        this.ensureClusterReposWithCredentials()
          .catch((e) => {
            console.warn('[SUSE-AI] ClusterRepo setup failed:', e);
            this.errors = [`Settings saved, but chart repository setup failed: ${ e?.message || e }`];
          });

        return true;
      } catch (e) {
        this.errors = [e?.message || String(e)];
        buttonDone(false);

        return false;
      }
    },

    secretRefComplete(ref) {
      return !!(ref && ref.name && ref.key);
    },

    // canTest gates the per-section Test button so it only fires with inputs the
    // probe can actually use. Registries need both secret name+key on each ref;
    // gitops needs a repoURL. Authentication is optional for an anonymous
    // repository; HTTPS CA trust is tested when configured.
    canTest(target) {
      if (target === 'gitops') {
        return !!this.spec.fleet.repoURL;
      }
      // rancherCatalog authenticates with a single API token (no username), so it
      // only needs a complete token secret ref.
      if (target === 'rancherCatalog') {
        return this.secretRefComplete(this.spec.rancherCatalog.tokenSecretRef);
      }
      const sec = this.spec[target];
      return this.secretRefComplete(sec?.userSecretRef) && this.secretRefComplete(sec?.tokenSecretRef);
    },

    async runTest(target, override, buttonDone) {
      try {
        const resp = await validateCredentials({ targets: [target], overrides: { [target]: override } });
        const res = (resp.results || []).find((r) => r.target === target) || null;
        this.testResults[target] = res;
        // skipped is a neutral outcome (nothing was probed), not a failure — don't
        // drive the button's error animation for it.
        buttonDone(!!res && res.status !== 'failed' && res.status !== 'error');
      } catch (e) {
        this.testResults[target] = { target, status: 'error', message: e?.message || String(e) };
        buttonDone(false);
      }
    },

    testResultText(target) {
      const r = this.testResults[target];
      if (!r) return '';
      const label = this.t(`suseai.pages.settings.test.${ r.status }`);
      if (r.status === 'ok') {
        return label + (r.host ? ` — ${ r.host }` : '') + (r.latencyMs != null ? ` (${ r.latencyMs } ms)` : '');
      }
      return r.message ? `${ label }: ${ r.message }` : label;
    },

    testResultClass(target) {
      const r = this.testResults[target];
      if (!r) return '';
      return r.status === 'ok' ? 'text-success' : (r.status === 'skipped' ? 'text-muted' : 'text-error');
    },

    // Reads the expiry annotations off the token Secret so the section can show
    // state without a second round trip. Absent Secret is not an error: it just
    // means "not authorized yet".
    async loadTokenState() {
      const ref = this.spec.rancherCatalog.tokenSecretRef;
      if (!ref?.name) {
        this.tokenState = { expiresAt: '', tokenName: '', configured: false, loaded: true };
        return;
      }
      try {
        const sec = await this.$store.dispatch('rancher/request', {
          url: `/k8s/clusters/local/api/v1/namespaces/${ this.settingsNamespace }/secrets/${ ref.name }`,
        });
        const ann = sec?.metadata?.annotations || {};
        this.tokenState = {
          expiresAt:   ann[TOKEN_EXPIRES_ANNOTATION] || '',
          tokenName:   ann[TOKEN_NAME_ANNOTATION] || '',
          configured:  true,
          loaded:      true,
        };
      } catch {
        this.tokenState = { expiresAt: '', tokenName: '', configured: false, loaded: true };
      }
    },

    // Mints a fresh token, stores it, points Settings at it, and removes the
    // token it replaced. Idempotent by design: pressing the button again is the
    // remedy for both first-time setup and expiry, and leaves exactly one live
    // token behind.
    async authorizeRancher(buttonDone) {
      this.authorizeError = '';
      const previous = this.tokenState.tokenName;
      try {
        const minted = await mintOperatorToken(this.$store);
        if (!minted.value) throw new Error('Rancher returned no bearer token');

        await ensureTokenSecret(this.$store, this.settingsNamespace, DEFAULT_TOKEN_SECRET_NAME, minted);

        // The Secret now holds this token, so it is the current one whether or not the
        // save below succeeds. Recording it here keeps the status line honest and makes
        // a retry delete the token it actually replaced.
        this.tokenState = { expiresAt: minted.expiresAt, tokenName: minted.tokenName, configured: true, loaded: true };

        this.spec.rancherCatalog.tokenSecretRef = {
          name: DEFAULT_TOKEN_SECRET_NAME,
          key:  DEFAULT_TOKEN_SECRET_KEY,
        };
        // eslint-disable-next-line @typescript-eslint/no-empty-function
        const saved = await this.save(() => {});

        if (!saved) {
          throw new Error(this.errors[0] || 'Saving Settings failed');
        }
        // save() replaces this.spec from the operator's response. An operator
        // whose Settings CRD predates rancherCatalog prunes the field, so confirm
        // the reference survived the round trip before revoking anything.
        if (this.spec.rancherCatalog?.tokenSecretRef?.name !== DEFAULT_TOKEN_SECRET_NAME) {
          throw new Error('The operator did not store the token reference; check that it is up to date');
        }

        // Only after the new token is committed, so a failure above never leaves
        // the operator with no working credential.
        if (previous && previous !== minted.tokenName) {
          await deleteToken(this.$store, previous);
        }
        buttonDone(true);
      } catch (e) {
        this.authorizeError = requestErrorMessage(e);
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
          <div class="row mb-15">
            <div class="col span-8">
              <LabeledInput
                v-model:value="spec.registryEndpoints.applicationCollection"
                :label="t('suseai.pages.settings.sections.appCollection.endpoint.label')"
                :placeholder="t('suseai.pages.settings.sections.appCollection.endpoint.placeholder')"
                :mode="mode"
              />
            </div>
          </div>

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

          <p class="text-label mb-5">
            {{ t('suseai.pages.settings.sections.appCollection.caBundleSecretRef.label') }}
          </p>
          <div class="row mb-15">
            <div class="col span-8">
              <SecretSelector
                :value="toSelectorValue(spec.applicationCollection.caBundleSecretRef)"
                :namespace="settingsNamespace"
                :show-key-selector="true"
                :secret-name-label="t('suseai.pages.settings.sections.appCollection.caBundleSecretRef.secretNameLabel')"
                :key-name-label="t('suseai.pages.settings.sections.appCollection.caBundleSecretRef.keyNameLabel')"
                :mode="mode"
                @update:value="spec.applicationCollection.caBundleSecretRef = fromSelectorValue($event)"
              />
            </div>
          </div>

          <!-- Hidden for MVP -- see issue: hide non-MVP Settings fields -->
          <div
            v-if="false"
            class="row"
          >
            <div class="col span-8">
              <LabeledInput
                v-model:value="categoriesString"
                :label="t('suseai.pages.settings.sections.appCollection.categories.label')"
                :placeholder="t('suseai.pages.settings.sections.appCollection.categories.placeholder')"
                :mode="mode"
              />
            </div>
          </div>

          <div class="row mt-10">
            <div class="col span-12">
              <AsyncButton
                mode="edit"
                :action-label="t('suseai.pages.settings.test.button')"
                :disabled="!canTest('applicationCollection')"
                @click="cb => runTest('applicationCollection', { url: spec.registryEndpoints.applicationCollection, userSecretRef: spec.applicationCollection.userSecretRef, tokenSecretRef: spec.applicationCollection.tokenSecretRef, caBundleSecretRef: spec.applicationCollection.caBundleSecretRef }, cb)"
              />
              <span
                v-if="testResults.applicationCollection"
                :class="testResultClass('applicationCollection')"
                class="ml-10"
              >{{ testResultText('applicationCollection') }}</span>
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
          <div class="row mb-15">
            <div class="col span-8">
              <LabeledInput
                v-model:value="spec.registryEndpoints.suseRegistry"
                :label="t('suseai.pages.settings.sections.suseRegistry.endpoint.label')"
                :placeholder="t('suseai.pages.settings.sections.suseRegistry.endpoint.placeholder')"
                :mode="mode"
              />
            </div>
          </div>

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

          <p class="text-label mb-5">
            {{ t('suseai.pages.settings.sections.suseRegistry.caBundleSecretRef.label') }}
          </p>
          <div class="row mb-15">
            <div class="col span-8">
              <SecretSelector
                :value="toSelectorValue(spec.suseRegistry.caBundleSecretRef)"
                :namespace="settingsNamespace"
                :show-key-selector="true"
                :secret-name-label="t('suseai.pages.settings.sections.suseRegistry.caBundleSecretRef.secretNameLabel')"
                :key-name-label="t('suseai.pages.settings.sections.suseRegistry.caBundleSecretRef.keyNameLabel')"
                :mode="mode"
                @update:value="spec.suseRegistry.caBundleSecretRef = fromSelectorValue($event)"
              />
            </div>
          </div>

          <!-- Hidden for MVP -- see issue: hide non-MVP Settings fields -->
          <div
            v-if="false"
            class="row"
          >
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

          <div class="row mt-10">
            <div class="col span-12">
              <AsyncButton
                mode="edit"
                :action-label="t('suseai.pages.settings.test.button')"
                :disabled="!canTest('suseRegistry')"
                @click="cb => runTest('suseRegistry', { url: spec.registryEndpoints.suseRegistry, userSecretRef: spec.suseRegistry.userSecretRef, tokenSecretRef: spec.suseRegistry.tokenSecretRef, caBundleSecretRef: spec.suseRegistry.caBundleSecretRef }, cb)"
              />
              <span
                v-if="testResults.suseRegistry"
                :class="testResultClass('suseRegistry')"
                class="ml-10"
              >{{ testResultText('suseRegistry') }}</span>
            </div>
          </div>
        </div>
      </div>

      <!-- NVIDIA -->
      <div class="box mt-10">
        <div
          class="accordion-header"
          role="button"
          tabindex="0"
          @click="toggle('nvidia')"
          @keydown.space.enter.prevent="toggle('nvidia')"
        >
          <i :class="expanded.nvidia ? 'icon icon-chevron-down' : 'icon icon-chevron-right'" />
          <h2>{{ t('suseai.pages.settings.sections.nvidia.title') }}</h2>
        </div>

        <div
          v-if="expanded.nvidia"
          class="mt-15"
        >
          <p class="text-muted mb-15">
            {{ t('suseai.pages.settings.sections.nvidia.description') }}
          </p>

          <div class="row mb-15">
            <div class="col span-8">
              <LabeledInput
                v-model:value="spec.registryEndpoints.nvidia"
                :label="t('suseai.pages.settings.sections.nvidia.endpoint.label')"
                :placeholder="t('suseai.pages.settings.sections.nvidia.endpoint.placeholder')"
                :mode="mode"
              />
            </div>
          </div>

          <p class="text-label mb-5">
            {{ t('suseai.pages.settings.sections.nvidia.userSecretRef.label') }}
          </p>
          <div class="row mb-15">
            <div class="col span-8">
              <SecretSelector
                :value="toSelectorValue(spec.nvidia.userSecretRef)"
                :namespace="settingsNamespace"
                :show-key-selector="true"
                :secret-name-label="t('suseai.pages.settings.sections.nvidia.userSecretRef.secretNameLabel')"
                :key-name-label="t('suseai.pages.settings.sections.nvidia.userSecretRef.keyNameLabel')"
                :mode="mode"
                @update:value="spec.nvidia.userSecretRef = fromSelectorValue($event)"
              />
            </div>
          </div>

          <p class="text-label mb-5">
            {{ t('suseai.pages.settings.sections.nvidia.tokenSecretRef.label') }}
          </p>
          <div class="row mb-15">
            <div class="col span-8">
              <SecretSelector
                :value="toSelectorValue(spec.nvidia.tokenSecretRef)"
                :namespace="settingsNamespace"
                :show-key-selector="true"
                :secret-name-label="t('suseai.pages.settings.sections.nvidia.tokenSecretRef.secretNameLabel')"
                :key-name-label="t('suseai.pages.settings.sections.nvidia.tokenSecretRef.keyNameLabel')"
                :mode="mode"
                @update:value="spec.nvidia.tokenSecretRef = fromSelectorValue($event)"
              />
            </div>
          </div>

          <p class="text-label mb-5">
            {{ t('suseai.pages.settings.sections.nvidia.caBundleSecretRef.label') }}
          </p>
          <div class="row mb-15">
            <div class="col span-8">
              <SecretSelector
                :value="toSelectorValue(spec.nvidia.caBundleSecretRef)"
                :namespace="settingsNamespace"
                :show-key-selector="true"
                :secret-name-label="t('suseai.pages.settings.sections.nvidia.caBundleSecretRef.secretNameLabel')"
                :key-name-label="t('suseai.pages.settings.sections.nvidia.caBundleSecretRef.keyNameLabel')"
                :mode="mode"
                @update:value="spec.nvidia.caBundleSecretRef = fromSelectorValue($event)"
              />
            </div>
          </div>

          <div class="row mt-10">
            <div class="col span-12">
              <AsyncButton
                mode="edit"
                :action-label="t('suseai.pages.settings.test.button')"
                :disabled="!canTest('nvidia')"
                @click="cb => runTest('nvidia', { url: spec.registryEndpoints.nvidia, userSecretRef: spec.nvidia.userSecretRef, tokenSecretRef: spec.nvidia.tokenSecretRef, caBundleSecretRef: spec.nvidia.caBundleSecretRef }, cb)"
              />
              <span
                v-if="testResults.nvidia"
                :class="testResultClass('nvidia')"
                class="ml-10"
              >{{ testResultText('nvidia') }}</span>
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
            <div
              v-if="spec.fleet.authType"
              class="col span-4"
            >
              <LabeledInput
                v-model:value="spec.fleet.username"
                :label="t('suseai.pages.settings.sections.fleet.username.label')"
                :placeholder="t('suseai.pages.settings.sections.fleet.username.placeholder')"
                :mode="mode"
              />
            </div>
          </div>
          <div
            v-if="spec.fleet.authType"
            class="row mb-15"
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

          <div class="row">
            <div class="col span-8">
              <p class="text-label mb-5">
                {{ t('suseai.pages.settings.sections.fleet.caBundleSecretRef.label') }}
              </p>
              <SecretSelector
                :value="toSelectorValue(spec.fleet.caBundleSecretRef)"
                :namespace="settingsNamespace"
                :show-key-selector="true"
                :secret-name-label="t('suseai.pages.settings.sections.fleet.caBundleSecretRef.secretNameLabel')"
                :key-name-label="t('suseai.pages.settings.sections.fleet.caBundleSecretRef.keyNameLabel')"
                :mode="mode"
                @update:value="spec.fleet.caBundleSecretRef = fromSelectorValue($event)"
              />
            </div>
          </div>

          <div class="row mt-10">
            <div class="col span-12">
              <AsyncButton
                mode="edit"
                :action-label="t('suseai.pages.settings.test.button')"
                :disabled="!canTest('gitops')"
                @click="cb => runTest('gitops', { repoURL: spec.fleet.repoURL, branch: spec.fleet.branch, authType: spec.fleet.authType, username: spec.fleet.username, credSecretRef: spec.fleet.credSecretRef, caBundleSecretRef: spec.fleet.caBundleSecretRef }, cb)"
              />
              <span
                v-if="testResults.gitops"
                :class="testResultClass('gitops')"
                class="ml-10"
              >{{ testResultText('gitops') }}</span>
            </div>
          </div>
        </div>
      </div>

      <!-- Rancher API Access -->
      <div id="rancherCatalog" class="box mt-10">
        <div
          class="accordion-header"
          role="button"
          tabindex="0"
          @click="toggle('rancherCatalog')"
          @keydown.space.enter.prevent="toggle('rancherCatalog')"
        >
          <i :class="expanded.rancherCatalog ? 'icon icon-chevron-down' : 'icon icon-chevron-right'" />
          <h2>{{ t('suseai.pages.settings.sections.rancherCatalog.title') }}</h2>
        </div>

        <div
          v-if="expanded.rancherCatalog"
          class="mt-15"
        >
          <p class="text-muted mb-15">
            {{ t('suseai.pages.settings.sections.rancherCatalog.description', {}, true) }}
          </p>

          <Banner
            v-if="tokenExpiryStatus"
            :color="tokenExpiryStatus === 'expired' ? 'error' : 'warning'"
            :label="tokenExpiryStatus === 'expired'
              ? t('suseai.pages.settings.sections.rancherCatalog.tokenExpired', { expires: new Date(tokenState.expiresAt).toLocaleDateString() }, true)
              : t('suseai.pages.settings.sections.rancherCatalog.tokenExpiring', { expires: new Date(tokenState.expiresAt).toLocaleDateString() }, true)"
          />

          <div class="row mb-10">
            <div class="col span-12">
              <span v-if="tokenState.loaded && tokenState.configured && !tokenExpiryStatus && tokenState.expiresAt" class="text-success">
                {{ t('suseai.pages.settings.sections.rancherCatalog.authorized', { expires: new Date(tokenState.expiresAt).toLocaleDateString() }, true) }}
              </span>
              <span v-else-if="tokenState.loaded && tokenState.configured && !tokenExpiryStatus && !tokenState.expiresAt" class="text-success">
                {{ t('suseai.pages.settings.sections.rancherCatalog.authorizedNoExpiry', {}, true) }}
              </span>
              <span v-else-if="tokenState.loaded && !tokenState.configured" class="text-warning">
                {{ t('suseai.pages.settings.sections.rancherCatalog.notAuthorized', {}, true) }}
              </span>
            </div>
          </div>

          <div class="row mb-10">
            <div class="col span-12">
              <AsyncButton
                :mode="tokenState.configured ? 'edit' : 'apply'"
                :action-label="tokenState.configured
                  ? t('suseai.pages.settings.sections.rancherCatalog.reauthorize', {}, true)
                  : t('suseai.pages.settings.sections.rancherCatalog.authorize', {}, true)"
                :waiting-label="t('suseai.pages.settings.sections.rancherCatalog.authorizing', {}, true)"
                @click="authorizeRancher"
              />
              <p class="text-muted mt-5">
                {{ t('suseai.pages.settings.sections.rancherCatalog.authorizeHelp', {}, true) }}
              </p>
            </div>
          </div>

          <Banner
            v-if="authorizeError"
            color="error"
            :label="t('suseai.pages.settings.sections.rancherCatalog.authorizeFailed', { error: authorizeError }, true)"
          />

          <div class="row mb-10">
            <div class="col span-12">
              <a
                href="#"
                @click.prevent="showAdvanced.rancherCatalog = !showAdvanced.rancherCatalog"
              >{{ t('suseai.pages.settings.sections.rancherCatalog.advanced', {}, true) }}</a>
            </div>
          </div>

          <template v-if="showAdvanced.rancherCatalog">
            <p class="text-label mb-5">
              {{ t('suseai.pages.settings.sections.rancherCatalog.tokenSecretRef.label') }}
            </p>
            <div class="row mb-15">
              <div class="col span-8">
                <SecretSelector
                  :value="toSelectorValue(spec.rancherCatalog.tokenSecretRef)"
                  :namespace="settingsNamespace"
                  :show-key-selector="true"
                  :secret-name-label="t('suseai.pages.settings.sections.rancherCatalog.tokenSecretRef.secretNameLabel')"
                  :key-name-label="t('suseai.pages.settings.sections.rancherCatalog.tokenSecretRef.keyNameLabel')"
                  :mode="mode"
                  @update:value="spec.rancherCatalog.tokenSecretRef = fromSelectorValue($event)"
                />
              </div>
            </div>

            <div class="row mb-15">
              <div class="col span-8">
                <LabeledInput
                  v-model:value="spec.rancherCatalog.url"
                  :label="t('suseai.pages.settings.sections.rancherCatalog.url.label')"
                  :placeholder="t('suseai.pages.settings.sections.rancherCatalog.url.placeholder')"
                  :mode="mode"
                />
              </div>
            </div>

            <p class="text-label mb-5">
              {{ t('suseai.pages.settings.sections.rancherCatalog.caBundleSecretRef.label') }}
            </p>
            <div class="row mb-15">
              <div class="col span-8">
                <SecretSelector
                  :value="toSelectorValue(spec.rancherCatalog.caBundleSecretRef)"
                  :namespace="settingsNamespace"
                  :show-key-selector="true"
                  :secret-name-label="t('suseai.pages.settings.sections.rancherCatalog.caBundleSecretRef.secretNameLabel')"
                  :key-name-label="t('suseai.pages.settings.sections.rancherCatalog.caBundleSecretRef.keyNameLabel')"
                  :mode="mode"
                  @update:value="spec.rancherCatalog.caBundleSecretRef = fromSelectorValue($event)"
                />
              </div>
            </div>

            <div class="row mb-10">
              <div class="col span-12">
                <Checkbox
                  v-model:value="spec.rancherCatalog.insecureSkipVerify"
                  :label="t('suseai.pages.settings.sections.rancherCatalog.insecureSkipVerify.label')"
                  :mode="mode"
                />
              </div>
            </div>
          </template>

          <div class="row mt-10">
            <div class="col span-12">
              <AsyncButton
                mode="edit"
                :action-label="t('suseai.pages.settings.test.button')"
                :disabled="!canTest('rancherCatalog')"
                @click="cb => runTest('rancherCatalog', { tokenSecretRef: spec.rancherCatalog.tokenSecretRef, caBundleSecretRef: spec.rancherCatalog.caBundleSecretRef, url: spec.rancherCatalog.url, insecureSkipVerify: spec.rancherCatalog.insecureSkipVerify }, cb)"
              />
              <span
                v-if="testResults.rancherCatalog"
                :class="testResultClass('rancherCatalog')"
                class="ml-10"
              >{{ testResultText('rancherCatalog') }}</span>
            </div>
          </div>
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
