import yaml from 'js-yaml';
import type { ComponentKustomize } from '../../../types/blueprint-types';

// KUSTOMIZE_INLINE_STARTER pre-fills the inline editor. Fully commented, so an
// untouched starter is treated as "no overlay" by buildInlineKustomize.
export const KUSTOMIZE_INLINE_STARTER = `# Kustomize post-render overlay for this app.
# Applied to the chart's rendered output. Fleet adds the rendered manifests
# as a resource automatically, so you only declare changes.
#
# The examples below are written for the qdrant chart. Uncomment and adapt
# them: kind and name must match what this app's chart renders.
#
# Example 1: patch the qdrant StatefulSet. Adds a label and sets memory
# for the "qdrant" container. Lists merge by container name, so only the
# fields given here change.
#
# patches:
#   - target:
#       kind: StatefulSet
#       name: qdrant
#     patch: |
#       apiVersion: apps/v1
#       kind: StatefulSet
#       metadata:
#         name: qdrant
#         labels:
#           team: ml-platform
#       spec:
#         template:
#           spec:
#             containers:
#               - name: qdrant
#                 resources:
#                   requests:
#                     memory: 1Gi
#                   limits:
#                     memory: 2Gi
#
# Example 2: add a new object. The ConfigMap in configmap.yaml (next tab)
# is only created once it is uncommented and listed here:
#
# resources:
#   - configmap.yaml
`;

// KUSTOMIZE_EXAMPLE_RESOURCE is the extra file a new inline overlay opens with.
// Like the starter it is fully commented, and it is not saved while untouched.
export const KUSTOMIZE_EXAMPLE_RESOURCE = {
  name:    'configmap.yaml',
  content: `# Example extra object for this overlay. Uncomment it and list
# configmap.yaml under resources in kustomization.yaml.
# It is created in the app's namespace, belongs to the app's Helm release
# and is deleted when the app is uninstalled.
#
# apiVersion: v1
# kind: ConfigMap
# metadata:
#   name: qdrant-overlay-settings
# data:
#   team: ml-platform
`,
};

// Root kustomization file names Kustomize accepts, in its lookup order. Mirrors
// the operator's kustomizeoverlay.FileNames; both sides pin the literals.
export const KUSTOMIZATION_FILE_NAMES = ['kustomization.yaml', 'kustomization.yml', 'Kustomization'];
export const DEFAULT_KUSTOMIZATION_FILE = KUSTOMIZATION_FILE_NAMES[0];

// InlineFile is one file of an inline overlay, named relative to the overlay dir.
export interface InlineFile {
  name:    string;
  content: string;
}

function hasContent(body: string): boolean {
  return body.split('\n').some((line) => line.trim() !== '' && !line.trim().startsWith('#'));
}

function isRootName(name: string): boolean {
  return KUSTOMIZATION_FILE_NAMES.includes(name);
}

// splitByDir separates an overlay's files into those under its dir and the rest.
function splitByDir(k: ComponentKustomize | undefined): { inside: [string, string][], outside: [string, string][] } {
  const prefix = `${ k?.dir }/`;
  const entries = Object.entries(k?.files || {});

  return {
    inside:  entries.filter(([key]) => key.startsWith(prefix)).map(([key, content]) => [key.slice(prefix.length), content]),
    outside: entries.filter(([key]) => !key.startsWith(prefix)),
  };
}

// initialInlineFiles is what the inline editor opens with: the files under the
// overlay dir (root kustomization first, the rest sorted), always including a
// root, which starts from the starter when missing. A new overlay also gets the
// example resource. Files outside the dir are not editable; see outsideDirFiles.
export function initialInlineFiles(k: ComponentKustomize | undefined): InlineFile[] {
  const files = splitByDir(k).inside.map(([name, content]) => ({ name, content }));

  if (!files.length) {
    return [{ name: DEFAULT_KUSTOMIZATION_FILE, content: KUSTOMIZE_INLINE_STARTER }, { ...KUSTOMIZE_EXAMPLE_RESOURCE }];
  }
  const root = files.find((f) => isRootName(f.name)) || { name: DEFAULT_KUSTOMIZATION_FILE, content: KUSTOMIZE_INLINE_STARTER };
  const rest = files.filter((f) => f !== root).sort((a, b) => a.name.localeCompare(b.name));

  return [root, ...rest];
}

// outsideDirFiles returns the overlay's files that are not under its dir, keyed
// verbatim. The operator rejects them, but the editor passes them through
// unchanged instead of silently moving them under the dir.
export function outsideDirFiles(k: ComponentKustomize | undefined): Record<string, string> {
  return Object.fromEntries(splitByDir(k).outside);
}

// buildInlineKustomize turns the editor's files into the files-map shape the
// operator embeds, keyed "<dir>/<name>", plus any passthrough files kept
// verbatim. The overlay is cleared only when no editor file has real
// (non-comment) content, so an untouched starter is "no overlay". The untouched
// example resource is left out unless the kustomization references it.
export function buildInlineKustomize(dir: string, files: InlineFile[], passthrough: Record<string, string> = {}): ComponentKustomize | undefined {
  if (!files.some((f) => hasContent(f.content))) return undefined;
  const d = dir.trim() || 'overlay';
  const out: Record<string, string> = { ...passthrough };
  const refs = rootRefs(files) || [];

  for (const f of files) {
    if (f.content === KUSTOMIZE_EXAMPLE_RESOURCE.content && !isReferenced(f.name, refs)) continue;
    out[`${ d }/${ f.name }`] = f.content;
  }

  return { dir: d, files: out };
}

