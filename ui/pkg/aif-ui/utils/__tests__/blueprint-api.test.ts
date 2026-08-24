import { beforeEach, describe, expect, it, vi } from 'vitest';

const operatorFetch = vi.fn();

vi.mock('../operator-config', () => ({
  operatorFetch: (...args: unknown[]) => operatorFetch(...args),
}));

import {
  createBlueprint,
  getBlueprint,
  prepareBlueprintSpecForWrite,
  resolveApplicationComponents,
  updateBlueprintDeprecated,
} from '../blueprint-api';
import type { Blueprint, BlueprintSpec } from '../../types/blueprint-types';

function logicalBlueprint(componentCount = 1): Blueprint {
  return {
    apiVersion: 'ai-factory.suse.com/v1alpha1',
    kind:       'Blueprint',
    metadata:   { name: 'chat-1-0-0' },
    spec:       {
      displayName: 'Chat',
      version:     '1.0.0',
      components:  Array.from({ length: componentCount }, () => ({
        chartRepo:      '',
        chartName:      '',
        chartVersion:   '',
        applicationRef: { name: 'suse.ollama', version: '1.55.0' },
        values:         { replicas: 2 },
      })),
    },
  };
}

const ollamaApplication = {
  apiVersion: 'ai-factory.suse.com/v1alpha1',
  kind:       'Application',
  metadata:   { name: 'suse.ollama' },
  spec:       {
    chart:             { name: 'ollama', sourceRef: 'private-apps' },
    credentialProfile: 'nvidia',
  },
};

describe('Blueprint logical Application resolution', () => {
  beforeEach(() => operatorFetch.mockReset());

  it('resolves a component for the existing UI without changing its logical reference', async() => {
    operatorFetch.mockResolvedValueOnce(ollamaApplication);

    const result = await resolveApplicationComponents(logicalBlueprint());
    const component = result.spec.components[0];

    expect(component).toMatchObject({
      applicationRef: { name: 'suse.ollama', version: '1.55.0' },
      chartRepo:       'private-apps',
      chartName:       'ollama',
      chartVersion:    '1.55.0',
      vendor:          'nvidia',
      values:          { replicas: 2 },
    });
  });

  it('fetches each distinct Application only once', async() => {
    operatorFetch.mockResolvedValue(ollamaApplication);

    await resolveApplicationComponents(logicalBlueprint(2));

    expect(operatorFetch).toHaveBeenCalledTimes(1);
    expect(operatorFetch).toHaveBeenCalledWith('/api/v1/applications/suse.ollama');
  });

  it('getBlueprint resolves the stored logical component', async() => {
    operatorFetch
      .mockResolvedValueOnce(logicalBlueprint())
      .mockResolvedValueOnce(ollamaApplication);

    const result = await getBlueprint('chat-1-0-0');

    expect(operatorFetch.mock.calls.map((call) => call[0])).toEqual([
      '/api/v1/blueprints/chat-1-0-0',
      '/api/v1/applications/suse.ollama',
    ]);
    expect(result.spec.components[0].chartRepo).toBe('private-apps');
  });

  it('rejects an Application without a complete chart mapping', async() => {
    operatorFetch.mockResolvedValue({
      ...ollamaApplication,
      spec: { chart: { name: '', sourceRef: '' } },
    });

    await expect(resolveApplicationComponents(logicalBlueprint())).rejects.toThrow(
      'Application "suse.ollama" has no usable chart mapping.',
    );
  });

  it('removes resolved coordinates before writing and preserves an edited version', () => {
    const spec = logicalBlueprint().spec;
    spec.components[0] = {
      ...spec.components[0],
      chartRepo:    'private-apps',
      chartName:    'ollama',
      chartVersion: '1.56.0',
      vendor:       'nvidia',
    };

    const persisted = prepareBlueprintSpecForWrite(spec) as {
      components: Array<Record<string, unknown>>;
    };

    expect(persisted.components[0]).toEqual({
      applicationRef: { name: 'suse.ollama', version: '1.56.0' },
      values:         { replicas: 2 },
    });
  });

  it('leaves legacy direct-chart components unchanged', () => {
    const spec: BlueprintSpec = {
      displayName: 'Legacy',
      version:     '1.0.0',
      components:  [{
        chartRepo:    'application-collection',
        chartName:    'ollama',
        chartVersion: '1.55.0',
        vendor:       'suse',
      }],
    };

    expect(prepareBlueprintSpecForWrite(spec)).toEqual(spec);
  });

  it('createBlueprint sends only the logical requirement', async() => {
    operatorFetch.mockResolvedValue(logicalBlueprint());
    const spec = logicalBlueprint().spec;
    spec.components[0] = {
      ...spec.components[0],
      chartRepo:    'private-apps',
      chartName:    'ollama',
      chartVersion: '1.55.0',
      vendor:       'nvidia',
    };

    await createBlueprint(spec);

    const options = operatorFetch.mock.calls[0][1];
    const body = JSON.parse(options.body);
    expect(body.spec.components[0]).toEqual({
      applicationRef: { name: 'suse.ollama', version: '1.55.0' },
      values:         { replicas: 2 },
    });
  });

  it('can deprecate a Blueprint without resolving its Application', async() => {
    operatorFetch
      .mockResolvedValueOnce(logicalBlueprint())
      .mockResolvedValueOnce(logicalBlueprint());

    await updateBlueprintDeprecated('chat-1-0-0', true);

    expect(operatorFetch).toHaveBeenCalledTimes(2);
    expect(operatorFetch.mock.calls.map((call) => call[0])).toEqual([
      '/api/v1/blueprints/chat-1-0-0',
      '/api/v1/blueprints/chat-1-0-0',
    ]);
    const body = JSON.parse(operatorFetch.mock.calls[1][1].body);
    expect(body.spec.deprecated).toBe(true);
    expect(body.spec.components[0]).toEqual({
      applicationRef: { name: 'suse.ollama', version: '1.55.0' },
      values:         { replicas: 2 },
    });
  });
});
