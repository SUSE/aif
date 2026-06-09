module.exports = {
  env: {
    browser: true,
    node:    true
  },
  globals: {
    NodeJS: true,
    Timer:  true
  },
  parser: 'vue-eslint-parser',
  parserOptions: {
    parser: '@typescript-eslint/parser',
    ecmaVersion: 2020,
    sourceType: 'module'
  },
  plugins: [
    '@typescript-eslint'
  ],
  extends: [
    'eslint:recommended',
    'plugin:@typescript-eslint/recommended',
    'plugin:vue/vue3-recommended',
  ],
  rules: {
    'semi-spacing':          'off',
    'space-in-parens':       'off',
    'array-bracket-spacing': 'warn',
    'arrow-parens':          'warn',
    'arrow-spacing':         [
      'warn',
      {
        before: true,
        after:  true
      }
    ],
    'block-spacing': [
      'warn',
      'always'
    ],
    'brace-style': [
      'warn',
      '1tbs'
    ],
    'comma-dangle': [
      'warn',
      'only-multiline'
    ],
    'comma-spacing': 'warn',
    indent:          [
      'warn',
      2
    ],
    'keyword-spacing':          'warn',
    'newline-per-chained-call': [
      'warn',
      { ignoreChainWithDepth: 4 }
    ],
    'no-caller':      'warn',
    'no-cond-assign': [
      'warn',
      'except-parens'
    ],
    'no-console':                    'warn',
    'no-debugger':                   'warn',
    'no-eq-null':                    'warn',
    'no-eval':                       'warn',
    'no-trailing-spaces':            'warn',
    'no-undef':                      'warn',
    'no-unused-vars':                'warn',
    'no-whitespace-before-property': 'warn',
    'object-curly-spacing':          [
      'warn',
      'always'
    ],
    'object-property-newline': 'warn',
    'object-shorthand':        'warn',
    'padded-blocks':           [
      'warn',
      'never'
    ],
    'prefer-arrow-callback': 'warn',
    'prefer-template':       'warn',
    'quote-props':           'warn',
    'rest-spread-spacing':   'warn',
    semi:                    [
      'warn',
      'always'
    ],
    'space-before-function-paren': [
      'warn',
      'never'
    ],
    'space-infix-ops':        'warn',
    'spaced-comment':         'warn',
    'switch-colon-spacing':   'warn',
    'template-curly-spacing': [
      'warn',
      'always'
    ],
    'yield-star-spacing': [
      'warn',
      'both'
    ],
    'key-spacing': [
      'warn',
      {
        align: {
          beforeColon: false,
          afterColon:  true,
          on:          'value',
          mode:        'minimum'
        },
        multiLine: {
          beforeColon: false,
          afterColon:  true
        }
      }
    ],
    'object-curly-newline': [
      'warn',
      {
        ObjectExpression: {
          multiline:     true,
          minProperties: 3
        },
        ObjectPattern: {
          multiline:     true,
          minProperties: 4
        },
        ImportDeclaration: {
          multiline:     true,
          minProperties: 5
        },
        ExportDeclaration: {
          multiline:     true,
          minProperties: 3
        }
      }
    ],
    'padding-line-between-statements': [
      'warn',
      {
        blankLine: 'always',
        prev:      '*',
        next:      'return'
      },
      {
        blankLine: 'always',
        prev:      'function',
        next:      'function'
      },
      {
        blankLine: 'always',
        prev:      [
          'const',
          'let',
          'var'
        ],
        next: '*'
      },
      {
        blankLine: 'any',
        prev:      [
          'const',
          'let',
          'var'
        ],
        next: [
          'const',
          'let',
          'var'
        ]
      }
    ],
    quotes: [
      'warn',
      'single',
      {
        avoidEscape:           true,
        allowTemplateLiterals: true
      }
    ],
    'space-unary-ops': [
      'warn',
      {
        words:    true,
        nonwords: false
      }
    ]
  }
};
