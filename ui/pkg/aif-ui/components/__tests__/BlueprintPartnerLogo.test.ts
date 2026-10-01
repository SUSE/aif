// @vitest-environment jsdom
import {
  afterEach, beforeEach, describe, expect, it, vi,
} from 'vitest';
import { mount } from '@vue/test-utils';
import { nextTick } from 'vue';
import BlueprintPartnerLogo from '../BlueprintPartnerLogo.vue';

const remote = 'https://partner.example.com/logo.png';

describe('BlueprintPartnerLogo', () => {
  beforeEach(() => {
    vi.stubGlobal('IntersectionObserver', class {
      constructor(private cb: (entries: { isIntersecting: boolean }[]) => void) {}

      observe() {
        this.cb([{ isIntersecting: true }]);
      }

      disconnect() {}
    });
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('renders nothing without an icon', () => {
    const wrapper = mount(BlueprintPartnerLogo, { props: { icon: undefined } });

    expect(wrapper.find('img').exists()).toBe(false);
  });

  it('renders a decorative Rancher LazyImage that sends no referrer', async () => {
    const wrapper = mount(BlueprintPartnerLogo, { props: { icon: remote } });

    await nextTick();
    const img = wrapper.find('img');

    expect(img.attributes('src')).toBe(remote);
    expect(img.attributes('referrerpolicy')).toBe('no-referrer');
    expect(img.attributes('decoding')).toBe('async');
    expect(img.attributes('crossorigin')).toBeUndefined();
    expect(img.attributes('alt')).toBe('');
    expect(img.attributes('aria-hidden')).toBe('true');
  });

  it('hides itself when the image fails to load (air gap / 404)', async () => {
    const wrapper = mount(BlueprintPartnerLogo, { props: { icon: remote } });

    await nextTick();
    await wrapper.find('img').trigger('error');
    expect(wrapper.find('img').exists()).toBe(false);
  });

  it('shows again when the icon changes after a failure', async () => {
    const wrapper = mount(BlueprintPartnerLogo, { props: { icon: remote } });

    await nextTick();
    await wrapper.find('img').trigger('error');
    await wrapper.setProps({ icon: 'data:image/png;base64,iVBORw0KGgo=' });
    expect(wrapper.find('img').exists()).toBe(true);
  });
});
