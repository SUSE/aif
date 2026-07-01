/**
 * Validation rules for resource management and configuration
 */

import type { ValidationResult, ValidationError, FieldValidationRule } from '../utils/validation';
import { ERROR_CODES } from '../utils/constants';

export function validateResourceQuantity(value: string, resourceType: 'cpu' | 'memory' | 'storage' = 'cpu'): ValidationResult {
  const errors: ValidationError[] = [];

  if (!value || !value.trim()) {
    return { valid: true, errors: [], warnings: [] }; // Optional field
  }

  const trimmedValue = value.trim();

  // CPU validation
  if (resourceType === 'cpu') {
    // Valid formats: "100m", "0.1", "1", "2.5"
    const cpuRegex = /^(\d+m|\d*\.?\d+)$/;
    if (!cpuRegex.test(trimmedValue)) {
      errors.push({
        field: 'cpu',
        message: 'CPU must be specified in cores (e.g., "1", "0.5") or millicores (e.g., "100m")',
        code: ERROR_CODES.INVALID_FORMAT,
        severity: 'error'
      });
    } else {
      // Convert to numeric value for range validation
      const numericValue = trimmedValue.endsWith('m')
        ? parseInt(trimmedValue.slice(0, -1)) / 1000
        : parseFloat(trimmedValue);

      if (numericValue <= 0) {
        errors.push({
          field: 'cpu',
          message: 'CPU value must be greater than 0',
          code: ERROR_CODES.INVALID_VALUE,
          severity: 'error'
        });
      }

      if (numericValue > 128) {
        errors.push({
          field: 'cpu',
          message: 'CPU value seems unusually high (>128 cores). Please verify.',
          code: ERROR_CODES.INVALID_VALUE,
          severity: 'warning'
        });
      }
    }
  }

  // Memory validation
  if (resourceType === 'memory') {
    // Valid formats: "128Mi", "1Gi", "512M", "1G"
    const memoryRegex = /^(\d+)(Mi|Gi|Ti|M|G|T|Ki|K)?$/;
    const match = trimmedValue.match(memoryRegex);

    if (!match) {
      errors.push({
        field: 'memory',
        message: 'Memory must be specified with valid units (e.g., "512Mi", "1Gi", "2G")',
        code: ERROR_CODES.INVALID_FORMAT,
        severity: 'error'
      });
    } else {
      const [, amount, unit = ''] = match;
      const numericAmount = parseInt(amount);

      if (numericAmount <= 0) {
        errors.push({
          field: 'memory',
          message: 'Memory value must be greater than 0',
          code: ERROR_CODES.INVALID_VALUE,
          severity: 'error'
        });
      }

      // Convert to bytes for validation
      const multipliers: Record<string, number> = {
        'Ki': 1024,
        'Mi': 1024 * 1024,
        'Gi': 1024 * 1024 * 1024,
        'Ti': 1024 * 1024 * 1024 * 1024,
        'K': 1000,
        'M': 1000 * 1000,
        'G': 1000 * 1000 * 1000,
        'T': 1000 * 1000 * 1000 * 1000,
        '': 1
      };

      const bytes = numericAmount * (multipliers[unit] || 1);

      // Warn about very small memory values (< 64Mi)
      if (bytes < 64 * 1024 * 1024) {
        errors.push({
          field: 'memory',
          message: 'Memory value is very small (<64Mi). This may cause application issues.',
          code: ERROR_CODES.INVALID_VALUE,
          severity: 'warning'
        });
      }

      // Warn about very large memory values (> 512Gi)
      if (bytes > 512 * 1024 * 1024 * 1024) {
        errors.push({
          field: 'memory',
          message: 'Memory value is very large (>512Gi). Please verify.',
          code: ERROR_CODES.INVALID_VALUE,
          severity: 'warning'
        });
      }
    }
  }

  // Storage validation
  if (resourceType === 'storage') {
    // Same format as memory
    const storageRegex = /^(\d+)(Mi|Gi|Ti|M|G|T|Ki|K)?$/;
    const match = trimmedValue.match(storageRegex);

    if (!match) {
      errors.push({
        field: 'storage',
        message: 'Storage must be specified with valid units (e.g., "10Gi", "1Ti")',
        code: ERROR_CODES.INVALID_FORMAT,
        severity: 'error'
      });
    } else {
      const [, amount] = match;
      const numericAmount = parseInt(amount);

      if (numericAmount <= 0) {
        errors.push({
          field: 'storage',
          message: 'Storage value must be greater than 0',
          code: ERROR_CODES.INVALID_VALUE,
          severity: 'error'
        });
      }
    }
  }

  return {
    valid: errors.filter(e => e.severity === 'error').length === 0,
    errors,
    warnings: []
  };
}

