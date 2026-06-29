/**
 * Form Validation Utilities for SUSE AI Extension
 * Provides comprehensive validation for forms, chart values, and configurations
 * Following Rancher UI patterns for consistent validation experience
 */

import { ERROR_CODES } from './constants';
import type { ErrorCode } from './constants';

// === Validation Result Types ===
export interface ValidationResult {
  valid: boolean;
  errors: ValidationError[];
  warnings: ValidationWarning[];
}

export interface ValidationError {
  field: string;
  message: string;
  code: ErrorCode;
  value?: any;
  severity: 'error' | 'warning';
}

export interface ValidationWarning {
  field: string;
  message: string;
  suggestion?: string;
}

export interface FieldValidationRule {
  name: string;
  validate: (value: any, context?: any) => ValidationResult | Promise<ValidationResult>;
  async?: boolean;
}

// === Form Field Types ===
export interface FormField {
  name: string;
  label: string;
  type: FieldType;
  required?: boolean;
  disabled?: boolean;
  placeholder?: string;
  description?: string;
  defaultValue?: any;
  rules?: FieldValidationRule[];
  dependsOn?: string[];
  showWhen?: (values: Record<string, any>) => boolean;
}

export type FieldType = 
  | 'text' 
  | 'password' 
  | 'email' 
  | 'url' 
  | 'number' 
  | 'integer'
  | 'boolean' 
  | 'select' 
  | 'multiselect'
  | 'textarea' 
  | 'json' 
  | 'yaml'
  | 'array'
  | 'object'
  | 'file'
  | 'cluster'
  | 'namespace'
  | 'secret';

export interface FormSchema {
  fields: FormField[];
  rules?: FormValidationRule[];
}

export interface FormValidationRule {
  name: string;
  validate: (values: Record<string, any>) => ValidationResult | Promise<ValidationResult>;
  async?: boolean;
}

// === Built-in Validation Rules ===

/**
 * Required field validation
 */
export const requiredRule: FieldValidationRule = {
  name: 'required',
  validate: (value: any): ValidationResult => {
    const isEmpty = value === null || 
                   value === undefined || 
                   value === '' || 
                   (Array.isArray(value) && value.length === 0) ||
                   (typeof value === 'object' && Object.keys(value).length === 0);
    
    if (isEmpty) {
      return {
        valid: false,
        errors: [{
          field: '',
          message: 'This field is required',
          code: ERROR_CODES.UNKNOWN,
          severity: 'error' as const
        }],
        warnings: []
      };
    }
    
    return { valid: true, errors: [], warnings: [] };
  }
};

/**
 * String length validation
 */
export const lengthRule = (min?: number, max?: number): FieldValidationRule => ({
  name: 'length',
  validate: (value: any): ValidationResult => {
    if (typeof value !== 'string') {
      return { valid: true, errors: [], warnings: [] };
    }
    
    const errors: ValidationError[] = [];
    
    if (min !== undefined && value.length < min) {
      errors.push({
        field: '',
        message: `Must be at least ${min} characters long`,
        code: ERROR_CODES.UNKNOWN,
        value,
        severity: 'error'
      });
    }
    
    if (max !== undefined && value.length > max) {
      errors.push({
        field: '',
        message: `Must be no more than ${max} characters long`,
        code: ERROR_CODES.UNKNOWN,
        value,
        severity: 'error'
      });
    }
    
    return {
      valid: errors.length === 0,
      errors,
      warnings: []
    };
  }
});

/**
 * Pattern matching validation
 */
export const patternRule = (pattern: RegExp, message: string): FieldValidationRule => ({
  name: 'pattern',
  validate: (value: any): ValidationResult => {
    if (typeof value !== 'string') {
      return { valid: true, errors: [], warnings: [] };
    }
    
    if (!pattern.test(value)) {
      return {
        valid: false,
        errors: [{
          field: '',
          message,
          code: ERROR_CODES.UNKNOWN,
          value,
          severity: 'error'
        }],
        warnings: []
      };
    }
    
    return { valid: true, errors: [], warnings: [] };
  }
});

/**
 * Email validation
 */
