<template>
  <div class="bp-detail">
    <h3 class="bp-section-heading bp-versions-heading">
      {{ t('suseai.common.labels.versions', 'Versions') }}
    </h3>
    <div
      v-for="bp in versions"
      :key="bp.spec.version"
      class="bp-version-section"
    >
      <button
        class="bp-version-heading"
        type="button"
        @click="toggleVersion(bp.spec.version)"
      >
        <i
          class="icon icon-chevron-down bp-version-chevron"
          :class="{ 'is-open': openVersions.has(bp.spec.version) }"
        />
        <span class="bp-version-tag">{{ versionLabel(bp) }}</span>
      </button>

      <div
        v-if="openVersions.has(bp.spec.version)"
        class="bp-version-body"
      >
        <template v-if="bp.spec.description">
          <h3 class="bp-section-heading">
            {{ t('suseai.wizard.form.description', 'Description') }}
          </h3>
          <p class="bp-version-description">
            {{ bp.spec.description }}
          </p>
        </template>

        <h3 class="bp-section-heading">
          {{ t('suseai.wizard.labels.applications', 'Applications') }}
        </h3>
        <table class="bp-components-table">
          <thead>
            <tr>
              <th>{{ t('suseai.wizard.labels.chart', 'Chart') }}</th>
              <th>{{ t('suseai.common.labels.version', 'Version') }}</th>
              <th>{{ t('suseai.wizard.labels.repository', 'Repository') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="comp in bp.spec.components"
              :key="comp.chartName"
            >
              <td>{{ comp.chartName }}</td>
              <td>{{ comp.chartVersion }}</td>
              <td>{{ comp.chartRepo }}</td>
            </tr>
            <tr v-if="!bp.spec.components.length">
              <td
                colspan="3"
                class="bp-no-components"
              >
                {{ t('suseai.common.labels.noComponents', 'No components defined') }}
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </div>
</template>

<script lang="ts">
import { defineComponent, ref, watch } from 'vue';
import { useT } from '../composables/useT';
import type { Blueprint } from '../types/blueprint-types';

export default defineComponent({
  name: 'BlueprintDetailPanel',
  props: {
    family: {
      type:     String,
      required: true,
    },
    versions: {
      type:     Array as () => Blueprint[],
      required: true,
    },
    expandedVersion: {
      type:    String,
      default: '',
    },
  },
  setup(props) {
    const t = useT();

    const defaultVersion = props.expandedVersion || props.versions[0]?.spec.version || '';
    const openVersions = ref<Set<string>>(new Set([defaultVersion].filter(Boolean)));

    watch(() => props.expandedVersion, (v) => {
      openVersions.value = new Set([v || props.versions[0]?.spec.version || ''].filter(Boolean));
    });

    function toggleVersion(version: string) {
      const next = new Set(openVersions.value);
      if (next.has(version)) {
        next.delete(version);
      } else {
        next.add(version);
      }
      openVersions.value = next;
    }

    function versionLabel(bp: Blueprint): string {
      const suffix = bp.spec.deprecated
        ? ` (${ t('suseai.common.labels.deprecated', 'deprecated') })`
        : '';
      return `v${ bp.spec.version }${ suffix }`;
    }

    return { t, openVersions, toggleVersion, versionLabel };
  },
});
</script>

<style lang="scss" scoped>


.bp-version-section {
  display: flex;
  flex-direction: column;

  & + .bp-version-section {
    border-top: 1px solid var(--border);
  }
}

.bp-version-heading {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  padding: 10px 0;
  background: none;
  border: none;
  cursor: pointer;
  text-align: left;
  color: var(--body-text);

  &:hover .bp-version-tag {
    color: var(--primary);
  }
}

.bp-version-chevron {
  font-size: 12px;
  color: var(--muted);
  transform: rotate(-90deg);
  transition: transform 0.2s ease;

  &.is-open {
    transform: rotate(0deg);
  }
}

.bp-version-body {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding-left: 26px;
  padding-bottom: 24px;
}

.bp-version-tag {
  font-size: 14px;
  font-weight: 600;
}

.bp-section-heading {
  font-size: 14px;
  font-weight: 400;
  color: var(--disabled-text);
  text-transform: uppercase;
  margin: 4px 0 4px;
}

.bp-versions-heading {
  margin-bottom: 0;
}

.bp-version-description {
  margin: 0;
  font-size: 13px;
  color: var(--body-text);
  line-height: 1.4;
  white-space: pre-wrap;
}

.bp-components-table {
  width: 100%;
  table-layout: fixed;
  border-collapse: collapse;
  font-size: 13px;

  th {
    text-align: left;
    padding: 4px 8px;
    border-bottom: 1px solid var(--border);
    color: var(--muted);
    font-weight: 500;

    &:nth-child(1) { width: 40%; }
    &:nth-child(2) { width: 25%; }
    &:nth-child(3) { width: 35%; }
  }

  td {
    padding: 4px 8px;
    border-bottom: 1px solid var(--border);
    color: var(--body-text);
  }

  tr:last-child td {
    border-bottom: none;
  }
}

.bp-no-components {
  color: var(--muted);
  font-style: italic;
}
</style>
