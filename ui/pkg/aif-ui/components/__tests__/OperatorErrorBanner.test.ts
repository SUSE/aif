// @vitest-environment jsdom
import { describe, expect, it } from 'vitest';
import { mount } from '@vue/test-utils';
import { readFileSync } from 'node:fs';
import path from 'node:path';
import yaml from 'js-yaml';
import OperatorErrorBanner from '../OperatorErrorBanner.vue';

const translations = yaml.load(readFileSync(path.resolve(__dirname, '../../l10n/en-us.yaml'), 'utf8'));

// Strict for this extension's keys, like Rancher's global t().
function translate(key: string): string {
  const value = key.split('.').reduce<any>((current, part) => current?.[part], translations);

  if (typeof value !== 'string') throw new Error(`Missing translation: ${ key }`);

  return value;
}

function mountBanner() {
  return mount(OperatorErrorBanner, {
    props:  { operatorError: 'OPERATOR_UNREACHABLE' },
    global: { mocks: { t: translate } },
  });
}

describe('OperatorErrorBanner', () => {
  it('renders the unreachable message, hint and retry label from l10n', () => {
    const text = mountBanner().text();

    expect(text).toContain('AI Factory operator is unreachable. Make sure the operator and the AI Factory UI extension are the same version.');
    expect(text).toContain(translate('suseai.operator.verifyHint'));
    expect(text).toContain(translate('suseai.operator.retry'));
    expect(text).not.toContain('OPERATOR_UNREACHABLE');
  });

  it('emits retry when the button is clicked', async() => {
    const wrapper = mountBanner();

    await wrapper.find('button').trigger('click');

    expect(wrapper.emitted('retry')).toHaveLength(1);
  });
});
