<template>
  <div v-if="show" class="aif-progress-modal__backdrop">
    <div class="aif-progress-modal">
      <h3>{{ title }}</h3>
      <ul class="aif-progress-modal__list">
        <li v-for="item in progress" :key="item.clusterId" class="aif-progress-modal__item">
          <span :class="`aif-progress-modal__icon aif-progress-modal__icon--${ item.status }`">
            <i v-if="item.status === 'installing'" class="icon icon-spinner icon-spin" />
            <i v-else-if="item.status === 'success'" class="icon icon-checkmark" />
            <i v-else class="icon icon-warning" />
          </span>
          <span class="aif-progress-modal__cluster">{{ item.clusterName || item.clusterId }}</span>
          <span class="aif-progress-modal__msg">{{ item.message }}</span>
        </li>
      </ul>
      <div class="aif-progress-modal__footer">
        <button v-if="isDone" class="btn role-primary" @click="$emit('done')">Done</button>
        <button v-else class="btn role-secondary" @click="$emit('cancel')">Cancel</button>
      </div>
    </div>
  </div>
</template>

<script>
import { defineComponent } from 'vue';

export default defineComponent({
  name: 'InstallProgressModal',

  props: {
    show:     { type: Boolean, default: false },
    title:    { type: String,  default: 'Installing' },
    progress: { type: Array,   default: () => [] },
  },

  emits: ['done', 'cancel'],

  computed: {
    isDone() {
      return this.progress.length > 0 && this.progress.every((p) => p.status !== 'installing');
    },
  },
});
</script>

<style scoped>
.aif-progress-modal__backdrop {
  position: fixed; inset: 0; background: rgba(0, 0, 0, .5); display: flex;
  align-items: center; justify-content: center; z-index: 1000;
}
.aif-progress-modal {
  background: var(--body-bg); border-radius: 6px; padding: 24px;
  min-width: 400px; max-width: 560px; width: 100%;
}
.aif-progress-modal h3 { margin: 0 0 16px; font-size: 16px; font-weight: 600; }
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
