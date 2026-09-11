import { configDefaults, defineConfig } from 'vitest/config';
import vue from '@vitejs/plugin-vue';
import { fileURLToPath } from 'node:url';

// Services run in Node. Component tests opt into jsdom per file; the Vue plugin
// compiles the real templates and their shared Rancher form/status components.
export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@shell': fileURLToPath(new URL('./node_modules/@rancher/shell', import.meta.url)),
      '@components': fileURLToPath(new URL('./node_modules/@rancher/shell/rancher-components', import.meta.url)),
    },
  },
  test: {
    environment: 'node',
    include:     ['pkg/aif-ui/**/__tests__/**/*.test.ts'],
    // build-pkg temporarily links Rancher's own sources (and Jest tests) here.
    exclude:     [...configDefaults.exclude, '**/.shell/**'],
  },
});
