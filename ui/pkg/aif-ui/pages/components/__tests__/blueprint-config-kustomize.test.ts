import { describe, it, expect } from 'vitest';
import { readFileSync } from 'node:fs';
import path from 'node:path';
import yaml from 'js-yaml';
import {
  buildInlineKustomize, buildGitRefKustomize, initialInlineFiles, outsideDirFiles, unreferencedFiles, emptyReferencedFiles,
  validateInlineFileName, looksLikeCrWrapper, KUSTOMIZE_INLINE_STARTER, KUSTOMIZE_EXAMPLE_RESOURCE, KUSTOMIZATION_FILE_NAMES, SAFE_SEGMENT,
} from '../wizard/kustomize-authoring';
import type { InlineFile } from '../wizard/kustomize-authoring';

const root = (content: string): InlineFile => ({ name: 'kustomization.yaml', content });

describe('buildInlineKustomize', () => {
  it('wraps editor content into a files map under dir', () => {
    const k = buildInlineKustomize('overlay', [root('resources: []\n')]);
    expect(k!.dir).toBe('overlay');
    expect(k!.files!['overlay/kustomization.yaml']).toContain('resources');
  });
  it('returns undefined for empty content', () => {
    expect(buildInlineKustomize('overlay', [root('  ')])).toBeUndefined();
  });
  it('returns undefined for comment-only content', () => {
    expect(buildInlineKustomize('overlay', [root('# just a note\n  # indented\n\n')])).toBeUndefined();
  });
  it('treats an untouched starter as no overlay', () => {
    expect(buildInlineKustomize('overlay', [root(KUSTOMIZE_INLINE_STARTER)])).toBeUndefined();
  });
  it('keeps comments alongside real content', () => {
    const body = '# add label\npatches: []\n';
    expect(buildInlineKustomize('overlay', [root(body)])!.files!['overlay/kustomization.yaml']).toBe(body);
  });
  it('emits every file under dir', () => {
    const k = buildInlineKustomize('overlay', [
      root('resources:\n  - namespace.yaml\n'),
      { name: 'namespace.yaml', content: 'apiVersion: v1\nkind: Namespace\n' },
      { name: 'patches/deploy.yaml', content: 'kind: Deployment\n' },
    ]);
    expect(Object.keys(k!.files!).sort()).toEqual(['overlay/kustomization.yaml', 'overlay/namespace.yaml', 'overlay/patches/deploy.yaml']);
  });
  it('keeps the overlay when only a sibling file has content', () => {
    const k = buildInlineKustomize('overlay', [root(KUSTOMIZE_INLINE_STARTER), { name: 'ns.yaml', content: 'kind: Namespace\n' }]);
    expect(k!.files!['overlay/kustomization.yaml']).toBe(KUSTOMIZE_INLINE_STARTER);
    expect(k!.files!['overlay/ns.yaml']).toBe('kind: Namespace\n');
  });
  it('treats the untouched starter and example resource as no overlay', () => {
    expect(buildInlineKustomize('overlay', initialInlineFiles(undefined))).toBeUndefined();
  });
  it('leaves out the untouched example resource when nothing references it', () => {
    const k = buildInlineKustomize('overlay', [root('patches: []\n'), { ...KUSTOMIZE_EXAMPLE_RESOURCE }]);
    expect(Object.keys(k!.files!)).toEqual(['overlay/kustomization.yaml']);
  });
  it('keeps the example resource once it is edited or referenced', () => {
    const edited = buildInlineKustomize('overlay', [root('patches: []\n'), { name: 'configmap.yaml', content: 'kind: ConfigMap\n' }]);
    const referenced = buildInlineKustomize('overlay', [root('resources:\n  - configmap.yaml\n'), { ...KUSTOMIZE_EXAMPLE_RESOURCE }]);
    expect(edited!.files!['overlay/configmap.yaml']).toBe('kind: ConfigMap\n');
    expect(referenced!.files!['overlay/configmap.yaml']).toBe(KUSTOMIZE_EXAMPLE_RESOURCE.content);
  });
  it('defaults dir to "overlay" when blank', () => {
    expect(buildInlineKustomize(' ', [root('patches: []\n')])!.dir).toBe('overlay');
  });
});

