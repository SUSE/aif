import { defineConfig } from 'vitest/config';
import vue from '@vitejs/plugin-vue';
import path from 'path';

export default defineConfig({
  plugins: [vue()],
  test: {
    globals: true,
    environment: 'happy-dom',
    setupFiles: ['./tests/unit/setup.js'],
    include: ['tests/unit/**/*.spec.js'],
    coverage: {
      provider: 'v8',
      reporter: ['text', 'json', 'html'],
      exclude: [
        'node_modules/',
        'tests/',
        '**/*.spec.js'
      ]
    }
  },
  resolve: {
    alias: {
      '@pkg/ai-factory': path.resolve(__dirname, './pkg/ai-factory'),
      '@': path.resolve(__dirname, './pkg/ai-factory'),
      '@shell': path.resolve(__dirname, './node_modules/@rancher/shell'),
      '@components': path.resolve(__dirname, './node_modules/@rancher/shell/components')
    },
    extensions: ['.js', '.ts', '.vue', '.json']
  }
});
