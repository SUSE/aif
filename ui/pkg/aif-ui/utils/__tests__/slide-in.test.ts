import { describe, it, expect, vi } from 'vitest';
import { openWideSlideIn, usesLegacySlideInSizes } from '../slide-in';

const Panel = { name: 'Panel' };

function fakeStore(serverVersion: string | undefined, hasSlideIn = true) {
  return {
    hasModule: vi.fn((name: string | string[]) => hasSlideIn && name === 'slideInPanel'),
    getters:   {
      'management/byId': vi.fn((type: string, id: string) => (
        type === 'management.cattle.io.setting' && id === 'server-version' && serverVersion !== undefined ? { value: serverVersion } : undefined
      )),
    },
    commit: vi.fn(),
  };
}

describe('usesLegacySlideInSizes', () => {
  it('is true for Rancher releases before 2.15', () => {
    expect(usesLegacySlideInSizes('v2.12.0')).toBe(true);
    expect(usesLegacySlideInSizes('v2.13.1')).toBe(true);
    expect(usesLegacySlideInSizes('v2.14.3')).toBe(true);
  });

  it('is false from Rancher 2.15 on', () => {
    expect(usesLegacySlideInSizes('v2.15.0')).toBe(false);
    expect(usesLegacySlideInSizes('v2.15.2')).toBe(false);
    expect(usesLegacySlideInSizes('v2.16.0-rc1')).toBe(false);
    expect(usesLegacySlideInSizes('v3.0.0')).toBe(false);
  });

  it('treats dev, head and missing versions as current', () => {
    expect(usesLegacySlideInSizes('head')).toBe(false);
    expect(usesLegacySlideInSizes('dev')).toBe(false);
    expect(usesLegacySlideInSizes('')).toBe(false);
    expect(usesLegacySlideInSizes(undefined)).toBe(false);
  });
});

describe('openWideSlideIn', () => {
  it('opens with raw CSS sizes on Rancher 2.13', () => {
    const store = fakeStore('v2.13.1');

    expect(openWideSlideIn(store, Panel, { props: { workload: 'w' }, closeOnRouteChange: ['name'] })).toBe(true);
    expect(store.commit).toHaveBeenCalledWith('slideInPanel/open', {
      component:      Panel,
      componentProps: {
        width: '73%', height: '100vh', top: '0', showHeader: false, closeOnRouteChange: ['name'], workload: 'w',
      },
    });
  });

  it('opens with size presets on Rancher 2.15', () => {
    const store = fakeStore('v2.15.2');

    openWideSlideIn(store, Panel, { props: { workload: 'w' } });
    expect(store.commit).toHaveBeenCalledWith('slideInPanel/open', {
      component:      Panel,
      componentProps: {
        width: 'wide', height: 'full', showHeader: false, closeOnRouteChange: undefined, workload: 'w',
      },
    });
  });

  it('does nothing on hosts without a slide-in panel (Rancher < 2.12)', () => {
    const store = fakeStore('v2.11.4', false);

    expect(openWideSlideIn(store, Panel)).toBe(false);
    expect(store.commit).not.toHaveBeenCalled();
  });
});
