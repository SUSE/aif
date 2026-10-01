// @vitest-environment jsdom
import { describe, expect, it } from 'vitest';
import { mount } from '@vue/test-utils';
import BlueprintSourceBadge from '../BlueprintSourceBadge.vue';

describe('BlueprintSourceBadge', () => {
  it('renders the Custom text label in a Rancher Tag', () => {
    const wrapper = mount(BlueprintSourceBadge, { props: { source: 'Custom' } });

    expect(wrapper.classes()).toEqual(expect.arrayContaining(['tag', 'source-badge', 'source-badge--custom']));
    expect(wrapper.text()).toBe('Custom');
    expect(wrapper.find('img').exists()).toBe(false);
    expect(wrapper.attributes('aria-label')).toBe('Source: Custom');
  });

  it('treats an empty source as Custom', () => {
    const wrapper = mount(BlueprintSourceBadge, { props: { source: '' } });

    expect(wrapper.text()).toBe('Custom');
    expect(wrapper.classes()).toContain('source-badge--custom');
  });

  it('renders both Nvidia theme logos', () => {
    const wrapper = mount(BlueprintSourceBadge, { props: { source: 'Nvidia' } });

    expect(wrapper.find('.nvidia-logo--light').exists()).toBe(true);
    expect(wrapper.find('.nvidia-logo--dark').exists()).toBe(true);
    expect(wrapper.attributes('aria-label')).toBe('Source: Nvidia');
  });

  it('renders both SUSE theme logos', () => {
    const wrapper = mount(BlueprintSourceBadge, { props: { source: 'SUSE' } });

    expect(wrapper.find('.suse-logo--light').exists()).toBe(true);
    expect(wrapper.find('.suse-logo--dark').exists()).toBe(true);
    expect(wrapper.attributes('aria-label')).toBe('Source: SUSE');
  });

  it('renders the Partner text label', () => {
    const wrapper = mount(BlueprintSourceBadge, { props: { source: 'Partner' } });

    expect(wrapper.classes()).toEqual(expect.arrayContaining(['tag', 'source-badge', 'source-badge--partner']));
    expect(wrapper.text()).toBe('Partner');
    expect(wrapper.find('img').exists()).toBe(false);
    expect(wrapper.attributes('aria-label')).toBe('Source: Partner');
  });

  it('has no icon prop: provenance is never replaced by a partner image', () => {
    const wrapper = mount(BlueprintSourceBadge, {
      props: { source: 'Custom' },
      attrs: { icon: 'https://partner.example.com/suse-lookalike.png' },
    });

    expect(wrapper.find('img').exists()).toBe(false);
    expect(wrapper.text()).toBe('Custom');
  });
});
