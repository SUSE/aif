/**
 * Chart Values Processing - Manages chart values, schema validation, and transformations
 * Provides values processing functionality following standard patterns
 */

export interface ValueSchema {
  type: 'string' | 'number' | 'boolean' | 'array' | 'object';
  description?: string;
  default?: any;
  required?: boolean;
  enum?: any[];
  minimum?: number;
  maximum?: number;
  pattern?: string;
  properties?: Record<string, ValueSchema>;
  items?: ValueSchema;
  examples?: any[];
}

export interface ValueValidationError {
  path: string;
  message: string;
  value: any;
  schema: ValueSchema;
}

export interface ValueDiff {
  path: string;
  oldValue: any;
  newValue: any;
  type: 'added' | 'removed' | 'modified';
}

export interface ProcessedValues {
  values: Record<string, any>;
  schema?: Record<string, ValueSchema>;
  errors: ValueValidationError[];
  warnings: string[];
  processed: boolean;
}

/**
 * Chart Values Processor class for handling chart values operations
 */
export class ChartValuesProcessor {
  private defaultValues: Record<string, any> = {};
  private schema?: Record<string, ValueSchema>;
  private userValues: Record<string, any> = {};

  constructor(defaultValues: Record<string, any> = {}, schema?: Record<string, ValueSchema>) {
    this.defaultValues = defaultValues;
    this.schema = schema;
    this.userValues = {};
  }

  // === Value Management ===

  /**
   * Set user-provided values
   */
  setUserValues(values: Record<string, any>): void {
    this.userValues = { ...values };
  }

  /**
   * Get user-provided values
   */
  getUserValues(): Record<string, any> {
    return { ...this.userValues };
  }

  /**
   * Get default values
   */
  getDefaultValues(): Record<string, any> {
    return { ...this.defaultValues };
  }

  /**
   * Get merged values (defaults + user values)
   */
  getMergedValues(): Record<string, any> {
    return this.deepMerge(this.defaultValues, this.userValues);
  }

  /**
   * Get value at specific path
   */
  getValue(path: string): any {
    const mergedValues = this.getMergedValues();
    return this.getNestedValue(mergedValues, path);
  }

  /**
   * Set value at specific path
   */
  setValue(path: string, value: any): void {
    this.setNestedValue(this.userValues, path, value);
  }

  /**
   * Remove value at specific path
   */
  removeValue(path: string): void {
    this.deleteNestedValue(this.userValues, path);
  }

  /**
   * Check if value has been modified from default
   */
  isValueModified(path: string): boolean {
    const defaultValue = this.getNestedValue(this.defaultValues, path);
    const userValue = this.getNestedValue(this.userValues, path);
    return !this.deepEqual(defaultValue, userValue);
  }

  /**
   * Reset value to default
   */
  resetValue(path: string): void {
    this.deleteNestedValue(this.userValues, path);
  }

  /**
   * Reset all values to defaults
   */
  resetAllValues(): void {
    this.userValues = {};
  }

  // === Schema Operations ===

  /**
   * Set values schema
   */
  setSchema(schema: Record<string, ValueSchema>): void {
    this.schema = schema;
  }

  /**
   * Get schema for specific path
   */
  getSchema(path: string): ValueSchema | undefined {
    if (!this.schema) return undefined;
    return this.getNestedValue(this.schema, path);
  }

  /**
   * Check if path has schema definition
   */
  hasSchema(path: string): boolean {
    return !!this.getSchema(path);
  }

  /**
   * Get all schema paths
   */
  getSchemaPaths(): string[] {
    if (!this.schema) return [];
    return this.getAllPaths(this.schema);
  }

  // === Validation ===

  /**
   * Validate values against schema
   */
  validate(): ValueValidationError[] {
    if (!this.schema) return [];
    
    const errors: ValueValidationError[] = [];
    const values = this.getMergedValues();
    
    this.validateObject(values, this.schema, '', errors);
    return errors;
  }

  /**
   * Validate specific value
   */
  validateValue(path: string, value: any): ValueValidationError[] {
    const schema = this.getSchema(path);
    if (!schema) return [];
    
    const errors: ValueValidationError[] = [];
    this.validateSingleValue(value, schema, path, errors);
    return errors;
  }

  /**
   * Check if values are valid
   */
  isValid(): boolean {
    return this.validate().length === 0;
  }

