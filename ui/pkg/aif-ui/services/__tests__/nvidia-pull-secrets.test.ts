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

  it('writes every pull-secret shape into empty values', () => {
    const v: Record<string, any> = {};
    injectNvidiaPullSecretRefs(v, 'nvidia');

    // Standard k8s pod-spec shape at the chart root: list of {name} objects.
    expect(v.imagePullSecrets).toEqual([{ name: NGC }]);
    // NIM workload shape: image.pullSecrets is a flat string list.
    expect(v.image.pullSecrets).toEqual([NGC]);
    // k8s-nim-operator shape: operator.image.pullSecrets, flat string list.
    expect(v.operator.image.pullSecrets).toEqual([NGC]);
    // Scalar name shape, read by some charts at the top level and by others
    // under global.
    expect(v.ngcImagePullSecretName).toBe(NGC);
    expect(v.global.ngcImagePullSecretName).toBe(NGC);
    // Must never force the global.imagePullSecrets list shape (owned by the
    // non-nvidia code); NVIDIA charts read the scalar name instead.
    expect(v.global.imagePullSecrets).toBeUndefined();
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

  it('treats an explicit null in the nested flat lists the same as absent', () => {
    // Mirrors the Go copy's `case nil`, which fires for both a missing key and a
    // JSON null on image.pullSecrets and operator.image.pullSecrets.
    const v: Record<string, any> = {
      image:    { pullSecrets: null },
      operator: { image: { pullSecrets: null } },
    };
    injectNvidiaPullSecretRefs(v, 'nvidia');

    expect(v.image.pullSecrets).toEqual([NGC]);
    expect(v.operator.image.pullSecrets).toEqual([NGC]);
  });
});

// The scalar ngcImagePullSecretName key is read by some NVIDIA charts at the top
// level and by others under global. The injector sees override-only values, so it
// sets the key unconditionally when absent, replaces an empty-string default,
// honors a non-empty user override, and leaves any non-string value untouched.
// Mirrors TestInjectNgcImagePullSecretName in blueprint_pullsecret_test.go.
describe('injectNvidiaPullSecretRefs — ngcImagePullSecretName', () => {
  it('replaces an empty-string default at both locations', () => {
    const v: Record<string, any> = { ngcImagePullSecretName: '', global: { ngcImagePullSecretName: '' } };
    injectNvidiaPullSecretRefs(v, 'nvidia');

    expect(v.ngcImagePullSecretName).toBe(NGC);
    expect(v.global.ngcImagePullSecretName).toBe(NGC);
  });

  it('honors a non-empty user override at each location independently', () => {
    const v: Record<string, any> = {
      ngcImagePullSecretName: 'user-top',
      global:                 { ngcImagePullSecretName: 'user-global' },
    };
    injectNvidiaPullSecretRefs(v, 'nvidia');

    expect(v.ngcImagePullSecretName).toBe('user-top');
    expect(v.global.ngcImagePullSecretName).toBe('user-global');
  });

  it('leaves a non-string top-level value untouched', () => {
    const v: Record<string, any> = { ngcImagePullSecretName: 42 };
    injectNvidiaPullSecretRefs(v, 'nvidia');

    expect(v.ngcImagePullSecretName).toBe(42);
    // global is still created for charts that read it there.
    expect(v.global.ngcImagePullSecretName).toBe(NGC);
  });

  it('leaves a non-object global untouched', () => {
    const v: Record<string, any> = { global: 'not-a-map' };
    injectNvidiaPullSecretRefs(v, 'nvidia');

    expect(v.global).toBe('not-a-map');
    expect(v.ngcImagePullSecretName).toBe(NGC);
  });

  it('adds the key to an existing global without clobbering siblings', () => {
    const v: Record<string, any> = {
      global: { imagePullSecrets: [{ name: 'author-secret' }] },
    };
    injectNvidiaPullSecretRefs(v, 'nvidia');

    expect(v.global.ngcImagePullSecretName).toBe(NGC);
    expect(v.global.imagePullSecrets).toEqual([{ name: 'author-secret' }]);
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

  it('fills an empty-string name (the 403-causing chart default)', () => {
    // An empty name renders imagePullSecrets: [{name:""}] and suppresses SA
    // injection → 403. It must be filled, not honored. Mirrors the Go copy.
    const v: Record<string, any> = {
      imagePullSecret: { create: true, name: '' },
      ngcApiSecret:    { create: true, name: '' },
    };
    disableNvidiaChartSecrets(v, 'nvidia');

    expect(v.imagePullSecret).toEqual({ create: false, name: 'ngc-secret' });
    expect(v.ngcApiSecret).toEqual({ create: false, name: 'ngc-api' });
  });

  it('leaves a non-string name untouched (parity with the Go copy)', () => {
    const v: Record<string, any> = { imagePullSecret: { create: true, name: 42 } };
    disableNvidiaChartSecrets(v, 'nvidia');

    expect(v.imagePullSecret).toEqual({ create: false, name: 42 });
  });
});
