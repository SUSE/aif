/**
 * Validation rules for AI/ML application installation
 */

import type { ValidationResult, ValidationError, FieldValidationRule } from '../utils/validation';
import { ERROR_CODES } from '../utils/constants';

export function validateReleaseName(name: string): ValidationResult {
  const errors: ValidationError[] = [];

  // Required check
  if (!name || !name.trim()) {
    errors.push({
      field: 'releaseName',
      message: 'Release name is required',
      code: ERROR_CODES.REQUIRED_FIELD,
      severity: 'error'
    });
    return { valid: false, errors, warnings: [] };
  }

  const trimmedName = name.trim();

  // Length validation (Helm release names should be <= 53 characters)
  if (trimmedName.length > 53) {
    errors.push({
      field: 'releaseName',
      message: 'Release name must be 53 characters or less',
      code: ERROR_CODES.INVALID_FORMAT,
      severity: 'error'
    });
  }

  // DNS-1123 subdomain validation (Helm requirement)
  const dnsRegex = /^[a-z0-9]([-a-z0-9]*[a-z0-9])?$/;
  if (!dnsRegex.test(trimmedName)) {
    errors.push({
      field: 'releaseName',
      message: 'Release name must be a valid DNS-1123 subdomain (lowercase letters, numbers, and hyphens only)',
      code: ERROR_CODES.INVALID_FORMAT,
      severity: 'error'
    });
  }

  // Cannot start or end with hyphen
  if (trimmedName.startsWith('-') || trimmedName.endsWith('-')) {
    errors.push({
      field: 'releaseName',
      message: 'Release name cannot start or end with a hyphen',
      code: ERROR_CODES.INVALID_FORMAT,
      severity: 'error'
    });
  }

  return {
    valid: errors.length === 0,
    errors,
    warnings: []
  };
}

export function validateNamespace(namespace: string): ValidationResult {
  const errors: ValidationError[] = [];

  // Required check
  if (!namespace || !namespace.trim()) {
    errors.push({
      field: 'namespace',
      message: 'Namespace is required',
      code: ERROR_CODES.REQUIRED_FIELD,
      severity: 'error'
    });
    return { valid: false, errors, warnings: [] };
  }

  const trimmedNamespace = namespace.trim();

  // Length validation (Kubernetes namespace names should be <= 63 characters)
  if (trimmedNamespace.length > 63) {
    errors.push({
      field: 'namespace',
      message: 'Namespace name must be 63 characters or less',
      code: ERROR_CODES.INVALID_FORMAT,
      severity: 'error'
    });
  }

  // DNS-1123 label validation
  const dnsRegex = /^[a-z0-9]([-a-z0-9]*[a-z0-9])?$/;
  if (!dnsRegex.test(trimmedNamespace)) {
    errors.push({
      field: 'namespace',
      message: 'Namespace must be a valid DNS-1123 label (lowercase letters, numbers, and hyphens only)',
      code: ERROR_CODES.INVALID_FORMAT,
      severity: 'error'
    });
  }

  // Reserved namespace check
  const reservedNamespaces = ['kube-system', 'kube-public', 'kube-node-lease', 'default'];
  if (reservedNamespaces.includes(trimmedNamespace)) {
    errors.push({
      field: 'namespace',
      message: `Cannot use reserved namespace: ${trimmedNamespace}`,
      code: ERROR_CODES.INVALID_VALUE,
      severity: 'error'
    });
  }

  return {
    valid: errors.length === 0,
    errors,
    warnings: []
  };
}

export function validateHelmValues(values: string): ValidationResult {
  const errors: ValidationError[] = [];
  const warnings: ValidationWarning[] = [];

  if (!values || !values.trim()) {
    // Empty values are valid (will use chart defaults)
    return { valid: true, errors: [], warnings: [] };
  }

  try {
    // Try to parse as YAML
    const yaml = require('js-yaml');
    const parsed = yaml.load(values);

    // Check if it's a valid object
    if (parsed !== null && typeof parsed !== 'object') {
      errors.push({
        field: 'helmValues',
        message: 'Helm values must be a valid YAML object',
        code: ERROR_CODES.INVALID_FORMAT,
        severity: 'error'
      });
    }

    // Warn about potentially dangerous values
    if (typeof parsed === 'object' && parsed !== null) {
      const dangerousKeys = ['image.pullPolicy', 'securityContext', 'nodeSelector'];
      dangerousKeys.forEach(key => {
        if (hasNestedKey(parsed, key)) {
          warnings.push({
            field: 'helmValues',
            message: `Be careful when overriding ${key} - this may affect security or scheduling`,
            suggestion: 'Review the documentation for this setting'
          });
        }
      });
    }

  } catch (error: any) {
    errors.push({
      field: 'helmValues',
      message: `Invalid YAML: ${error.message}`,
      code: ERROR_CODES.INVALID_FORMAT,
      severity: 'error'
    });
  }

  return {
    valid: errors.length === 0,
    errors,
    warnings
  };
}

export function validateClusterSelection(clusterIds: string[]): ValidationResult {
  const errors: ValidationError[] = [];

  if (!clusterIds || clusterIds.length === 0) {
    errors.push({
      field: 'clusters',
      message: 'At least one cluster must be selected',
      code: ERROR_CODES.REQUIRED_FIELD,
      severity: 'error'
    });
  }

  // Check for duplicates
  const uniqueClusters = new Set(clusterIds);
  if (uniqueClusters.size !== clusterIds.length) {
    errors.push({
      field: 'clusters',
      message: 'Duplicate clusters selected',
      code: ERROR_CODES.INVALID_VALUE,
      severity: 'error'
    });
  }

  return {
    valid: errors.length === 0,
    errors,
    warnings: []
  };
}

// Utility function to check nested object keys
function hasNestedKey(obj: any, key: string): boolean {
  const keys = key.split('.');
  let current = obj;

  for (const k of keys) {
    if (current && typeof current === 'object' && k in current) {
      current = current[k];
    } else {
      return false;
    }
  }

  return true;
}

// Export validation rules for use in forms
export const releaseNameRule: FieldValidationRule = {
  name: 'releaseName',
  validate: validateReleaseName
};

export const namespaceRule: FieldValidationRule = {
  name: 'namespace',
  validate: validateNamespace
};

export const helmValuesRule: FieldValidationRule = {
  name: 'helmValues',
  validate: validateHelmValues
};

export const clusterSelectionRule: FieldValidationRule = {
  name: 'clusterSelection',
  validate: (value: any) => validateClusterSelection(Array.isArray(value) ? value : [])
};

// Type import for warnings
interface ValidationWarning {
  field: string;
  message: string;
  suggestion?: string;
}