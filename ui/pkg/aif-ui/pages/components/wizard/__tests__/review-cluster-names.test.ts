// @vitest-environment jsdom
import { describe, expect, it, vi } from 'vitest';
import { mount } from '@vue/test-utils';
import ReviewStep from '../ReviewStep.vue';
import BlueprintInstallReviewStep from '../BlueprintInstallReviewStep.vue';

// CodeMirror needs a real layout engine; the tests only care about the cluster labels.
vi.mock('@shell/components/YamlEditor', async(importOriginal) => ({
  ...(await importOriginal<Record<string, unknown>>()),
  default: { props: ['value'], template: '<pre />' },
}));

const global = { mocks: { t: (key: string) => key }, provide: {} };
const clusterNames = { 'c-m-abc123': 'gpu-alpha', 'c-m-def456': 'gpu-beta' };

function mountAppReview(clusters: string[]) {
  return mount(ReviewStep, {
    global,
    props: {
      mode:         'install',
      release:      'ollama',
      namespace:    'ai',
      chartRepo:    'repo',
      chartName:    'ollama',
      chartVersion: '1.0.0',
      clusters,
      clusterNames,
      values:       {},
    },
  });
}

function mountBlueprintReview(clusters: string[]) {
  return mount(BlueprintInstallReviewStep, {
    global,
    props: {
      workloadName:   'rag',
      namespace:      'rag-system',
      displayName:    'RAG',
      version:        '1.0.0',
      componentCount: 0,
      deployType:     'Fleet',
      clusters,
      clusterNames,
      components:     [],
    },
  });
}

describe('install review cluster labels', () => {
  it('App review shows the display name of a single target cluster', () => {
    const text = mountAppReview(['c-m-abc123']).text();

    expect(text).toContain('gpu-alpha');
    expect(text).not.toContain('c-m-abc123');
  });

  it('App review shows display names for multiple target clusters', () => {
    const chips = mountAppReview(['c-m-abc123', 'c-m-def456']).findAll('.cluster-chip').map((c) => c.text());

    expect(chips).toEqual(['gpu-alpha', 'gpu-beta']);
  });

  it('App review falls back to the ID for a cluster without a known name', () => {
    expect(mountAppReview(['local']).text()).toContain('local');
  });

  it('Blueprint review shows display names for target clusters', () => {
    const text = mountBlueprintReview(['c-m-abc123', 'c-m-def456']).text();

    expect(text).toContain('gpu-alpha, gpu-beta');
    expect(text).not.toContain('c-m-abc123');
  });
});
