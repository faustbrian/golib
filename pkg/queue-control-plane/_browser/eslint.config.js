import eslint from "@eslint/js";
import globals from "globals";

export default [
  {
    ignores: [
      "_browser/node_modules/**",
      "_browser/playwright-report/**",
      "_browser/test-results/**",
    ],
  },
  eslint.configs.recommended,
  {
    files: [
      "_browser/browser-tests/**/*.js",
      "_browser/eslint.config.js",
      "_browser/playwright.config.js",
    ],
    languageOptions: {
      ecmaVersion: "latest",
      globals: globals.node,
      sourceType: "module",
    },
  },
  {
    files: ["ui/assets/**/*.js"],
    languageOptions: {
      ecmaVersion: "latest",
      globals: globals.browser,
      sourceType: "module",
    },
  },
];