  /**
   * Get validation errors for specific paths
   */
  getValidationErrors(paths?: string[]): ValueValidationError[] {
    const allErrors = this.validate();
    if (!paths) return allErrors;
    
    return allErrors.filter(error => 
      paths.some(path => error.path.startsWith(path))
    );
  }

  // === Processing ===

  /**
   * Process values with validation and transformation
   */
  process(): ProcessedValues {
    const values = this.getMergedValues();
    const errors = this.validate();
    const warnings: string[] = [];
    
    // Check for unused schema properties
    if (this.schema) {
      const schemaPaths = this.getSchemaPaths();
      const valuePaths = this.getAllPaths(values);
      
      const unusedPaths = schemaPaths.filter(path => 
        !valuePaths.includes(path) && this.getSchema(path)?.required
      );
      
      if (unusedPaths.length > 0) {
        warnings.push(`Missing required values: ${unusedPaths.join(', ')}`);
      }
    }
    
    // Check for unknown properties
    if (this.schema) {
      const schemaPaths = this.getSchemaPaths();
      const valuePaths = this.getAllPaths(values);
      
      const unknownPaths = valuePaths.filter(path => 
        !schemaPaths.some(schemaPath => path.startsWith(schemaPath))
      );
      
      if (unknownPaths.length > 0) {
        warnings.push(`Unknown properties: ${unknownPaths.join(', ')}`);
      }
    }
    
    return {
      values,
      schema: this.schema,
      errors,
      warnings,
      processed: true
    };
  }

  // === Comparison and Diff ===

  /**
   * Compare with another values processor
   */
  compare(other: ChartValuesProcessor): ValueDiff[] {
    const thisValues = this.getMergedValues();
    const otherValues = other.getMergedValues();
    
    return this.generateDiff(thisValues, otherValues);
  }

  /**
   * Generate diff between two value objects
   */
  generateDiff(oldValues: Record<string, any>, newValues: Record<string, any>): ValueDiff[] {
    const diffs: ValueDiff[] = [];
    const allPaths = new Set([
      ...this.getAllPaths(oldValues),
      ...this.getAllPaths(newValues)
    ]);
    
    for (const path of allPaths) {
      const oldValue = this.getNestedValue(oldValues, path);
      const newValue = this.getNestedValue(newValues, path);
      
      if (oldValue === undefined && newValue !== undefined) {
        diffs.push({ path, oldValue, newValue, type: 'added' });
      } else if (oldValue !== undefined && newValue === undefined) {
        diffs.push({ path, oldValue, newValue, type: 'removed' });
      } else if (!this.deepEqual(oldValue, newValue)) {
        diffs.push({ path, oldValue, newValue, type: 'modified' });
      }
    }
    
    return diffs;
  }

  // === Export/Import ===

  /**
   * Export values to YAML string
   */
  exportToYaml(): string {
    const values = this.getMergedValues();
    return this.objectToYaml(values);
  }

  /**
   * Export user values to YAML string
   */
  exportUserValuesToYaml(): string {
    return this.objectToYaml(this.userValues);
  }

  /**
   * Import values from YAML string
   */
  importFromYaml(yamlString: string): void {
    try {
      const values = this.yamlToObject(yamlString);
      this.userValues = values;
    } catch (error) {
      throw new Error(`Failed to parse YAML: ${error}`);
    }
  }

  /**
   * Export to JSON
   */
  exportToJson(): string {
    return JSON.stringify(this.getMergedValues(), null, 2);
  }

  /**
   * Import from JSON
   */
  importFromJson(jsonString: string): void {
    try {
      const values = JSON.parse(jsonString);
      this.userValues = values;
    } catch (error) {
      throw new Error(`Failed to parse JSON: ${error}`);
    }
  }

  // === Utility Methods ===

  /**
   * Deep merge two objects
   */
  private deepMerge(target: any, source: any): any {
    if (source === null || source === undefined) return target;
    if (target === null || target === undefined) return source;
    
    if (Array.isArray(source)) {
      return [...source];
    }
    
    if (typeof source === 'object' && typeof target === 'object') {
      const result = { ...target };
      
      for (const key in source) {
        if (Object.prototype.hasOwnProperty.call(source, key)) {
          result[key] = this.deepMerge(result[key], source[key]);
        }
      }
      
      return result;
    }
    
    return source;
  }

