// Mirror of operator api/v1alpha1 SettingsHelmManagedAnnotation. The aif-operator
// chart lists the Settings values it declared here; those are changed through
// Helm only, so the Settings page shows them read-only and never sends them.
export const HELM_MANAGED_ANNOTATION = 'ai-factory.suse.com/helm-managed';

export interface HelmManaged {
  blueprintCatalogs: Set<string>;
}

export function emptyHelmManaged(): HelmManaged {
  return { blueprintCatalogs: new Set() };
}

// A missing or unreadable annotation marks nothing as managed. The operator
// refuses to save with an unreadable one, so the page never claims Helm values.
export function parseHelmManaged(settings: any): HelmManaged {
  const raw = settings?.metadata?.annotations?.[HELM_MANAGED_ANNOTATION];

  if (!raw) return emptyHelmManaged();

  try {
    const value = JSON.parse(raw);
    const names = Array.isArray(value?.blueprintCatalogs) ? value.blueprintCatalogs : [];

    return { blueprintCatalogs: new Set(names.filter((n: unknown) => typeof n === 'string' && n)) };
  } catch {
    return emptyHelmManaged();
  }
}