export const emailRule: FieldValidationRule = {
  name: 'email',
  validate: (value: any): ValidationResult => {
    if (!value || typeof value !== 'string') {
      return { valid: true, errors: [], warnings: [] };
    }
    
    const emailRegex = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;
    
    if (!emailRegex.test(value)) {
      return {
        valid: false,
        errors: [{
          field: '',
          message: 'Please enter a valid email address',
          code: ERROR_CODES.UNKNOWN,
          value,
          severity: 'error'
        }],
        warnings: []
      };
    }
    
    return { valid: true, errors: [], warnings: [] };
  }
};

/**
 * URL validation
 */
export const urlRule: FieldValidationRule = {
  name: 'url',
  validate: (value: any): ValidationResult => {
    if (!value || typeof value !== 'string') {
      return { valid: true, errors: [], warnings: [] };
    }
    
    try {
      new URL(value);
      return { valid: true, errors: [], warnings: [] };
    } catch {
      return {
        valid: false,
        errors: [{
          field: '',
          message: 'Please enter a valid URL',
          code: ERROR_CODES.UNKNOWN,
          value,
          severity: 'error'
        }],
        warnings: []
      };
    }
  }
};

/**
 * Number range validation
 */
export const rangeRule = (min?: number, max?: number): FieldValidationRule => ({
  name: 'range',
  validate: (value: any): ValidationResult => {
    const num = typeof value === 'string' ? parseFloat(value) : value;
    
    if (typeof num !== 'number' || isNaN(num)) {
      return { valid: true, errors: [], warnings: [] };
    }
    
    const errors: ValidationError[] = [];
    
    if (min !== undefined && num < min) {
      errors.push({
        field: '',
        message: `Value must be at least ${min}`,
        code: ERROR_CODES.UNKNOWN,
        value,
        severity: 'error'
      });
    }
    
    if (max !== undefined && num > max) {
      errors.push({
        field: '',
        message: `Value must be at most ${max}`,
        code: ERROR_CODES.UNKNOWN,
        value,
        severity: 'error'
      });
    }
    
    return {
      valid: errors.length === 0,
      errors,
      warnings: []
    };
  }
});

/**
 * JSON validation
 */
export const jsonRule: FieldValidationRule = {
  name: 'json',
  validate: (value: any): ValidationResult => {
    if (!value || typeof value !== 'string') {
      return { valid: true, errors: [], warnings: [] };
    }
    
    try {
      JSON.parse(value);
      return { valid: true, errors: [], warnings: [] };
    } catch (error: any) {
      return {
        valid: false,
        errors: [{
          field: '',
          message: `Invalid JSON: ${error.message}`,
          code: ERROR_CODES.UNKNOWN,
          value,
          severity: 'error'
        }],
        warnings: []
      };
    }
  }
};

/**
 * YAML validation
 */
export const yamlRule: FieldValidationRule = {
  name: 'yaml',
  validate: (value: any): ValidationResult => {
    if (!value || typeof value !== 'string') {
      return { valid: true, errors: [], warnings: [] };
    }
    
    try {
      // This would use a proper YAML parser like js-yaml
      // For now, do basic validation
      if (value.includes('\t')) {
        return {
          valid: false,
          errors: [{
            field: '',
            message: 'YAML should use spaces, not tabs for indentation',
            code: ERROR_CODES.UNKNOWN,
            value,
            severity: 'warning'
          }],
          warnings: []
        };
      }
      
      return { valid: true, errors: [], warnings: [] };
    } catch (error: any) {
      return {
        valid: false,
        errors: [{
          field: '',
          message: `Invalid YAML: ${error.message}`,
          code: ERROR_CODES.UNKNOWN,
          value,
          severity: 'error'
        }],
        warnings: []
      };
    }
  }
};

// === Kubernetes-specific Validation Rules ===

/**
 * Kubernetes name validation (RFC 1123)
 */
export const k8sNameRule: FieldValidationRule = {
  name: 'k8s-name',
  validate: (value: any): ValidationResult => {
    if (!value || typeof value !== 'string') {
      return { valid: true, errors: [], warnings: [] };
    }
    
    const k8sNameRegex = /^[a-z0-9]([-a-z0-9]*[a-z0-9])?$/;
    const errors: ValidationError[] = [];
    
    if (value.length > 63) {
      errors.push({
        field: '',
        message: 'Name must be 63 characters or less',
        code: ERROR_CODES.UNKNOWN,
        value,
        severity: 'error'
      });
    }
    
    if (!k8sNameRegex.test(value)) {
      errors.push({
        field: '',
        message: 'Name must consist of lowercase letters, numbers, and hyphens, and must start and end with an alphanumeric character',
        code: ERROR_CODES.UNKNOWN,
        value,
        severity: 'error'
      });
    }
    
    return {
      valid: errors.length === 0,
      errors,
      warnings: []
    };
  }
};

