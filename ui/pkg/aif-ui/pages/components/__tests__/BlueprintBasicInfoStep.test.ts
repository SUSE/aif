// @vitest-environment jsdom
import { describe, expect, it } from 'vitest';
import { mount } from '@vue/test-utils';
import { Checkbox } from '@components/Form/Checkbox';
import BlueprintBasicInfoStep from '../wizard/BlueprintBasicInfoStep.vue';
import { wizardMountGlobal } from './wizard-mount';

const form = (allowRollback: boolean) => ({
  displayName: 'Run:ai', version: '1.0.0', description: '', allowRollback,
});

function mountStep(allowRollback: boolean) {
  return mount(BlueprintBasicInfoStep, { props: { form: form(allowRollback) }, global: wizardMountGlobal() });
}

describe('BlueprintBasicInfoStep rollback', () => {
  it.each([true, false])('reflects allowRollback=%s in the checkbox', (allowRollback) => {
    const checkbox = mountStep(allowRollback).findComponent(Checkbox);

    expect(checkbox.props('label')).toBe('Allow rollback');
    expect(checkbox.props('value')).toBe(allowRollback);
  });

  it('explains when to turn rollback off', () => {
    expect(mountStep(true).findComponent(Checkbox).props('description')).toContain("older versions can't read");
  });

  it('emits the form with the toggled value', async() => {
    const wrapper = mountStep(true);

    await wrapper.findComponent(Checkbox).vm.$emit('update:value', false);

    const events = wrapper.emitted('update:form') || [];

    expect(events[events.length - 1]).toEqual([form(false)]);
  });
});
