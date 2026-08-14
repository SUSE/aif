const js = require('@eslint/js');
const globals = require('globals');
const tseslint = require('typescript-eslint');
const pluginVue = require('eslint-plugin-vue');

// Flat-config migration of the legacy pkg/aif-ui/.eslintrc(.default).js setup.
// ESLint v9 resolves eslint.config.js from the working directory (ui/), which is
// why this lives at the extension root rather than alongside the linted sources.
module.exports = tseslint.config(
  {
    ignores: [
      '**/node_modules/',
      '**/dist/',
      '**/dist-pkg/',
      '**/*.min.js',
      '**/babel.config.js',
      '**/vue.config.js',
    ],
  },

  js.configs.recommended,
  ...tseslint.configs.recommended,
  ...pluginVue.configs['flat/recommended'],

  {
    // Match the legacy setup, which did not flag unused eslint-disable directives.
    linterOptions: { reportUnusedDisableDirectives: 'off' },
  },

  {
    // Explicit so a preset reorder can't silently drop .ts/.vue from coverage
    // (base rules for those types arrive via the tseslint/vue flat presets).
    files: ['**/*.js', '**/*.ts', '**/*.vue'],
    languageOptions: {
      // `latest` (vs. a fixed 2020) avoids logical-assignment (??=/||=/&&=) and
      // other newer syntax parsing as a fatal error that looks like a config bug.
      ecmaVersion: 'latest',
      sourceType:  'module',
      globals:     {
        ...globals.browser,
        ...globals.node,
        NodeJS: 'readonly',
        Timer:  'readonly',
      },
      parserOptions: {
        // Parse <script lang="ts"> inside .vue files (vue-eslint-parser is set by
        // eslint-plugin-vue's flat config) and .ts files with the TS parser.
        parser: tseslint.parser,
      },
    },

    rules: {
      // Stylistic rules intentionally disabled — the extension does not enforce
      // formatting via ESLint (carried over from the legacy config).
      'dot-notation':                           'off',
      'generator-star-spacing':                 'off',
      'guard-for-in':                           'off',
      'linebreak-style':                        'off',
      'new-cap':                                'off',
      'no-empty':                               'off',
      'no-extra-boolean-cast':                  'off',
      'no-new':                                 'off',
      'no-plusplus':                            'off',
      'no-useless-escape':                      'off',
      'semi-spacing':                           'off',
      'space-in-parens':                        'off',
      strict:                                   'off',
      'vue/html-self-closing':                  'off',
      'vue/multi-word-component-names':         'off',
      'vue/no-reserved-component-names':        'off',
      'vue/no-deprecated-v-on-native-modifier': 'off',
      'vue/no-useless-template-attributes':     'off',
      'vue/no-unused-components':               'warn',
      'vue/no-v-html':                          'error',
      'quote-props':                            'off',
      'key-spacing':                            'off',
      'object-property-newline':                'off',
      'template-curly-spacing':                 'off',
      'no-trailing-spaces':                     'off',
      'padding-line-between-statements':        'off',
      'wrap-iife':                              'off',
      'array-bracket-spacing':                  'off',
      'arrow-parens':                           'off',
      'arrow-spacing':                          'off',
      'block-spacing':                          'off',
      'brace-style':                            'off',
      'comma-dangle':                           'off',
      'comma-spacing':                          'off',
      curly:                                    'off',
      eqeqeq:                                   'warn',
      'func-call-spacing':                      'off',
      'implicit-arrow-linebreak':               'off',
      indent:                                   'off',
      'keyword-spacing':                        'off',
      'lines-between-class-members':            'off',
      'multiline-ternary':                      'off',
      'newline-per-chained-call':              'off',
      'no-caller':                              'off',
      'no-cond-assign':                        'off',
      'no-console':                            'warn',
      'no-debugger':                           'warn',
      'no-eq-null':                            'off',
      'no-eval':                               'warn',
      'no-undef':                              'warn',
      'no-unused-vars':                        'warn',
      'no-whitespace-before-property':         'off',
      'object-curly-spacing':                  'off',
      'object-shorthand':                      'off',
      'padded-blocks':                         'off',
      'prefer-arrow-callback':                 'off',
      'prefer-template':                       'off',
      'rest-spread-spacing':                   'off',
      semi:                                    'off',
      'space-before-function-paren':           'off',
      'space-infix-ops':                       'off',
      'spaced-comment':                        'off',
      'switch-colon-spacing':                  'off',
      'yield-star-spacing':                    'off',
      'object-curly-newline':                  'off',
      quotes:                                  'off',
      'space-unary-ops':                       'off',
      'vue/order-in-components':               'off',
      'vue/no-lone-template':                  'off',
      'vue/v-slot-style':                      'off',
      'vue/component-tags-order':              'off',
      'vue/no-mutating-props':                 'off',
      '@typescript-eslint/no-unused-vars':     'off',
      '@typescript-eslint/no-var-requires':    'off',
      '@typescript-eslint/no-this-alias':      'off',
      // typescript-eslint v8 promotes these to errors in `recommended`; the legacy
      // config kept them non-blocking (no-require-imports supersedes no-var-requires).
      '@typescript-eslint/no-explicit-any':       'warn',
      '@typescript-eslint/no-require-imports':    'off',
      '@typescript-eslint/no-unused-expressions': 'warn',
      // v8 moved this out of `recommended` into `strict`; the legacy v5 config had
      // it as a warning, so restore it to preserve prior severity (32 findings).
      '@typescript-eslint/no-non-null-assertion': 'warn',
      'array-callback-return':                 'off',
      'vue/one-component-per-file':            'off',
      'vue/no-deprecated-slot-attribute':      'off',
      'vue/require-explicit-emits':            'off',
      'vue/v-on-event-hyphenation':            'off',
    },
  },

  {
    files: ['**/*.js'],
    rules: {
      'prefer-regex-literals':                'off',
      'vue/component-definition-name-casing': 'off',
      'no-unreachable-loop':                  'off',
      'computed-property-spacing':            'off',
    },
  },
);
