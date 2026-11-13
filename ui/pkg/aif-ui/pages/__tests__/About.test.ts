// @vitest-environment jsdom
import {
  describe, expect, it, vi,
} from 'vitest';
import { flushPromises, mount } from '@vue/test-utils';
import About from '../About.vue';
import { getVersion } from '../../utils/operator-api';

vi.mock('../../utils/operator-api', () => ({ getVersion: vi.fn() }));

async function mountAbout() {
  const wrapper = mount(About, { global: { mocks: { t: (key: string) => key } } });

  await (wrapper.vm as any).$options.fetch.call(wrapper.vm);
  await flushPromises();

  return wrapper;
}

describe('About operator version', () => {
  it('shows the operator version', async() => {
    vi.mocked(getVersion).mockResolvedValue({ version: 'v2.5.0', commit: 'abc', chartVersion: '2.5.0' });

    const wrapper = await mountAbout();

    expect(wrapper.text()).toContain('v2.5.0');
  });

  it('shows "not permitted" instead of failing when the user may not read the version (403)', async() => {
    vi.mocked(getVersion).mockRejectedValue(Object.assign(new Error('forbidden'), { status: 403, code: 'FORBIDDEN' }));
    vi.spyOn(console, 'warn').mockImplementation(() => {});

    const wrapper = await mountAbout();

    // Operator and chart rows both depend on GET /api/v1/version.
    expect(wrapper.text().split('suseai.pages.about.sections.versions.notPermitted')).toHaveLength(3);
  });

  it('shows "unavailable" for other errors', async() => {
    vi.mocked(getVersion).mockRejectedValue(Object.assign(new Error('down'), { status: 503, code: 'OPERATOR_UNREACHABLE' }));
    vi.spyOn(console, 'warn').mockImplementation(() => {});

    const wrapper = await mountAbout();

    expect(wrapper.text()).toContain('suseai.pages.about.sections.versions.unavailable');
  });
});