/**
 * Kubernetes label validation
 */
export const k8sLabelRule: FieldValidationRule = {
  name: 'k8s-label',
  validate: (value: any): ValidationResult => {
    if (!value || typeof value !== 'string') {
      return { valid: true, errors: [], warnings: [] };
    }
    
    const labelRegex = /^[a-z0-9A-Z]([-._a-z0-9A-Z]*[a-z0-9A-Z])?$/;
    const errors: ValidationError[] = [];
    
    if (value.length > 63) {
      errors.push({
        field: '',
        message: 'Label value must be 63 characters or less',
        code: ERROR_CODES.UNKNOWN,
        value,
        severity: 'error'
      });
    }
    
    if (!labelRegex.test(value)) {
      errors.push({
        field: '',
        message: 'Label value must consist of alphanumeric characters, hyphens, underscores, and dots',
        code: ERROR_CODES.UNKNOWN,
        value,
        severity: 'error'
      });
    }
    
    return {
      valid: errors.length === 0,
      errors,
      warnings: []
    };
  }
};

/**
 * Resource quantity validation (CPU, memory, storage)
 */
export const resourceQuantityRule: FieldValidationRule = {
  name: 'resource-quantity',
  validate: (value: any): ValidationResult => {
    if (!value || typeof value !== 'string') {
      return { valid: true, errors: [], warnings: [] };
    }
    
    const quantityRegex = /^[0-9]+(\.[0-9]+)?(m|[KMGTPE]i?)?$/;
    
    if (!quantityRegex.test(value)) {
      return {
        valid: false,
        errors: [{
          field: '',
          message: 'Invalid resource quantity format (e.g., 100m, 1Gi, 500Mi)',
          code: ERROR_CODES.UNKNOWN,
          value,
          severity: 'error'
        }],
        warnings: []
      };
    }
    
    return { valid: true, errors: [], warnings: [] };
  }
};

// === Chart Values Validation ===

/**
 * Validate chart values against schema
 */
