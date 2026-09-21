// @vitest-environment jsdom
import { describe, expect, it } from 'vitest';
import { mount } from '@vue/test-utils';
import { readFileSync } from 'node:fs';
import path from 'node:path';
import yaml from 'js-yaml';
import RepositoryHealthBanner from '../RepositoryHealthBanner.vue';

const translations = yaml.load(readFileSync(path.resolve(__dirname, '../../../l10n/en-us.yaml'), 'utf8'));
const global = {
  mocks: {
    $route: { params: { cluster: 'downstream' } },
    t: (key: string) => key.split('.').reduce<any>((value, part) => value?.[part], translations),
  },
  stubs: { RouterLink: { props: ['to'], template: '<a :href="typeof to === \'string\' ? to : to.name"><slot /></a>' } },
};

describe('overview repository warning', () => {
  it('identifies failed repositories and provides links to Rancher and Settings', () => {
    const wrapper = mount(RepositoryHealthBanner, { global, props: { repositories: [
      { name: 'nvidia-runai', ready: false, message: 'no API version specified. Will retry after 8m21' },
      { name: 'application-collection', ready: true },
    ] } });
    expect(wrapper.get('[role="status"]').text()).toContain('Some chart repositories are unavailable');
    expect(wrapper.text()).toContain('nvidia-runai');
    expect(wrapper.text()).toContain('no API version specified');
    expect(wrapper.text()).not.toContain('application-collection');
    expect(wrapper.findAll('a').map(a => a.attributes('href'))).toEqual([
      '/c/local/apps/catalog.cattle.io.clusterrepo/nvidia-runai', 'c-cluster-suseai-settings',
    ]);
    wrapper.unmount();
  });

  it('clears the warning when the repository recovers', async () => {
    const wrapper = mount(RepositoryHealthBanner, { global, props: { repositories: [{ name: 'repo', ready: false }] } });
    await wrapper.setProps({ repositories: [{ name: 'repo', ready: true }] });
    expect(wrapper.find('[role="status"]').exists()).toBe(false);
    wrapper.unmount();
  });

  it('reports an unreadable repository list as unknown', () => {
    const wrapper = mount(RepositoryHealthBanner, { global, props: { error: 'Forbidden' } });
    expect(wrapper.text()).toContain('Chart repository status could not be checked');
    expect(wrapper.text()).toContain('Forbidden');
    wrapper.unmount();
  });
});
