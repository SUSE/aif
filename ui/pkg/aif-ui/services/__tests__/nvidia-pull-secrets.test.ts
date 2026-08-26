import { describe, it, expect } from 'vitest';

import { injectNvidiaPullSecretRefs, disableNvidiaChartSecrets } from '../fleet-bundle';

// TS port of the operator's injectNvidiaPullSecretRefs
// (operator/internal/controller/aiworkload/blueprint.go). These cases mirror
// TestNvidiaInjector_WritesBothPathShapes / _PreservesAuthorPullSecrets /
// _IdempotentSelfEntry / _LeavesUnexpectedShapesAlone and
// TestInjectNvidiaPullSecretRefs_OperatorImagePullSecrets in
// blueprint_pullsecret_test.go. The two copies MUST stay in sync.

const NGC = 'ngc-secret';

describe('injectNvidiaPullSecretRefs', () => {
  it('is a no-op for non-nvidia libraries', () => {
    const v: Record<string, any> = {};
    injectNvidiaPullSecretRefs(v, 'suse-ai');
    expect(v).toEqual({});
  });

  it('writes all three pull-secret shapes into empty values', () => {
    const v: Record<string, any> = {};
    injectNvidiaPullSecretRefs(v, 'nvidia');

    // Standard k8s pod-spec shape at the chart root: list of {name} objects.
    expect(v.imagePullSecrets).toEqual([{ name: NGC }]);
    // NIM workload shape: image.pullSecrets is a flat string list.
    expect(v.image.pullSecrets).toEqual([NGC]);
    // k8s-nim-operator shape: operator.image.pullSecrets, flat string list.
    expect(v.operator.image.pullSecrets).toEqual([NGC]);
    // Must never touch global (that path is owned by the non-nvidia code).
    expect(v.global).toBeUndefined();
  });

  it('prepends ngc-secret, preserving the chart author entries', () => {
    const v: Record<string, any> = {
      imagePullSecrets: [{ name: 'nvcrimagepullsecret' }],
      image:            { pullSecrets: ['author-string'] },
    };
    injectNvidiaPullSecretRefs(v, 'nvidia');

    expect(v.imagePullSecrets).toEqual([{ name: NGC }, { name: 'nvcrimagepullsecret' }]);
    expect(v.image.pullSecrets).toEqual([NGC, 'author-string']);
  });

  it('is idempotent — re-applying does not duplicate ngc-secret', () => {
    const v: Record<string, any> = {};
    injectNvidiaPullSecretRefs(v, 'nvidia');
    injectNvidiaPullSecretRefs(v, 'nvidia');

    expect(v.imagePullSecrets).toEqual([{ name: NGC }]);
    expect(v.image.pullSecrets).toEqual([NGC]);
    expect(v.operator.image.pullSecrets).toEqual([NGC]);
  });

  it('leaves unexpected shapes untouched (honors author intent)', () => {
    const v: Record<string, any> = { imagePullSecrets: 42, image: 'not-a-map' };
    injectNvidiaPullSecretRefs(v, 'nvidia');

    expect(v.imagePullSecrets).toBe(42);
    expect(v.image).toBe('not-a-map');
  });

  it('creates the nested operator.image.pullSecrets when operator exists without image', () => {
    const v: Record<string, any> = { operator: { replicas: 2 } };
    injectNvidiaPullSecretRefs(v, 'nvidia');

    expect(v.operator.replicas).toBe(2);
    expect(v.operator.image.pullSecrets).toEqual([NGC]);
  });

  it('treats an explicit null the same as absent', () => {
    const v: Record<string, any> = { imagePullSecrets: null };
    injectNvidiaPullSecretRefs(v, 'nvidia');

    expect(v.imagePullSecrets).toEqual([{ name: NGC }]);
  });
});

// TS port of the operator's disableChartSecretCreation
// (operator/internal/controller/aiworkload/blueprint.go), mirroring
// TestDisableChartSecretCreation. Turns off the charts' built-in secret creation
// so they reference the operator-delivered ngc-secret / ngc-api instead.
describe('disableNvidiaChartSecrets', () => {
  it('is a no-op for non-nvidia libraries', () => {
    const v: Record<string, any> = {};
    disableNvidiaChartSecrets(v, 'suse-ai');
    expect(v).toEqual({});
  });

  it('creates disabled secret refs with fallback names when absent', () => {
    const v: Record<string, any> = {};
    disableNvidiaChartSecrets(v, 'nvidia');

    expect(v.imagePullSecret).toEqual({ create: false, name: 'ngc-secret' });
    expect(v.ngcApiSecret).toEqual({ create: false, name: 'ngc-api' });
  });

  it('preserves an author-set name and only flips create to false', () => {
    const v: Record<string, any> = {
      imagePullSecret: { create: true, name: 'my-pull-secret' },
      ngcApiSecret:    { create: true },
    };
    disableNvidiaChartSecrets(v, 'nvidia');

    // Existing name kept; create forced off.
    expect(v.imagePullSecret).toEqual({ create: false, name: 'my-pull-secret' });
    // Missing name filled from the fallback.
    expect(v.ngcApiSecret).toEqual({ create: false, name: 'ngc-api' });
  });

  it('overwrites an unexpected non-object shape with a disabled ref', () => {
    const v: Record<string, any> = { imagePullSecret: 'nope' };
    disableNvidiaChartSecrets(v, 'nvidia');

    expect(v.imagePullSecret).toEqual({ create: false, name: 'ngc-secret' });
  });
});
