import { describe, it, expect } from 'vitest';
import { phaseBadgeColor, phaseBadgeIcon, phaseColor, workloadStatusMessage } from '../workload-status';
import type { AIWorkload } from '../../types/aiworkload-types';

function wl(status?: AIWorkload['status']): AIWorkload {
  return {
    apiVersion: 'ai-factory.suse.com/v1alpha1',
    kind:       'AIWorkload',
    metadata:   { name: 'x', namespace: 'y' },
    spec:       { displayName: 'X', targetNamespace: 'y', source: { sourceType: 'App' } },
    status,
  } as AIWorkload;
}

describe('phaseColor / phaseBadgeColor / phaseBadgeIcon', () => {
  it('maps known phases and defaults to info', () => {
    expect(phaseColor('Running')).toBe('success');
    expect(phaseColor('Degraded')).toBe('warning');
    expect(phaseColor('Failed')).toBe('error');
    expect(phaseColor(undefined)).toBe('info');

    expect(phaseBadgeColor('Running')).toBe('bg-success');
    expect(phaseBadgeColor('Degraded')).toBe('bg-warning');
    expect(phaseBadgeColor('Failed')).toBe('bg-error');
    expect(phaseBadgeColor(undefined)).toBe('bg-info');

    expect(phaseBadgeIcon('Running')).toBe('icon-checkmark');
    expect(phaseBadgeIcon('Degraded')).toBe('icon-warning');
    expect(phaseBadgeIcon('Failed')).toBe('icon-x');
    expect(phaseBadgeIcon(undefined)).toBe('icon-info');
  });
});

describe('workloadStatusMessage', () => {
  it('prefers the Ready=False condition message', () => {
    expect(
      workloadStatusMessage(wl({ conditions: [{ type: 'Ready', status: 'False', message: 'repo missing' }] })),
    ).toBe('repo missing');
  });

  it('falls back to the first cluster status message', () => {
    expect(
      workloadStatusMessage(wl({ clusterStatuses: [{ clusterId: 'c1', phase: 'Failed', message: 'boom' }] })),
    ).toBe('boom');
  });

  it('suppresses messages once the workload is Running', () => {
    expect(
      workloadStatusMessage(wl({
        phase:           'Running',
        clusterStatuses: [{ clusterId: 'c1', phase: 'Running', message: 'Helm install complete' }],
      })),
    ).toBe('');
  });

  it('returns empty string when there is nothing to surface', () => {
    expect(workloadStatusMessage(wl())).toBe('');
    expect(workloadStatusMessage(wl({ phase: 'Running' }))).toBe('');
  });
});
