<template>
  <aside
    class="versions-panel"
    role="dialog"
    aria-modal="true"
    tabindex="-1"
    @keydown.esc="$emit('close')"
  >
    <div class="versions-panel__backdrop" @click="$emit('close')" />
    <div class="versions-panel__body">
      <header class="versions-panel__header">
        <h2>{{ t('aif.pages.blueprints.versionsPanel.title', { lineage: lineage.lineage }) }}</h2>
        <button
          ref="closeButton"
          type="button"
          class="btn btn-sm role-link"
          :aria-label="t('aif.pages.blueprints.versionsPanel.close')"
          @click="$emit('close')"
        >
          <i class="icon icon-close" />
        </button>
      </header>

      <ul v-if="lineage.versions.length" class="versions-panel__list">
        <li v-for="v in lineage.versions" :key="v.id" class="versions-panel__item">
          <div class="versions-panel__item-head">
            <span class="version-label">v{{ v.version }}</span>
            <BlueprintPhasePill :phase="v.phase" />
          </div>
          <p class="versions-panel__meta">
            {{ t('aif.pages.blueprints.card.publishedBy', { user: v.publishedBy || '—', date: formatDate(v.publishedAt) }) }}
          </p>
          <p v-if="v.changeDescription" class="versions-panel__change">
            <strong>{{ t('aif.pages.blueprints.versionsPanel.changeDescription') }}:</strong>
            {{ v.changeDescription }}
          </p>
        </li>
      </ul>

      <p v-else class="versions-panel__empty">
        {{ t('aif.pages.blueprints.versionsPanel.empty') }}
      </p>
    </div>
  </aside>
</template>

<script>
import { defineComponent, ref, onMounted } from 'vue';
import BlueprintPhasePill from './BlueprintPhasePill.vue';
import { formatDate } from '../../utils/date';

export default defineComponent({
  name: 'BlueprintVersionsPanel',

  components: { BlueprintPhasePill },

  props: {
    lineage: {
      type:     Object,
      required: true
    }
  },

  emits: ['close'],

  setup() {
    const closeButton = ref(null);

    onMounted(() => {
      closeButton.value?.focus?.();
    });

    return { closeButton, formatDate };
  }
});
</script>

<style lang="scss" scoped>
.versions-panel {
  position: fixed;
  inset:    0;
  z-index:  100;

  &__backdrop {
    position:   absolute;
    inset:      0;
    background: rgba(0, 0, 0, 0.4);
  }
  &__body {
    position:   absolute;
    top:        0;
    right:      0;
    bottom:     0;
    width:      min(480px, 100%);
    background: var(--body-bg);
    border-left: 1px solid var(--border);
    padding:    20px;
    overflow-y: auto;
  }
  &__header {
    display:         flex;
    align-items:     center;
    justify-content: space-between;
    margin-bottom:   15px;

    h2 { margin: 0; }
  }
  &__list { list-style: none; padding: 0; margin: 0; }
  &__item {
    border-bottom: 1px solid var(--border);
    padding:       12px 0;
  }
  &__item-head {
    display:     flex;
    align-items: center;
    gap:         10px;

    .version-label { font-weight: 600; }
  }
  &__meta { color: var(--muted); font-size: 0.9em; margin: 4px 0; }
  &__change { margin: 4px 0 0 0; }
  &__empty { color: var(--muted); }
}
</style>
