import { vi } from 'vitest';
import type { App } from 'vue';

// Mount options shared by the Blueprint wizard step tests: Rancher's global t()
// (a globalProperty, so a component's own useT() t still wins), the shell
// directives and a store whose dispatches resolve empty.
export function wizardMountGlobal() {
  const store = { dispatch: vi.fn(async() => ({})), getters: {} };

  return {
    mocks:   { $store: store },
    plugins: [{
      install: (app: App) => {
        // The shell also types an options-object overload, which these components never use.
        app.config.globalProperties.t = ((key: string) => key) as unknown as typeof app.config.globalProperties.t;
      },
    }],
    directives: { 'clean-html': {}, 'clean-tooltip': {}, 'stripped-aria-label': {} },
    stubs:      { t: true },
  };
}
