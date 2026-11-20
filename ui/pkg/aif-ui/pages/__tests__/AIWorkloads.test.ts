// @vitest-environment jsdom
import {
  afterEach, beforeEach, describe, expect, it, vi,
} from 'vitest';
import { flushPromises, mount } from '@vue/test-utils';
import type { ComponentCustomProperties } from 'vue';
import AIWorkloads from '../AIWorkloads.vue';
import { listAIWorkloads } from '../../utils/operator-api';
import { findBlueprint, groupBlueprintsByFamily, listBlueprints } from '../../utils/blueprint-api';
import {
  OPERATOR_UNREACHABLE, checkOperatorConnection, getConnectionError, recheckOperatorConnection,
} from '../../utils/operator-config';

vi.mock('../../utils/operator-api', () => ({
  listAIWorkloads:    vi.fn(),
  upgradeAIWorkload:  vi.fn(),
  rollbackAIWorkload: vi.fn(),
  retryAIWorkload:    vi.fn(),
}));
vi.mock('../../utils/blueprint-api', async(importOriginal) => ({
  semverCompare:           (await importOriginal<typeof import('../../utils/blueprint-api')>()).semverCompare,
  listBlueprints:          vi.fn().mockResolvedValue({ items: [] }),
  groupBlueprintsByFamily: vi.fn(() => new Map()),
  findBlueprint:           vi.fn(),
}));
vi.mock('../../utils/operator-config', async(importOriginal) => ({
  ...(await importOriginal<typeof import('../../utils/operator-config')>()),
  checkOperatorConnection:   vi.fn().mockResolvedValue(undefined),
  getConnectionError:        vi.fn(),
  recheckOperatorConnection: vi.fn(),
}));
vi.mock('../../services/cluster-service', () => ({ getClusters: vi.fn().mockResolvedValue([]) }));
vi.mock('../../services/workload-pods', () => ({ fetchWorkloadPodStatus: vi.fn().mockResolvedValue([]) }));

const POLL_MS = 10_000;
// Errors as operatorFetch throws them (toOperatorError).
const unreachable = Object.assign(new Error('Handler disconnected'), { status: 503, code: OPERATOR_UNREACHABLE, unreachable: true });
const forbidden   = Object.assign(new Error('forbidden'), { status: 403, code: 'FORBIDDEN', unreachable: false });

async function mountWorkloads(stubs: Record<string, unknown> = {}) {
  const store = { getters: { 'i18n/t': (key: string) => key }, dispatch: vi.fn() };
  const wrapper = mount(AIWorkloads, {
    global: {
      mocks:  { t: (key: string) => key },
      config: {
        globalProperties: {
          $store: store, $router: { push: vi.fn() }, $route: { params: { cluster: 'local' }, query: {} },
        } as unknown as ComponentCustomProperties,
      },
      stubs: {
        Banner: true, LabeledSelect: true, AppModal: true, BadgeState: true, ...stubs,
      },
    },
  });

  await flushPromises();

  return wrapper;
}

const banner = (wrapper: ReturnType<typeof mount>) => wrapper.findComponent({ name: 'OperatorErrorBanner' });

describe('AI workloads operator banner', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.mocked(checkOperatorConnection).mockClear();
    vi.mocked(listAIWorkloads).mockReset().mockResolvedValue({ items: [] } as any);
    vi.mocked(recheckOperatorConnection).mockReset();
    vi.mocked(getConnectionError).mockReset().mockReturnValue(OPERATOR_UNREACHABLE);
  });
  afterEach(() => vi.useRealTimers());

  it('clears the banner and loads the workloads once the poll finds the operator back', async() => {
    const wrapper = await mountWorkloads();

    expect(banner(wrapper).exists()).toBe(true);
    expect(listAIWorkloads).not.toHaveBeenCalled();

    vi.mocked(recheckOperatorConnection).mockResolvedValue(true);
    vi.mocked(getConnectionError).mockReturnValue(null);
    await vi.advanceTimersByTimeAsync(POLL_MS);
    await flushPromises();

    expect(recheckOperatorConnection).toHaveBeenCalledTimes(1);
    expect(listAIWorkloads).toHaveBeenCalled();
    expect(banner(wrapper).exists()).toBe(false);
  });

  it('keeps the banner and loads nothing while the operator stays unreachable', async() => {
    const wrapper = await mountWorkloads();

    vi.mocked(recheckOperatorConnection).mockResolvedValue(false);
    await vi.advanceTimersByTimeAsync(POLL_MS);
    await flushPromises();

    expect(recheckOperatorConnection).toHaveBeenCalledTimes(1);
    expect(listAIWorkloads).not.toHaveBeenCalled();
    expect(banner(wrapper).exists()).toBe(true);
  });

  it('shows the banner when the operator stops answering while the page is open, and clears it once it is back', async() => {
    vi.mocked(getConnectionError).mockReturnValue(null);
    const wrapper = await mountWorkloads();

    expect(banner(wrapper).exists()).toBe(false);

    vi.mocked(listAIWorkloads).mockRejectedValueOnce(unreachable);
    await vi.advanceTimersByTimeAsync(POLL_MS);
    await flushPromises();

    expect(banner(wrapper).exists()).toBe(true);

    vi.mocked(recheckOperatorConnection).mockResolvedValue(true);
    await vi.advanceTimersByTimeAsync(POLL_MS);
    await flushPromises();

    expect(recheckOperatorConnection).toHaveBeenCalledTimes(1);
    expect(banner(wrapper).exists()).toBe(false);
  });

  it('keeps an error the operator answered out of the banner during the poll', async() => {
    vi.mocked(getConnectionError).mockReturnValue(null);
    const wrapper = await mountWorkloads();

    vi.mocked(listAIWorkloads).mockRejectedValueOnce(forbidden);
    await vi.advanceTimersByTimeAsync(POLL_MS);
    await flushPromises();

    expect(banner(wrapper).exists()).toBe(false);
  });

  it('polls the workloads without re-checking the connection while the operator is reachable', async() => {
    vi.mocked(getConnectionError).mockReturnValue(null);
    await mountWorkloads();
    vi.mocked(listAIWorkloads).mockClear();

    await vi.advanceTimersByTimeAsync(POLL_MS);
    await flushPromises();

    expect(recheckOperatorConnection).not.toHaveBeenCalled();
    expect(listAIWorkloads).toHaveBeenCalledTimes(1);
  });
});

