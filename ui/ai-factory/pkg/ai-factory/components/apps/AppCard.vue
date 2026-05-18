<template>
  <div :class="['app-card', { 'app-card--ref-blueprint': app.referenceBlueprint }]">
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
          <span v-if="app.referenceBlueprint" class="publisher-badge publisher-badge--ref-blueprint">
            {{ t('aif.pages.apps.badge.referenceBlueprint') }}
          </span>
          <span class="app-card__version">v{{ app.version }}</span>
          <span v-if="formattedDate" class="app-card__updated">{{ formattedDate }}</span>
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

    <div v-if="displayTags.length" class="app-card__tags">
      <span v-for="tag in displayTags" :key="tag" class="app-card__tag">{{ tag }}</span>
    </div>

    <div class="app-card__actions">
      <button
        class="btn role-primary btn-sm"
        disabled
        :title="t('aif.pages.apps.card.installDisabled')"
        @click.stop="$emit('install', app)"
      >
        {{ t('aif.pages.apps.card.install') }}
      </button>
      <button
        class="btn role-secondary btn-sm"
        @click.stop="$event.currentTarget.blur(); $emit('add-to-bundle', app)"
      >
        {{ t('aif.pages.apps.card.addToBundle') }}
      </button>
    </div>
  </div>
</template>

<script>
import { defineComponent, computed, getCurrentInstance } from 'vue';
import { formatDate } from '../../utils/date';
import { FALLBACK_LOGO } from '../../config/constants';

export default defineComponent({
  name: 'AppCard',

  props: {
    app: {
      type:     Object,
      required: true
    }
  },

  emits: ['install', 'add-to-bundle'],

  setup(props) {
    const instance = getCurrentInstance();
    const t = instance?.proxy?.t?.bind(instance.proxy) || ((key) => key);
    const fallbackLogo = FALLBACK_LOGO;

    const formattedDate = computed(() => {
      const formatted = formatDate(props.app.lastUpdatedAt);

      return formatted === '—' ? '' : formatted;
    });

    const displayTags = computed(() => {
      const all = [...(props.app.categories || []), ...(props.app.tags || [])];
      const unique = [...new Set(all)];

      return unique.slice(0, 2);
    });

    const onImgError = (event) => {
      event.target.src = FALLBACK_LOGO;
    };

    return { fallbackLogo, formattedDate, displayTags, onImgError, t };
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
  min-height: 200px;
  transition: border-color 0.2s ease;

  &:hover {
    border-color: var(--primary);
  }

  &--ref-blueprint {
    border-left: 3px solid var(--warning, #ff9800);
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

  &--ref-blueprint {
    background: var(--warning-banner-bg, #fff3e0);
    color: var(--warning, #e65100);
  }
}

.app-card__version,
.app-card__updated {
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

.app-card__tags {
  display: flex;
  gap: 4px;
}

.app-card__tag {
  background: var(--accent-btn, #f5f5f5);
  padding: 2px 8px;
  border-radius: 4px;
  font-size: 11px;
  color: var(--muted);
}

.app-card__actions {
  display: flex;
  gap: 8px;
  margin-top: auto;

  .btn {
    flex: 1;
  }
}
</style>
