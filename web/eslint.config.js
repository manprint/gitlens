import vitestPlugin from '@vitest/eslint-plugin'
import eslint from '@eslint/js'
import jestDom from 'eslint-plugin-jest-dom'
import reactHooks from 'eslint-plugin-react-hooks'
import reactRefresh from 'eslint-plugin-react-refresh'
import testingLibrary from 'eslint-plugin-testing-library'
import globals from 'globals'
import tseslint from 'typescript-eslint'

const typedFiles = ['**/*.{ts,tsx}']
const testFiles = ['src/**/*.test.{ts,tsx}']
const componentTestFiles = ['src/**/*.test.tsx']

export default tseslint.config(
  {
    ignores: ['dist/**', '../internal/webui/dist/**', 'coverage/**', 'src/api/generated.ts'],
  },
  eslint.configs.recommended,
  ...tseslint.configs.recommendedTypeChecked.map((config) => ({
    ...config,
    files: typedFiles,
  })),
  ...tseslint.configs.stylisticTypeChecked.map((config) => ({
    ...config,
    files: typedFiles,
  })),
  {
    files: typedFiles,
    languageOptions: {
      globals: {
        ...globals.browser,
        ...globals.node,
      },
      parserOptions: {
        projectService: true,
        tsconfigRootDir: import.meta.dirname,
      },
    },
    plugins: {
      'react-hooks': reactHooks,
      'react-refresh': reactRefresh,
    },
    rules: {
      ...reactHooks.configs.flat.recommended.rules,
      'no-restricted-imports': [
        'error',
        {
          paths: [
            {
              name: '@testing-library/user-event',
              message: 'use the user factory exported by @/test/render',
            },
          ],
        },
      ],
      'no-restricted-globals': [
        'error',
        {
          name: 'fetch',
          message: 'use the typed client from @/api/client',
        },
      ],
      '@typescript-eslint/no-floating-promises': 'error',
      '@typescript-eslint/no-misused-promises': 'error',
      '@typescript-eslint/switch-exhaustiveness-check': 'error',
      'react-hooks/exhaustive-deps': 'error',
      'react-refresh/only-export-components': ['warn', { allowConstantExport: true }],
      'no-restricted-syntax': [
        'error',
        {
          selector: "CallExpression[callee.property.name='toLocaleString']",
          message: 'use an explicit locale in src/lib/format',
        },
        {
          selector: "CallExpression[callee.property.name='toLocaleDateString']",
          message: 'use an explicit locale in src/lib/format',
        },
        {
          selector: "CallExpression[callee.property.name='toLocaleTimeString']",
          message: 'use an explicit locale in src/lib/format',
        },
      ],
      'no-restricted-properties': [
        'error',
        {
          object: 'Math',
          property: 'random',
          message: 'use a deterministic identifier source such as React useId',
        },
        {
          object: 'crypto',
          property: 'randomUUID',
          message: 'use a deterministic identifier source such as React useId',
        },
      ],
    },
  },
  {
    files: ['src/lib/format/**/*.{ts,tsx}'],
    rules: {
      'no-restricted-syntax': 'off',
    },
  },
  {
    files: ['src/api/**/*.{ts,tsx}', 'src/features/**/*.{ts,tsx}'],
    rules: {
      'no-restricted-syntax': [
        'error',
        {
          selector: "CallExpression[callee.name='Number']",
          message: 'cluster_id and queryid are strings (I-5); use the string form',
        },
        {
          selector: "CallExpression[callee.property.name='toLocaleString']",
          message: 'use an explicit locale in src/lib/format',
        },
        {
          selector: "CallExpression[callee.property.name='toLocaleDateString']",
          message: 'use an explicit locale in src/lib/format',
        },
        {
          selector: "CallExpression[callee.property.name='toLocaleTimeString']",
          message: 'use an explicit locale in src/lib/format',
        },
      ],
    },
  },
  {
    files: testFiles,
    ...vitestPlugin.configs.recommended,
  },
  {
    files: ['src/test/render.tsx'],
    rules: {
      'no-restricted-imports': 'off',
    },
  },
  {
    files: ['e2e/**/*.{ts,tsx}'],
    rules: {
      'react-hooks/rules-of-hooks': 'off',
      'no-empty-pattern': 'off',
      'no-restricted-syntax': [
        'error',
        {
          selector: "CallExpression[callee.property.name='waitForTimeout']",
          message: 'wait for an observable condition with expect.poll instead of sleeping',
        },
      ],
    },
  },
  {
    files: componentTestFiles,
    ...testingLibrary.configs['flat/react'],
    ...jestDom.configs['flat/recommended'],
    plugins: {
      ...testingLibrary.configs['flat/react'].plugins,
      ...jestDom.configs['flat/recommended'].plugins,
    },
    rules: {
      'testing-library/no-node-access': 'error',
      'testing-library/prefer-screen-queries': 'error',
    },
  },
)
