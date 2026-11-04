<template>
  <div class="app-detail-panel">
    <div class="app-detail-header">
      <template v-if="app.library === 'nvidia'">
        <img
          :src="nvidiaLogo"
          alt=""
          class="app-detail-logo nvidia-logo--light"
        />
        <img
          :src="nvidiaLogoDark"
          alt=""
          class="app-detail-logo nvidia-logo--dark"
        />
      </template>
      <img
        v-else
        :src="logo"
        alt=""
        class="app-detail-logo"
        @error="onLogoError"
      />

      <div class="app-detail-info">
        <h3 class="app-detail-title">
          {{ app.name }}
        </h3>
        <div class="app-detail-meta">
          <span
            v-if="app.packaging_format"
            class="app-detail-meta-item"
          >
            {{ formatPackagingType(app.packaging_format) }}
          </span>
          <AppLabels :labels="app.labels" />
        </div>
      </div>

      <button
        class="app-detail-close"
        type="button"
        data-testid="app-detail-close"
        :aria-label="t('suseai.apps.detail.close', 'Close')"
        @click="$emit('close')"
      >
        <i class="icon icon-close" />
      </button>
    </div>

    <hr class="app-detail-divider">

    <div
      v-if="unavailable"
      class="app-detail-availability"
    >
      <strong>{{ availabilityMessage }}</strong>
      <router-link :to="settingsLink">
        {{ t('suseai.repositoryHealth.settings', 'Open Settings') }}
      </router-link>
    </div>

    <section
      class="app-detail-section"
      data-testid="app-detail-overview"
    >
      <h4 class="app-detail-section-title">
        {{ t('suseai.apps.detail.overview', 'Overview') }}
      </h4>

      <!-- The tile truncates the short description, so show it in full here.
           The em dash only covers apps with no description of any kind. -->
      <p
        v-if="app.description || !app.overview"
        class="app-detail-summary"
      >
        {{ app.description || '—' }}
      </p>

      <dl
        v-if="repository"
        class="app-detail-facts"
      >
        <template v-if="repository">
          <dt>{{ t('suseai.apps.detail.repository', 'Repository') }}</dt>
          <dd>{{ repository }}</dd>
        </template>
      </dl>

      <div
        v-if="app.documentation_url || app.project_url || ngcUrl"
        class="app-detail-links"
      >
        <a
          v-if="app.documentation_url"
          :href="app.documentation_url"
          target="_blank"
          rel="noopener noreferrer"
        >
          <i class="icon icon-document" /> {{ t('suseai.apps.detail.documentation', 'Documentation') }}
        </a>
        <a
          v-if="app.project_url"
          :href="app.project_url"
          target="_blank"
          rel="noopener noreferrer"
        >
          <i class="icon icon-external-link" /> {{ t('suseai.apps.detail.project', 'Project page') }}
        </a>
        <a
          v-if="ngcUrl"
          :href="ngcUrl"
          target="_blank"
          rel="noopener noreferrer"
        >
          <i class="icon icon-external-link" /> {{ t('suseai.apps.detail.viewOnNgc', 'View on NGC') }}
        </a>
      </div>
    </section>

    <section
      v-if="app.overview"
      class="app-detail-section"
      data-testid="app-detail-description"
    >
      <h4 class="app-detail-section-title">
        {{ t('suseai.apps.detail.description', 'Description') }}
      </h4>
      <div class="app-detail-body">
        <Markdown :value="app.overview" />
      </div>
    </section>

    <section
      v-if="showVersions"
      class="app-detail-section"
      data-testid="app-detail-versions"
    >
      <h4 class="app-detail-section-title">
        {{ t('suseai.apps.detail.versions', 'Versions') }}
      </h4>
      <div
        v-if="versionsLoading"
        class="app-detail-versions-loading"
      >
        <i class="icon icon-spinner icon-spin" />
      </div>
      <p
        v-else-if="versionsError || !versions || !versions.length"
        class="app-detail-versions-empty"
      >
        {{ t('suseai.apps.detail.versionsUnavailable', 'Versions unavailable') }}
      </p>
      <template v-else>
        <ul class="app-detail-versions">
          <li
            v-for="entry in visibleVersions"
            :key="entry.version"
          >
            <!-- Like Rancher's chart page: a version opens the install wizard with it selected. -->
            <button
              type="button"
              class="app-detail-version"
              data-testid="app-detail-version"
              @click="$emit('select-version', entry.version)"
            >
              <span class="app-detail-version-number">{{ entry.version }}</span>
              <span
                v-if="formatDate(entry.created)"
                class="app-detail-version-date"
              >{{ formatDate(entry.created) }}</span>
            </button>
          </li>
        </ul>
        <button
          v-if="versions.length > versionLimit"
          type="button"
          class="btn role-link app-detail-versions-toggle"
          data-testid="app-detail-versions-toggle"
          @click="showAllVersions = !showAllVersions"
        >
          {{ showAllVersions
            ? t('suseai.apps.detail.showFewerVersions', 'Show fewer')
            : t('suseai.apps.detail.showAllVersions', 'Show all {count} versions', { count: String(versions.length) }) }}
        </button>
      </template>
    </section>
  </div>