describe('initialInlineFiles', () => {
  it('returns the starter and the example resource for no existing overlay', () => {
    expect(initialInlineFiles(undefined)).toEqual([root(KUSTOMIZE_INLINE_STARTER), KUSTOMIZE_EXAMPLE_RESOURCE]);
  });
  it('returns the starter and the example resource for a gitRef overlay', () => {
    expect(initialInlineFiles({ dir: 'overlay', gitRef: { chartRepo: 'r', path: 'p' } })).toEqual([root(KUSTOMIZE_INLINE_STARTER), KUSTOMIZE_EXAMPLE_RESOURCE]);
  });
  it('does not add the example resource to an existing overlay', () => {
    expect(initialInlineFiles({ dir: 'ov', files: { 'ov/kustomization.yaml': 'patches: []\n' } }).map((f) => f.name)).toEqual(['kustomization.yaml']);
  });
  it('returns the existing inline kustomization.yaml', () => {
    expect(initialInlineFiles({ dir: 'ov', files: { 'ov/kustomization.yaml': 'patches: []\n' } })).toEqual([root('patches: []\n')]);
  });
  it('loads every file, root first and the rest sorted', () => {
    const files = initialInlineFiles({
      dir:   'ov',
      files: { 'ov/z.yaml': 'z', 'ov/kustomization.yaml': 'k', 'ov/a/b.yaml': 'ab' },
    });
    expect(files.map((f) => f.name)).toEqual(['kustomization.yaml', 'a/b.yaml', 'z.yaml']);
  });
  it('round-trips a multi-file overlay without dropping files', () => {
    const k = { dir: 'ov', files: { 'ov/kustomization.yaml': 'resources:\n  - ns.yaml\n', 'ov/ns.yaml': 'kind: Namespace\n' } };
    expect(buildInlineKustomize('ov', initialInlineFiles(k))).toEqual(k);
  });
  it('recognizes kustomization.yml as the root', () => {
    expect(initialInlineFiles({ dir: 'ov', files: { 'ov/a.yaml': 'a', 'ov/kustomization.yml': 'k' } })[0].name).toBe('kustomization.yml');
  });
  it('leaves files outside dir out of the editor', () => {
    const files = initialInlineFiles({ dir: 'ov', files: { 'ov/kustomization.yaml': 'k', 'other/x.yaml': 'x', 'kustomization.yaml': 'top' } });
    expect(files).toEqual([root('k')]);
  });
  it('adds the starter root when dir holds no root file', () => {
    expect(initialInlineFiles({ dir: 'ov', files: { 'ov/ns.yaml': 'n' } }).map((f) => f.name)).toEqual(['kustomization.yaml', 'ns.yaml']);
  });
});

describe('outsideDirFiles', () => {
  it('returns only keys outside dir, verbatim', () => {
    expect(outsideDirFiles({ dir: 'ov', files: { 'ov/kustomization.yaml': 'k', 'other/x.yaml': 'x', 'ovx/y.yaml': 'y' } }))
      .toEqual({ 'other/x.yaml': 'x', 'ovx/y.yaml': 'y' });
  });
  it('is empty without inline files', () => {
    expect(outsideDirFiles(undefined)).toEqual({});
    expect(outsideDirFiles({ dir: 'ov', gitRef: { chartRepo: 'r', path: 'p' } })).toEqual({});
  });
  it('round-trips outside files unchanged instead of moving them under dir', () => {
    const k = { dir: 'ov', files: { 'ov/kustomization.yaml': 'patches: []\n', 'other/x.yaml': 'x' } };
    expect(buildInlineKustomize('ov', initialInlineFiles(k), outsideDirFiles(k))).toEqual(k);
  });
});

describe('unreferencedFiles', () => {
  const ns = { name: 'namespace.yaml', content: 'kind: Namespace\n' };
  it('flags a sibling the root does not name', () => {
    expect(unreferencedFiles([root('patches: []\n'), ns])).toEqual(['namespace.yaml']);
  });
  it('flags siblings of a comment-only root', () => {
    expect(unreferencedFiles([root(KUSTOMIZE_INLINE_STARTER), ns])).toEqual(['namespace.yaml']);
  });
  it('accepts files named by resources, patches and generators', () => {
    const files = [
      root([
        'resources:\n  - ./namespace.yaml\n  - extra/',
        'patches:\n  - path: patch.yaml\n    target:\n      kind: StatefulSet',
        'configMapGenerator:\n  - name: cm\n    files:\n      - app.conf=conf/app.conf\n    envs:\n      - cm.env',
        'components:\n  - comp',
      ].join('\n')),
      ns,
      { name: 'extra/kustomization.yaml', content: 'x' },
      { name: 'extra/svc.yaml', content: 'x' },
      { name: 'patch.yaml', content: 'x' },
      { name: 'conf/app.conf', content: 'x' },
      { name: 'cm.env', content: 'x' },
      { name: 'comp/kustomization.yaml', content: 'x' },
    ];
    expect(unreferencedFiles(files)).toEqual([]);
  });
  it('does not treat a name prefix as a directory reference', () => {
    expect(unreferencedFiles([root('resources:\n  - extra\n'), { name: 'extra2.yaml', content: 'x' }])).toEqual(['extra2.yaml']);
  });
  it('reports nothing when the root is not valid YAML', () => {
    expect(unreferencedFiles([root('resources: [\n'), ns])).toEqual([]);
  });
  it('reports nothing for a root-only overlay', () => {
    expect(unreferencedFiles([root('patches: []\n')])).toEqual([]);
  });
  it('ignores empty and comment-only files such as the untouched example', () => {
    expect(unreferencedFiles(initialInlineFiles(undefined))).toEqual([]);
    expect(unreferencedFiles([root('patches: []\n'), { name: 'blank.yaml', content: '' }])).toEqual([]);
  });
});

