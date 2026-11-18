import { describe, expect, it } from 'vitest';
import {
  LEGACY_SUSE_PULL_SECRET_PREFIX, mergeImagePullSecrets, operatorPullSecretNames, OPERATOR_NVIDIA_PULL_SECRET, OPERATOR_SUSE_PULL_SECRET,
} from '../pull-secrets';
import type { RegistryStatus } from '../../types/aiworkload-types';

const status = (overrides: Partial<RegistryStatus> = {}): RegistryStatus => ({
  applicationCollection: false, suseRegistry: false, nvidia: false, ...overrides,
});
const allTrue = status({ applicationCollection: true, suseRegistry: true, nvidia: true });

describe('operatorPullSecretNames', () => {
  it('matches the operator secret names', () => {
    expect(OPERATOR_SUSE_PULL_SECRET).toBe('suse-ai-pull-combined');
    expect(OPERATOR_NVIDIA_PULL_SECRET).toBe('ngc-secret');
  });

  it('returns the combined secret for suse-ai when App Collection is configured', () => {
    expect(operatorPullSecretNames('suse-ai', status({ applicationCollection: true }))).toEqual(['suse-ai-pull-combined']);
  });

  it('returns the combined secret for suse-ai when only NVIDIA is configured (it carries the NVIDIA auths)', () => {
    expect(operatorPullSecretNames('suse-ai', status({ nvidia: true }))).toEqual(['suse-ai-pull-combined']);
  });

  it('returns nothing for suse-ai when no registry is configured', () => {
    expect(operatorPullSecretNames('suse-ai', status())).toEqual([]);
  });

  it('returns ngc-secret for nvidia only when NVIDIA is configured', () => {
    expect(operatorPullSecretNames('nvidia', status({ nvidia: true }))).toEqual(['ngc-secret']);
    expect(operatorPullSecretNames('nvidia', status({ applicationCollection: true, suseRegistry: true }))).toEqual([]);
  });

  it('returns nothing for custom or unclassified repositories', () => {
    expect(operatorPullSecretNames('custom', allTrue)).toEqual([]);
    expect(operatorPullSecretNames(undefined, allTrue)).toEqual([]);
  });

  it('returns nothing when the registry status is unavailable', () => {
    expect(operatorPullSecretNames('suse-ai', null)).toEqual([]);
  });
});

describe('mergeImagePullSecrets', () => {
  const COMBINED = OPERATOR_SUSE_PULL_SECRET;
  const LEGACY = 'suse-ai-pull-secret-dp-apps-rancher-io';

  it('matches the prefix of the legacy browser-created secrets', () => {
    expect(LEGACY_SUSE_PULL_SECRET_PREFIX).toBe('suse-ai-pull-secret-');
  });

  it('keeps user entries, drops legacy names not requested, and appends the requested names', () => {
    const list = () => [{ name: 'my-mirror' }, { name: LEGACY }];
    const values: Record<string, any> = { imagePullSecrets: list(), global: { imagePullSecrets: list() } };
    mergeImagePullSecrets(values, [COMBINED]);

    expect(values.imagePullSecrets).toEqual([{ name: 'my-mirror' }, { name: COMBINED }]);
    expect(values.global.imagePullSecrets).toEqual([{ name: 'my-mirror' }, { name: COMBINED }]);
  });

  it('keeps a legacy name that is requested, without duplicating any name', () => {
    const values: Record<string, any> = { imagePullSecrets: [{ name: LEGACY }, { name: COMBINED }, { name: 'my-mirror' }] };
    mergeImagePullSecrets(values, [COMBINED, LEGACY, COMBINED]);

    expect(values.imagePullSecrets).toEqual([{ name: LEGACY }, { name: COMBINED }, { name: 'my-mirror' }]);
    expect(values.global.imagePullSecrets).toEqual([{ name: COMBINED }, { name: LEGACY }]);
  });

  it('preserves string-form entries and dedupes against them', () => {
    const values: Record<string, any> = { global: { imagePullSecrets: ['my-mirror', COMBINED, LEGACY] } };
    mergeImagePullSecrets(values, [COMBINED]);

    expect(values.global.imagePullSecrets).toEqual(['my-mirror', COMBINED]);
    expect(values.imagePullSecrets).toEqual([{ name: COMBINED }]);
  });

  it('leaves the other global keys untouched', () => {
    const values: Record<string, any> = { global: { storageClass: 'fast', security: { allowInsecureImages: true } } };
    mergeImagePullSecrets(values, [COMBINED]);

    expect(values.global).toEqual({
      storageClass: 'fast', security: { allowInsecureImages: true }, imagePullSecrets: [{ name: COMBINED }],
    });
  });

  it('does not create the lists when there is nothing to put in them', () => {
    const values: Record<string, any> = { global: { storageClass: 'fast' } };
    mergeImagePullSecrets(values, []);

    expect(values).toEqual({ global: { storageClass: 'fast' } });
  });

  it('treats a null list as empty', () => {
    const values: Record<string, any> = { imagePullSecrets: null };
    mergeImagePullSecrets(values, [COMBINED]);

    expect(values.imagePullSecrets).toEqual([{ name: COMBINED }]);
  });
});
