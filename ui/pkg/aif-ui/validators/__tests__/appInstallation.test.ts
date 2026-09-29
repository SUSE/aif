import { describe, expect, it } from 'vitest';
import {
  defaultInstanceName,
  defaultReleaseName,
  defaultNamespace,
  validateReleaseName,
  validateNamespace,
  instanceNameError,
} from '../appInstallation';

describe('app installation defaults', () => {
  describe('defaultInstanceName / defaultReleaseName', () => {
    it('uses catalog item default_instance_name when present', () => {
      expect(defaultInstanceName('helm-chart', { default_instance_name: 'openshell-gw' })).toBe('openshell-gw');
      expect(defaultReleaseName('helm-chart', { default_instance_name: 'openshell-gw' })).toBe('openshell-gw');
      expect(defaultInstanceName('custom-chart', { default_instance_name: 'my-instance' })).toBe('my-instance');
    });

    it('falls back to the slug when no catalog default_instance_name is specified', () => {
      expect(defaultInstanceName('qdrant')).toBe('qdrant');
      expect(defaultInstanceName('vllm')).toBe('vllm');
      expect(defaultInstanceName('milvus')).toBe('milvus');
      expect(defaultInstanceName('openshell-workspace')).toBe('openshell-workspace');
      expect(defaultInstanceName('helm-chart')).toBe('helm-chart');
    });
  });

  describe('defaultNamespace', () => {
    it('uses catalog item default_namespace when present', () => {
      expect(defaultNamespace('helm-chart', { default_namespace: 'openshell' })).toBe('openshell');
      expect(defaultNamespace('custom-chart', { default_namespace: 'custom-ns' })).toBe('custom-ns');
    });

    it('falls back to `${slug}-system` when no catalog default_namespace is specified', () => {
      expect(defaultNamespace('qdrant')).toBe('qdrant-system');
      expect(defaultNamespace('vllm')).toBe('vllm-system');
      expect(defaultNamespace('milvus')).toBe('milvus-system');
      expect(defaultNamespace('openshell-workspace')).toBe('openshell-workspace-system');
      expect(defaultNamespace('helm-chart')).toBe('helm-chart-system');
    });
  });

  describe('validation with default openshell names', () => {
    it('validates "openshell-gw" as a valid release / instance name', () => {
      expect(validateReleaseName('openshell-gw').valid).toBe(true);
      expect(instanceNameError('openshell-gw')).toBe('');
    });

    it('validates "openshell" as a valid namespace', () => {
      expect(validateNamespace('openshell').valid).toBe(true);
    });
  });
});