describe('emptyReferencedFiles', () => {
  it('flags a referenced file that is still only comments', () => {
    expect(emptyReferencedFiles([root('resources:\n  - configmap.yaml\n'), { ...KUSTOMIZE_EXAMPLE_RESOURCE }])).toEqual(['configmap.yaml']);
  });
  it('ignores referenced files with content and unreferenced empty files', () => {
    expect(emptyReferencedFiles([root('resources:\n  - a.yaml\n'), { name: 'a.yaml', content: 'kind: ConfigMap\n' }, { name: 'b.yaml', content: '' }])).toEqual([]);
  });
});

describe('KUSTOMIZE starter examples', () => {
  // Uncommenting the YAML lines of the examples gives a working overlay.
  const uncomment = (s: string) => s.split('\n').filter((l) => /^#( {2,}| ?(patches|resources|apiVersion|kind|metadata|data):)/.test(l)).map((l) => l.replace(/^# ?/, '')).join('\n');

  it('patches the qdrant StatefulSet and lists the example ConfigMap', () => {
    const k = yaml.load(uncomment(KUSTOMIZE_INLINE_STARTER)) as Record<string, any>;
    const patch = yaml.load(k.patches[0].patch) as Record<string, any>;

    expect(k.patches[0].target).toEqual({ kind: 'StatefulSet', name: 'qdrant' });
    expect(patch.spec.template.spec.containers[0]).toMatchObject({ name: 'qdrant', resources: { limits: { memory: '2Gi' } } });
    expect(k.resources).toEqual([KUSTOMIZE_EXAMPLE_RESOURCE.name]);
  });
  it('the example resource is a ConfigMap', () => {
    expect(yaml.load(uncomment(KUSTOMIZE_EXAMPLE_RESOURCE.content))).toMatchObject({ apiVersion: 'v1', kind: 'ConfigMap' });
  });
  it('referencing the uncommented example clears every warning', () => {
    const files = [root(uncomment(KUSTOMIZE_INLINE_STARTER)), { name: 'configmap.yaml', content: uncomment(KUSTOMIZE_EXAMPLE_RESOURCE.content) }];
    expect(unreferencedFiles(files)).toEqual([]);
    expect(emptyReferencedFiles(files)).toEqual([]);
  });
});

describe('cross-language overlay constants (drift pins)', () => {
  // KUSTOMIZATION_FILE_NAMES mirrors the Go kustomizeoverlay.FileNames, which
  // pins the same literals (TestFileNamesLiteral). Change BOTH together.
  it('KUSTOMIZATION_FILE_NAMES matches the Go FileNames literal', () => {
    expect(KUSTOMIZATION_FILE_NAMES).toEqual(['kustomization.yaml', 'kustomization.yml', 'Kustomization']);
  });
  // The UI CI also runs on changes to this CRD, so a pattern change there fails here.
  it('SAFE_SEGMENT is the segment of the CRD kustomize.dir pattern', () => {
    const crd = readFileSync(path.resolve(__dirname, '../../../../../../charts/aif-operator/crds/ai-factory.suse.com_blueprints.yaml'), 'utf8');
    const seg = SAFE_SEGMENT.source.slice(1, -1);
    expect(crd).toContain(`pattern: ^${ seg }(/${ seg })*$`);
  });
});

describe('validateInlineFileName', () => {
  const existing = [root(''), { name: 'ns.yaml', content: '' }];
  it.each([
    ['', 'empty'],
    ['  ', 'empty'],
    ['/abs.yaml', 'invalid'],
    ['../escape.yaml', 'invalid'],
    ['a/../b.yaml', 'invalid'],
    ['./a.yaml', 'invalid'],
    ['dir/', 'invalid'],
    ['a//b.yaml', 'invalid'],
    ['a\\b.yaml', 'invalid'],
    ['kustomization.yaml', 'reserved'],
    ['Kustomization', 'reserved'],
    ['ns.yaml', 'duplicate'],
  ])('rejects %j as %s', (name, err) => {
    expect(validateInlineFileName(name, existing)).toBe(err);
  });
  it.each(['namespace.yaml', 'patches/deploy.yaml', '.hidden.yaml', '..dots.yaml'])('accepts %j', (name) => {
    expect(validateInlineFileName(name, existing)).toBeUndefined();
  });
});

describe('looksLikeCrWrapper', () => {
  it('flags the ComponentKustomize wrapper pasted into kustomization.yaml', () => {
    expect(looksLikeCrWrapper('kustomize:\n  dir: overlay\n  files: {}\n')).toBe(true);
  });
  it('does not flag a regular kustomization', () => {
    expect(looksLikeCrWrapper('resources:\n  - ns.yaml\npatches:\n  - path: p.yaml\n')).toBe(false);
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
