/**
 * Internationalization configuration for SUSE AI Extension
 * Following Rancher UI patterns for consistent i18n implementation
 */

export interface TranslationKey {
  [key: string]: string | TranslationKey;
}

export interface Translations {
  [locale: string]: TranslationKey;
}

// Default locale
export const DEFAULT_LOCALE = 'en-us';

// Available locales
export const AVAILABLE_LOCALES = [
  'en-us'
  // Future locales can be added here
  // 'de-de',
  // 'ja-jp',
  // 'zh-cn'
] as const;

// Translation structure for SUSE AI
export const translations: Translations = {
  'en-us': {
    suseai: {
      // General
      product: {
        name: 'SUSE AI',
        title: 'AI/ML Applications',
        description: 'Deploy and manage AI/ML applications in your clusters'
      },

      // Navigation
      nav: {
        apps: 'Applications',
        install: 'Install',
        manage: 'Manage',
        repositories: 'Repositories',
        settings: 'Settings'
      },

      // Applications
      apps: {
        title: 'AI/ML Applications',
        subtitle: 'Browse and install AI/ML applications',
        search: 'Search applications...',
        install: 'Install Application',
        manage: 'Manage Application',
        noApps: 'No applications found',
        loading: 'Loading applications and discovering installations...',

        status: {
          available: 'Available',
          installing: 'Installing...',
          deployed: 'Running',
          upgrading: 'Upgrading...',
          failed: 'Failed',
          unknown: 'Unknown'
        },

        categories: {
          all: 'All Categories',
          ai: 'AI/Machine Learning',
          data: 'Data Processing',
          inference: 'Inference',
          training: 'Training',
          workflow: 'Workflow'
        }
      },

      // Installation
      install: {
        title: 'Install Application',
        wizard: {
          basic: 'Basic Information',
          cluster: 'Target Cluster',
          configuration: 'Configuration',
          review: 'Review & Install'
        },

        form: {
          releaseName: 'Release Name',
          releaseNamePlaceholder: 'Enter a unique release name',
          releaseNameHelp: 'Must be a valid DNS-1123 subdomain',

          namespace: 'Namespace',
          namespacePlaceholder: 'default',
          namespaceHelp: 'Kubernetes namespace for deployment',

          cluster: 'Target Cluster',
          clusterPlaceholder: 'Select cluster',
          clusterHelp: 'Cluster where the application will be installed',

          values: 'Helm Values',
          valuesPlaceholder: 'Custom YAML configuration...',
          valuesHelp: 'Override default chart values (optional)'
        },

        actions: {
          install: 'Install',
          installing: 'Installing...',
          cancel: 'Cancel',
          back: 'Back',
          next: 'Next',
          review: 'Review'
        },

        success: 'Application installed successfully',
        error: 'Failed to install application'
      },

      // Management
      manage: {
        title: 'Manage Applications',
        instances: 'Application Instances',
        noInstances: 'No application instances found',

        actions: {
          start: 'Start',
          stop: 'Stop',
          restart: 'Restart',
          upgrade: 'Upgrade',
          uninstall: 'Uninstall',
          configure: 'Configure'
        },

        status: {
          healthy: 'Healthy',
          degraded: 'Degraded',
          unhealthy: 'Unhealthy',
          unknown: 'Unknown'
        }
      },

      // Repositories
      repositories: {
        title: 'Repositories',
        add: 'Add Repository',
        refresh: 'Refresh',

        form: {
          name: 'Repository Name',
          url: 'Repository URL',
          username: 'Username',
          password: 'Password'
        }
      },

      // Common UI elements
      common: {
        actions: {
          save: 'Save',
          cancel: 'Cancel',
          edit: 'Edit',
          delete: 'Delete',
          refresh: 'Refresh',
          close: 'Close',
          confirm: 'Confirm',
          view: 'View'
        },

        status: {
          loading: 'Loading...',
          error: 'Error',
          success: 'Success',
          pending: 'Pending',
          completed: 'Completed'
        },

        labels: {
          name: 'Name',
          description: 'Description',
          version: 'Version',
          created: 'Created',
          updated: 'Updated',
          status: 'Status',
          cluster: 'Cluster',
          namespace: 'Namespace'
        },

        messages: {
          noData: 'No data available',
          loadingFailed: 'Failed to load data',
          saveSuccess: 'Saved successfully',
          deleteSuccess: 'Deleted successfully',
          confirmDelete: 'Are you sure you want to delete this item?'
        }
      },

      // Validation messages
      validation: {
        required: '{field} is required',
        invalidFormat: '{field} has invalid format',
        invalidValue: '{field} has invalid value',
        tooShort: '{field} is too short (minimum {min} characters)',
        tooLong: '{field} is too long (maximum {max} characters)',

        releaseName: {
          required: 'Release name is required',
          invalid: 'Release name must be a valid DNS-1123 subdomain',
          tooLong: 'Release name must be 53 characters or less'
        },

        namespace: {
          required: 'Namespace is required',
          invalid: 'Namespace must be a valid DNS-1123 label',
          reserved: 'Cannot use reserved namespace'
        },

        helmValues: {
          invalidYaml: 'Invalid YAML format',
          warning: 'Be careful when overriding {key}'
        }
      }
    }
  }
};

/**
 * Get translated string by key
 * Supports nested keys with dot notation (e.g., 'suseai.apps.title')
 */
export function t(key: string, params?: Record<string, any>, locale: string = DEFAULT_LOCALE): string {
  const translation = getNestedValue(translations[locale] || translations[DEFAULT_LOCALE], key);

  if (typeof translation !== 'string') {
    console.warn(`Translation key not found: ${key}`);
    return key;
  }

  // Simple parameter substitution
  if (params) {
    return Object.keys(params).reduce((text, param) => {
      return text.replace(new RegExp(`\\{${param}\\}`, 'g'), String(params[param]));
    }, translation);
  }

  return translation;
}

/**
 * Get nested value from object using dot notation
 */
function getNestedValue(obj: any, path: string): any {
  return path.split('.').reduce((current, key) => {
    return current && current[key] !== undefined ? current[key] : undefined;
  }, obj);
}

/**
 * Check if translation key exists
 */
export function hasTranslation(key: string, locale: string = DEFAULT_LOCALE): boolean {
  const translation = getNestedValue(translations[locale] || translations[DEFAULT_LOCALE], key);
  return typeof translation === 'string';
}

/**
 * Get all available locales
 */
export function getAvailableLocales(): readonly string[] {
  return AVAILABLE_LOCALES;
}

/**
 * Validate locale
 */
export function isValidLocale(locale: string): boolean {
  return AVAILABLE_LOCALES.includes(locale as any);
}