describe('AI workloads minimum safe version', () => {
  const bp = (version: string, rollback?: 'Disabled') => ({ metadata: { name: `rag-${ version }` }, spec: { version, rollback } });
  const blueprints = [bp('3.0.0'), bp('2.0.0', 'Disabled'), bp('1.0.0')];
  const workload = (version: string, deployed: string, minimumSafeVersion?: string) => ({
    metadata: { name: 'rag', namespace: 'ns' },
    spec:     { displayName: 'RAG', targetNamespace: 't', source: { sourceType: 'Blueprint', blueprint: { name: 'rag', version } } },
    status:   { phase: 'Running', deployedSource: { version: deployed, renderDigest: 'd', certifiedAt: '' }, minimumSafeVersion },
  });

  beforeEach(() => {
    vi.mocked(getConnectionError).mockReturnValue(null);
    vi.mocked(listBlueprints).mockResolvedValue({ items: blueprints } as any);
    vi.mocked(groupBlueprintsByFamily).mockReturnValue(new Map([['rag', blueprints]]) as any);
    vi.mocked(findBlueprint).mockImplementation(((_items: unknown, _name: string, version: string) => blueprints.find((b) => b.spec.version === version) || null) as any);
  });

  const rollbackButton = (wrapper: ReturnType<typeof mount>) => wrapper.findAll('button').find((b) => b.text().includes('Roll Back'))!;

  it('disables Roll Back with the floor when the deployed version is below minimumSafeVersion', async() => {
    vi.mocked(listAIWorkloads).mockResolvedValue({ items: [workload('3.0.0', '1.0.0', 'rag@3.0.0')] } as any);
    const wrapper = await mountWorkloads();

    expect(rollbackButton(wrapper).attributes('disabled')).toBeDefined();
    expect(rollbackButton(wrapper).attributes('title')).toBe('suseai.workloads.rollbackBelowMinimumSafe');
  });

  it('keeps Roll Back enabled at or above minimumSafeVersion', async() => {
    vi.mocked(listAIWorkloads).mockResolvedValue({ items: [workload('3.0.0', '1.0.0', 'rag@1.0.0')] } as any);
    const wrapper = await mountWorkloads();

    expect(rollbackButton(wrapper).attributes('disabled')).toBeUndefined();
  });

  it('disables upgrade targets the operator would refuse', async() => {
    const w = workload('3.0.0', '3.0.0', 'rag@2.0.0');
    vi.mocked(listAIWorkloads).mockResolvedValue({ items: [w] } as any);
    const wrapper = await mountWorkloads({ AppModal: { template: '<div><slot /></div>' } });
    const vm = wrapper.vm as any;

    vm.openUpgradeModal(w);
    expect(vm.upgradeVersionSelectOptions.map((o: any) => [o.value, o.disabled])).toEqual([
      // 2.0.0 disables rollback; 1.0.0 is below the floor.
      ['3.0.0', false], ['2.0.0', true], ['1.0.0', true],
    ]);
    const upgradeButton = () => wrapper.findAll('button').find((b) => b.text() === 'Upgrade' && b.classes('role-primary'))!;
    vm.upgradeModal.selectedVersion = '1.0.0';
    await flushPromises();
    expect(vm.upgradeTargetBlocked).toBe(true);
    expect(upgradeButton().attributes('disabled')).toBeDefined();
  });
});
