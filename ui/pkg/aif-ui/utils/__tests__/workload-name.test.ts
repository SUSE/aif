import { describe, expect, it } from 'vitest';
import { defaultInstallNamespace } from '../workload-name';

const DNS1123_LABEL = /^[a-z0-9]([-a-z0-9]*[a-z0-9])?$/;

describe('defaultInstallNamespace', () => {
  it('suffixes the slug with a random 7-character label', () => {
    expect(defaultInstallNamespace('coredns')).toMatch(/^coredns-[a-z0-9]{7}$/);
  });

  it('suggests a different namespace for each install', () => {
    const names = new Set(Array.from({ length: 20 }, () => defaultInstallNamespace('coredns')));

    expect(names.size).toBe(20);
  });

  it('keeps long or unusual slugs a valid namespace name', () => {
    for (const slug of ['a'.repeat(80), 'My_App.Name', `${ 'x'.repeat(54) }-yz`, '---']) {
      const ns = defaultInstallNamespace(slug);

      expect(ns.length).toBeLessThanOrEqual(63);
      expect(ns).toMatch(DNS1123_LABEL);
    }
  });
});
