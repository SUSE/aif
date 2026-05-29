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
        :show-deprecated="showDeprecated"
        @update:model-value="onVersionChange"
      />
    </header>

    <div class="bp-card__meta">
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
        class="btn btn-sm role-primary"
        @click="$emit('deploy', selected)"
      >
        {{ t('aif.pages.blueprints.actions.install') }}
      </button>
      <ActionMenuShell
        button-variant="tertiary"
        :button-aria-label="t('aif.pages.blueprints.actions.moreOptionsAria')"
        :custom-actions="tileActions"
        @action-invoked="onAction"
      />
    </div>
  </div>
</template>

<script>
import { defineComponent, ref, computed, watch, getCurrentInstance } from 'vue';
import ActionMenuShell from '@shell/components/ActionMenuShell';
import BlueprintPhasePill from './BlueprintPhasePill.vue';
import BlueprintVersionPicker from './BlueprintVersionPicker.vue';
import { selectDefaultVersion } from '../../utils/blueprint';
import { formatDate } from '../../utils/date';

export default defineComponent({
  name: 'BlueprintCard',

  components: { ActionMenuShell, BlueprintPhasePill, BlueprintVersionPicker },

  props: {
    lineage: {
      type:     Object,
      required: true
    },
    isAdmin: {
      type:    Boolean,
      default: false
    },
    showDeprecated: {
      type:    Boolean,
      default: false
    }
  },

  emits: ['deploy', 'copy', 'edit', 'deprecate', 'delete'],

  setup(props, { emit }) {
    const vm = getCurrentInstance().proxy;
    const selectedId = ref(selectDefaultVersion(props.lineage).id);
    const selected = computed(() =>
      props.lineage.versions.find((v) => v.id === selectedId.value)
    );

    watch(() => props.lineage, (next) => {
      selectedId.value = selectDefaultVersion(next).id;
    });

    // When hiding deprecated versions, reset the selection if the currently
    // selected version is deprecated. Re-derive from props directly rather
    // than reading the `selected` computed to make the dependency obvious.
    watch(() => props.showDeprecated, (next) => {
      if (next) return;
      const current = props.lineage.versions.find((v) => v.id === selectedId.value);

      if (current && current.phase !== 'Active') {
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

    const isDeprecated = computed(() => selected.value.phase !== 'Active');

    const tileActions = computed(() => {
      const actions = [
        { action: 'copy', label: vm.t('aif.pages.blueprints.actions.copy'), enabled: true },
      ];
      if (props.isAdmin) {
        actions.push(
          { action: 'edit', label: vm.t('aif.pages.blueprints.actions.edit'), enabled: true },
          {
            action:  'deprecate',
            label:   isDeprecated.value
              ? vm.t('aif.pages.blueprints.actions.undeprecate')
              : vm.t('aif.pages.blueprints.actions.deprecate'),
            enabled: true,
          },
          { divider: true, label: '', enabled: false },
          { action: 'delete', label: vm.t('aif.pages.blueprints.actions.delete'), enabled: true },
        );
      }
      return actions;
    });

    function onAction(payload) {
      // payload.action is one of copy|edit|deprecate|delete
      emit(payload.action, selected.value);
    }

    return { selectedId, selected, originKey, originTooltip, onVersionChange, formatDate, tileActions, onAction };
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
}
</style>
