import { describe, it, expect } from 'vitest';
import {
  buildInlineKustomize, buildGitRefKustomize, initialInlineBody, KUSTOMIZE_INLINE_STARTER,
} from '../wizard/kustomize-authoring';

describe('buildInlineKustomize', () => {
  it('wraps editor content into a files map under dir', () => {
    const k = buildInlineKustomize('overlay', 'resources: []\n');
    expect(k!.dir).toBe('overlay');
    expect(k!.files!['overlay/kustomization.yaml']).toContain('resources');
  });
  it('returns undefined for empty content', () => {
    expect(buildInlineKustomize('overlay', '   ')).toBeUndefined();
  });
  it('returns undefined for comment-only content', () => {
    expect(buildInlineKustomize('overlay', '# just a note\n  # indented\n\n')).toBeUndefined();
  });
  it('treats the untouched starter as no overlay', () => {
    expect(buildInlineKustomize('overlay', KUSTOMIZE_INLINE_STARTER)).toBeUndefined();
  });
  it('keeps comments alongside real content', () => {
    const body = '# add a label\npatches: []\n';
    expect(buildInlineKustomize('overlay', body)!.files!['overlay/kustomization.yaml']).toBe(body);
  });
});

describe('initialInlineBody', () => {
  it('returns the starter when there is no existing overlay', () => {
    expect(initialInlineBody(undefined)).toBe(KUSTOMIZE_INLINE_STARTER);
  });
  it('returns the existing inline kustomization.yaml', () => {
    expect(initialInlineBody({ dir: 'ov', files: { 'ov/kustomization.yaml': 'patches: []\n' } })).toBe('patches: []\n');
  });
  it('returns the starter for a gitRef overlay', () => {
    expect(initialInlineBody({ dir: 'overlay', gitRef: { chartRepo: 'r', path: 'p' } })).toBe(KUSTOMIZE_INLINE_STARTER);
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
