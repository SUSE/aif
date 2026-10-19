import { describe, expect, it } from 'vitest';
import { HELM_MANAGED_ANNOTATION, parseHelmManaged } from '../helm-managed';

const withAnnotation = (value: string) => ({ metadata: { annotations: { [HELM_MANAGED_ANNOTATION]: value } } });

describe('parseHelmManaged', () => {
  it('returns an empty set for a missing object, metadata or annotation', () => {
    expect(parseHelmManaged(undefined).blueprintCatalogs.size).toBe(0);
    expect(parseHelmManaged({ spec: {} }).blueprintCatalogs.size).toBe(0);
    expect(parseHelmManaged({ metadata: { annotations: {} } }).blueprintCatalogs.size).toBe(0);
  });

  it('reads the Helm-declared catalog names', () => {
    const got = parseHelmManaged(withAnnotation('{"blueprintCatalogs":["partner-blueprints","team-a"]}'));

    expect([...got.blueprintCatalogs]).toEqual(['partner-blueprints', 'team-a']);
  });

  it('ignores unknown keys reserved for later scopes', () => {
    const got = parseHelmManaged(withAnnotation('{"blueprintCatalogs":["a"],"fields":["rancherCatalog.url"]}'));

    expect([...got.blueprintCatalogs]).toEqual(['a']);
  });

  // Review Focus 4: the page must still render; the API refuses the save.
  it('treats invalid JSON or wrong types as nothing managed', () => {
    expect(parseHelmManaged(withAnnotation('{"blueprintCatalogs":')).blueprintCatalogs.size).toBe(0);
    expect(parseHelmManaged(withAnnotation('{"blueprintCatalogs":"a"}')).blueprintCatalogs.size).toBe(0);
    expect([...parseHelmManaged(withAnnotation('{"blueprintCatalogs":["a",3,""]}')).blueprintCatalogs]).toEqual(['a']);
  });
});
