// Connected-mode chart repositories. These constants are the UI's source of
// truth for both catalog lookup and the effective values shown in Settings.
export const APP_COLLECTION_REPO_URL = 'oci://dp.apps.rancher.io/charts';
export const SUSE_REGISTRY_REPO_URL  = 'oci://registry.suse.com/ai/charts';

// NVIDIA uses two public HTTPS repositories in connected mode. A configured
// NVIDIA endpoint is therefore always a private OCI mirror, never a replacement
// default for one public repository.
export const NGC_HOST                      = 'helm.ngc.nvidia.com';
export const NVIDIA_REPO_URL               = `https://${ NGC_HOST }/nvidia`;
export const NVIDIA_BLUEPRINT_REPO_URL     = `https://${ NGC_HOST }/nvidia/blueprint`;

export interface RegistryEndpoints {
  applicationCollection: string;
  suseRegistry: string;
  nvidia: string;
}

export type RegistryEndpointOverrides = Partial<RegistryEndpoints>;

/** Resolve stored overrides to the endpoint values that are actually in use. */
export function resolveRegistryEndpoints(
  configured?: RegistryEndpointOverrides | null,
): RegistryEndpoints {
  return {
    applicationCollection: configured?.applicationCollection || APP_COLLECTION_REPO_URL,
    suseRegistry:          configured?.suseRegistry || SUSE_REGISTRY_REPO_URL,
    nvidia:                configured?.nvidia || '',
  };
}

/**
 * Return only real overrides for persistence. Connected defaults stay implicit
 * so an upgrade can change them without leaving an old URL pinned in Settings.
 */
export function registryEndpointOverrides(
  effective: RegistryEndpoints,
): RegistryEndpointOverrides {
  const configured: RegistryEndpointOverrides = {};

  if (effective.applicationCollection && effective.applicationCollection !== APP_COLLECTION_REPO_URL) {
    configured.applicationCollection = effective.applicationCollection;
  }
  if (effective.suseRegistry && effective.suseRegistry !== SUSE_REGISTRY_REPO_URL) {
    configured.suseRegistry = effective.suseRegistry;
  }
  if (effective.nvidia) {
    configured.nvidia = effective.nvidia;
  }

  return configured;
}
