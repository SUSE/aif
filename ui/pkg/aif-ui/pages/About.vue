<script>
import { getVersion }    from '../utils/operator-api';
import { EXTENSION_VERSION } from '../utils/constants';
import { SUSEAI_PRODUCT } from '../config/suseai';

export default {
  name: 'AboutPage',

  data() {
    return {
      operatorVersion:       null,
      operatorCommit:        null,
      chartVersion:          null,
      operatorVersionLoaded: false,
      extensionVersion:      EXTENSION_VERSION,
      docsUrl:               SUSEAI_PRODUCT.docsRoute,
      supportUrl:            SUSEAI_PRODUCT.supportRoute,
    };
  },

  async fetch() {
    await this.fetchOperatorVersion();
  },

  methods: {
    async fetchOperatorVersion() {
      const notUnknown = (v) => (v && v !== 'unknown' ? v : null);

      try {
        const data = await getVersion();

        this.operatorVersion = notUnknown(data.version);
        this.operatorCommit  = notUnknown(data.commit);
        this.chartVersion    = notUnknown(data.chartVersion);
      } catch (e) {
        // eslint-disable-next-line no-console
        console.warn('[SUSE-AI] failed to fetch operator version', e);
        this.operatorVersion = null;
      } finally {
        this.operatorVersionLoaded = true;
      }
    },
  },
};
</script>

<template>
  <div>
    <header class="page-header">
      <h1>{{ t('suseai.pages.about.title') }}</h1>
    </header>

    <!-- Component Versions section -->
    <div class="box mt-10">
      <h2 class="section-title">
        {{ t('suseai.pages.about.sections.versions.title') }}
      </h2>
      <table class="info-table mt-10">
        <tbody>
          <tr>
            <td class="label-col text-muted">
              {{ t('suseai.pages.about.sections.versions.extension') }}
            </td>
            <td>{{ extensionVersion }}</td>
          </tr>
          <tr>
            <td class="label-col text-muted">
              {{ t('suseai.pages.about.sections.versions.operator') }}
            </td>
            <td>
              <span
                v-if="!operatorVersionLoaded"
                class="text-muted"
              >…</span>
              <span v-else-if="operatorVersion">{{ operatorVersion }}<span
                v-if="operatorCommit"
                class="text-muted"
              > (commit {{ operatorCommit }})</span></span>
              <span
                v-else
                class="text-muted"
              >{{ t('suseai.pages.about.sections.versions.unavailable') }}</span>
            </td>
          </tr>
          <tr>
            <td class="label-col text-muted">
              {{ t('suseai.pages.about.sections.versions.chart') }}
            </td>
            <td>
              <span
                v-if="!operatorVersionLoaded"
                class="text-muted"
              >…</span>
              <span v-else-if="chartVersion">{{ chartVersion }}</span>
              <span
                v-else
                class="text-muted"
              >{{ t('suseai.pages.about.sections.versions.unavailable') }}</span>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- Resources section -->
    <div class="box mt-10">
      <h2 class="section-title">
        {{ t('suseai.pages.about.sections.resources.title') }}
      </h2>
      <table class="info-table mt-10">
        <tbody>
          <tr>
            <td class="label-col text-muted">
              {{ t('suseai.pages.about.sections.resources.documentation') }}
            </td>
            <td>
              <a
                :href="docsUrl"
                target="_blank"
                rel="noopener noreferrer"
              >{{ docsUrl }} <i class="icon icon-external-link" /></a>
            </td>
          </tr>
          <tr>
            <td class="label-col text-muted">
              {{ t('suseai.pages.about.sections.resources.support') }}
            </td>
            <td>
              <a
                :href="supportUrl"
                target="_blank"
                rel="noopener noreferrer"
              >{{ supportUrl }} <i class="icon icon-external-link" /></a>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>

<style lang="scss" scoped>
.page-header {
  display: flex;
  align-items: center;
  margin-bottom: 24px;

  h1 { margin: 0; }
}

.box {
  border-radius: var(--border-radius);
  border: 1px solid var(--border);
  padding: 15px;
}

.section-title {
  margin: 0;
}

.info-table {
  width: 100%;
  border-collapse: collapse;

  td {
    padding: 6px 0;
    vertical-align: top;
  }

  .label-col {
    width: 180px;
  }
}

</style>
