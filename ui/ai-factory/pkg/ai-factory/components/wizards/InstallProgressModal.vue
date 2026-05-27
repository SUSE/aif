<template>
  <ModalWithCard
    v-if="show"
    name="install-progress"
    width="560"
    @close="$emit('cancel')"
  >
    <template #title>{{ resolvedTitle }}</template>
    <template #content>
      <ul class="aif-progress-modal__list">
        <li v-for="item in progress" :key="item.clusterId" class="aif-progress-modal__item">
          <span :class="`aif-progress-modal__icon aif-progress-modal__icon--${ item.status }`">
            <i v-if="item.status === PROGRESS_STATUS.INSTALLING" class="icon icon-spinner icon-spin" />
            <i v-else-if="item.status === PROGRESS_STATUS.SUCCESS" class="icon icon-checkmark" />
            <i v-else class="icon icon-warning" />
          </span>
          <span class="aif-progress-modal__cluster">{{ item.clusterName || item.clusterId }}</span>
          <span class="aif-progress-modal__msg">{{ item.message }}</span>
        </li>
      </ul>
    </template>
    <template #footer>
      <div class="aif-progress-modal__footer">
        <button v-if="isDone" class="btn role-primary" @click="$emit('done')">
          <t k="aif.wizards.installProgress.done" />
        </button>
        <button v-else class="btn role-secondary" @click="$emit('cancel')">
          <t k="aif.wizards.installProgress.cancel" />
        </button>
      </div>
    </template>
  </ModalWithCard>
</template>

<script>
import { defineComponent } from 'vue';
import ModalWithCard from '@shell/components/ModalWithCard';

export const PROGRESS_STATUS = Object.freeze({
  INSTALLING: 'installing',
  SUCCESS:    'success',
  FAILED:     'failed',
});

export default defineComponent({
  name: 'InstallProgressModal',

  components: { ModalWithCard },

  props: {
    show:     { type: Boolean, default: false },
    title:    { type: String,  default: null },
    progress: { type: Array,   default: () => [] },
  },

  emits: ['done', 'cancel'],

  computed: {
    PROGRESS_STATUS() {
      return PROGRESS_STATUS;
    },

    resolvedTitle() {
      return this.title || this.t('aif.wizards.installProgress.title');
    },

    isDone() {
      return this.progress.length > 0 && this.progress.every((p) => p.status !== PROGRESS_STATUS.INSTALLING);
    },
  },
});
</script>

<style scoped>
.aif-progress-modal__list { list-style: none; padding: 0; margin: 0 0 16px; }
.aif-progress-modal__item {
  display: flex; align-items: center; gap: 10px; padding: 8px 0;
  border-bottom: 1px solid var(--border);
}
.aif-progress-modal__item:last-child { border-bottom: none; }
.aif-progress-modal__icon--success { color: var(--success); }
.aif-progress-modal__icon--failed  { color: var(--error); }
.aif-progress-modal__msg { font-size: 12px; color: var(--muted); margin-left: auto; }
.aif-progress-modal__footer { display: flex; justify-content: flex-end; }
</style>
