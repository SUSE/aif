<template>
  <div class="bp-card">
    <header class="bp-card__header">
      <div class="bp-card__title-row">
        <h3 class="bp-card__title">{{ lineage.lineage }}</h3>
        <BlueprintPhasePill :phase="selected.phase" />
      </div>
      <BlueprintVersionPicker
        :versions="lineage.versions"
        :model-value="selectedId"
        :show-withdrawn="showWithdrawn"
        @update:model-value="onVersionChange"
      />
    </header>

    <div class="bp-card__meta">
      <span class="bp-card__chip bp-card__chip--use-case">
        {{ t('aif.pages.blueprints.card.useCase') }}: {{ selected.useCase || '—' }}
      </span>
      <span
        class="bp-card__chip bp-card__chip--origin"
        :title="originTooltip"
      >
        {{ t(`aif.pages.blueprints.card.origin.${ originKey }`) }}
      </span>
    </div>

    <p class="bp-card__description">{{ selected.description || '—' }}</p>

    <div v-if="selected.components.length" class="bp-card__components">
      <span class="bp-card__components-label">
        {{ t('aif.pages.blueprints.card.components') }}:
      </span>
      <span
        v-for="c in selected.components"
        :key="c.name"
        class="bp-card__chip bp-card__chip--component"
      >
        {{ c.name }}
      </span>
    </div>

    <p class="bp-card__published">
      {{ t('aif.pages.blueprints.card.publishedBy', { user: selected.publishedBy || '—', date: formatDate(selected.publishedAt) }) }}
    </p>

    <div class="bp-card__actions">
      <button
        type="button"
        class="btn btn-sm role-secondary"
        @click="$event.currentTarget.blur(); $emit('view-versions', lineage)"
      >
        {{ t('aif.pages.blueprints.card.viewVersions') }}
      </button>
      <button
        type="button"
        class="btn btn-sm role-secondary"
        :disabled="true"
        :title="t('aif.pages.blueprints.actions.startBundleComingSoon')"
      >
        {{ t('aif.pages.blueprints.actions.startBundle') }}
      </button>
      <button
        type="button"
        class="btn btn-sm role-primary"
        :disabled="true"
        :title="t('aif.pages.blueprints.actions.deployComingSoon')"
      >
        {{ t('aif.pages.blueprints.actions.deploy') }}
      </button>
    </div>

    <div v-if="isPublisher" class="bp-card__publisher-actions">
      <span class="bp-card__publisher-label">
        {{ t('aif.pages.blueprints.actions.publisherLabel') }}
      </span>
      <button
        v-if="selected.phase === 'Active'"
        type="button"
        class="btn btn-sm role-secondary"
        :disabled="true"
        :title="t('aif.pages.blueprints.actions.publisherEndpointComingSoon')"
      >
        {{ t('aif.pages.blueprints.actions.deprecate') }}
      </button>
      <button
        v-if="selected.phase === 'Active'"
        type="button"
        class="btn btn-sm role-secondary"
        :disabled="true"
        :title="t('aif.pages.blueprints.actions.publisherEndpointComingSoon')"
      >
        {{ t('aif.pages.blueprints.actions.withdraw') }}
      </button>
      <button
        v-if="selected.phase === 'Withdrawn'"
        type="button"
        class="btn btn-sm role-secondary"
        :disabled="true"
        :title="t('aif.pages.blueprints.actions.publisherEndpointComingSoon')"
      >
        {{ t('aif.pages.blueprints.actions.reactivate') }}
      </button>
    </div>
  </div>
</template>

<script>
import { defineComponent, ref, computed, watch } from 'vue';
import BlueprintPhasePill from './BlueprintPhasePill.vue';
import BlueprintVersionPicker from './BlueprintVersionPicker.vue';
import { selectDefaultVersion } from '../../utils/blueprint';
import { formatDate } from '../../utils/date';

export default defineComponent({
  name: 'BlueprintCard',

  components: { BlueprintPhasePill, BlueprintVersionPicker },

  props: {
    lineage: {
      type:     Object,
      required: true
    },
    isPublisher: {
      type:    Boolean,
      default: false
    },
    showWithdrawn: {
      type:    Boolean,
      default: false
    }
  },

  emits: ['view-versions'],

  setup(props) {
    const selectedId = ref(selectDefaultVersion(props.lineage).id);
    const selected = computed(() =>
      props.lineage.versions.find((v) => v.id === selectedId.value)
    );

    watch(() => props.lineage, (next) => {
      selectedId.value = selectDefaultVersion(next).id;
    });

    // When hiding withdrawn versions, reset the selection if the currently
    // selected version is Withdrawn. Re-derive from props directly rather
    // than reading the `selected` computed to make the dependency obvious.
    watch(() => props.showWithdrawn, (next) => {
      if (next) return;
      const current = props.lineage.versions.find((v) => v.id === selectedId.value);

      if (current?.phase === 'Withdrawn') {
        selectedId.value = selectDefaultVersion(props.lineage).id;
      }
    });

    const originKey = computed(() =>
      selected.value.origin === 'WrapsVendorChart' ? 'wrapsVendorChart' : 'published'
    );

    const originTooltip = computed(() => {
      if (selected.value.origin !== 'WrapsVendorChart' || !selected.value.vendorChart) return '';

      return `${ selected.value.vendorChart.chart } @ ${ selected.value.vendorChart.version }`;
    });

    const onVersionChange = (id) => { selectedId.value = id; };

    return { selectedId, selected, originKey, originTooltip, onVersionChange, formatDate };
  }
});
</script>

<style lang="scss" scoped>
.bp-card {
  border:        1px solid var(--border);
  border-radius: var(--border-radius);
  padding:       15px;
  background:    var(--body-bg);
  display:       flex;
  flex-direction: column;
  gap:           10px;

  &__header {
    display:        flex;
    flex-direction: column;
    gap:            8px;
  }
  &__title-row {
    display:     flex;
    align-items: center;
    gap:         10px;

    .bp-card__title { margin: 0; flex: 1; }
  }
  &__meta {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }
  &__chip {
    display:        inline-block;
    padding:        2px 8px;
    border-radius:  4px;
    background:     var(--accent-btn);
    color:          var(--body-text);
    font-size:      0.85em;
    border:         1px solid var(--border);
  }
  &__description {
    margin: 0;
    color:  var(--body-text);
    display: -webkit-box;
    -webkit-line-clamp: 3;
    -webkit-box-orient: vertical;
    overflow: hidden;
  }
  &__components {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
    align-items: center;

    &-label { font-weight: 500; margin-right: 4px; }
  }
  &__published { color: var(--muted); font-size: 0.85em; margin: 0; }
  &__actions {
    display:         flex;
    flex-wrap:       wrap;
    align-items:     center;
    justify-content: flex-end;
    gap:             6px;
    margin-top:      6px;
  }
  &__publisher-actions {
    display:        flex;
    flex-wrap:      wrap;
    align-items:    center;
    justify-content: flex-end;
    gap:            6px;
    margin-top:     10px;
    padding-top:    10px;
    border-top:     1px dashed var(--border);
  }
  &__publisher-label {
    color:          var(--muted);
    font-size:      0.75em;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    margin-right:   2px;
  }
}
</style>
