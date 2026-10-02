import { describe, it, expect } from 'vitest';
import {
  findBlueprint, sourceFor, blueprintCRName, slugifyBlueprintName, buildBlueprintSpec,
} from '../blueprint-api';
import { BLUEPRINT_NAME_LABEL } from '../../types/blueprint-types';
import type { Blueprint, BlueprintComponent, BlueprintSpec } from '../../types/blueprint-types';

function mockBlueprint(overrides: Partial<Blueprint['spec']> = {}): Blueprint {
  return {
    apiVersion: 'ai-factory.suse.com/v1alpha1',
    kind:       'Blueprint',
    metadata:   { name: 'test-bp-1-0-0' },
    spec:       {
      displayName: 'Test Blueprint',
      version:     '1.0.0',
      components:  [],
      ...overrides,
    },
  };
}

describe('sourceFor', () => {
  it('defaults to Custom when source is undefined', () => {
    expect(sourceFor(mockBlueprint())).toBe('Custom');
  });

  it('returns the specified origin', () => {
    expect(sourceFor(mockBlueprint({ source: 'SUSE' }))).toBe('SUSE');
    expect(sourceFor(mockBlueprint({ source: 'Nvidia' }))).toBe('Nvidia');
    expect(sourceFor(mockBlueprint({ source: 'Custom' }))).toBe('Custom');
    expect(sourceFor(mockBlueprint({ source: 'Partner' }))).toBe('Partner');
  });
});

describe('slugifyBlueprintName and blueprintCRName', () => {
  it('slugifies blueprint names properly', () => {
    expect(slugifyBlueprintName('My AI Stack')).toBe('my-ai-stack');
    expect(slugifyBlueprintName('  --RAG Service--  ')).toBe('rag-service');
  });

  it('derives the CR name matching backend', () => {
    expect(blueprintCRName('My AI Stack', '1.0.0')).toBe('my-ai-stack-1-0-0');
    expect(blueprintCRName('My AI Stack', '2.1.0+build.4')).toBe('my-ai-stack-2-1-0');
  });
});

describe('buildBlueprintSpec', () => {
  const info = { displayName: 'Acme RAG', version: '1.0.1', description: '' };
  const components: BlueprintComponent[] = [{ chartRepo: 'suse-ai', chartName: 'ollama', chartVersion: '1.0.0' }];

  it('carries the icon and source from the prefill when editing', () => {
    const prefill: BlueprintSpec = {
      displayName: 'Acme RAG', version: '1.0.1', components, source: 'Custom', icon: 'https://partner.example.com/logo.png',
    };
    const spec = buildBlueprintSpec(info, components, prefill);

    expect(spec.icon).toBe('https://partner.example.com/logo.png');
    expect(spec.source).toBe('Custom');
  });

  it('omits icon from the request body and defaults source to Custom without a prefill', () => {
    const spec = buildBlueprintSpec(info, components);

    // An older operator rejects unknown fields (DisallowUnknownFields), so an
    // absent icon must not be serialized.
    expect(JSON.parse(JSON.stringify(spec))).not.toHaveProperty('icon');
    expect(spec.source).toBe('Custom');
  });

  it('keeps a Partner source when editing', () => {
    const prefill: BlueprintSpec = {
      displayName: 'Acme RAG', version: '1.0.1', components, source: 'Partner',
    };

    expect(buildBlueprintSpec(info, components, prefill).source).toBe('Partner');
  });

  it('maps an empty description to undefined', () => {
    expect(buildBlueprintSpec(info, components).description).toBeUndefined();
  });
});

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

  it('matches unlabelled CRs by their slugified display name', () => {
    const unlabelled = { ...bp('x', '3.0.0'), metadata: { name: 'custom' }, spec: { displayName: 'My Custom BP', version: '3.0.0', components: [] } } as Blueprint;
    expect(findBlueprint([unlabelled], 'my-custom-bp', '3.0.0')).toBe(unlabelled);
  });

  it('returns null for empty list or missing args', () => {
    expect(findBlueprint([], 'x', '1.0.0')).toBeNull();
    expect(findBlueprint(items, '', '1.0.0')).toBeNull();
    expect(findBlueprint(items, 'simple-chatbot-with-rag', '')).toBeNull();
  });
});
