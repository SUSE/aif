/**
 * Common validation rules used across the application
 */

import type { ValidationResult, ValidationError, FieldValidationRule } from '../utils/validation';
import { ERROR_CODES } from '../utils/constants';

export function validateRequired(value: any, fieldName = 'field'): ValidationResult {
  const errors: ValidationError[] = [];

  if (value === null || value === undefined || value === '' ||
      (Array.isArray(value) && value.length === 0) ||
      (typeof value === 'string' && !value.trim())) {
    errors.push({
      field: fieldName,
      message: `${fieldName.charAt(0).toUpperCase() + fieldName.slice(1)} is required`,
      code: ERROR_CODES.REQUIRED_FIELD,
      severity: 'error'
    });
  }

  return {
    valid: errors.length === 0,
    errors,
    warnings: []
  };
}

export function validateLength(value: string, min?: number, max?: number, fieldName = 'field'): ValidationResult {
  const errors: ValidationError[] = [];

  if (!value) {
    return { valid: true, errors: [], warnings: [] }; // Let required validator handle empty values
  }

  const length = value.length;

  if (min !== undefined && length < min) {
    errors.push({
      field: fieldName,
      message: `${fieldName} must be at least ${min} characters long`,
      code: ERROR_CODES.INVALID_LENGTH,
      severity: 'error'
    });
  }

  if (max !== undefined && length > max) {
    errors.push({
      field: fieldName,
      message: `${fieldName} must be no more than ${max} characters long`,
      code: ERROR_CODES.INVALID_LENGTH,
      severity: 'error'
    });
  }

  return {
    valid: errors.length === 0,
    errors,
    warnings: []
  };
}

export function validatePattern(value: string, pattern: RegExp, message: string, fieldName = 'field'): ValidationResult {
  const errors: ValidationError[] = [];

  if (!value) {
    return { valid: true, errors: [], warnings: [] }; // Let required validator handle empty values
  }

  if (!pattern.test(value)) {
    errors.push({
      field: fieldName,
      message,
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

export function validateEmail(email: string): ValidationResult {
  const emailRegex = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;
  return validatePattern(
    email,
    emailRegex,
    'Please enter a valid email address',
    'email'
  );
}

export function validateUrl(url: string): ValidationResult {
  const errors: ValidationError[] = [];

  if (!url) {
    return { valid: true, errors: [], warnings: [] };
  }

  try {
    new URL(url);

    // Additional validation for allowed protocols
    const allowedProtocols = ['http:', 'https:'];
    const urlObj = new URL(url);

    if (!allowedProtocols.includes(urlObj.protocol)) {
      errors.push({
        field: 'url',
        message: 'URL must use HTTP or HTTPS protocol',
        code: ERROR_CODES.INVALID_FORMAT,
        severity: 'error'
      });
    }
  } catch {
    errors.push({
      field: 'url',
      message: 'Please enter a valid URL',
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

export function validateJson(jsonString: string): ValidationResult {
  const errors: ValidationError[] = [];

  if (!jsonString || !jsonString.trim()) {
    return { valid: true, errors: [], warnings: [] };
  }

  try {
    JSON.parse(jsonString);
  } catch (error: any) {
    errors.push({
      field: 'json',
      message: `Invalid JSON: ${error.message}`,
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

export function validateYaml(yamlString: string): ValidationResult {
  const errors: ValidationError[] = [];

  if (!yamlString || !yamlString.trim()) {
    return { valid: true, errors: [], warnings: [] };
  }

  try {
    const yaml = require('js-yaml');
    yaml.load(yamlString);
  } catch (error: any) {
    errors.push({
      field: 'yaml',
      message: `Invalid YAML: ${error.message}`,
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

export function validateRange(value: number, min?: number, max?: number, fieldName = 'value'): ValidationResult {
  const errors: ValidationError[] = [];

  if (value === null || value === undefined) {
    return { valid: true, errors: [], warnings: [] };
  }

  if (min !== undefined && value < min) {
    errors.push({
      field: fieldName,
      message: `${fieldName} must be at least ${min}`,
      code: ERROR_CODES.INVALID_VALUE,
      severity: 'error'
    });
  }

  if (max !== undefined && value > max) {
    errors.push({
      field: fieldName,
      message: `${fieldName} must be no more than ${max}`,
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

// Export common validation rules
export const requiredRule: FieldValidationRule = {
  name: 'required',
  validate: (value: any) => validateRequired(value)
};

export const emailRule: FieldValidationRule = {
  name: 'email',
  validate: validateEmail
};

export const urlRule: FieldValidationRule = {
  name: 'url',
  validate: validateUrl
};

export const jsonRule: FieldValidationRule = {
  name: 'json',
  validate: validateJson
};

export const yamlRule: FieldValidationRule = {
  name: 'yaml',
  validate: validateYaml
};

// Factory functions for parameterized rules
export function lengthRule(min?: number, max?: number): FieldValidationRule {
  return {
    name: 'length',
    validate: (value: any) => validateLength(value, min, max)
  };
}

export function patternRule(pattern: RegExp, message: string): FieldValidationRule {
  return {
    name: 'pattern',
    validate: (value: any) => validatePattern(value, pattern, message)
  };
}

export function rangeRule(min?: number, max?: number): FieldValidationRule {
  return {
    name: 'range',
    validate: (value: any) => validateRange(value, min, max)
  };
}