<template>
  <span
    v-if="icon && !failed"
    class="partner-logo"
  >
    <LazyImage
      :src="icon"
      :error-src="BLANK_IMAGE"
      alt=""
      aria-hidden="true"
      referrerpolicy="no-referrer"
      decoding="async"
      class="partner-logo-img"
      @error="failed = true"
    />
  </span>
</template>

<script lang="ts">
import { defineComponent, ref, watch } from 'vue';
import LazyImage from '@shell/components/LazyImage.vue';
import { BLANK_IMAGE } from '@shell/utils/style';

// Renders a pre-validated partner icon (see browserSafeBlueprintIcon) next to
// the source badge. It never replaces the badge, so a look-alike logo cannot
// hide a blueprint's provenance. LazyImage defers the request until the tile
// is on screen; on error it swaps to a blank image and we hide the wrapper.
export default defineComponent({
  name:       'BlueprintPartnerLogo',
  components: { LazyImage },
  props:      {
    icon: {
      type:    String,
      default: undefined,
    },
  },
  setup(props) {
    const failed = ref(false);

    watch(() => props.icon, () => {
      failed.value = false;
    });

    return { failed, BLANK_IMAGE };
  },
});
</script>

<style lang="scss" scoped>
.partner-logo {
  display: inline-flex;
  align-items: center;
  height: 20px;
  padding: 1px 3px;
  border-radius: 3px;
  line-height: 0;
}

.partner-logo-img {
  max-height: 16px;
  max-width: 60px;
  width: auto;
  object-fit: contain;
}
</style>

<style lang="scss">
/* Not scoped: the theme class lives on <body>. A light backing keeps dark-on-
   transparent partner logos legible in the dark theme. */
body.theme-dark .partner-logo { background: rgba(255, 255, 255, 0.9); }
</style>
