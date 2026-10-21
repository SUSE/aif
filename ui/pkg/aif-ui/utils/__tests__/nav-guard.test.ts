import { beforeEach, describe, expect, it, vi } from 'vitest';
import { canAccessExtension, ManagementNotReadyError } from '../access';
import { createAccessNavGuard } from '../nav-guard';
import type { RancherStore } from '../../types/rancher-types';

vi.mock('../access', async(importOriginal) => ({
  ...(await importOriginal<typeof import('../access')>()),
  canAccessExtension: vi.fn(),
}));
vi.mock('../logger', () => ({ logger: { warn: vi.fn() } }));

const GROWL = {
  title:   'Access check failed',
  message: 'Unable to verify extension access. Please reload the page.',
  timeout: 8000,
};

describe('createAccessNavGuard', () => {
  let store: RancherStore;
  let next: ReturnType<typeof vi.fn>;
  const guard = (name: string) => createAccessNavGuard(store, 'suseai')({ name } as never, {} as never, next as never);

  beforeEach(() => {
    store = { dispatch: vi.fn() } as unknown as RancherStore;
    next = vi.fn();
    vi.mocked(canAccessExtension).mockReset();
  });

  it('ignores routes outside the extension', async () => {
    await guard('c-cluster-explorer');

    expect(canAccessExtension).not.toHaveBeenCalled();
    expect(next).toHaveBeenCalledWith();
  });

  it('allows access when the check passes', async () => {
    vi.mocked(canAccessExtension).mockResolvedValue(true);

    await guard('c-cluster-suseai-overview');

    expect(next).toHaveBeenCalledWith();
    expect(store.dispatch).not.toHaveBeenCalled();
  });

  it('redirects home without a growl when access is denied', async () => {
    vi.mocked(canAccessExtension).mockResolvedValue(false);

    await guard('c-cluster-suseai-overview');

    expect(next).toHaveBeenCalledWith({ name: 'home' });
    expect(store.dispatch).not.toHaveBeenCalled();
  });

  it('shows a growl and redirects home when the management store never loads', async () => {
    vi.mocked(canAccessExtension).mockRejectedValue(new ManagementNotReadyError());

    await guard('c-cluster-suseai-overview');

    expect(store.dispatch).toHaveBeenCalledWith('growl/error', GROWL);
    expect(next).toHaveBeenCalledWith({ name: 'home' });
  });

  it('shows the same growl when the check throws unexpectedly', async () => {
    vi.mocked(canAccessExtension).mockRejectedValue(new Error('boom'));

    await guard('c-cluster-suseai-overview');

    expect(store.dispatch).toHaveBeenCalledWith('growl/error', GROWL);
    expect(next).toHaveBeenCalledWith({ name: 'home' });
  });
});
