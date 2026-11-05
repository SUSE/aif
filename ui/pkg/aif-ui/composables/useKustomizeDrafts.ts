import { inject, provide } from 'vue';
import type { InjectionKey } from 'vue';
import type { InlineFile } from '../pages/components/wizard/kustomize-authoring';
import type { ComponentKustomize } from '../types/blueprint-types';

// KustomizeDraft is a KustomizeOverlayEditor's full authoring state. The value
// it emits is lossy on purpose (a draft with no real content emits no overlay),
// so the state is kept here to survive the editor remounting.
export interface KustomizeDraft {
  emitted:     ComponentKustomize | undefined;
  source:      'none' | 'inline' | 'git';
  dir:         string;
  files:       InlineFile[];
  passthrough: Record<string, string>;
  chartRepo:   string;
  path:        string;
  activeName:  string;
}

export const KUSTOMIZE_DRAFTS: InjectionKey<Map<string, KustomizeDraft>> = Symbol('kustomizeDrafts');

/**
 * Gives the editors below a draft store that lives as long as the calling
 * component. Wizards call it so overlay edits survive moving between steps,
 * which unmount the step holding the editor.
 */
export function provideKustomizeDrafts(): void {
  provide(KUSTOMIZE_DRAFTS, new Map());
}

// The draft store, or null outside a wizard (drafts then end with the editor).
export function useKustomizeDrafts(): Map<string, KustomizeDraft> | null {
  return inject(KUSTOMIZE_DRAFTS, null);
}