export function validateNodeSelector(nodeSelector: Record<string, string>): ValidationResult {
  const errors: ValidationError[] = [];

  if (!nodeSelector || Object.keys(nodeSelector).length === 0) {
    return { valid: true, errors: [], warnings: [] }; // Optional field
  }

  // Validate each key-value pair
  Object.entries(nodeSelector).forEach(([key, value]) => {
    // Validate key format (Kubernetes label key)
    const keyRegex = /^([a-z0-9A-Z\-_.]+\/)?[a-z0-9A-Z\-_.]+$/;
    if (!keyRegex.test(key)) {
      errors.push({
        field: 'nodeSelector',
        message: `Invalid node selector key: "${key}". Must be a valid Kubernetes label key.`,
        code: ERROR_CODES.INVALID_FORMAT,
        severity: 'error'
      });
    }

    // Validate value format (Kubernetes label value)
    const valueRegex = /^[a-z0-9A-Z\-_.]*$/;
    if (!valueRegex.test(value)) {
      errors.push({
        field: 'nodeSelector',
        message: `Invalid node selector value: "${value}". Must be a valid Kubernetes label value.`,
        code: ERROR_CODES.INVALID_FORMAT,
        severity: 'error'
      });
    }

    // Check for common keys and provide warnings
    const commonKeys = ['kubernetes.io/arch', 'kubernetes.io/os', 'node-role.kubernetes.io/master'];
    if (commonKeys.includes(key) && key === 'node-role.kubernetes.io/master') {
      errors.push({
        field: 'nodeSelector',
        message: 'Scheduling on master nodes is not recommended for application workloads.',
        code: ERROR_CODES.INVALID_VALUE,
        severity: 'warning'
      });
    }
  });

  return {
    valid: errors.filter(e => e.severity === 'error').length === 0,
    errors,
    warnings: []
  };
}

export function validateTolerations(tolerations: Array<Record<string, any>>): ValidationResult {
  const errors: ValidationError[] = [];

  if (!tolerations || tolerations.length === 0) {
    return { valid: true, errors: [], warnings: [] }; // Optional field
  }

  tolerations.forEach((toleration, index) => {
    // Required fields for toleration
    const requiredFields = ['key'];
    requiredFields.forEach(field => {
      if (!(field in toleration) || !toleration[field]) {
        errors.push({
          field: 'tolerations',
          message: `Toleration ${index + 1}: "${field}" is required`,
          code: ERROR_CODES.REQUIRED_FIELD,
          severity: 'error'
        });
      }
    });

    // Validate effect if present
    if (toleration.effect) {
      const validEffects = ['NoSchedule', 'PreferNoSchedule', 'NoExecute'];
      if (!validEffects.includes(toleration.effect)) {
        errors.push({
          field: 'tolerations',
          message: `Toleration ${index + 1}: Invalid effect "${toleration.effect}". Must be one of: ${validEffects.join(', ')}`,
          code: ERROR_CODES.INVALID_VALUE,
          severity: 'error'
        });
      }
    }

    // Validate operator if present
    if (toleration.operator) {
      const validOperators = ['Equal', 'Exists'];
      if (!validOperators.includes(toleration.operator)) {
        errors.push({
          field: 'tolerations',
          message: `Toleration ${index + 1}: Invalid operator "${toleration.operator}". Must be one of: ${validOperators.join(', ')}`,
          code: ERROR_CODES.INVALID_VALUE,
          severity: 'error'
        });
      }
    }
  });

  return {
    valid: errors.filter(e => e.severity === 'error').length === 0,
    errors,
    warnings: []
  };
}

// Export validation rules for use in forms
export const cpuRule: FieldValidationRule = {
  name: 'cpu',
  validate: (value: any) => validateResourceQuantity(value, 'cpu')
};

export const memoryRule: FieldValidationRule = {
  name: 'memory',
  validate: (value: any) => validateResourceQuantity(value, 'memory')
};

export const storageRule: FieldValidationRule = {
  name: 'storage',
  validate: (value: any) => validateResourceQuantity(value, 'storage')
};

export const nodeSelectorRule: FieldValidationRule = {
  name: 'nodeSelector',
  validate: validateNodeSelector
};

export const tolerationsRule: FieldValidationRule = {
  name: 'tolerations',
  validate: validateTolerations
};