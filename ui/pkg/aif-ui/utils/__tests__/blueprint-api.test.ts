import { describe, it, expect } from 'vitest';
import { findBlueprint } from '../blueprint-api';
import { BLUEPRINT_NAME_LABEL } from '../../types/blueprint-types';
import type { Blueprint } from '../../types/blueprint-types';

function bp(family: string, version: string): Blueprint {
  return {
    apiVersion: 'ai-factory.suse.com/v1alpha1',
    kind:       'Blueprint',
    metadata:   {
      name:   `${ family }-${ version.replace(/\./g, '-') }`,
      labels: { [BLUEPRINT_NAME_LABEL]: family },
    },
    spec: { displayName: family, version, components: [] },
  } as Blueprint;
}

describe('findBlueprint', () => {
  const items = [
    bp('simple-chatbot-with-rag', '1.0.0'),
    bp('simple-chatbot-with-rag', '1.1.0'),
    bp('other', '2.0.0'),
  ];

  it('returns the exact family + version match', () => {
    expect(findBlueprint(items, 'simple-chatbot-with-rag', '1.1.0')?.spec.version).toBe('1.1.0');
  });

  it('returns null when the version is absent from the family', () => {
    expect(findBlueprint(items, 'simple-chatbot-with-rag', '9.9.9')).toBeNull();
  });

  it('returns null for an unknown family', () => {
    expect(findBlueprint(items, 'does-not-exist', '1.0.0')).toBeNull();
  });

  it('returns null for empty list or missing args', () => {
    expect(findBlueprint([], 'x', '1.0.0')).toBeNull();
    expect(findBlueprint(items, '', '1.0.0')).toBeNull();
    expect(findBlueprint(items, 'simple-chatbot-with-rag', '')).toBeNull();
  });
});
