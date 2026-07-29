import { defineConfig } from 'vitest/config';

// The extension has no other test tooling. Services under pkg/aif-ui/services
// are plain TypeScript with the Vue store injected as a parameter, so they need
// no jsdom environment and no @shell webpack aliases.
export default defineConfig({
  test: {
    environment: 'node',
    include:     ['pkg/aif-ui/**/__tests__/**/*.test.ts'],
  },
});