</template>

<script lang="ts">
import { defineComponent, computed, ref, watch } from 'vue';
import day from 'dayjs';
import type { PropType } from 'vue';
import type { RouteLocationRaw } from 'vue-router';
import Markdown from '@shell/components/Markdown.vue';
import { useT } from '../composables/useT';
import type { AppCollectionItem } from '../services/app-collection';
import type { ChartVersionEntry } from '../services/rancher-apps';
import AppLabels from '../formatters/AppLabels.vue';
import { resolveCatalogLogo, onCatalogLogoError } from '../utils/catalog-logo';
import { ngcChartPageUrl } from '../utils/ngc-links';
import nvidiaLogo from '../assets/nvidia-logo.svg';
import nvidiaLogoDark from '../assets/nvidia-logo-light.svg';
import genericLogo from '../assets/generic-app.svg';

// Drawer content only: the drawer shell (backdrop, slide transition, mount
// point) lives in the page that hosts this component.
export default defineComponent({
  name: 'AppDetailPanel',

  components: { AppLabels, Markdown },

  props: {
    app: {
      type:     Object as PropType<AppCollectionItem>,
      required: true,
    },
    unavailable: {
      type:    Boolean,
      default: false,
    },
    availabilityMessage: {
      type:    String,
      default: '',
    },
    settingsLink: {
      type:     Object as PropType<RouteLocationRaw>,
      required: true,
    },
    // null means versions were not requested (the section stays hidden).
    versions: {
      type:    Array as PropType<ChartVersionEntry[] | null>,
      default: null,
    },
    versionsLoading: {
      type:    Boolean,
      default: false,
    },
    versionsError: {
      type:    Boolean,
      default: false,
    },
  },

  emits: ['close', 'select-version'],

  setup(props) {
    const t = useT();

    const formatPackagingType = (format: string) => format === 'HELM_CHART' ? 'Helm' : 'Container';

    const logo = computed(() => resolveCatalogLogo(props.app) || genericLogo);
    const onLogoError = (event: Event) => onCatalogLogoError(event, props.app, genericLogo);

    const repository = computed(() => props.app.repository_name || props.app.repository_url || '');

    // Same format Rancher uses for chart dates. An unparseable date or a Go
    // zero-value timestamp ("0001-01-01T...") yields '' so no date is shown
    // rather than a misleading one.
    const formatDate = (value?: string): string => {
      if (!value || value.startsWith('0001-01-01')) return '';
      const date = day(value);
      return date.isValid() ? date.format('MMM D, YYYY') : '';
    };

    const showVersions = computed(() => !props.unavailable &&
      (props.versionsLoading || props.versionsError || props.versions !== null));

    const versionLimit = 10;
    const showAllVersions = ref(false);
    const visibleVersions = computed(() => {
      const all = props.versions || [];
      return showAllVersions.value ? all : all.slice(0, versionLimit);
    });

    // Opening another app collapses the list again.
    watch(() => props.app.slug_name, () => {
      showAllVersions.value = false;
    });

    const ngcUrl = computed(() => ngcChartPageUrl(props.app.repository_url, props.app.slug_name));

    return {
      t,
      formatPackagingType,
      logo,
      onLogoError,
      repository,
      formatDate,
      showVersions,
      versionLimit,
      showAllVersions,
      visibleVersions,
      ngcUrl,
      nvidiaLogo,
      nvidiaLogoDark,
    };
  },
});
</script>

<style lang="scss" scoped>
// Content is inset; the divider deliberately is not, so it spans the drawer.
$padding-x: 28px;

.app-detail-header {
  padding: 0 $padding-x;
  display: flex;
  align-items: flex-start;
  gap: 16px;
}

.app-detail-logo {
  width: 52px;
  height: 52px;
  object-fit: contain;
  border-radius: 12px;
  background: var(--accent-btn);
  border: 1px solid var(--border, #e5e7eb);
  flex-shrink: 0;
  padding: 8px;
}

.app-detail-info {
  flex: 1;
  min-width: 0;
}

.app-detail-title {
  margin: 0;
  font-size: 16px;
  font-weight: 600;
  color: var(--body-text);
}

.app-detail-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
  margin-top: 6px;
  color: var(--muted);
  font-size: 12px;
}

