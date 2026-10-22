export type ThemeId = "nebula" | "aurora" | "sunset" | "crimson" | "oceanic" | "midnight";

export type ThemeDef = {
  id: ThemeId;
  // A theme NAME (Nebula, Aurora, …) — a brand, never translated.
  label: string;
  // Catalogue key for the descriptive prose ("Default purple & mint", …).
  // theme.ts is a plain module (no React, no useT()), so — same pattern as
  // commandCenter/builtins.ts's PAGES — the key is stored here and resolved
  // with t() at the render site (Settings.tsx's Appearance tab).
  descriptionKey: string;
  swatch: [string, string, string];
};

export const THEMES: ThemeDef[] = [
  {
    id: "nebula",
    label: "Nebula",
    descriptionKey: "settings.appearance.themes.nebula",
    swatch: ["#7c5cff", "#5cffd1", "#1a0b2e"],
  },
  {
    id: "aurora",
    label: "Aurora",
    descriptionKey: "settings.appearance.themes.aurora",
    swatch: ["#10d995", "#5ce1ff", "#06281f"],
  },
  {
    id: "sunset",
    label: "Sunset",
    descriptionKey: "settings.appearance.themes.sunset",
    swatch: ["#ff8a3d", "#ffd166", "#2e0f0b"],
  },
  {
    id: "crimson",
    label: "Crimson",
    descriptionKey: "settings.appearance.themes.crimson",
    swatch: ["#ff4d6d", "#ff8fab", "#2a0612"],
  },
  {
    id: "oceanic",
    label: "Oceanic",
    descriptionKey: "settings.appearance.themes.oceanic",
    swatch: ["#4d8eff", "#7fdcff", "#0b1c3a"],
  },
  {
    id: "midnight",
    label: "Midnight",
    descriptionKey: "settings.appearance.themes.midnight",
    swatch: ["#9ca3af", "#e5e7eb", "#0a0a0a"],
  },
];

const STORAGE_KEY = "l4e:theme";
const DEFAULT_THEME: ThemeId = "nebula";

const isValidTheme = (v: string | null): v is ThemeId =>
  !!v && THEMES.some((t) => t.id === v);

export function getCurrentTheme(): ThemeId {
  const stored = localStorage.getItem(STORAGE_KEY);
  return isValidTheme(stored) ? stored : DEFAULT_THEME;
}

export function applyTheme(id: ThemeId): void {
  document.documentElement.setAttribute("data-theme", id);
  localStorage.setItem(STORAGE_KEY, id);
}

export function loadTheme(): ThemeId {
  const id = getCurrentTheme();
  document.documentElement.setAttribute("data-theme", id);
  return id;
}
