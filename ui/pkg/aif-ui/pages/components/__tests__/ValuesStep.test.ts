// @vitest-environment jsdom
import { describe, expect, it, vi } from 'vitest';
import { mount } from '@vue/test-utils';
import ValuesStep from '../wizard/ValuesStep.vue';
import { wizardMountGlobal } from './wizard-mount';

vi.mock('@shell/components/YamlEditor', () => ({ default: { template: '<div />' }, EDITOR_MODES: {} }));
vi.mock('@shell/components/Questions', () => ({ default: { template: '<div />' } }));
vi.mock('@shell/components/Loading', () => ({ default: { template: '<div />' } }));
vi.mock('@shell/components/Tabbed', () => ({ default: { template: '<div><slot /></div>' } }));

describe('ValuesStep', () => {
  it('warns that the version changed after values were edited', () => {
    const wrapper = mount(ValuesStep, {
      props: {
        values: {}, chartRepo: 'r', chartName: 'c', chartVersion: '1.0.0', loadingValues: false, versionDirty: true, hasQuestions: false, questionsSource: null, questionsLoading: false, ignoreVariables: [], targetNamespace: '', mode: 'install', inStore: 'cluster',
      },
      global: wizardMountGlobal(),
    });

    expect(wrapper.text()).toContain('Version changed after you edited values.');
  });
});
