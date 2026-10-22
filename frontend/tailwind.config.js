/** @type {import('tailwindcss').Config} */
export default {
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  theme: {
    extend: {
      borderRadius: {
        DEFAULT: "var(--radius-control)",
        md: "var(--radius-control)",
        lg: "var(--radius-panel)",
        xl: "var(--radius-panel)",
        '2xl': "var(--radius-panel)",
        '3xl': "var(--radius-panel)",
      },
      colors: {
        // Preserve the sRGB palette already used by the screens. Tailwind v4's
        // default OKLCH palette changes these colors even with identical classes.
        amber: { 50: "#fffbeb", 100: "#fef3c7", 200: "#fde68a", 300: "#fcd34d", 400: "#fbbf24", 500: "#f59e0b" },
        blue: { 200: "#bfdbfe", 400: "#60a5fa", 500: "#3b82f6" },
        cyan: { 200: "#a5f3fc", 400: "#22d3ee", 500: "#06b6d4" },
        emerald: { 50: "#ecfdf5", 100: "#d1fae5", 200: "#a7f3d0", 300: "#6ee7b7", 400: "#34d399", 500: "#10b981" },
        orange: { 200: "#fed7aa", 300: "#fdba74", 400: "#fb923c", 500: "#f97316" },
        pink: { 300: "#f9a8d4", 400: "#f472b6", 500: "#ec4899" },
        purple: { 200: "#e9d5ff", 300: "#d8b4fe", 400: "#c084fc", 500: "#a855f7" },
        red: { 100: "#fee2e2", 200: "#fecaca", 300: "#fca5a5", 400: "#f87171", 500: "#ef4444" },
        rose: { 300: "#fda4af", 500: "#f43f5e" },
        sky: { 200: "#bae6fd", 300: "#7dd3fc", 400: "#38bdf8", 500: "#0ea5e9" },
        slate: { 400: "#94a3b8", 900: "#0f172a" },
        violet: { 200: "#ddd6fe", 400: "#a78bfa", 500: "#8b5cf6" },
        zinc: { 100: "#f4f4f5", 300: "#d4d4d8", 400: "#a1a1aa", 500: "#71717a", 600: "#52525b", 900: "#18181b" },
        midnight: "var(--c-bg-2)",
        deeppurple: "var(--c-bg-1)",
        accent: "var(--c-accent)",
        accent2: "var(--c-accent2)",
      },
      fontFamily: {
        sans: ["ui-sans-serif", "system-ui", "sans-serif", '"Apple Color Emoji"', '"Segoe UI Emoji"', '"Segoe UI Symbol"', '"Noto Color Emoji"'],
        mono: ["JetBrains Mono", "Menlo", "monospace"],
      },
      // V3 used fixed line heights. Unitless v4 defaults also shrink child
      // labels with arbitrary font sizes, which moves the dashboard badges.
      fontSize: {
        xs: ["0.75rem", { lineHeight: "1rem" }],
        sm: ["0.875rem", { lineHeight: "1.25rem" }],
        base: ["1rem", { lineHeight: "1.5rem" }],
        lg: ["1.125rem", { lineHeight: "1.75rem" }],
        xl: ["1.25rem", { lineHeight: "1.75rem" }],
        '2xl': ["1.5rem", { lineHeight: "2rem" }],
        '3xl': ["1.875rem", { lineHeight: "2.25rem" }],
        '4xl': ["2.25rem", { lineHeight: "2.5rem" }],
      },
      blur: { sm: "4px" },
      backdropBlur: { sm: "4px" },
      boxShadow: { sm: "0 1px 2px 0 rgb(0 0 0 / 0.05)" },
    },
  },
  plugins: [],
};
