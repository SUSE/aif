import { describe, it, expect } from 'vitest';
import { missingCredentialsForBlueprint } from '../blueprint-preflight';
import type { BlueprintComponent } from '../../types/blueprint-types';
import type { RegistryStatus } from '../../types/aiworkload-types';

const comp = (chartRepo: string) => ({ chartName: `c-${ chartRepo }`, chartRepo } as BlueprintComponent);

describe('missingCredentialsForBlueprint', () => {
  const status: RegistryStatus = { applicationCollection: true, suseRegistry: false, nvidia: false };

  it('returns only the required credentials the operator reports as unconfigured', () => {
    expect(missingCredentialsForBlueprint([comp('application-collection'), comp('nvidia')], status)).toEqual(['nvidia']);
  });

  it('returns nothing when the registry status is unknown', () => {
    expect(missingCredentialsForBlueprint([comp('application-collection'), comp('nvidia')], null)).toEqual([]);
  });

  it('skips components whose chartRepo needs no managed credential', () => {
    expect(missingCredentialsForBlueprint([comp('my-custom-repo')], status)).toEqual([]);
  });
});