// Kustomization fields whose values name files or directories.
const REF_LISTS = ['resources', 'components', 'crds', 'generators', 'transformers', 'validators', 'patchesStrategicMerge'];
const PATH_LISTS = ['patches', 'patchesJson6902', 'replacements'];
const GENERATORS = ['configMapGenerator', 'secretGenerator'];

type YamlMap = Record<string, unknown>;

function asList(v: unknown): unknown[] {
  return Array.isArray(v) ? v : [];
}

function field(v: unknown, key: string): unknown {
  return v && typeof v === 'object' ? (v as YamlMap)[key] : undefined;
}

function kustomizationRefs(k: YamlMap): string[] {
  const refs: unknown[] = [];

  REF_LISTS.forEach((key) => refs.push(...asList(k[key])));
  PATH_LISTS.forEach((key) => asList(k[key]).forEach((p) => refs.push(field(p, 'path'))));
  GENERATORS.forEach((key) => asList(k[key]).forEach((g) => {
    // files entries may be "key=path".
    asList(field(g, 'files')).forEach((f) => refs.push(typeof f === 'string' ? f.slice(f.indexOf('=') + 1) : f));
    refs.push(...asList(field(g, 'envs')), field(g, 'env'));
  }));

  return refs
    .filter((r): r is string => typeof r === 'string')
    .map((r) => r.trim().replace(/^(\.\/)+/, '').replace(/\/+$/, ''))
    .filter(Boolean);
}

// rootRefs returns what the root kustomization references, or undefined when it
// is not parseable YAML (the deploy reports that).
function rootRefs(files: InlineFile[]): string[] | undefined {
  const root = files.find((f) => isRootName(f.name));
  let parsed: unknown;

  try {
    parsed = yaml.load(root?.content || '');
  } catch {
    return undefined;
  }

  return parsed && typeof parsed === 'object' ? kustomizationRefs(parsed as YamlMap) : [];
}

// A file is referenced directly or through a referenced directory.
function isReferenced(name: string, refs: string[]): boolean {
  return refs.some((r) => name === r || name.startsWith(`${ r }/`));
}

// unreferencedFiles lists the non-root files with content that the root
// kustomization does not reference. Kustomize silently ignores them, so a
// forgotten resources/patches entry deploys without any effect.
export function unreferencedFiles(files: InlineFile[]): string[] {
  const refs = rootRefs(files);

  if (!refs) return [];

  return files.filter((f) => !isRootName(f.name) && hasContent(f.content) && !isReferenced(f.name, refs)).map((f) => f.name);
}

// emptyReferencedFiles lists referenced files that are empty or only comments,
// such as a still-commented example. Kustomize accepts them as no objects.
export function emptyReferencedFiles(files: InlineFile[]): string[] {
  const refs = rootRefs(files) || [];

  return files.filter((f) => !isRootName(f.name) && !hasContent(f.content) && isReferenced(f.name, refs)).map((f) => f.name);
}

// Same segment rules as the operator's ComponentKustomize.Dir pattern: no
// empty, "." or ".." segments and no leading or trailing slash. Pinned against
// the CRD in blueprint-config-kustomize.test.ts.
export const SAFE_SEGMENT = /^([^/.][^/]*|\.[^/.][^/]*|\.\.[^/]+)$/;

export type InlineFileNameError = 'empty' | 'invalid' | 'reserved' | 'duplicate';

// validateInlineFileName checks a new file name, relative to the overlay dir.
// Subdirectories ("patches/deploy.yaml") are allowed.
export function validateInlineFileName(name: string, existing: InlineFile[]): InlineFileNameError | undefined {
  const n = name.trim();

  if (!n) return 'empty';
  if (n.includes('\\') || !n.split('/').every((s) => SAFE_SEGMENT.test(s))) return 'invalid';
  if (isRootName(n)) return 'reserved';
  if (existing.some((f) => f.name === n)) return 'duplicate';

  return undefined;
}

// Top-level keys of the ComponentKustomize CR wrapper. Pasting the CR snippet
// into the kustomization editor fails at deploy time with
// `invalid Kustomization: json: unknown field "kustomize"`, so it is flagged up front.
const WRAPPER_KEY = /^(kustomize|files|dir|gitRef)\s*:/m;

export function looksLikeCrWrapper(kustomizationBody: string): boolean {
  return WRAPPER_KEY.test(kustomizationBody);
}

// buildGitRefKustomize points the overlay at a path within a git-backed
// ClusterRepo instead of inlining files. Both chartRepo and path are required.
export function buildGitRefKustomize(dir: string, chartRepo: string, path: string): ComponentKustomize | undefined {
  if (!chartRepo.trim() || !path.trim()) return undefined;

  return { dir: dir.trim() || 'overlay', gitRef: { chartRepo: chartRepo.trim(), path: path.trim() } };
}
