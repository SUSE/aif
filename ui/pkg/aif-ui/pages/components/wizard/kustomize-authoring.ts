import type { ComponentKustomize } from '../../../types/blueprint-types';

// buildInlineKustomize wraps the editor's kustomization.yaml body into the
// files-map shape the operator embeds. Empty content clears the overlay.
export function buildInlineKustomize(dir: string, kustomizationBody: string): ComponentKustomize | undefined {
  if (!kustomizationBody.trim()) return undefined;
  const d = dir.trim() || 'overlay';
  return { dir: d, files: { [`${d}/kustomization.yaml`]: kustomizationBody } };
}

// buildGitRefKustomize points the overlay at a path within a git-backed
// ClusterRepo instead of inlining files. Both chartRepo and path are required.
export function buildGitRefKustomize(dir: string, chartRepo: string, path: string): ComponentKustomize | undefined {
  if (!chartRepo.trim() || !path.trim()) return undefined;
  return { dir: dir.trim() || 'overlay', gitRef: { chartRepo: chartRepo.trim(), path: path.trim() } };
}
