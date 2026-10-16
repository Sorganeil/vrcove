import js from "@eslint/js";
import { defineConfig, globalIgnores } from "eslint/config";
import reactHooks from "eslint-plugin-react-hooks";
import globals from "globals";
import tseslint from "typescript-eslint";

export default defineConfig([
  globalIgnores(["web/dist/**", "node_modules/**", "work/**", "scripts/**"]),
  js.configs.recommended,
  ...tseslint.configs.recommended,
  reactHooks.configs.flat["recommended-latest"],
  {
    files: ["**/*.{ts,tsx}"],
    languageOptions: { globals: globals.browser },
    rules: {
      // The app parses VRChat JSON loosely and syncs state from props in effects.
      "@typescript-eslint/no-explicit-any": "off",
      "react-hooks/set-state-in-effect": "warn",
    },
  },
]);
