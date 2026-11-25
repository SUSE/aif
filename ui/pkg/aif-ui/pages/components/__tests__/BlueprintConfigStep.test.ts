// @vitest-environment jsdom
import { describe, expect, it, vi } from 'vitest';
import { flushPromises, mount } from '@vue/test-utils';
import { Banner } from '@components/Banner';
import BlueprintConfigStep from '../wizard/BlueprintConfigStep.vue';
import { wizardMountGlobal } from './wizard-mount';
import type { BlueprintComponent } from '../../../types/blueprint-types';

vi.mock('@shell/components/form/LabeledSelect', () => ({
  default: {
    name: 'LabeledSelectStub', props: ['value', 'options', 'label', 'mode'], emits: ['update:value'], template: '<div class="select-stub" />',
  },
}));
// Tab switching is Rancher's; render both tabs so their content is inspectable.
vi.mock('@shell/components/Tabbed', () => ({ default: { template: '<div><slot /></div>' } }));
vi.mock('@shell/components/Tabbed/Tab', () => ({ default: { props: ['name', 'label'], template: '<section :data-tab="name"><slot /></section>' } }));
vi.mock('../wizard/ValuesStep.vue', () => ({ default: { name: 'ValuesStepStub', emits: ['update:values'], template: '<div />' } }));
vi.mock('../wizard/KustomizeOverlayEditor.vue', () => ({ default: { template: '<div />' } }));
vi.mock('../../../services/rancher-apps', () => ({
  listGitClusterRepoNames: vi.fn(async() => []),
  fetchChartDefaultValues: vi.fn(async() => ''),
}));

const secretSource = { secretKeyRef: { name: 'runai-cluster-values', namespace: 'runai-backend', key: 'values.yaml' } };
const configMapSource = { configMapKeyRef: { name: 'other-values' } };

function components(): BlueprintComponent[] {
  return [
    {
      chartRepo: 'r', chartName: 'runai-cluster', chartVersion: '2.0.0', values: { a: 1 }, valuesFrom: [secretSource],
    },
    {
      chartRepo: 'r', chartName: 'other', chartVersion: '1.0.0', targetNamespace: 'runai-backend', values: { b: 1 }, valuesFrom: [configMapSource],
    },
  ];
}

async function mountStep(initial: BlueprintComponent[] = components()) {
  const wrapper = mount(BlueprintConfigStep, { props: { components: initial }, global: wizardMountGlobal() });

  await flushPromises();

  return wrapper;
}

function lastEmitted<T>(wrapper: Awaited<ReturnType<typeof mountStep>>, event: string): T {
  const events = wrapper.emitted(event) || [];

  return events[events.length - 1]?.[0] as T;
}

const errorBanner = (wrapper: Awaited<ReturnType<typeof mountStep>>) => wrapper.findAllComponents(Banner).find((b) => b.props('color') === 'error');

const input = (wrapper: Awaited<ReturnType<typeof mountStep>>, testid: string) => wrapper.find(`input[data-testid="${ testid }"]`);

