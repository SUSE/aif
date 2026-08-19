<script lang="ts" setup>
import { ref, reactive, onMounted, onBeforeUnmount } from 'vue';
import type { WorkloadLink } from '../utils/rancher-links';
import { useT } from '../composables/useT';

defineProps<{
  link:          WorkloadLink;
  openLabel:     string;
  triggerClass?: string;
}>();

const t       = useT();
const open    = ref(false);
const root    = ref<HTMLElement | null>(null);
const trigger = ref<HTMLElement | null>(null);
const menu    = ref<HTMLElement | null>(null);

// The menu is teleported to <body> so it escapes the table's `overflow: hidden`
// clip and the global `.btn-group` styles in the actions column. Because it is
// no longer positioned by an ancestor, we place it with fixed coordinates
// computed from the trigger's on-screen rect each time it opens.
const menuStyle = reactive<Record<string, string>>({});

function positionMenu() {
  const el = trigger.value;
  if (!el) return;
  const r = el.getBoundingClientRect();
  menuStyle.position = 'fixed';
  menuStyle.top      = `${ r.bottom + 4 }px`;
  menuStyle.left     = `${ r.left }px`;
}

// Maps a descriptor's disabledReason code to an l10n key + English fallback.
const REASONS: Record<string, [string, string]> = {
  noTargetCluster: ['suseai.workloads.noTargetCluster', 'No target cluster'],
  noNamespace:     ['suseai.workloads.noNamespace', 'No namespace'],
};

function reasonText(code: string): string {
  const entry = REASONS[code];
  return entry ? t(entry[0], entry[1]) : '';
}

function toggle() {
  if (open.value) close();
  else openMenu();
}

function openMenu() {
  open.value = true;
  positionMenu();
  // The fixed menu detaches from the trigger on scroll/resize, so listen only
  // while open — otherwise every row's cell would run a scroll handler per frame.
  window.addEventListener('scroll', closeOnViewportChange, true);
  window.addEventListener('resize', closeOnViewportChange);
}

function close() {
  open.value = false;
  window.removeEventListener('scroll', closeOnViewportChange, true);
  window.removeEventListener('resize', closeOnViewportChange);
}

// Click-outside and escape-to-close handlers. The menu is teleported out of
// `root`, so a click inside it must be checked separately. Both early-return
// when closed, so they stay registered for the component's lifetime.
function handleClickOutside(event: MouseEvent) {
  if (!open.value) return;
  const target = event.target as Node;
  if (root.value?.contains(target) || menu.value?.contains(target)) return;
  close();
}

function handleEscape(event: KeyboardEvent) {
  if (open.value && event.key === 'Escape') {
    close();
  }
}

function closeOnViewportChange() {
  if (open.value) close();
}

onMounted(() => {
  document.addEventListener('click', handleClickOutside);
  document.addEventListener('keydown', handleEscape);
});

onBeforeUnmount(() => {
  document.removeEventListener('click', handleClickOutside);
  document.removeEventListener('keydown', handleEscape);
  // Ensure the open-only viewport listeners are gone if we unmount while open.
  window.removeEventListener('scroll', closeOnViewportChange, true);
  window.removeEventListener('resize', closeOnViewportChange);
});
</script>

<template>
  <span ref="root" class="rancher-link-cell">
    <!-- Disabled -->
    <span
      v-if="link.disabled"
      class="rl-disabled"
      :class="triggerClass"
      :title="reasonText(link.disabledReason)"
      aria-disabled="true"
    >
      <slot />
    </span>

    <!-- Single target: direct link -->
    <router-link
      v-else-if="link.targets.length === 1"
      :to="link.targets[0].url"
      :class="triggerClass"
      :title="openLabel"
    >
      <slot />
    </router-link>

    <!-- Multiple targets: cluster picker -->
    <span v-else class="rl-multi">
      <button
        ref="trigger"
        type="button"
        :class="triggerClass"
        :title="openLabel"
        :aria-expanded="open"
        aria-haspopup="menu"
        @click="toggle"
      >
        <slot />
      </button>
      <Teleport to="body">
        <ul v-if="open" ref="menu" class="rl-menu" :style="menuStyle" role="menu" @mouseleave="close">
          <li class="rl-menu-header" role="none">{{ t('suseai.workloads.selectCluster', 'Select a cluster') }}</li>
          <li v-for="target in link.targets" :key="target.clusterId" role="none">
            <router-link
              :to="target.url"
              class="rl-menu-item"
              role="menuitem"
              @click="close"
            >{{ target.clusterName }}</router-link>
          </li>
        </ul>
      </Teleport>
    </span>
  </span>
</template>

<style lang="scss" scoped>
.rancher-link-cell { position: relative; display: inline-flex; align-items: center; }

.rancher-link-cell .btn { cursor: pointer; }

// The multi-cluster picker trigger is a <button>, not an <a>, so it misses the
// global link styles; strip its native chrome and mimic the link look.
.rancher-link-cell .rl-multi > button:not(.btn) {
  display: inline;
  padding: 0;
  border: none;
  background: none;
  font: inherit;
  line-height: inherit;
  vertical-align: baseline;
  cursor: pointer;
  color: var(--link);
  text-decoration: none;
}
.rancher-link-cell .rl-multi > button:not(.btn):hover {
  color: var(--body-text);
  text-decoration: underline;
}

.rl-disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.rl-multi { position: relative; display: inline-block; }

.rl-menu {
  // Positioned via inline `fixed` coords (teleported to <body>); these are
  // fallbacks only.
  position: absolute;
  z-index: 1000;
  top: 100%;
  left: 0;
  margin: 4px 0 0;
  padding: 4px 0;
  list-style: none;
  min-width: 160px;
  background: var(--body-bg);
  border: 1px solid var(--border);
  border-radius: 6px;
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.15);

  .rl-menu-header {
    padding: 4px 12px;
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: var(--muted);
  }

  .rl-menu-item {
    display: block;
    padding: 6px 12px;
    color: var(--body-text);
    text-decoration: none;
  }

  a.rl-menu-item:hover { background: var(--sortable-table-accent-bg); }
}
</style>
