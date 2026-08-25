import { describe, expect, it } from 'vitest';
import {
  APP_COLLECTION_REPO_URL,
  SUSE_REGISTRY_REPO_URL,
  resolveRegistryEndpoints,
  registryEndpointOverrides,
} from '../registry-endpoints';

describe('registry endpoint settings', () => {
  it('shows the effective connected defaults for both SUSE repositories', () => {
    expect(resolveRegistryEndpoints()).toEqual({
      applicationCollection: APP_COLLECTION_REPO_URL,
      suseRegistry:          SUSE_REGISTRY_REPO_URL,
      nvidia:                '',
    });
  });

  it('shows configured private mirrors instead of connected defaults', () => {
    expect(resolveRegistryEndpoints({
      applicationCollection: 'oci://harbor.example.test/appco',
      suseRegistry:          'oci://harbor.example.test/suse',
      nvidia:                'oci://harbor.example.test/nvidia',
    })).toEqual({
      applicationCollection: 'oci://harbor.example.test/appco',
      suseRegistry:          'oci://harbor.example.test/suse',
      nvidia:                'oci://harbor.example.test/nvidia',
    });
  });

  it('keeps connected defaults implicit when serializing Settings', () => {
    expect(registryEndpointOverrides(resolveRegistryEndpoints())).toEqual({});
  });

  it('serializes only endpoint values that override connected behavior', () => {
    expect(registryEndpointOverrides(resolveRegistryEndpoints({
      applicationCollection: 'oci://harbor.example.test/appco',
      nvidia:                'oci://harbor.example.test/nvidia',
    }))).toEqual({
      applicationCollection: 'oci://harbor.example.test/appco',
      nvidia:                'oci://harbor.example.test/nvidia',
    });
  });
});
