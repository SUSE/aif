/**
 * Mock API responses for UI development before backend integration is ready.
 */

export const mockAPI = {
  bundles: {
    // submit, withdraw, approve, requestChanges, testDeploy, pendingReview
  },

  blueprints: {
    // versions, deploy, deprecate, withdraw, reactivate
  },

  workloads: {
    // start, stop, restart, upgrade
  },

  apps: {
    // list, categories
  },

  settings: {
    // get, update, testConnection
  }
};

// VueCLI's DefinePlugin inlines process.env.USE_MOCK_API at build time — this is not a runtime toggle.
// Set USE_MOCK_API=true in the build environment to enable; the value is frozen into the bundle.
export const USE_MOCK_API = process.env.USE_MOCK_API === 'true';