  /**
   * Deep equality check
   */
  private deepEqual(a: any, b: any): boolean {
    if (a === b) return true;
    
    if (a === null || b === null || a === undefined || b === undefined) {
      return a === b;
    }
    
    if (typeof a !== typeof b) return false;
    
    if (Array.isArray(a) && Array.isArray(b)) {
      if (a.length !== b.length) return false;
      for (let i = 0; i < a.length; i++) {
        if (!this.deepEqual(a[i], b[i])) return false;
      }
      return true;
    }
    
    if (typeof a === 'object' && typeof b === 'object') {
      const keysA = Object.keys(a);
      const keysB = Object.keys(b);
      
      if (keysA.length !== keysB.length) return false;
      
      for (const key of keysA) {
        if (!keysB.includes(key)) return false;
        if (!this.deepEqual(a[key], b[key])) return false;
      }
      
      return true;
    }
    
    return false;
  }

  /**
   * Get nested value by path
   */
  private getNestedValue(obj: any, path: string): any {
    const parts = path.split('.');
    let current = obj;
    
    for (const part of parts) {
      if (current === null || current === undefined) return undefined;
      current = current[part];
    }
    
    return current;
  }

  /**
   * Set nested value by path
   */
  private setNestedValue(obj: any, path: string, value: any): void {
    const parts = path.split('.');
    let current = obj;
    
    for (let i = 0; i < parts.length - 1; i++) {
      const part = parts[i];
      if (current[part] === undefined || current[part] === null) {
        current[part] = {};
      }
      current = current[part];
    }
    
    current[parts[parts.length - 1]] = value;
  }

  /**
   * Delete nested value by path
   */
  private deleteNestedValue(obj: any, path: string): void {
    const parts = path.split('.');
    let current = obj;
    
    for (let i = 0; i < parts.length - 1; i++) {
      const part = parts[i];
      if (current[part] === undefined || current[part] === null) {
        return;
      }
      current = current[part];
    }
    
    delete current[parts[parts.length - 1]];
  }

  /**
   * Get all paths in an object
   */
  private getAllPaths(obj: any, prefix = ''): string[] {
    const paths: string[] = [];
    
    if (obj === null || obj === undefined || typeof obj !== 'object') {
      return prefix ? [prefix] : [];
    }
    
    if (Array.isArray(obj)) {
      return prefix ? [prefix] : [];
    }
    
    for (const key in obj) {
      if (Object.prototype.hasOwnProperty.call(obj, key)) {
        const path = prefix ? `${prefix}.${key}` : key;
        const value = obj[key];
        
        if (value !== null && typeof value === 'object' && !Array.isArray(value)) {
          paths.push(...this.getAllPaths(value, path));
        } else {
          paths.push(path);
        }
      }
    }
    
    return paths;
  }

  /**
   * Validate object against schema
   */
  private validateObject(obj: any, schema: Record<string, ValueSchema>, prefix: string, errors: ValueValidationError[]): void {
    for (const key in schema) {
      if (Object.prototype.hasOwnProperty.call(schema, key)) {
        const path = prefix ? `${prefix}.${key}` : key;
        const valueSchema = schema[key];
        const value = obj?.[key];
        
        this.validateSingleValue(value, valueSchema, path, errors);
        
        if (valueSchema.type === 'object' && valueSchema.properties && value) {
          this.validateObject(value, valueSchema.properties, path, errors);
        }
      }
    }
  }

  /**
   * Validate single value against schema
   */
  private validateSingleValue(value: any, schema: ValueSchema, path: string, errors: ValueValidationError[]): void {
    // Required check
    if (schema.required && (value === undefined || value === null)) {
      errors.push({
        path,
        message: 'Required value is missing',
        value,
        schema
      });
      return;
    }
    
    // Skip validation if value is undefined/null and not required
    if (value === undefined || value === null) {
      return;
    }
    
    // Type check
    if (!this.isTypeValid(value, schema.type)) {
      errors.push({
        path,
        message: `Expected type '${schema.type}' but got '${typeof value}'`,
        value,
        schema
      });
      return;
    }
    
    // Enum check
    if (schema.enum && !schema.enum.includes(value)) {
      errors.push({
        path,
        message: `Value must be one of: ${schema.enum.join(', ')}`,
        value,
        schema
      });
    }
    
    // Range checks for numbers
    if (schema.type === 'number') {
      if (schema.minimum !== undefined && value < schema.minimum) {
        errors.push({
          path,
          message: `Value must be at least ${schema.minimum}`,
          value,
          schema
        });
      }
      
      if (schema.maximum !== undefined && value > schema.maximum) {
        errors.push({
          path,
          message: `Value must be at most ${schema.maximum}`,
          value,
          schema
        });
      }
    }
    
    // Pattern check for strings
    if (schema.type === 'string' && schema.pattern) {
      const regex = new RegExp(schema.pattern);
      if (!regex.test(value)) {
        errors.push({
          path,
          message: `Value does not match pattern: ${schema.pattern}`,
          value,
          schema
        });
      }
    }
    
    // Array item validation
    if (schema.type === 'array' && schema.items && Array.isArray(value)) {
      value.forEach((item, index) => {
        this.validateSingleValue(item, schema.items!, `${path}[${index}]`, errors);
      });
    }
  }

