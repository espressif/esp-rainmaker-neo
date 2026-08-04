/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import js from "@eslint/js";
import tseslint from "typescript-eslint";
import react from "eslint-plugin-react";
import reactHooks from "eslint-plugin-react-hooks";
import globals from "globals";

export default [
  js.configs.recommended,
  ...tseslint.configs.recommended,

  {
    files: ["**/*.{js,mjs,cjs,jsx,ts,tsx}"],

    languageOptions: {
      ecmaVersion: "latest",
      sourceType: "module",
      globals: {
        ...globals.browser,
        ...globals.node,
      },
    },

    plugins: {
      react,
      "react-hooks": reactHooks,
    },

    settings: {
      react: {
        version: "detect",
      },
    },

    rules: {
      /*
       * React
       */
      "react/jsx-key": "error",
      "react/jsx-no-duplicate-props": "error",
      "react/no-danger-with-children": "error",
      "react/no-direct-mutation-state": "error",
      "react/no-unknown-property": "error",
      "react/self-closing-comp": "error",

      /*
       * React Hooks
       */
      "react-hooks/rules-of-hooks": "error",
      "react-hooks/exhaustive-deps": "warn",

      /*
       * TypeScript
       */
      "@typescript-eslint/no-unused-vars": [
        "error",
        {
          argsIgnorePattern: "^_",
          varsIgnorePattern: "^_",
          caughtErrorsIgnorePattern: "^_",
        },
      ],

      "@typescript-eslint/no-explicit-any": "warn",
      "@typescript-eslint/no-empty-object-type": "error",
      "@typescript-eslint/no-non-null-assertion": "warn",
      "@typescript-eslint/consistent-type-imports": "error",

      /*
       * General Code Quality
       */
      "no-console": [
        "warn",
        {
          allow: ["warn", "error"],
        },
      ],

      "no-debugger": "error",
      "no-alert": "error",
      "no-var": "error",
      "prefer-const": "error",
      eqeqeq: ["error", "always", { null: "ignore" }],
      curly: ["error", "all"],

      /*
       * Readability
       */
      "no-nested-ternary": "error",
      "no-unneeded-ternary": "error",

      /*
       * Complexity
       */
      complexity: ["warn", 15],
      "max-depth": ["warn", 4],
      "max-lines-per-function": [
        "warn",
        {
          max: 200,
          skipBlankLines: true,
          skipComments: true,
        },
      ],
    },
  },

  /*
   * Type-aware rules.
   *
   * Scoped to `src` and `app` — exactly tsconfig.json's `include`. Widening this
   * to `**` makes vite.config.ts / vitest.config.ts / postcss.config.ts fail to
   * parse, because they sit outside the TS project.
   */
  {
    files: ["src/**/*.{ts,tsx}", "app/**/*.{ts,tsx}"],

    languageOptions: {
      parser: tseslint.parser,
      parserOptions: {
        project: ["./tsconfig.json"],
        tsconfigRootDir: import.meta.dirname,
      },
    },

    rules: {
      /*
       * Promise safety — the reason this block exists. A floating promise is an
       * unhandled rejection with no error surfaced to the user; an async
       * function in a void-typed slot swallows its rejection entirely.
       */
      "@typescript-eslint/no-floating-promises": "error",
      "@typescript-eslint/no-misused-promises": "error",
      "@typescript-eslint/await-thenable": "error",
      "@typescript-eslint/require-await": "error",

      /*
       * Correctness
       */
      "@typescript-eslint/no-unnecessary-type-assertion": "error",
      "@typescript-eslint/no-base-to-string": "error",

      // `throw redirect(...)` is TanStack Router's documented control flow.
      "@typescript-eslint/only-throw-error": [
        "error",
        {
          allow: [
            {
              from: "package",
              package: "@tanstack/router-core",
              name: ["Redirect"],
            },
          ],
        },
      ],

      /*
       * any-propagation. Warn severity is the editor signal that these are the
       * negotiable ones — `lint` runs with --max-warnings 0, so they still gate.
       */
      "@typescript-eslint/no-unsafe-assignment": "warn",
      "@typescript-eslint/no-unsafe-member-access": "warn",
      "@typescript-eslint/no-unsafe-call": "warn",
      "@typescript-eslint/no-unsafe-return": "warn",
      "@typescript-eslint/no-unsafe-argument": "warn",
    },
  },

  {
    files: ["scripts/**/*.{js,mjs,cjs}"],
    rules: {
      "no-console": "off",
    },
  },

  {
    ignores: [
      "dist/**",
      "build/**",
      "coverage/**",
      "node_modules/**",
      ".tanstack/**",
      "public/**",
    ],
  },
];
