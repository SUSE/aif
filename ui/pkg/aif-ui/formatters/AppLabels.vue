<template>
  <span v-if="ordered.length" class="tile-label-group">
    <span
      class="badge-state badge-sm tile-label"
      :class="labelBadgeClass(ordered[0].code)"
      :title="`NVIDIA program: ${ordered[0].name}`"
    >
      {{ ordered[0].name }}
    </span>
    <span
      v-if="ordered.length > 1"
      class="tile-label-more"
      tabindex="0"
      role="button"
      :aria-label="moreLabel"
      @click.stop
      @mouseenter="flipPopoverIfNeeded"
      @focusin="flipPopoverIfNeeded"
    >
      <span class="badge-state badge-sm tile-label tile-label-count">+{{ ordered.length - 1 }}</span>
      <span class="tile-label-list" role="list">
        <span
          v-for="label in ordered"
          :key="label.code"
          class="badge-state badge-sm tile-label tile-label-list__item"
          :class="labelBadgeClass(label.code)"
          role="listitem"
        >{{ label.name }}</span>
      </span>
    </span>
  </span>
</template>

<script lang="ts">
import { defineComponent, computed } from 'vue';
import type { PropType } from 'vue';
import type { AppLabel } from '../services/app-collection';

// Renders NVIDIA program/support labels for an app: the first badge inline, the
// rest collapsed into a "+N" chip whose hover/focus popover lists them all. Used
// by both the tile grid and the list view on the Apps page.
export default defineComponent({
  name: 'AppLabels',

  props: {
    labels: {
      type: Array as PropType<AppLabel[]>,
      default: () => []
    }
  },

  setup(props) {
    // Green "…Supported" badges first; stable sort keeps original order otherwise.
    const ordered = computed(() =>
      [...props.labels].sort(
        (a, b) => Number(b.code.endsWith('_supported')) - Number(a.code.endsWith('_supported'))
      )
    );

    const moreLabel = computed(() => {
      const n = ordered.value.length - 1;
      return `Show ${n} more NVIDIA program${n === 1 ? '' : 's'}`;
    });

    // "…Supported" programs get the success (green) treatment (as NGC highlights
    // supported software); other program labels use info (blue).
    const labelBadgeClass = (code: string) =>
      code.endsWith('_supported') ? 'bg-success' : 'bg-info';

    // The popover opens left-aligned and grows right; on an app near the right
    // edge that runs off-screen, so flip it to right-aligned when it would
    // overflow the viewport. Runs on enter/focus, once the list is shown.
    const flipPopoverIfNeeded = (e: Event) => {
      const more = e.currentTarget as HTMLElement;
      const list = more.querySelector('.tile-label-list') as HTMLElement | null;
      if (!list) return;
      list.classList.remove('tile-label-list--flip');
      const rect = list.getBoundingClientRect();
      const width = rect.width || 260; // approx popover width if not yet measurable
      const left = rect.width ? rect.left : more.getBoundingClientRect().left;
      if (left + width > window.innerWidth - 8) {
        list.classList.add('tile-label-list--flip');
      }
    };

    return { ordered, moreLabel, labelBadgeClass, flipPopoverIfNeeded };
  }
});
</script>

<style lang="scss" scoped>
.badge-state {
  display: inline-block;
  padding: 4px 10px;
  font-size: 11px;
  border-radius: 16px;
  font-weight: 500;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  border: none;

  &.badge-sm {
    padding: 3px 8px;
    font-size: 10px;
    border-radius: 12px;
  }

  &.bg-success {
    background: var(--success-banner-bg, #dcfce7);
    color: var(--success, #166534);
  }

  &.bg-info {
    background: var(--info-banner-bg, #dbeafe);
    color: var(--info, #1d4ed8);
  }
}

// Program labels are not uppercased. Qualified with .badge-state so it out-ranks
// the base rule regardless of source order.
.badge-state.tile-label {
  text-transform: none;
  letter-spacing: 0;
}

// Keep the visible badge and its "+N" chip together on one line; the pair wraps
// as a unit rather than the chip dropping to the next row.
.tile-label-group {
  display: inline-flex;
  align-items: center;
  min-width: 0;
}

// Overflow indicator: first badge shows inline, the rest collapse into a neutral
// "+N" chip that reveals the full list on hover/focus.
.tile-label-more {
  position: relative;
  display: inline-flex;
  align-items: center;

  &:focus-visible {
    outline: 2px solid var(--primary);
    outline-offset: 2px;
    border-radius: 12px;
  }
}

.tile-label-count {
  cursor: pointer;
  background: var(--hover-bg);
  color: var(--body-text);
}

.tile-label-list {
  position: absolute;
  z-index: 20;
  top: calc(100% + 6px);
  left: 0;
  display: none;
  flex-direction: column;
  align-items: flex-start;
  gap: 6px;
  padding: 8px;
  background: var(--body-bg);
  border: 1px solid var(--border);
  border-radius: 10px;
  box-shadow: 0 6px 20px rgba(0, 0, 0, 0.18);
  white-space: nowrap;
}

.tile-label-more:hover .tile-label-list,
.tile-label-more:focus-within .tile-label-list {
  display: flex;
}

// Right-align the popover (set by flipPopoverIfNeeded) when left-aligned would
// run off the right edge of the viewport.
.tile-label-list.tile-label-list--flip {
  left: auto;
  right: 0;
}
</style>
