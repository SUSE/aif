/**
 * Vue mixin for internationalization support
 * Provides translation methods to Vue components
 */

import { defineComponent } from 'vue';
import { t, hasTranslation, getAvailableLocales, isValidLocale } from '../config/i18n';

export const i18nMixin = defineComponent({
  methods: {
    /**
     * Translate a key to localized string
     * @param key Translation key (dot notation supported)
     * @param params Parameters for string interpolation
     * @param locale Locale to use (defaults to current locale)
     */
    t(key: string, params?: Record<string, any>, locale?: string): string {
      return t(key, params, locale);
    },

    /**
     * Check if translation exists for a key
     * @param key Translation key
     * @param locale Locale to check
     */
    hasTranslation(key: string, locale?: string): boolean {
      return hasTranslation(key, locale);
    },

    /**
     * Get available locales
     */
    getAvailableLocales(): readonly string[] {
      return getAvailableLocales();
    },

    /**
     * Check if locale is valid
     * @param locale Locale to validate
     */
    isValidLocale(locale: string): boolean {
      return isValidLocale(locale);
    }
  }
});

// Export for direct import in TypeScript components
export { t, hasTranslation, getAvailableLocales, isValidLocale };