.app-detail-close {
  align-self: center;
  padding: 0;
  width: 28px;
  height: 28px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: none;
  border: none;
  color: var(--body-text);
  cursor: pointer;

  &:hover {
    color: var(--primary);
  }
}

.app-detail-availability {
  display: flex;
  flex-direction: column;
  gap: 6px;
  margin: 16px $padding-x 0;
  padding: 12px;
  border-radius: var(--border-radius);
  background: var(--disabled-bg);
  color: var(--body-text);
  overflow-wrap: anywhere;
}

.app-detail-divider {
  margin: 12px 0 0;
  border: none;
  border-top: 1px solid var(--border, #e5e7eb);
}

.app-detail-section {
  margin-top: 16px;
  padding: 0 $padding-x;

  // Extra room between consecutive sections (above the Description title).
  & + & {
    margin-top: 32px;
  }
}

// Same treatment as the section headings in the Blueprints drawer.
.app-detail-section-title {
  margin: 4px 0 16px;
  font-size: 14px;
  font-weight: 400;
  color: var(--disabled-text);
  text-transform: uppercase;
}

.app-detail-summary {
  margin: 0 0 16px;
  color: var(--body-text);
  line-height: 1.5;
  white-space: pre-wrap;
}

.app-detail-facts {
  display: grid;
  grid-template-columns: auto 1fr;
  gap: 4px 12px;
  margin: 0;

  dt {
    color: var(--muted);
    font-size: 13px;
  }

  dd {
    margin: 0;
    color: var(--body-text);
    font-size: 13px;
  }
}

.app-detail-versions {
  margin: 0;
  padding: 0;
  list-style: none;
}

.app-detail-version {
  display: flex;
  justify-content: space-between;
  gap: 16px;
  width: 100%;
  // Undo Rancher's global button sizing (40px line-height and min-height).
  min-height: 0;
  padding: 3px 0;
  border: none;
  background: none;
  font-size: 13px;
  line-height: 1.5;
  text-align: left;
  cursor: pointer;

  .app-detail-version-number {
    color: var(--link);
  }

  .app-detail-version-date {
    color: var(--muted);
  }

  &:hover .app-detail-version-number {
    text-decoration: underline;
  }
}

.app-detail-versions-loading {
  display: flex;
  justify-content: center;
  padding: 8px 0;
}

.app-detail-versions-empty {
  margin: 0;
  color: var(--muted);
}

.app-detail-versions-toggle {
  margin-top: 8px;
  padding: 0;
}

.app-detail-links {
  display: flex;
  flex-wrap: wrap;
  gap: 16px;
  margin-top: 16px;

  a {
    color: var(--link);
  }
}

// Rendered markdown never outgrows the 14px section title: structure is
// carried by weight, not size.
.app-detail-body {
  font-size: 13px;
  line-height: 1.5;
  color: var(--body-text);

  :deep(h1),
  :deep(h2),
  :deep(h3),
  :deep(h4),
  :deep(h5),
  :deep(h6) {
    margin: 16px 0 6px;
    font-weight: 600;
    line-height: 1.4;
    color: var(--body-text);
  }

  :deep(h1),
  :deep(h2) {
    font-size: 14px;
  }

  :deep(h3),
  :deep(h4),
  :deep(h5),
  :deep(h6) {
    font-size: 13px;
  }

  :deep(.markdown > :first-child) {
    margin-top: 0;
  }

  // The shell Markdown component resets paragraphs to `font-size: initial`
  // (the 16px browser default); inherit the panel's size instead.
  :deep(p),
  :deep(li) {
    font-size: inherit;
    line-height: inherit;
  }

  :deep(p),
  :deep(ul),
  :deep(ol) {
    margin: 0 0 8px;
  }

  // Rancher styles all code as a padded inline-block, which makes any line with
  // inline code taller than the rest. Keep the shading but flow it inline.
  :deep(:not(pre) > code) {
    display: inline;
    padding: 1px 4px;
    font-size: 0.9em;
    box-decoration-break: clone;
    -webkit-box-decoration-break: clone;
  }

  :deep(pre) {
    display: block;
    max-width: 100%;
    overflow-x: auto;
  }

  :deep(table) {
    display: block;
    max-width: 100%;
    overflow-x: auto;
  }
}
</style>

<style lang="scss">
body:not(.theme-dark) .app-detail-header .nvidia-logo--dark { display: none; }
body.theme-dark .app-detail-header .nvidia-logo--light { display: none; }
</style>
