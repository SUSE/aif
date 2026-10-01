<template>
  <Tag
    class="source-badge"
    :class="`source-badge--${ label.toLowerCase() }`"
    :aria-label="`Source: ${ label }`"
  >
    <template v-if="label === 'Nvidia'">
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
    <template v-else-if="label === 'SUSE'">
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
    <template v-else>
      {{ label }}
    </template>
  </Tag>
</template>

<script lang="ts">
import { defineComponent, computed } from 'vue';
import Tag from '@shell/components/Tag.vue';

const nvidiaLogo     = require('../assets/nvidia-logo-horz.svg') as string;
const nvidiaLogoDark = require('../assets/nvidia-logo-horz-light.svg') as string;
const suseLogo       = require('../assets/SUSE_Logo-hor_L_Green-pos_sRGB.svg') as string;
const suseLogoDark   = require('../assets/SUSE_Logo-hor_L_Green-White-neg_sRGB.svg') as string;

export default defineComponent({
  name:       'BlueprintSourceBadge',
  components: { Tag },
  props:      {
    source: {
      type:    String,
      default: 'Custom',
    },
  },
  setup(props) {
    const label = computed(() => props.source || 'Custom');

    return {
      label, nvidiaLogo, nvidiaLogoDark, suseLogo, suseLogoDark,
    };
  },
});
</script>

<style lang="scss" scoped>
/* Tag supplies colors, radius and font size. `.tag.source-badge` outranks
   Tag's own scoped `.tag` so the logo layout wins regardless of CSS order. */
.tag.source-badge {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  height: 20px;
  padding: 0 6px;

  .source-logo {
    height: 13px;
    width: auto;
  }
}
</style>

<style lang="scss">
/* Not scoped: the theme class lives on <body>, outside this component. A
   scoped :global(body...) would compile to a bare `body` rule and hide the page. */
body:not(.theme-dark) .nvidia-logo--dark { display: none; }
body.theme-dark .nvidia-logo--light { display: none; }
body:not(.theme-dark) .suse-logo--dark { display: none; }
body.theme-dark .suse-logo--light { display: none; }
</style>
