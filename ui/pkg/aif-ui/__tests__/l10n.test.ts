import { describe, it, expect } from 'vitest';
import fs from 'fs';
import path from 'path';
import yaml from 'js-yaml';

// @rancher/shell/pkg/auto-import.js only generates imports for `l10n/<locale>.yaml`:
//
//   const ext = (f === 'l10n') ? '.yaml' : '';
//
// A locale shipped as .json is silently skipped, no translations reach the
// Rancher i18n store, and every key renders as `%the.key%` in the UI.
const PKG = path.resolve(__dirname, '..');
const L10N = path.join(PKG, 'l10n');

function localeKeys(): Record<string, unknown> {
  const raw = fs.readFileSync(path.join(L10N, 'en-us.yaml'), 'utf8');

  return yaml.load(raw) as Record<string, unknown>;
}

function lookup(obj: unknown, dotted: string): unknown {
  return dotted.split('.').reduce<any>((cur, k) => (cur && cur[k] !== undefined ? cur[k] : undefined), obj);
}

function vueFiles(dir: string, out: string[] = []): string[] {
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const p = path.join(dir, entry.name);

    if (entry.isDirectory() && entry.name !== 'node_modules') {
      vueFiles(p, out);
    } else if (entry.name.endsWith('.vue')) {
      out.push(p);
    }
  }

  return out;
}

describe('l10n locale files', () => {
  it('ships every locale as .yaml so auto-import picks it up', () => {
    const files = fs.readdirSync(L10N);

    expect(files.filter((f) => f.endsWith('.yaml')).length).toBeGreaterThan(0);
    expect(files.filter((f) => f.endsWith('.json'))).toEqual([]);
  });

  it('parses en-us.yaml', () => {
    expect(lookup(localeKeys(), 'suseai.pages.about.title')).toBeTypeOf('string');
  });
});

describe('components calling t()', () => {
  // Two legitimate ways a template's bare `t(...)` resolves:
  //  - Composition API: a local `const t = useT();` (checked below).
  //  - Options API: @rancher/shell's mixin exposes `this.t`, which templates
  //    can call as bare `t(...)` without the `this.` — no local `t` to check.
  // A miss renders the fallback silently (useT) or `%key%` (shell's global
  // t(key, args) — note arg 2 there is interpolation args, not a fallback).
  // Either way the key must exist, so every t()/this.t() call is checked
  // regardless of which flavor sources it — a component silently shadowing
  // `t` with something that isn't useT() is exactly the bug this guards.
  const translations = localeKeys();
  const unresolvedKeys: string[] = [];
  const unwiredT: string[] = [];

  for (const file of vueFiles(PKG)) {
    const src = fs.readFileSync(file, 'utf8');
    const rel = path.relative(PKG, file);

    for (const m of src.matchAll(/\bt\(\s*['"]([a-zA-Z0-9._]+)['"]/g)) {
      if (typeof lookup(translations, m[1]) !== 'string') {
        unresolvedKeys.push(`${rel}: ${m[1]}`);
      }
    }

    // Composition API components have no implicit `this` in the template, so
    // a bare `t(...)` call there can only be legitimate if the script wires
    // `t` from useT(). Without that wiring, `t` is either undefined (a
    // template error) or a local reimplementation nothing here can vouch
    // for — which is exactly how AppInstances.vue's tooltip keys went dead:
    // a hand-rolled `const t = (key, fallback) => fallback` shadowed useT
    // and every key resolved in this file's other check while doing nothing
    // at runtime.
    const isCompositionAPI = /\bsetup\s*\(/.test(src);
    const hasBareT = /(?<!\.)\bt\(/.test(src);
    const wiresUseT = /\bt\s*=\s*useT\(\)/.test(src);

    if (isCompositionAPI && hasBareT && !wiresUseT) {
      unwiredT.push(rel);
    }
  }

  it('resolves every key it references', () => {
    expect(unresolvedKeys).toEqual([]);
  });

  it('sources every Composition API t() from useT()', () => {
    expect(unwiredT).toEqual([]);
  });
});
