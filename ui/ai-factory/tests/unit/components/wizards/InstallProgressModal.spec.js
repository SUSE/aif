import { describe, it, expect, vi } from 'vitest';
import { shallowMount } from '@vue/test-utils';

// Stub @shell/components/ModalWithCard at the resolver. The real module
// transitively imports @components/Card / @components/Banner from the
// Rancher monorepo's separate @rancher/components package, which is not
// wired into the test alias map. We only assert on InstallProgressModal's
// own computed/methods, so a no-op stub is enough.
vi.mock('@shell/components/ModalWithCard', () => ({
  default: { name: 'ModalWithCard', template: '<div><slot /></div>' },
}));

import InstallProgressModal, { PROGRESS_STATUS } from '@pkg/ai-factory/components/wizards/InstallProgressModal.vue';

// Mount the component with the @shell <t> translation tag stubbed and
// this.t() mocked to echo the key — both are Rancher globals and not the
// subject of this test.
function factory(props = {}) {
  return shallowMount(InstallProgressModal, {
    props: { show: true, ...props },
    global: {
      mocks: { t: (key) => key },
      stubs: ['t', 'ModalWithCard'],
    },
  });
}

describe('InstallProgressModal.hasFailures', () => {
  it('is false for an empty progress list', () => {
    const wrapper = factory({ progress: [] });

    expect(wrapper.vm.hasFailures).toBe(false);
  });

  it('is false when all items succeeded', () => {
    const wrapper = factory({
      progress: [
        { clusterId: 'a', status: PROGRESS_STATUS.SUCCESS },
        { clusterId: 'b', status: PROGRESS_STATUS.SUCCESS },
      ],
    });

    expect(wrapper.vm.hasFailures).toBe(false);
  });

  it('is true when any item failed', () => {
    const wrapper = factory({
      progress: [
        { clusterId: 'a', status: PROGRESS_STATUS.SUCCESS },
        { clusterId: 'b', status: PROGRESS_STATUS.FAILED },
        { clusterId: 'c', status: PROGRESS_STATUS.SUCCESS },
      ],
    });

    expect(wrapper.vm.hasFailures).toBe(true);
  });

  it('is true when all items failed (drives footer Close vs Done)', () => {
    const wrapper = factory({
      progress: [
        { clusterId: 'a', status: PROGRESS_STATUS.FAILED },
        { clusterId: 'b', status: PROGRESS_STATUS.FAILED },
      ],
    });

    expect(wrapper.vm.hasFailures).toBe(true);
    // isDone short-circuits to true once nothing is still installing — the
    // "Close" footer button is gated on (isDone && hasFailures).
    expect(wrapper.vm.isDone).toBe(true);
  });
});
