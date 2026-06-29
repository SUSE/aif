import { getCurrentInstance } from 'vue';

/**
 * Translation helper — reads from the Rancher i18n store (l10n/en-us.json),
 * falling back to the literal string when a key is missing.
 *
 * Must be called synchronously during component setup so that
 * `getCurrentInstance()` resolves to the calling component.
 */
export function useT() {
  const store = (getCurrentInstance()!.proxy as any)?.$store;

  return (key: string, fallback: string): string => store?.getters['i18n/t']?.(key) || fallback;
}
