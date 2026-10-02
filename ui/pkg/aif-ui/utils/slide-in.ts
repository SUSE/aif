// Opens a full-height, wide slide-in drawer on every Rancher version that has one.
//
// The extension is built against the latest @rancher/shell, but the panel that
// renders the drawer (SlideInPanelManager) belongs to the host dashboard, so it
// is whatever the installed Rancher ships:
//
//   - Rancher 2.10/2.11 have no slide-in panel at all.
//   - Rancher 2.12–2.14 take raw CSS sizes, the way their own resource detail
//     drawer opens (width '73%', height '100vh', top '0').
//   - Rancher 2.15+ take the 'wide'/'full' presets and ignore raw widths.
//
// useShell().slideIn would be the natural call, but $shell only exists from
// Rancher 2.14 and throws on anything older. All it does is commit to the
// slideInPanel store, which is available since 2.12, so we commit directly.
import type { Component } from 'vue';
import type { Store } from 'vuex';
import { MANAGEMENT } from '@shell/config/types';
import { SETTING } from '@shell/config/settings';

// The slice of the Vuex root store this needs, so tests don't have to supply a
// full Store.
export type SlideInStore = Pick<Store<unknown>, 'hasModule' | 'getters' | 'commit'>;

const LEGACY_SIZE = { width: '73%', height: '100vh', top: '0' };
const PRESET_SIZE = { width: 'wide', height: 'full' };

/**
 * Whether the host dashboard expects raw CSS sizes, i.e. Rancher < 2.15.
 * Release versions look like "v2.13.1"; anything unparseable (dev or head
 * builds) is assumed to be current.
 */
export function usesLegacySlideInSizes(serverVersion: string | undefined): boolean {
  const match = /^v?(\d+)\.(\d+)/.exec(serverVersion || '');

  if (!match) return false;

  const major = Number(match[1]);
  const minor = Number(match[2]);

  return major < 2 || (major === 2 && minor < 15);
}

export interface WideSlideInConfig {
  /** Passed to the component; props named onX act as listeners for its emits. */
  props?: Record<string, unknown>;
  /** Route changes that close the panel (default: any). */
  closeOnRouteChange?: string[];
}

/**
 * Opens `component` in the host's slide-in panel, taking the same config as
 * useShell().slideIn.open() minus the sizing, which is picked per Rancher version.
 * Returns false when the host has no slide-in panel (Rancher < 2.12).
 */
export function openWideSlideIn(store: SlideInStore, component: Component, { props = {}, closeOnRouteChange }: WideSlideInConfig = {}): boolean {
  if (!store.hasModule('slideInPanel')) return false;

  const setting = store.getters['management/byId'](MANAGEMENT.SETTING, SETTING.VERSION_RANCHER) as { value?: string } | undefined;
  const serverVersion = setting?.value;

  store.commit('slideInPanel/open', {
    component,
    componentProps: {
      ...(usesLegacySlideInSizes(serverVersion) ? LEGACY_SIZE : PRESET_SIZE),
      // The drawer renders its own title bar; pre-2.15 panels add a generic
      // "Details" header unless told not to.
      showHeader: false,
      closeOnRouteChange,
      ...props,
    },
  });

  return true;
}