  /**
   * Check if value matches expected type
   */
  private isTypeValid(value: any, expectedType: ValueSchema['type']): boolean {
    switch (expectedType) {
      case 'string':
        return typeof value === 'string';
      case 'number':
        return typeof value === 'number' && !isNaN(value);
      case 'boolean':
        return typeof value === 'boolean';
      case 'array':
        return Array.isArray(value);
      case 'object':
        return value !== null && typeof value === 'object' && !Array.isArray(value);
      default:
        return false;
    }
  }

  /**
   * Convert object to YAML (simple implementation)
   */
  private objectToYaml(obj: any, indent = 0): string {
    const spaces = '  '.repeat(indent);
    
    if (obj === null) return 'null';
    if (obj === undefined) return 'undefined';
    if (typeof obj === 'string') return `"${obj.replace(/"/g, '\\"')}"`;
    if (typeof obj === 'number' || typeof obj === 'boolean') return String(obj);
    
    if (Array.isArray(obj)) {
      if (obj.length === 0) return '[]';
      return '\n' + obj.map(item => `${spaces}- ${this.objectToYaml(item, indent + 1).trim()}`).join('\n');
    }
    
    if (typeof obj === 'object') {
      const entries = Object.entries(obj);
      if (entries.length === 0) return '{}';
      
      return '\n' + entries.map(([key, value]) => {
        const yamlValue = this.objectToYaml(value, indent + 1);
        return `${spaces}${key}:${yamlValue.startsWith('\n') ? yamlValue : ` ${yamlValue}`}`;
      }).join('\n');
    }
    
    return String(obj);
  }

  /**
   * Convert YAML to object (simple implementation)
   * Note: In a real implementation, you'd use a proper YAML parser like js-yaml
   */
  private yamlToObject(yaml: string): any {
    // This is a very basic YAML parser - in production, use js-yaml
    try {
      // Try JSON parsing first for simple cases
      return JSON.parse(yaml);
    } catch {
      // Fallback to basic YAML parsing
      const lines = yaml.split('\n').filter(line => line.trim() && !line.trim().startsWith('#'));
      const result: any = {};
      
      for (const line of lines) {
        const match = line.match(/^(\s*)([^:]+):\s*(.*)$/);
        if (match) {
          const [, , key, value] = match;
          result[key.trim()] = this.parseYamlValue(value.trim());
        }
      }
      
      return result;
    }
  }

  /**
   * Parse individual YAML value
   */
  private parseYamlValue(value: string): any {
    if (value === 'null' || value === '~' || value === '') return null;
    if (value === 'true') return true;
    if (value === 'false') return false;
    
    const numberMatch = value.match(/^-?\d+(\.\d+)?$/);
    if (numberMatch) return parseFloat(value);
    
    if (value.startsWith('"') && value.endsWith('"')) {
      return value.slice(1, -1).replace(/\\"/g, '"');
    }
    
    if (value.startsWith("'") && value.endsWith("'")) {
      return value.slice(1, -1).replace(/''/g, "'");
    }
    
    return value;
  }

  /**
   * Create a clone of this processor
   */
  clone(): ChartValuesProcessor {
    const cloned = new ChartValuesProcessor(this.defaultValues, this.schema);
    cloned.setUserValues(this.userValues);
    return cloned;
  }

  /**
   * Get summary information
   */
  getSummary() {
    const merged = this.getMergedValues();
    const validation = this.validate();
    const modifiedPaths = this.getAllPaths(this.userValues);
    
    return {
      totalValues: this.getAllPaths(merged).length,
      userModifiedValues: modifiedPaths.length,
      validationErrors: validation.length,
      isValid: validation.length === 0,
      hasUserValues: Object.keys(this.userValues).length > 0,
      hasSchema: !!this.schema
    };
  }
}