import { describe, it, expect } from 'vitest';
import {
  canRollback, floorFor, hasRollbackTarget, highestVersion, isRollback, moveBlocked, phaseBadgeColor, phaseBadgeIcon, phaseColor,
  rollbackBelowMinimumSafe, rollbackFloor, workloadStatusMessage,
} from '../workload-status';
import type { AIWorkload } from '../../types/aiworkload-types';
import type { Blueprint } from '../../types/blueprint-types';

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

describe('canRollback', () => {
  function bpWL(version: string, deployed?: string): AIWorkload {
    return {
      ...wl(deployed ? { deployedSource: { version: deployed, renderDigest: 'd', certifiedAt: '' } } as AIWorkload['status'] : undefined),
      spec: {
        displayName: 'X', targetNamespace: 'y', source: { sourceType: 'Blueprint', blueprint: { name: 'rag', version } },
      },
    } as AIWorkload;
  }
  function bp(version: string, rollback?: 'Allowed' | 'Disabled'): Blueprint {
    return {
      apiVersion: 'ai-factory.suse.com/v1alpha1',
      kind:       'Blueprint',
      metadata:   { name: `rag-${ version }` },
      spec:       {
        displayName: 'RAG', version, rollback, components: [],
      },
    };
  }

  it('needs a different deployed version to roll back to', () => {
    expect(hasRollbackTarget(bpWL('2.0.0'))).toBe(false);
    expect(hasRollbackTarget(bpWL('2.0.0', '2.0.0'))).toBe(false);
    expect(canRollback(bpWL('2.0.0', '1.0.0'), [bp('2.0.0'), bp('1.0.0')])).toBe(true);
  });

  it('is allowed when the Blueprints are unknown', () => {
    expect(canRollback(bpWL('2.0.0', '1.0.0'), [null, undefined])).toBe(true);
  });

  it('is blocked when either the current or the deployed Blueprint disables rollback', () => {
    const w = bpWL('2.0.0', '1.0.0');

    expect(hasRollbackTarget(w)).toBe(true);
    expect(canRollback(w, [bp('2.0.0', 'Disabled'), bp('1.0.0')])).toBe(false);
    expect(canRollback(w, [bp('2.0.0', 'Allowed'), bp('1.0.0', 'Disabled')])).toBe(false);
  });

  function withFloor(version: string, deployed: string, minimumSafeVersion: string): AIWorkload {
    const w = bpWL(version, deployed);

    return { ...w, status: { ...w.status, minimumSafeVersion } };
  }

  it('is blocked when the deployed version is below minimumSafeVersion', () => {
    const w = withFloor('2.0.0', '1.0.0', 'rag@2.0.0');

    expect(rollbackBelowMinimumSafe(w)).toBe(true);
    expect(canRollback(w, [bp('2.0.0'), bp('1.0.0')])).toBe(false);
    expect(rollbackFloor(w)).toBe('2.0.0');
  });

  it('is allowed when the deployed version is at or above minimumSafeVersion', () => {
    expect(canRollback(withFloor('2.0.0', '1.0.0', 'rag@1.0.0'), [bp('2.0.0'), bp('1.0.0')])).toBe(true);
    expect(canRollback(withFloor('2.0.0', '1.0.0', 'other@2.0.0'), [bp('2.0.0'), bp('1.0.0')])).toBe(true);
    expect(canRollback(withFloor('2.0.0', '1.0.0', ''), [bp('2.0.0'), bp('1.0.0')])).toBe(true);
  });
});

describe('floorFor', () => {
  it('reads the family-scoped floor, a bare version for any family, and nothing for another family', () => {
    expect(floorFor('rag@2.0.0', 'rag')).toBe('2.0.0');
    expect(floorFor('2.0.0', 'rag')).toBe('2.0.0');
    expect(floorFor('other@2.0.0', 'rag')).toBe('');
    expect(floorFor(undefined, 'rag')).toBe('');
  });
});

describe('isRollback / highestVersion', () => {
  it('compares semver and never treats non-semver as a rollback', () => {
    expect(isRollback('2.0.0', '1.9.9')).toBe(true);
    expect(isRollback('1.0.0', '1.0.0-rc.1')).toBe(true);
    expect(isRollback('1.0.0-rc.2', '1.0.0-rc.10')).toBe(false);
    expect(isRollback('1.0.0+build.2', '1.0.0+build.1')).toBe(false);
    expect(isRollback('1.0.0', '2.0.0')).toBe(false);
    expect(isRollback('latest', '1.0.0')).toBe(false);
    expect(isRollback(undefined, '1.0.0')).toBe(false);
  });

  it('returns the highest semver, ignoring empty and non-semver versions', () => {
    expect(highestVersion('1.2.0', undefined, 'x', '1.10.0', '')).toBe('1.10.0');
    expect(highestVersion('', 'x')).toBe('');
  });
});

describe('moveBlocked', () => {
  function bpAt(version: string, rollback?: 'Allowed' | 'Disabled'): Blueprint {
    return { apiVersion: '', kind: 'Blueprint', metadata: { name: `rag-${ version }` }, spec: { displayName: 'RAG', version, rollback, components: [] } } as Blueprint;
  }
  function workload(version: string, deployed?: string, minimumSafeVersion?: string): AIWorkload {
    return {
      apiVersion: '', kind: 'AIWorkload', metadata: { name: 'x', namespace: 'y' },
      spec:       { displayName: 'X', targetNamespace: 'y', source: { sourceType: 'Blueprint', blueprint: { name: 'rag', version } } },
      status:     { deployedSource: deployed ? { version: deployed, renderDigest: 'd', certifiedAt: '' } : undefined, minimumSafeVersion },
    } as AIWorkload;
  }
  const policies: Record<string, Blueprint> = { '1.0.0': bpAt('1.0.0'), '2.0.0': bpAt('2.0.0', 'Disabled'), '3.0.0': bpAt('3.0.0') };
  const lookup = (v: string) => policies[v] ?? null;

  it('refuses any move below minimumSafeVersion', () => {
    expect(moveBlocked(workload('3.0.0', '3.0.0', 'rag@3.0.0'), '2.0.0', () => null)).toBe(true);
    expect(moveBlocked(workload('3.0.0', '3.0.0', 'rag@3.0.0'), '3.0.0', () => null)).toBe(false);
    expect(moveBlocked(workload('3.0.0', '3.0.0', 'other@3.0.0'), '2.0.0', () => null)).toBe(false);
  });

  it('refuses a rollback from the deployed version when either Blueprint disables it', () => {
    expect(moveBlocked(workload('3.0.0', '3.0.0'), '2.0.0', lookup)).toBe(true);
    expect(moveBlocked(workload('3.0.0', '2.0.0'), '1.0.0', lookup)).toBe(true);
    // The spec version may never have rendered, so only the deployed version counts.
    expect(moveBlocked(workload('2.0.0', '1.0.0'), '1.0.0', lookup)).toBe(false);
    expect(moveBlocked(workload('3.0.0', '3.0.0'), '1.0.0', lookup)).toBe(false);
  });

  it('allows upgrades and unknown Blueprints', () => {
    expect(moveBlocked(workload('2.0.0', '2.0.0'), '3.0.0', lookup)).toBe(false);
    expect(moveBlocked(workload('2.0.0', '2.0.0'), '1.0.0', () => null)).toBe(false);
  });
});
