import { vi } from 'vitest';

// Mock global functions that Vue components might use
global.console = {
  ...console,
  error: vi.fn(),
  warn: vi.fn(),
};

// Mock localStorage
const localStorageMock = {
  getItem: vi.fn(),
  setItem: vi.fn(),
  removeItem: vi.fn(),
  clear: vi.fn(),
};

global.localStorage = localStorageMock;
