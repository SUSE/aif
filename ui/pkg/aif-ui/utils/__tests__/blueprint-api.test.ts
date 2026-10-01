import { describe, it, expect } from 'vitest';
import {
  sourceFor, blueprintCRName, slugifyBlueprintName, buildBlueprintSpec,
} from '../blueprint-api';
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
