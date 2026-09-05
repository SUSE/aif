import { configDefaults, defineConfig } from 'vitest/config';
import vue from '@vitejs/plugin-vue';

// Services run in Node. Component tests opt into jsdom per file; the Vue plugin
// compiles the real templates without pulling in Rancher's webpack aliases.
export default defineConfig({
  plugins: [vue()],
  test: {
    environment: 'node',
    include:     ['pkg/aif-ui/**/__tests__/**/*.test.ts'],
    // build-pkg temporarily links Rancher's own sources (and Jest tests) here.
    exclude:     [...configDefaults.exclude, '**/.shell/**'],
  },
});
