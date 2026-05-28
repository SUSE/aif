<template>
  <div
    :class="['app-card', { 'app-card--ref-blueprint': app.referenceBlueprint }]"
    role="button"
    tabindex="0"
    @click="$emit('install', app)"
    @keydown.enter="$emit('install', app)"
    @keydown.space.prevent="$emit('install', app)"
  >
    <div class="app-card__header">
      <img
        :src="app.logoURL || fallbackLogo"
        :alt="app.displayName || app.name"
        class="app-card__logo"
        @error="onImgError"
      />
      <div class="app-card__info">
        <h3 class="app-card__title">{{ app.displayName || app.name }}</h3>
        <div class="app-card__badges">
          <span :class="['publisher-badge', `publisher-badge--${app.source}`]">
            {{ t(`aif.pages.apps.badge.${app.source}`) }}
          </span>
          <span class="packaging-badge">
            {{ app.assetType === 'chart' ? t('aif.pages.apps.packaging.helm') : t('aif.pages.apps.packaging.container') }}
          </span>
          <span v-if="formattedVersion" class="app-card__version">{{ formattedVersion }}</span>
        </div>
      </div>
      <a
        v-if="app.projectURL"
        :href="app.projectURL"
        target="_blank"
        rel="noopener noreferrer"
        class="app-card__external-link"
        :title="t('aif.pages.apps.card.externalLink')"
        @click.stop
      >
        <i class="icon icon-external-link" />
      </a>
    </div>

    <p class="app-card__description">{{ app.description || '—' }}</p>
  </div>
</template>

<script>
import { defineComponent, computed, getCurrentInstance } from 'vue';
import { FALLBACK_LOGO } from '../../config/constants';

export default defineComponent({
  name: 'AppCard',

  props: {
    app: {
      type:     Object,
      required: true
    }
  },

  emits: ['install'],

  setup(props) {
    const instance = getCurrentInstance();
    const t = instance?.proxy?.t?.bind(instance.proxy) || ((key) => key);
    const fallbackLogo = FALLBACK_LOGO;

    const formattedVersion = computed(() => {
      const v = props.app.version;

      if (!v) {
        return '';
      }

      return /^\d/.test(v) ? `v${ v }` : v;
    });

    const onImgError = (event) => {
      event.target.src = FALLBACK_LOGO;
    };

    return { fallbackLogo, formattedVersion, onImgError, t };
  }
});
</script>

<style lang="scss" scoped>
.app-card {
  display: flex;
  flex-direction: column;
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: 16px;
  gap: 12px;
  min-height: 160px;
  cursor: pointer;
  transition: border-color 0.2s ease, box-shadow 0.2s ease;

  &:hover,
  &:focus {
    border-color: var(--primary);
    box-shadow: 0 1px 4px rgba(0, 0, 0, 0.08);
    outline: none;
  }
}

.app-card__header {
  display: flex;
  align-items: flex-start;
  gap: 12px;
}

.app-card__logo {
  width: 44px;
  height: 44px;
  object-fit: contain;
  border-radius: 8px;
  background: var(--accent-btn);
  border: 1px solid var(--border);
  flex-shrink: 0;
  padding: 6px;
}

.app-card__info {
  flex: 1;
  min-width: 0;
}

.app-card__title {
  margin: 0;
  font-size: 14px;
  font-weight: 600;
  line-height: 1.4;
  color: var(--body-text);
}

.app-card__badges {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  align-items: center;
  margin-top: 4px;
}

.publisher-badge {
  padding: 1px 6px;
  border-radius: 8px;
  font-size: 10px;
  font-weight: 600;

  &--nvidia {
    background: var(--success-banner-bg, #dcfce7);
    color: var(--success, #166534);
  }

  &--suse {
    background: var(--info-banner-bg, #dbeafe);
    color: var(--info, #1d4ed8);
  }
}

.packaging-badge {
  padding: 1px 6px;
  border-radius: 8px;
  font-size: 10px;
  font-weight: 600;
  background: var(--accent-btn, #f5f5f5);
  color: var(--muted);
}

.app-card__version {
  color: var(--muted);
  font-size: 10px;
}

.app-card__external-link {
  color: var(--muted);
  font-size: 14px;
  transition: color 0.2s ease;
  flex-shrink: 0;

  &:hover {
    color: var(--primary);
    text-decoration: none;
  }
}

.app-card__description {
  margin: 0;
  color: var(--body-text);
  font-size: 13px;
  line-height: 1.5;
  flex: 1;
  display: -webkit-box;
  -webkit-line-clamp: 3;
  -webkit-box-orient: vertical;
  overflow: hidden;
}
</style>
