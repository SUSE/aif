/**
 * Application Settings Configuration
 * Following standard patterns for settings management
 * Defines application settings and their defaults
 */

import { DEFAULT_VALUES, TIMEOUT_VALUES, STORAGE_KEYS } from '../utils/constants';

// === Setting Types ===
export type SettingType = 'boolean' | 'number' | 'string' | 'select' | 'multiselect';

export interface SettingOption {
  value: string | number | boolean;
  label: string;
  description?: string;
}

export interface SettingDefinition {
  key: string;
  name: string;
  description: string;
  type: SettingType;
  defaultValue: any;
  options?: SettingOption[];
  min?: number;
  max?: number;
  validation?: (value: any) => string | null;
  category: SettingCategory;
  required?: boolean;
  sensitive?: boolean;
  restartRequired?: boolean;
}

// === Setting Categories ===
export const SETTING_CATEGORIES = {
  GENERAL: 'general',
  DISCOVERY: 'discovery',
  INSTALLATION: 'installation',
  NOTIFICATIONS: 'notifications',
  ADVANCED: 'advanced'
} as const;

export type SettingCategory = typeof SETTING_CATEGORIES[keyof typeof SETTING_CATEGORIES];

// === Settings Definitions ===
export const SETTINGS_DEFINITIONS: Record<string, SettingDefinition> = {
  autoRefresh: {
    key: 'autoRefresh',
    name: 'Auto Refresh',
    description: 'Automatically refresh application data',
    type: 'boolean',
    defaultValue: true,
    category: SETTING_CATEGORIES.GENERAL
  },

  refreshInterval: {
    key: 'refreshInterval',
    name: 'Refresh Interval',
    description: 'How often to refresh data (seconds)',
    type: 'number',
    defaultValue: 300,
    min: 30,
    max: 3600,
    category: SETTING_CATEGORIES.GENERAL,
    validation: (value: number) => {
      if (value < 30) return 'Minimum refresh interval is 30 seconds';
      if (value > 3600) return 'Maximum refresh interval is 1 hour';
      return null;
    }
  },

  defaultNamespace: {
    key: 'defaultNamespace',
    name: 'Default Namespace',
    description: 'Default namespace for new installations',
    type: 'string',
    defaultValue: DEFAULT_VALUES.NAMESPACE,
    category: SETTING_CATEGORIES.INSTALLATION,
    validation: (value: string) => {
      if (!value.trim()) return 'Default namespace cannot be empty';
      if (!/^[a-z0-9]([a-z0-9-]*[a-z0-9])?$/.test(value)) {
        return 'Invalid namespace name format';
      }
      return null;
    }
  },

  showSystemApps: {
    key: 'showSystemApps',
    name: 'Show System Apps',
    description: 'Display system and infrastructure applications',
    type: 'boolean',
    defaultValue: false,
    category: SETTING_CATEGORIES.GENERAL
  },

  preferredView: {
    key: 'preferredView',
    name: 'Preferred View',
    description: 'Default view mode for application list',
    type: 'select',
    defaultValue: 'table',
    options: [
      { value: 'table', label: 'Table View', description: 'Detailed table with columns' },
      { value: 'grid', label: 'Grid View', description: 'Card-based grid layout' },
      { value: 'list', label: 'List View', description: 'Compact list format' }
    ],
    category: SETTING_CATEGORIES.GENERAL
  },

  discoveryTimeout: {
    key: 'discoveryTimeout',
    name: 'Discovery Timeout',
    description: 'Timeout for application discovery (milliseconds)',
    type: 'number',
    defaultValue: TIMEOUT_VALUES.EXTENDED,
    min: TIMEOUT_VALUES.SHORT,
    max: TIMEOUT_VALUES.EXTENDED * 2,
    category: SETTING_CATEGORIES.DISCOVERY
  },

  maxConcurrentOperations: {
    key: 'maxConcurrentOperations',
    name: 'Max Concurrent Operations',
    description: 'Maximum number of simultaneous operations',
    type: 'number',
    defaultValue: DEFAULT_VALUES.MAX_CONCURRENT_OPERATIONS,
    min: 1,
    max: 10,
    category: SETTING_CATEGORIES.INSTALLATION
  },

  notificationsEnabled: {
    key: 'notificationsEnabled',
    name: 'Enable Notifications',
    description: 'Show notification alerts for operations',
    type: 'boolean',
    defaultValue: true,
    category: SETTING_CATEGORIES.NOTIFICATIONS
  },

  notificationDuration: {
    key: 'notificationDuration',
    name: 'Notification Duration',
    description: 'How long to show notifications (seconds)',
    type: 'select',
    defaultValue: 5,
    options: [
      { value: 3, label: '3 seconds', description: 'Short duration' },
      { value: 5, label: '5 seconds', description: 'Default duration' },
      { value: 8, label: '8 seconds', description: 'Long duration' },
      { value: 0, label: 'Permanent', description: 'Until manually closed' }
    ],
    category: SETTING_CATEGORIES.NOTIFICATIONS
  },

  showInstallSuccess: {
    key: 'showInstallSuccess',
    name: 'Show Install Success',
    description: 'Show notifications for successful installations',
    type: 'boolean',
    defaultValue: true,
    category: SETTING_CATEGORIES.NOTIFICATIONS
  },

  showUpgradeSuccess: {
    key: 'showUpgradeSuccess',
    name: 'Show Upgrade Success',
    description: 'Show notifications for successful upgrades',
    type: 'boolean',
    defaultValue: true,
    category: SETTING_CATEGORIES.NOTIFICATIONS
  },

  showErrors: {
    key: 'showErrors',
    name: 'Show Errors',
    description: 'Show notifications for errors',
    type: 'boolean',
    defaultValue: true,
    category: SETTING_CATEGORIES.NOTIFICATIONS
  },

  showWarnings: {
    key: 'showWarnings',
    name: 'Show Warnings',
    description: 'Show notifications for warnings',
    type: 'boolean',
    defaultValue: true,
    category: SETTING_CATEGORIES.NOTIFICATIONS
  },

  autoHideAfter: {
    key: 'autoHideAfter',
    name: 'Auto Hide After',
    description: 'Automatically hide success notifications (seconds)',
    type: 'number',
    defaultValue: 5,
    min: 0,
    max: 30,
    category: SETTING_CATEGORIES.NOTIFICATIONS
  },

  debugMode: {
    key: 'debugMode',
    name: 'Debug Mode',
    description: 'Enable debug logging and additional information',
    type: 'boolean',
    defaultValue: false,
    category: SETTING_CATEGORIES.ADVANCED,
    restartRequired: true
  },

  enableExperimentalFeatures: {
    key: 'enableExperimentalFeatures',
    name: 'Experimental Features',
    description: 'Enable experimental and preview features',
    type: 'boolean',
    defaultValue: false,
    category: SETTING_CATEGORIES.ADVANCED
  }
};

