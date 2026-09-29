<template>
  <span
    class="source-badge"
    :class="`source-badge--${ (source || 'custom').toLowerCase() }`"
    :aria-label="`Source: ${ source || 'Custom' }`"
  >
    <template v-if="hasCustomIcon">
      <span class="bp-source-badge__partner-logo-wrapper">
        <img
          :src="icon"
          alt=""
          aria-hidden="true"
          class="source-logo partner-logo"
          @error="onImageError"
        />
      </span>
    </template>
    <template v-else-if="source === 'Nvidia'">
      <img
        :src="nvidiaLogo"
        alt=""
        aria-hidden="true"
        class="source-logo nvidia-logo--light"
      />
      <img
        :src="nvidiaLogoDark"
        alt=""
        aria-hidden="true"
        class="source-logo nvidia-logo--dark"
      />
    </template>
    <template v-else-if="source === 'SUSE'">
      <img
        :src="suseLogo"
        alt=""
        aria-hidden="true"
        class="source-logo suse-logo--light"
      />
      <img
        :src="suseLogoDark"
        alt=""
        aria-hidden="true"
        class="source-logo suse-logo--dark"
      />
    </template>
    <template v-else>{{ source || 'Custom' }}</template>
  </span>
</template>

<script lang="ts">
import { defineComponent, ref, computed, watch } from 'vue';

const nvidiaLogo     = require('../assets/nvidia-logo-horz.svg') as string;
const nvidiaLogoDark = require('../assets/nvidia-logo-horz-light.svg') as string;
const suseLogo       = require('../assets/SUSE_Logo-hor_L_Green-pos_sRGB.svg') as string;
const suseLogoDark   = require('../assets/SUSE_Logo-hor_L_Green-White-neg_sRGB.svg') as string;

export default defineComponent({
  name: 'BlueprintSourceBadge',
  props: {
    source: {
      type:    String,
      default: 'Custom',
    },
    icon: {
      type:    String,
      default: undefined,
    },
  },
  setup(props) {
    const imageError = ref(false);

    watch(
      () => props.icon,
      () => {
        imageError.value = false;
      }
    );

    const hasCustomIcon = computed(() => Boolean(props.icon) && !imageError.value);

    function onImageError() {
      imageError.value = true;
    }

    return {
      nvidiaLogo,
      nvidiaLogoDark,
      suseLogo,
      suseLogoDark,
      hasCustomIcon,
      onImageError,
    };
  },
});
</script>

<style lang="scss" scoped>
.source-badge {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  min-height: 20px;
  font-size: 12px;
  line-height: 16px;
  padding: 0 6px;
  border-radius: var(--border-radius);
  color: var(--tag-primary);
  background: var(--tag-bg);

  .source-logo {
    height: 13px;
    width: auto;
    max-width: 60px;
    object-fit: contain;
  }

  .bp-source-badge__partner-logo-wrapper {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    border-radius: 3px;
    padding: 1px 3px;
    line-height: 0;
  }

  .partner-logo {
    height: auto;
    max-height: 16px;
    width: auto;
    max-width: 60px;
    object-fit: contain;
  }
}

:global(body:not(.theme-dark)) .nvidia-logo--dark { display: none; }
:global(body.theme-dark) .nvidia-logo--light { display: none; }
:global(body:not(.theme-dark)) .suse-logo--dark { display: none; }
:global(body.theme-dark) .suse-logo--light { display: none; }
:global(body.theme-dark) .source-badge .bp-source-badge__partner-logo-wrapper {
  background: rgba(255, 255, 255, 0.9);
}
</style>
