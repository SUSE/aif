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



// === Chart Values Validation ===


// === Form Validation ===



