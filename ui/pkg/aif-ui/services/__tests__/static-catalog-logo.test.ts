import { beforeEach, describe, expect, it, vi } from 'vitest';

const getCatalog = vi.fn();

vi.mock('../../utils/operator-api', () => ({
  getCatalog: (...args: unknown[]) => getCatalog(...args),
}));

import { fetchStaticCatalog } from '../static-catalog';
import { resolveCatalogLogo } from '../../utils/catalog-logo';

describe('static catalog logo isolation', () => {
  beforeEach(() => getCatalog.mockReset());

  it('removes network-backed logo metadata while retaining a bundled display logo', async() => {
    getCatalog.mockResolvedValue([
      { name: 'Ollama', slug_name: 'ollama', library: 'suse-ai', logo_url: 'https://apps.rancher.io/logos/ollama.png' },
      { name: 'Local', slug_name: 'local', logo_url: 'data:image/png;base64,iVBORw0KGgo=' },
    ]);

    const apps = await fetchStaticCatalog();
    expect(apps).toEqual([
      { name: 'Ollama', slug_name: 'ollama', library: 'suse-ai', logo_url: undefined },
      { name: 'Local', slug_name: 'local', logo_url: 'data:image/png;base64,iVBORw0KGgo=' },
    ]);
    expect(resolveCatalogLogo(apps[0])).toMatch(/^data:image\/png;base64,/);
    expect(resolveCatalogLogo(apps[1])).toBe('data:image/png;base64,iVBORw0KGgo=');
  });
});
