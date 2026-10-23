import { describe, it, expect } from 'vitest';
import { buildInlineKustomize, buildGitRefKustomize } from '../wizard/kustomize-authoring';

describe('buildInlineKustomize', () => {
  it('wraps editor content into a files map under dir', () => {
    const k = buildInlineKustomize('overlay', 'resources: []\n');
    expect(k!.dir).toBe('overlay');
    expect(k!.files!['overlay/kustomization.yaml']).toContain('resources');
  });
  it('returns undefined for empty content', () => {
    expect(buildInlineKustomize('overlay', '   ')).toBeUndefined();
  });
});

describe('buildGitRefKustomize', () => {
  it('builds a gitRef overlay with the given dir, repo, and path', () => {
    const k = buildGitRefKustomize('overlay', 'my-git-repo', 'overlays/prod');
    expect(k).toEqual({ dir: 'overlay', gitRef: { chartRepo: 'my-git-repo', path: 'overlays/prod' } });
  });
  it('defaults dir to "overlay" when blank', () => {
    const k = buildGitRefKustomize('  ', 'my-git-repo', 'overlays/prod');
    expect(k!.dir).toBe('overlay');
  });
  it('trims chartRepo and path', () => {
    const k = buildGitRefKustomize('overlay', '  my-git-repo  ', '  overlays/prod  ');
    expect(k!.gitRef).toEqual({ chartRepo: 'my-git-repo', path: 'overlays/prod' });
  });
  it('returns undefined when chartRepo is empty', () => {
    expect(buildGitRefKustomize('overlay', '   ', 'overlays/prod')).toBeUndefined();
  });
  it('returns undefined when path is empty', () => {
    expect(buildGitRefKustomize('overlay', 'my-git-repo', '   ')).toBeUndefined();
  });
});
