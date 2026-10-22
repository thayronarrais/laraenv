import { loader } from "@monaco-editor/react";

// Monaco ships with the app instead of being fetched from cdn.jsdelivr.net on
// first use. A CDN miss never rejects visibly: @monaco-editor/react keeps
// rendering its "Loading..." placeholder and logs to a console a packaged
// WebView2 build cannot open, which is what issue #30 looked like from the
// outside — "the built-in editor doesn't launch". frontend/public/vs is
// populated by scripts/vendor-monaco.mjs and served at the app root.
export const MONACO_VS_PATH = "/vs";

loader.config({ paths: { vs: MONACO_VS_PATH } });
