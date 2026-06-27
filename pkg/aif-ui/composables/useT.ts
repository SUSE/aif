import { getCurrentInstance } from 'vue';

/**
 * Translation helper — reads from the Rancher i18n store (l10n/en-us.json),
 * falling back to the literal string when a key is missing.
 *
 * Must be called synchronously during component setup so that
 * `getCurrentInstance()` resolves to the calling component.
 */
export function useT() {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const store = (getCurrentInstance()?.proxy as any)?.$store; // Vue component proxy has $store but no type declaration in this context

  return (key: string, fallback: string): string => store?.getters['i18n/t']?.(key) || fallback;
}
