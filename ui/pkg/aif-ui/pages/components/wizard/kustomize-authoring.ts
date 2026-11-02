import type { ComponentKustomize } from '../../../types/blueprint-types';

// KUSTOMIZE_INLINE_STARTER pre-fills the inline editor. Fully commented, so an
// untouched starter is treated as "no overlay" by buildInlineKustomize.
export const KUSTOMIZE_INLINE_STARTER = `# Kustomize post-render overlay for this app.
# Applied to the chart's rendered output. Fleet adds the rendered manifests
# as a resource automatically, so you only declare changes.
#
# Example: add a label and raise memory on the chart's Deployments.
#
# patches:
#   - target:
#       kind: Deployment
#       name: .*            # regex; narrow to a specific name if needed
#     patch: |
#       - op: add
#         path: /metadata/labels/team
#         value: ml-platform
#       - op: replace
#         path: /spec/template/spec/containers/0/resources/limits/memory
#         value: 2Gi
`;

function hasContent(body: string): boolean {
  return body.split('\n').some(line => line.trim() !== '' && !line.trim().startsWith('#'));
}

// buildInlineKustomize wraps the editor's kustomization.yaml body into the
// files-map shape the operator embeds. Empty or comment-only content clears the overlay.
export function buildInlineKustomize(dir: string, kustomizationBody: string): ComponentKustomize | undefined {
  if (!hasContent(kustomizationBody)) return undefined;
  const d = dir.trim() || 'overlay';
  return { dir: d, files: { [`${d}/kustomization.yaml`]: kustomizationBody } };
}

// initialInlineBody is what the inline editor opens with: the component's
// existing inline kustomization.yaml, or the starter.
export function initialInlineBody(k: ComponentKustomize | undefined): string {
  return (k?.files && k.files[`${k.dir}/kustomization.yaml`]) || KUSTOMIZE_INLINE_STARTER;
}

// buildGitRefKustomize points the overlay at a path within a git-backed
// ClusterRepo instead of inlining files. Both chartRepo and path are required.
export function buildGitRefKustomize(dir: string, chartRepo: string, path: string): ComponentKustomize | undefined {
  if (!chartRepo.trim() || !path.trim()) return undefined;
  return { dir: dir.trim() || 'overlay', gitRef: { chartRepo: chartRepo.trim(), path: path.trim() } };
}
