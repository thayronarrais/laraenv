import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  build: {
    outDir: "dist",
    emptyOutDir: true,
  },
  server: {
    port: 5173,
    strictPort: true,
    // langs/ lives at the repo root so it mirrors the public repo's path
    // exactly and the sync script is a straight copy. The catalogue glob in
    // src/lib/i18n.tsx reaches it from outside the Vite root.
    fs: { allow: [".."] },
  },
});