export function validateChartValues(
  values: Record<string, any>,
  schema: Record<string, any>
): ValidationResult {
  const errors: ValidationError[] = [];
  const warnings: ValidationWarning[] = [];
  
  function validateValue(
    path: string,
    value: any,
    fieldSchema: any
  ): void {
    // Required field check
    if (fieldSchema.required && (value === undefined || value === null || value === '')) {
      errors.push({
        field: path,
        message: 'This field is required',
        code: ERROR_CODES.UNKNOWN,
        value,
        severity: 'error'
      });
      return;
    }
    
    // Skip validation if value is empty and not required
    if (value === undefined || value === null) {
      return;
    }
    
    // Type validation
    if (fieldSchema.type) {
      const expectedType = fieldSchema.type;
      const actualType = Array.isArray(value) ? 'array' : typeof value;
      
      if (expectedType !== actualType) {
        if (!(expectedType === 'number' && actualType === 'string' && !isNaN(Number(value)))) {
          errors.push({
            field: path,
            message: `Expected ${expectedType}, got ${actualType}`,
            code: ERROR_CODES.UNKNOWN,
            value,
            severity: 'error'
          });
          return;
        }
      }
    }
    
    // Enum validation
    if (fieldSchema.enum && !fieldSchema.enum.includes(value)) {
      errors.push({
        field: path,
        message: `Value must be one of: ${fieldSchema.enum.join(', ')}`,
        code: ERROR_CODES.UNKNOWN,
        value,
        severity: 'error'
      });
    }
    
    // String validations
    if (typeof value === 'string') {
      if (fieldSchema.minLength && value.length < fieldSchema.minLength) {
        errors.push({
          field: path,
          message: `Must be at least ${fieldSchema.minLength} characters`,
          code: ERROR_CODES.UNKNOWN,
          value,
          severity: 'error'
        });
      }
      
      if (fieldSchema.maxLength && value.length > fieldSchema.maxLength) {
        errors.push({
          field: path,
          message: `Must be no more than ${fieldSchema.maxLength} characters`,
          code: ERROR_CODES.UNKNOWN,
          value,
          severity: 'error'
        });
      }
      
      if (fieldSchema.pattern && !new RegExp(fieldSchema.pattern).test(value)) {
        errors.push({
          field: path,
          message: fieldSchema.patternErrorMessage || 'Value does not match required pattern',
          code: ERROR_CODES.UNKNOWN,
          value,
          severity: 'error'
        });
      }
    }
    
    // Number validations
    if (typeof value === 'number') {
      if (fieldSchema.minimum !== undefined && value < fieldSchema.minimum) {
        errors.push({
          field: path,
          message: `Value must be at least ${fieldSchema.minimum}`,
          code: ERROR_CODES.UNKNOWN,
          value,
          severity: 'error'
        });
      }
      
      if (fieldSchema.maximum !== undefined && value > fieldSchema.maximum) {
        errors.push({
          field: path,
          message: `Value must be at most ${fieldSchema.maximum}`,
          code: ERROR_CODES.UNKNOWN,
          value,
          severity: 'error'
        });
      }
    }
    
    // Array validation
    if (Array.isArray(value) && fieldSchema.items) {
      value.forEach((item, index) => {
        validateValue(`${path}[${index}]`, item, fieldSchema.items);
      });
    }
    
    // Object validation
    if (typeof value === 'object' && value !== null && !Array.isArray(value) && fieldSchema.properties) {
      for (const [key, subValue] of Object.entries(value)) {
        const subSchema = fieldSchema.properties[key];
        if (subSchema) {
          validateValue(`${path}.${key}`, subValue, subSchema);
        } else if (fieldSchema.additionalProperties === false) {
          warnings.push({
            field: `${path}.${key}`,
            message: 'Unknown property',
            suggestion: 'Remove this property or check the chart documentation'
          });
        }
      }
      
      // Check for missing required properties
      if (fieldSchema.required && Array.isArray(fieldSchema.required)) {
        for (const requiredKey of fieldSchema.required) {
          if (!(requiredKey in value)) {
            errors.push({
              field: `${path}.${requiredKey}`,
              message: 'Required property is missing',
              code: ERROR_CODES.UNKNOWN,
              severity: 'error'
            });
          }
        }
      }
    }
  }
  
  // Validate root level properties
  for (const [key, value] of Object.entries(values)) {
    const fieldSchema = schema[key];
    if (fieldSchema) {
      validateValue(key, value, fieldSchema);
    } else {
      warnings.push({
        field: key,
        message: 'Unknown property',
        suggestion: 'Remove this property or check the chart documentation'
      });
    }
  }
  
  return {
    valid: errors.length === 0,
    errors,
    warnings
  };
}

// === Form Validation ===

/**
 * Validate entire form
 */
export async function validateForm(
  values: Record<string, any>,
  schema: FormSchema
): Promise<ValidationResult> {
  const errors: ValidationError[] = [];
  const warnings: ValidationWarning[] = [];
  
  // Field-level validation
  for (const field of schema.fields) {
    const value = values[field.name];
    
    // Check if field should be shown
    if (field.showWhen && !field.showWhen(values)) {
      continue;
    }
    
    // Required validation
    if (field.required) {
      const result = await Promise.resolve(requiredRule.validate(value));
      if (!result.valid) {
        errors.push(...result.errors.map((e: any) => ({ ...e, field: field.name })));
      }
    }
    
    // Field-specific rules
    if (field.rules) {
      for (const rule of field.rules) {
        try {
          const result = rule.async ? 
            await rule.validate(value, values) : 
            rule.validate(value, values);
          
          const resolvedResult = await Promise.resolve(result);
          
          if (!resolvedResult.valid) {
            errors.push(...resolvedResult.errors.map((e: any) => ({ ...e, field: field.name })));
            warnings.push(...resolvedResult.warnings.map((w: any) => ({ ...w, field: field.name })));
          }
        } catch (error: any) {
          errors.push({
            field: field.name,
            message: `Validation failed: ${error.message}`,
            code: ERROR_CODES.UNKNOWN,
            severity: 'error'
          });
        }
      }
    }
  }
  
  // Form-level rules
  if (schema.rules) {
    for (const rule of schema.rules) {
      try {
        const result = rule.async ? 
          await rule.validate(values) : 
          rule.validate(values);
        
        const resolvedResult = await Promise.resolve(result);
        
        if (!resolvedResult.valid) {
          errors.push(...resolvedResult.errors);
          warnings.push(...resolvedResult.warnings);
        }
      } catch (error: any) {
        errors.push({
          field: 'form',
          message: `Form validation failed: ${error.message}`,
          code: ERROR_CODES.UNKNOWN,
          severity: 'error' as const
        });
      }
    }
  }
  
  return {
    valid: errors.length === 0,
    errors,
    warnings
  };
}

