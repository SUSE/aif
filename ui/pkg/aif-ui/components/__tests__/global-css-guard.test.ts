import { readFileSync } from 'node:fs';
import path from 'node:path';
import { describe, expect, it } from 'vitest';
import { parse, compileStyle, type SFCStyleCompileOptions } from '@vue/compiler-sfc';
import postcss from 'postcss';

// In scoped styles, `:global(X) .y` compiles to just `X`, dropping `.y`. That
// turned `body.theme-dark` into `display: none` for the whole page (PR #292).
// jsdom does not apply CSS, so compile each style block (SCSS included) and
// reject any rule whose selector targets bare <html>/<body>.
const files = [
  '../BlueprintSourceBadge.vue',
  '../BlueprintPartnerLogo.vue',
  '../../pages/Apps.vue',
];

function compiledSelectors(rel: string): string[] {
  const filename = path.resolve(__dirname, rel);
  const { descriptor } = parse(readFileSync(filename, 'utf8'), { filename });
  const selectors: string[] = [];

  for (const block of descriptor.styles) {
    const { code, errors } = compileStyle({
      source:         block.content,
      filename,
      id:             'data-v-guard',
      scoped:         !!block.scoped,
      preprocessLang: block.lang as SFCStyleCompileOptions['preprocessLang'],
    });

    expect(errors).toEqual([]);
    postcss.parse(code).walkRules((rule) => {
      selectors.push(...rule.selector.split(',').map((s) => s.trim()));
    });
  }

  return selectors;
}

describe('component global CSS', () => {
  it.each(files)('%s never styles bare html/body', (rel) => {
    const bare = compiledSelectors(rel).filter((s) => /^(html|body)([.:[][^\s>+~]*)?$/.test(s));

    expect(bare).toEqual([]);
  });
});