describe('BlueprintConfigStep values from Secrets and ConfigMaps', () => {
  it('renders a row for each existing source', async() => {
    const wrapper = await mountStep();
    const kind = wrapper.findAllComponents({ name: 'LabeledSelectStub' }).find((s) => s.props('label') === 'Kind');

    expect(kind?.props('value')).toBe('Secret');
    expect((input(wrapper, 'values-from-runai-cluster-name-0').element as HTMLInputElement).value).toBe('runai-cluster-values');
    expect((input(wrapper, 'values-from-runai-cluster-namespace-0').element as HTMLInputElement).value).toBe('runai-backend');
    expect((input(wrapper, 'values-from-runai-cluster-key-0').element as HTMLInputElement).value).toBe('values.yaml');
  });

  it('emits components with the edited source and leaves other components unchanged', async() => {
    const wrapper = await mountStep();

    await input(wrapper, 'values-from-runai-cluster-name-0').setValue('edited-values');
    await vi.waitFor(() => {
      expect(lastEmitted<BlueprintComponent[]>(wrapper, 'update:components')?.[0].valuesFrom)
        .toEqual([{ secretKeyRef: { name: 'edited-values', namespace: 'runai-backend', key: 'values.yaml' } }]);
    });
    expect(lastEmitted<BlueprintComponent[]>(wrapper, 'update:components')[1].valuesFrom).toEqual([configMapSource]);
  });

  it('emits a field edit made just before the step unmounts', async() => {
    const wrapper = await mountStep();

    await input(wrapper, 'values-from-runai-cluster-key-0').setValue('custom.yaml');
    // Emitted synchronously: ArrayList's debounced update would come after the unmount and be dropped.
    const emitted = lastEmitted<BlueprintComponent[]>(wrapper, 'update:components');

    wrapper.unmount();

    expect(emitted[0].valuesFrom)
      .toEqual([{ secretKeyRef: { name: 'runai-cluster-values', namespace: 'runai-backend', key: 'custom.yaml' } }]);
  });

  it('keeps every valuesFrom when another field is edited', async() => {
    const wrapper = await mountStep();

    await wrapper.findComponent({ name: 'ValuesStepStub' }).vm.$emit('update:values', { a: 2 });

    const emitted = lastEmitted<BlueprintComponent[]>(wrapper, 'update:components');

    expect(emitted.map((c) => c.valuesFrom)).toEqual([[secretSource], [configMapSource]]);
  });

  it('keeps additionalPullSecretVendors when another field is edited', async() => {
    const initial = components();

    initial[0].additionalPullSecretVendors = ['suse'];
    const wrapper = await mountStep(initial);

    await wrapper.findComponent({ name: 'ValuesStepStub' }).vm.$emit('update:values', { a: 2 });

    const emitted = lastEmitted<BlueprintComponent[]>(wrapper, 'update:components');

    expect(emitted.map((c) => c.additionalPullSecretVendors)).toEqual([['suse'], undefined]);
  });

  it('shows an error for an invalid source', async() => {
    const wrapper = await mountStep();

    expect(errorBanner(wrapper)).toBeUndefined();
    await input(wrapper, 'values-from-runai-cluster-namespace-0').setValue('Not_A_Namespace');

    expect(errorBanner(wrapper)?.text()).toContain('runai-cluster: the namespace of values source 1');
  });

  it('shows an error for a namespace no application deploys to', async() => {
    const wrapper = await mountStep();

    await input(wrapper, 'values-from-runai-cluster-namespace-0').setValue('kube-system');

    expect(errorBanner(wrapper)?.text()).toContain('runai-cluster: values source 1 reads from namespace kube-system, which no application of this Blueprint deploys to. Use runai-backend');
  });

  it('shows an error when a source keeps its namespace but loses its name', async() => {
    const wrapper = await mountStep();

    await input(wrapper, 'values-from-runai-cluster-name-0').setValue('');

    expect(errorBanner(wrapper)?.text()).toContain('runai-cluster: values source 1 needs a name.');
  });

  it('adds an empty source row that does not change the spec until filled in', async() => {
    const wrapper = await mountStep();

    await wrapper.find('[data-testid="values-from-runai-cluster-button"]').trigger('click');
    await flushPromises();

    expect(wrapper.find('[data-testid="values-from-runai-cluster-name-1"]').exists()).toBe(true);
    expect(errorBanner(wrapper)).toBeUndefined();
  });
});

describe('BlueprintConfigStep deploy order', () => {
  const single = (extra: Partial<BlueprintComponent> = {}): BlueprintComponent[] => [{
    chartRepo: 'r', chartName: 'b', chartVersion: '1.0.0', ...extra,
  }];
  const deployOrderSelect = (wrapper: Awaited<ReturnType<typeof mountStep>>) => wrapper.findAllComponents({ name: 'LabeledSelectStub' }).find((s) => s.props('label') === 'Deploys after');

  it('shows the single-application hint without a dependency', async() => {
    const wrapper = await mountStep(single());

    expect(deployOrderSelect(wrapper)).toBeUndefined();
    expect(wrapper.text()).toContain('single application');
  });

  it('lets a single application clear a dependency on a removed one', async() => {
    const wrapper = await mountStep(single({ dependsOn: ['a'] }));
    const select = deployOrderSelect(wrapper)!;

    expect(select.props('value')).toEqual(['a']);
    expect(errorBanner(wrapper)?.text()).toContain('depends on unknown component "a"');
    expect(wrapper.text()).not.toContain('single application');

    await select.vm.$emit('update:value', []);
    const emitted = lastEmitted<BlueprintComponent[]>(wrapper, 'update:components');

    expect(emitted[0].dependsOn).toBeUndefined();
    // Feed the emitted list back as the parent wizard would; remounting avoids
    // setProps, whose typing cannot resolve a prop named `components`.
    const parentUpdated = await mountStep(emitted);

    expect(errorBanner(parentUpdated)).toBeUndefined();
  });
});
