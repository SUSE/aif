import { describe, it, expect } from 'vitest';
import { shallowMount } from '@vue/test-utils';
import WizardStepIndicator from '@pkg/ai-factory/components/wizards/WizardStepIndicator.vue';

const STEPS = [
  { label: 'Source' },
  { label: 'Configuration' },
  { label: 'Review' },
];

function factory(currentStep) {
  return shallowMount(WizardStepIndicator, {
    props: { steps: STEPS, currentStep },
  });
}

describe('WizardStepIndicator.onStepClick', () => {
  it('emits go-to-step when clicking a previously-completed step', () => {
    const wrapper = factory(2);

    wrapper.vm.onStepClick(0);
    wrapper.vm.onStepClick(1);

    expect(wrapper.emitted('go-to-step')).toEqual([[0], [1]]);
  });

  it('does NOT emit when clicking the current step (no self-navigation)', () => {
    const wrapper = factory(1);

    wrapper.vm.onStepClick(1);

    expect(wrapper.emitted('go-to-step')).toBeUndefined();
  });

  it('does NOT emit when clicking a future step (forward nav is gated by the parent)', () => {
    const wrapper = factory(0);

    wrapper.vm.onStepClick(1);
    wrapper.vm.onStepClick(2);

    expect(wrapper.emitted('go-to-step')).toBeUndefined();
  });
});
