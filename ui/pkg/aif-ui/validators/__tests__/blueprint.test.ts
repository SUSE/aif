import { describe, it, expect } from 'vitest';
import { validateDependsOn } from '../blueprint';

describe('validateDependsOn', () => {
  it('rejects a cycle', () => {
    const err = validateDependsOn([
      { chartName: 'a', dependsOn: ['b'] },
      { chartName: 'b', dependsOn: ['a'] },
    ]);
    expect(err).toMatch(/cycle/i);
  });
  it('rejects unknown ref', () => {
    expect(validateDependsOn([{ chartName: 'a', dependsOn: ['ghost'] }])).toMatch(/ghost/);
  });
  it('rejects self ref', () => {
    expect(validateDependsOn([{ chartName: 'a', dependsOn: ['a'] }])).toBeTruthy();
  });
  it('accepts a valid DAG', () => {
    expect(validateDependsOn([{ chartName: 'a', dependsOn: ['b'] }, { chartName: 'b' }])).toBeNull();
  });
});
