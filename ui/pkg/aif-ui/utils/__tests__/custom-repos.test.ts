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

  it('rejects invalid names and duplicates and scheme mismatches', () => {
    expect(validateCustomRepoForm({ ...emptyCustomRepo(), name: '', type: 'helm', url: 'https://x' }, [])).toBeTruthy();
    expect(validateCustomRepoForm({ ...emptyCustomRepo(), name: 'Acme', type: 'helm', url: 'https://x' }, [])).toBeTruthy();
    expect(validateCustomRepoForm({ ...emptyCustomRepo(), name: 'nvidia', type: 'helm', url: 'https://x' }, [])).toBeTruthy();
    expect(validateCustomRepoForm({ ...emptyCustomRepo(), name: 'a', type: 'helm', url: 'https://x' }, ['a'])).toBeTruthy();
    expect(validateCustomRepoForm({ ...emptyCustomRepo(), name: 'a', type: 'oci', url: 'https://x' }, [])).toBeTruthy();
    expect(validateCustomRepoForm({ ...emptyCustomRepo(), name: 'a', type: 'git', gitRepo: 'https://x', gitBranch: '' }, [])).toBeTruthy();
    expect(validateCustomRepoForm({ ...emptyCustomRepo(), name: 'a', type: 'helm', url: 'https://x' }, [])).toBeNull();
  });
});
