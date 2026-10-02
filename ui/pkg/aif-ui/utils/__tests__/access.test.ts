import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { isAdminUser } from '@shell/store/type-map';
import { canAccessExtension, invalidateAccessCache, MANAGEMENT_READY_TIMEOUT_MS } from '../access';
import type { RancherStore } from '../../types/rancher-types';

vi.mock('@shell/store/type-map', () => ({ isAdminUser: vi.fn() }));

type State = { managementReady: boolean };
type Watcher = { getter: (state: State) => boolean; cb: (value: boolean, oldValue: boolean) => void; active: boolean };

// Minimal store: watch() re-evaluates its getter whenever setReady() changes state.
function createStore(managementReady: boolean) {
  const watchers: Watcher[] = [];
  const state: State = { managementReady };
  const store = {
    state,
    getters:  { 'management/schemaFor': vi.fn(() => null) },
    dispatch: vi.fn(),
    watch:    vi.fn((getter: Watcher['getter'], cb: Watcher['cb']) => {
      const w: Watcher = { getter, cb, active: true };

      watchers.push(w);

      return () => {
        w.active = false;
      };
    }),
  };
  const setReady = (ready: boolean) => {
    const old = state.managementReady;

    state.managementReady = ready;
    watchers.filter((w) => w.active).forEach((w) => w.cb(w.getter(state), old));
  };

  return { store: store as unknown as RancherStore, setReady, watchers };
}

describe('canAccessExtension', () => {
  beforeEach(() => {
    invalidateAccessCache();
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('checks access straight away when the management store is ready', async () => {
    const { store } = createStore(true);

    vi.mocked(isAdminUser).mockReset().mockReturnValue(true);

    await expect(canAccessExtension(store)).resolves.toBe(true);
    expect(store.watch).not.toHaveBeenCalled();
  });

  it('waits for the management store before calling isAdminUser', async () => {
    const { store, setReady, watchers } = createStore(false);

    // Like the real getter: throws "Schemas aren't loaded yet" until the store is ready
    vi.mocked(isAdminUser).mockReset().mockImplementation(() => {
      if (!store.state.managementReady) throw new Error("Schemas aren't loaded yet");

      return true;
    });

    const result = canAccessExtension(store);

    await vi.advanceTimersByTimeAsync(1000);
    expect(isAdminUser).not.toHaveBeenCalled();

    setReady(true);

    await expect(result).resolves.toBe(true);
    expect(watchers[0].active).toBe(false);
  });

  it('keeps waiting while managementReady stays false', async () => {
    const { store, setReady } = createStore(false);

    vi.mocked(isAdminUser).mockReset().mockReturnValue(true);

    const result = canAccessExtension(store);

    setReady(false);
    await vi.advanceTimersByTimeAsync(1000);
    expect(isAdminUser).not.toHaveBeenCalled();

    setReady(true);

    await expect(result).resolves.toBe(true);
  });

  it('fails closed without caching when the management store never loads', async () => {
    const { store, setReady, watchers } = createStore(false);

    vi.mocked(isAdminUser).mockReset().mockReturnValue(true);

    const result = canAccessExtension(store);

    await vi.advanceTimersByTimeAsync(MANAGEMENT_READY_TIMEOUT_MS);

    await expect(result).resolves.toBe(false);
    expect(isAdminUser).not.toHaveBeenCalled();
    expect(watchers[0].active).toBe(false);

    // Not cached: once the store loads, the next navigation is allowed
    setReady(true);
    await expect(canAccessExtension(store)).resolves.toBe(true);
  });
});