/**
 * Validate single field
 */
export async function validateField(
  value: any,
  field: FormField,
  context?: Record<string, any>
): Promise<ValidationResult> {
  const errors: ValidationError[] = [];
  const warnings: ValidationWarning[] = [];
  
  // Required validation
  if (field.required) {
    const result = await Promise.resolve(requiredRule.validate(value));
    if (!result.valid) {
      errors.push(...result.errors.map((e: ValidationError) => ({ ...e, field: field.name })));
    }
  }
  
  // Skip other validations if value is empty and not required
  if (!field.required && (value === null || value === undefined || value === '')) {
    return { valid: true, errors: [], warnings: [] };
  }
  
  // Type-specific validation
  switch (field.type) {
    case 'email': {
      const emailResult = await Promise.resolve(emailRule.validate(value));
      if (!emailResult.valid) {
        errors.push(...emailResult.errors.map((e: ValidationError) => ({ ...e, field: field.name })));
      }
      break;
    }
      
    case 'url': {
      const urlResult = await Promise.resolve(urlRule.validate(value));
      if (!urlResult.valid) {
        errors.push(...urlResult.errors.map((e: ValidationError) => ({ ...e, field: field.name })));
      }
      break;
    }
      
    case 'json': {
      const jsonResult = await Promise.resolve(jsonRule.validate(value));
      if (!jsonResult.valid) {
        errors.push(...jsonResult.errors.map((e: ValidationError) => ({ ...e, field: field.name })));
      }
      break;
    }
      
    case 'yaml': {
      const yamlResult = await Promise.resolve(yamlRule.validate(value));
      if (!yamlResult.valid) {
        errors.push(...yamlResult.errors.map((e: ValidationError) => ({ ...e, field: field.name })));
      }
      break;
    }
  }
  
  // Custom field rules
  if (field.rules) {
    for (const rule of field.rules) {
      try {
        const result = rule.async ? 
          await rule.validate(value, context) : 
          rule.validate(value, context);
        
        // Ensure result is resolved if it's a Promise
        const resolvedResult = await Promise.resolve(result);
        
        if (!resolvedResult.valid) {
          errors.push(...resolvedResult.errors.map((e: ValidationError) => ({ ...e, field: field.name })));
          warnings.push(...resolvedResult.warnings.map((w: ValidationWarning) => ({ ...w, field: field.name })));
        }
      } catch (error: any) {
        errors.push({
          field: field.name,
          message: `Validation failed: ${error.message}`,
          code: ERROR_CODES.UNKNOWN,
          severity: 'error' as const
        });
      }
    }
  }
  
  return {
    valid: errors.length === 0,
    errors,
    warnings
  };
}

// === Utility Functions ===

/**
 * Get validation rules for field type
 */
export function getDefaultRulesForFieldType(type: FieldType): FieldValidationRule[] {
  switch (type) {
    case 'email':
      return [emailRule];
    case 'url':
      return [urlRule];
    case 'json':
      return [jsonRule];
    case 'yaml':
      return [yamlRule];
    case 'cluster':
    case 'namespace':
      return [k8sNameRule];
    default:
      return [];
  }
}

/**
 * Format validation errors for display
 */
export function formatValidationErrors(errors: ValidationError[]): string[] {
  return errors.map(error => {
    const fieldName = error.field ? `${error.field}: ` : '';
    return `${fieldName}${error.message}`;
  });
}

/**
 * Check if validation result has critical errors
 */
export function hasCriticalErrors(result: ValidationResult): boolean {
  return result.errors.some(error => error.severity === 'error');
}

/**
 * Filter errors by severity
 */
export function filterErrorsBySeverity(
  errors: ValidationError[], 
  severity: 'error' | 'warning'
): ValidationError[] {
  return errors.filter(error => error.severity === severity);
}