// === Category Configurations ===
export const CATEGORY_CONFIGS = {
  [SETTING_CATEGORIES.GENERAL]: {
    name: 'General Settings',
    description: 'Basic application preferences',
    icon: 'gear',
    priority: 1
  },
  [SETTING_CATEGORIES.DISCOVERY]: {
    name: 'Discovery Settings',
    description: 'Application and cluster discovery options',
    icon: 'search',
    priority: 2
  },
  [SETTING_CATEGORIES.INSTALLATION]: {
    name: 'Installation Settings',
    description: 'Default settings for application installations',
    icon: 'plus',
    priority: 3
  },
  [SETTING_CATEGORIES.NOTIFICATIONS]: {
    name: 'Notification Settings',
    description: 'Configure alerts and notifications',
    icon: 'bell',
    priority: 4
  },
  [SETTING_CATEGORIES.ADVANCED]: {
    name: 'Advanced Settings',
    description: 'Advanced configuration options',
    icon: 'cog',
    priority: 5
  }
};

// === Default Settings Values ===
export const DEFAULT_SETTINGS: Record<string, any> = {};

// Initialize default values from definitions
Object.entries(SETTINGS_DEFINITIONS).forEach(([key, definition]) => {
  DEFAULT_SETTINGS[key] = definition.defaultValue;
});

// === Helper Functions ===

/**
 * Get settings by category
 */
export function getSettingsByCategory(category: SettingCategory): SettingDefinition[] {
  return Object.values(SETTINGS_DEFINITIONS).filter(setting => setting.category === category);
}

/**
 * Get setting definition
 */
export function getSettingDefinition(key: string): SettingDefinition | null {
  return SETTINGS_DEFINITIONS[key] || null;
}

/**
 * Validate setting value
 */
export function validateSettingValue(key: string, value: any): string | null {
  const definition = SETTINGS_DEFINITIONS[key];
  if (!definition) {
    return 'Unknown setting';
  }

  // Required check
  if (definition.required && (value === null || value === undefined || value === '')) {
    return 'This setting is required';
  }

  // Type validation
  if (value !== null && value !== undefined) {
    const expectedType = definition.type === 'select' || definition.type === 'multiselect' 
      ? typeof definition.options?.[0]?.value 
      : definition.type;
    
    if (expectedType && typeof value !== expectedType) {
      return `Expected ${expectedType}, got ${typeof value}`;
    }
  }

  // Range validation for numbers
  if (definition.type === 'number' && typeof value === 'number') {
    if (definition.min !== undefined && value < definition.min) {
      return `Value must be at least ${definition.min}`;
    }
    if (definition.max !== undefined && value > definition.max) {
      return `Value must be at most ${definition.max}`;
    }
  }

  // Custom validation
  if (definition.validation) {
    return definition.validation(value);
  }

  return null;
}

/**
 * Get storage key for settings
 */
export function getSettingsStorageKey(): string {
  return STORAGE_KEYS.SETTINGS;
}

export default SETTINGS_DEFINITIONS;