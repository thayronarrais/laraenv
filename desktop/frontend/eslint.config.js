// Focused lint: only the React hooks correctness rules. rules-of-hooks catches
// the "hooks after an early return" class of bug that blanks the whole window
// (no ErrorBoundary can prevent the report, only soften it). Style rules are
// intentionally left out.
import tseslint from "typescript-eslint";
import reactHooks from "eslint-plugin-react-hooks";

export default tseslint.config(
  {
    ignores: ["dist/**", "node_modules/**", "../wailsjs/**"],
  },
  {
    files: ["src/**/*.{ts,tsx}"],
    languageOptions: {
      parser: tseslint.parser,
    },
    plugins: {
      "react-hooks": reactHooks,
    },
    rules: {
      "react-hooks/rules-of-hooks": "error",
      "react-hooks/exhaustive-deps": "warn",
    },
  },
);
