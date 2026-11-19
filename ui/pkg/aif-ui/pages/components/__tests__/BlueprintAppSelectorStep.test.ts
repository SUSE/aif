// @vitest-environment jsdom
import { describe, expect, it, vi } from 'vitest';
import { flushPromises, mount } from '@vue/test-utils';
import { LabeledInput } from '@components/Form/LabeledInput';
import BlueprintAppSelectorStep from '../wizard/BlueprintAppSelectorStep.vue';
import { wizardMountGlobal } from './wizard-mount';
import type { BlueprintComponent } from '../../../types/blueprint-types';

vi.mock('@shell/components/form/LabeledSelect', () => ({ default: { template: '<div />' } }));
vi.mock('../../../services/app-collection', () => ({
  fetchSuseAiApps:          vi.fn(async() => []),
  fetchNvidiaApps:          vi.fn(async() => []),
  fetchCustomRepoApps:      vi.fn(async() => []),
  fetchManagedRepos:        vi.fn(async() => []),
  fetchSettingsOrNull:      vi.fn(async() => null),
  resolveInstallRepoName:   vi.fn(),
  getLibraryForClusterRepo: vi.fn(),
}));
vi.mock('../../../services/rancher-apps', () => ({
  listChartVersions:         vi.fn(async() => []),
  inferClusterRepoForChart:  vi.fn(),
}));

async function mountStep(components: BlueprintComponent[]) {
  const wrapper = mount(BlueprintAppSelectorStep, { props: { components }, global: wizardMountGlobal() });

  await flushPromises();

  return wrapper;
}

const subLabels = (wrapper: Awaited<ReturnType<typeof mountStep>>) => wrapper.findAllComponents(LabeledInput).map((i) => i.props('subLabel'));

const comp = (chartName: string, extra: Partial<BlueprintComponent> = {}): BlueprintComponent => ({
  chartRepo: 'r', chartName, chartVersion: '1.0.0', ...extra,
});

describe('BlueprintAppSelectorStep removing an application', () => {
  it('drops the removed application from the others\' deploy order', async() => {
    const wrapper = await mountStep([comp('a'), comp('b', { dependsOn: ['a'] }), comp('c', { dependsOn: ['a', 'b'] })]);

    await wrapper.findAll('.btn-remove')[0].trigger('click');

    expect(wrapper.emitted('update:components')?.[0]?.[0]).toEqual([comp('b'), comp('c', { dependsOn: ['b'] })]);
  });
});

describe('BlueprintAppSelectorStep validation messages', () => {
  it('explains an invalid namespace and release name', async() => {
    const wrapper = await mountStep([comp('a', { targetNamespace: 'Bad_NS', releaseName: 'Bad_Name' })]);

    expect(subLabels(wrapper)).toEqual([
      expect.stringContaining('Must be a valid namespace'),
      expect.stringContaining('Must be a valid release name'),
    ]);
  });

  it('explains a duplicate release name in the same namespace', async() => {
    const wrapper = await mountStep([comp('a', { releaseName: 'same' }), comp('b', { releaseName: 'same' })]);

    expect(subLabels(wrapper).filter(Boolean)).toEqual([
      'Another component in the same namespace already uses this release name.',
      'Another component in the same namespace already uses this release name.',
    ]);
  });
});
