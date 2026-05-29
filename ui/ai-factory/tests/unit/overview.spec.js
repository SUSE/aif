import {
  describe, it, expect, beforeEach, afterEach, vi
} from 'vitest';

// Pure-logic mirror of the silentRefresh / refresh / fetchData methods on
// pages/overview.vue. Mirrors the settings.spec.js pattern (see that file's
// design-intent comment) — the SFC imports `@components/Banner` and an
// alias-resolved Steve type whose underlying paths are not reachable in
// unit-test mode without the full Rancher shell installed. The methods are
// reproduced here verbatim; keep them in sync with
// pkg/ai-factory/pages/overview.vue when the implementation changes.
//
// What this pins:
//   - silentRefresh MUST NOT set this.error on a fetch failure (silent poll
//     contract — the user must never see an error banner mid-poll).
//   - refresh (user-initiated) MUST set this.error on a fetch failure so
//     the Banner is rendered when the user explicitly asks to retry.
//   - The 10s setInterval / clearInterval pairing must release callbacks
//     after teardown (Vue 3 lifecycle hook contract).
//
// The scaffold test (pkg/ai-factory/test/p6-10-overview.test.mjs) already
// pins the source-level shape (`setInterval(...silentRefresh...)`), so the
// lifecycle wiring itself isn't reasserted here.

// NOTE: fresh `listWorkloads` per test (not module-scope). vitest 4.x flags
// unhandled rejection when a module-scope `vi.fn()` is reset via mockReset
// between tests AND the fresh implementation returns a rejected promise;
// constructing a new vi.fn per test sidesteps that.
let listWorkloads;

function makeCtx({ dispatch } = {}) {
  return {
    workloads:  [],
    blueprints: [],
    error:      null,
    _timer:     null,
    $store:     { dispatch: dispatch || vi.fn().mockResolvedValue([]) },

    // Methods mirrored from pages/overview.vue. Keep structurally identical.
    async fetchData() {
      const [workloads, blueprints] = await Promise.all([
        listWorkloads(),
        this.$store.dispatch('management/findAll', { type: 'ai.suse.com.blueprint' }),
      ]);

      this.workloads = workloads;
      this.blueprints = blueprints;
    },

    async loadData() {
      this.error = null;
      try {
        await this.fetchData();
      } catch (e) {
        this.error = e;
      }
    },

    async silentRefresh() {
      try {
        await this.fetchData();
      } catch (e) {
        /* swallow — keep last good data */
      }
    },

    async refresh() {
      await this.loadData();
    },
  };
}

describe('OverviewPage silentRefresh (background poll)', () => {
  beforeEach(() => { listWorkloads = vi.fn(); });

  it('swallows fetch errors and leaves this.error null', async() => {
    listWorkloads.mockImplementation(() => Promise.reject(new Error('boom')));
    const ctx = makeCtx();

    await ctx.silentRefresh();

    expect(ctx.error).toBeNull();
  });

  it('keeps last-good workloads on transient failure', async() => {
    const stale = [{ metadata: { name: 'old' }, status: { phase: 'Running' } }];

    listWorkloads.mockImplementation(() => Promise.reject(new Error('network')));
    const ctx = makeCtx();

    ctx.workloads = stale;

    await ctx.silentRefresh();

    expect(ctx.workloads).toBe(stale);
    expect(ctx.error).toBeNull();
  });

  it('updates workloads on success', async() => {
    const fresh = [{ metadata: { name: 'new' }, status: { phase: 'Running' } }];

    listWorkloads.mockResolvedValue(fresh);
    const ctx = makeCtx();

    await ctx.silentRefresh();

    expect(ctx.workloads).toBe(fresh);
    expect(ctx.error).toBeNull();
  });
});

describe('OverviewPage refresh (user-initiated)', () => {
  beforeEach(() => { listWorkloads = vi.fn(); });

  it('surfaces listWorkloads errors via this.error so the Banner renders', async() => {
    const err = new Error('explicit-failure');

    listWorkloads.mockImplementation(() => Promise.reject(err));
    const ctx = makeCtx();

    await ctx.refresh();

    expect(ctx.error).toBe(err);
  });

  it('clears a prior error on a successful refresh', async() => {
    listWorkloads.mockResolvedValue([]);
    const ctx = makeCtx();

    ctx.error = new Error('previous');

    await ctx.refresh();

    expect(ctx.error).toBeNull();
  });
});

describe('OverviewPage poll-timer teardown', () => {
  beforeEach(() => {
    listWorkloads = vi.fn().mockResolvedValue([]);
    vi.useFakeTimers();
  });

  afterEach(() => vi.useRealTimers());

  it('clearInterval on the stored _timer prevents further silentRefresh callbacks', () => {
    const ctx = makeCtx();

    // Mirror mounted(): install a 10s setInterval bound to silentRefresh.
    ctx._timer = setInterval(ctx.silentRefresh.bind(ctx), 10 * 1000);
    expect(ctx._timer).not.toBeNull();

    // Mirror beforeUnmount(): tear the timer down. Advancing fake timers
    // afterwards must not invoke listWorkloads (silentRefresh would call it).
    clearInterval(ctx._timer);

    vi.advanceTimersByTime(60_000);

    expect(listWorkloads).not.toHaveBeenCalled();
  });

  it('without teardown, the timer fires silentRefresh on schedule', async() => {
    const ctx = makeCtx();

    ctx._timer = setInterval(ctx.silentRefresh.bind(ctx), 10 * 1000);

    await vi.advanceTimersByTimeAsync(25_000);

    // 25s / 10s = 2 fires (at 10s and 20s).
    expect(listWorkloads).toHaveBeenCalledTimes(2);

    clearInterval(ctx._timer);
  });
});
