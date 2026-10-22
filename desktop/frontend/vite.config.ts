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
    // desktop/langs/ is adjacent to the frontend. The catalogue glob in
    // src/lib/i18n.tsx reaches it from outside the Vite root.
    fs: { allow: [".."] },
  },
});
