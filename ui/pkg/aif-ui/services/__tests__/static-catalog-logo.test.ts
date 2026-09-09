import { beforeEach, describe, expect, it, vi } from 'vitest';

const getCatalog = vi.fn();

vi.mock('../../utils/operator-api', () => ({
  getCatalog: (...args: unknown[]) => getCatalog(...args),
}));

import { fetchStaticCatalog } from '../static-catalog';

describe('static catalog logo isolation', () => {
  beforeEach(() => getCatalog.mockReset());

  it('removes network-backed logo metadata before the Apps view receives it', async() => {
    getCatalog.mockResolvedValue([
      { name: 'Ollama', slug_name: 'ollama', logo_url: 'https://apps.rancher.io/logos/ollama.png' },
      { name: 'Local', slug_name: 'local', logo_url: 'data:image/png;base64,iVBORw0KGgo=' },
    ]);

    await expect(fetchStaticCatalog()).resolves.toEqual([
      { name: 'Ollama', slug_name: 'ollama', logo_url: undefined },
      { name: 'Local', slug_name: 'local', logo_url: 'data:image/png;base64,iVBORw0KGgo=' },
    ]);
  });
});
