import { describe, it, expect } from 'vitest';
import { emptyCustomRepo, buildCustomReposCrd, buildCustomReposForm, validateCustomRepoForm } from '../custom-repos';

describe('custom-repos form helpers', () => {
  it('round-trips a helm repo through crd and back', () => {
    const f = { ...emptyCustomRepo(), name: 'acme', type: 'helm' as const, url: 'https://charts.example.com' };
    const crd = buildCustomReposCrd([f]);
    expect(crd).toEqual([{ name: 'acme', type: 'helm', url: 'https://charts.example.com' }]);
    const back = buildCustomReposForm(crd);
    expect(back[0].name).toBe('acme');
    expect(back[0].url).toBe('https://charts.example.com');
  });

  it('shapes a git repo with gitRepo/gitBranch and no url', () => {
    const f = { ...emptyCustomRepo(), name: 'g', type: 'git' as const, gitRepo: 'https://git/r.git', gitBranch: 'main' };
    expect(buildCustomReposCrd([f])).toEqual([{ name: 'g', type: 'git', gitRepo: 'https://git/r.git', gitBranch: 'main' }]);
  });

  it('includes secret refs and insecure flag when set', () => {
    const f = { ...emptyCustomRepo(), name: 'a', type: 'oci' as const, url: 'oci://r/c', userSecretRef: { name: 's', key: 'u' }, tokenSecretRef: { name: 's', key: 't' }, insecureSkipTLSVerify: true };
    expect(buildCustomReposCrd([f])[0]).toMatchObject({ userSecretRef: { name: 's', key: 'u' }, insecureSkipTLSVerify: true });
  });

  it('saves the chart used to test the repository, trimmed, and reads it back', () => {
    const f = { ...emptyCustomRepo(), name: 'a', type: 'oci' as const, url: 'oci://r/c', testChart: ' demo ' };
    const crd = buildCustomReposCrd([f]);
    expect(crd[0].testChart).toBe('demo');
    expect(buildCustomReposForm(crd)[0].testChart).toBe('demo');
  });

  it('omits an empty test chart, and the test chart of git repos (they have no chart test)', () => {
    expect(buildCustomReposCrd([{ ...emptyCustomRepo(), name: 'a', type: 'helm' as const, url: 'https://x' }])[0]).not.toHaveProperty('testChart');
    expect(buildCustomReposCrd([{ ...emptyCustomRepo(), name: 'g', type: 'git' as const, gitRepo: 'https://git/r.git', testChart: 'demo' }])[0]).not.toHaveProperty('testChart');
    expect(buildCustomReposForm([{ name: 'a', type: 'helm', url: 'https://x' }])[0].testChart).toBe('');
  });

  it('rejects invalid names and duplicates and scheme mismatches', () => {
    expect(validateCustomRepoForm({ ...emptyCustomRepo(), name: '', type: 'helm', url: 'https://x' }, [])).toBeTruthy();
    expect(validateCustomRepoForm({ ...emptyCustomRepo(), name: 'Acme', type: 'helm', url: 'https://x' }, [])).toBeTruthy();
    expect(validateCustomRepoForm({ ...emptyCustomRepo(), name: 'nvidia', type: 'helm', url: 'https://x' }, [])).toBeTruthy();
    expect(validateCustomRepoForm({ ...emptyCustomRepo(), name: 'a', type: 'helm', url: 'https://x' }, ['a'])).toBeTruthy();
    expect(validateCustomRepoForm({ ...emptyCustomRepo(), name: 'a', type: 'oci', url: 'https://x' }, [])).toBeTruthy();
    expect(validateCustomRepoForm({ ...emptyCustomRepo(), name: 'a', type: 'git', gitRepo: 'https://x', gitBranch: '' }, [])).toBeTruthy();
    expect(validateCustomRepoForm({ ...emptyCustomRepo(), name: 'a', type: 'helm', url: 'https://x' }, [])).toBeNull();
  });

  it('reserves Rancher default repository names', () => {
    for (const name of ['rancher-charts', 'rancher-partner-charts', 'rancher-rke2-charts']) {
      expect(validateCustomRepoForm({ ...emptyCustomRepo(), name, type: 'helm', url: 'https://x' }, [])).toBeTruthy();
    }
  });

  it('allows names up to the 63-character ClusterRepo name limit', () => {
    expect(validateCustomRepoForm({ ...emptyCustomRepo(), name: 'a'.repeat(63), type: 'helm', url: 'https://x' }, [])).toBeNull();
    expect(validateCustomRepoForm({ ...emptyCustomRepo(), name: 'a'.repeat(64), type: 'helm', url: 'https://x' }, [])).toBeTruthy();
  });
});
