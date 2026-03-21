import eslint from '@eslint/js';
import tseslint from 'typescript-eslint';
import vue from 'eslint-plugin-vue';
import security from 'eslint-plugin-security';
import vuejsAccessibility from 'eslint-plugin-vuejs-accessibility';
import importX from 'eslint-plugin-import-x';
import prettier from 'eslint-config-prettier';
import { fileURLToPath } from 'node:url';

const tsconfigPath = fileURLToPath(new URL('./tsconfig.json', import.meta.url));

export default tseslint.config(
  // Base configs
  eslint.configs.recommended,
  ...tseslint.configs.strictTypeChecked,
  ...vue.configs['flat/recommended'],
  // @ts-expect-error — vuejs-accessibility types are loose
  ...vuejsAccessibility.configs['flat/recommended'],
  // @ts-expect-error — eslint-plugin-security types are loose
  security.configs.recommended,

  // TypeScript typed linting — point at root tsconfig.json which extends .nuxt/tsconfig.json
  {
    languageOptions: {
      parserOptions: {
        project: tsconfigPath,
        extraFileExtensions: ['.vue'],
      },
    },
  },

  // Vue SFC parser config
  {
    files: ['**/*.vue'],
    languageOptions: {
      parserOptions: {
        parser: tseslint.parser,
        project: tsconfigPath,
        extraFileExtensions: ['.vue'],
      },
    },
  },

  // Import plugin config
  {
    plugins: {
      'import-x': importX,
    },
    rules: {
      'import-x/no-unresolved': 'off', // TypeScript handles this
      'import-x/no-duplicates': 'error',
      'import-x/no-self-import': 'error',
      'import-x/no-cycle': ['error', { maxDepth: 3 }],
      'import-x/order': [
        'error',
        {
          groups: ['builtin', 'external', 'internal', 'parent', 'sibling', 'index'],
          'newlines-between': 'always',
        },
      ],
    },
  },

  // Project rules
  {
    rules: {
      // Existing rules (preserved)
      '@typescript-eslint/no-unused-vars': ['error', { argsIgnorePattern: '^_' }],
      '@typescript-eslint/no-explicit-any': 'error',
      'vue/multi-word-component-names': 'off',

      // New strict rules
      '@typescript-eslint/no-floating-promises': 'error',
      '@typescript-eslint/strict-boolean-expressions': [
        'error',
        {
          allowNullableObject: true,
          allowNullableBoolean: true,
          allowNumber: true,
          allowNullableString: false,
        },
      ],
      'vue/no-v-html': 'error',

      // TypeScript handles undefined-reference checking for .ts/.vue files;
      // no-undef produces false positives for Nuxt auto-imports.
      'no-undef': 'off',

      // Accept either nesting (label wraps input) or explicit for+id — both are valid a11y patterns.
      'vuejs-accessibility/label-has-for': ['error', { required: { some: ['nesting', 'id'] } }],
    },
  },

  // Disable all ESLint formatting rules that conflict with Prettier (must be last)
  prettier,

  // Ignores
  {
    ignores: ['.nuxt/', '.output/', 'node_modules/', 'dist/', 'eslint.config.mjs'],
  },
);
