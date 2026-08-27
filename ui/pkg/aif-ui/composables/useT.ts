import { getCurrentInstance } from 'vue';

/**
 * Translation helper — reads from the Rancher i18n store (l10n/en-us.yaml),
 * falling back to the literal string when a key is missing.
 *
 * Prefer this over @rancher/shell's global `t()`: that helper's second argument
 * is the interpolation `args` object, not a fallback, so `t('a.key', 'Label')`
 * renders `%a.key%` on a miss — or `%a.key%(0: L, 1: a, ...)`, since the string
 * gets spread as args.
 *
 * Must be called synchronously during component setup so that
 * `getCurrentInstance()` resolves to the calling component.
 */
export function useT() {
  const store = (getCurrentInstance()!.proxy as any)?.$store;

  return (key: string, fallback: string): string => store?.getters['i18n/t']?.(key) || fallback;
}
