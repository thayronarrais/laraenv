// The "Tabby Default" color scheme, extracted verbatim from Tabby's
// tabby-terminal/src/colorSchemes.ts, plus LaraEnv's font metrics.
// See docs/workspace.md for the terminal integration.
import type { ITheme } from "xterm";

export const TERMINAL_BG = "#171717";

export const tabbyTheme: ITheme = {
  background: TERMINAL_BG,
  foreground: "#cacaca",
  cursor: "#bbbbbb",
  cursorAccent: TERMINAL_BG,
  selectionBackground: "rgba(255,255,255,0.25)",
  black: "#000000",
  red: "#ff615a",
  green: "#b1e969",
  yellow: "#ebd99c",
  blue: "#5da9f6",
  magenta: "#e86aff",
  cyan: "#82fff7",
  white: "#dedacf",
  brightBlack: "#313131",
  brightRed: "#f58c80",
  brightGreen: "#ddf88f",
  brightYellow: "#eee5b2",
  brightBlue: "#a5c7ff",
  brightMagenta: "#ddaaff",
  brightCyan: "#b7fff9",
  brightWhite: "#ffffff",
};

// Tabby uses 14px with zero line padding; the font itself stays our bundled
// JetBrains Mono (Tabby's Windows default is Consolas — ours is nicer).
export const TERMINAL_FONT = {
  fontFamily: '"JetBrains Mono", Menlo, monospace',
  fontSize: 14,
  fontWeight: 400,
  fontWeightBold: 700,
  lineHeight: 1.0,
  letterSpacing: 0,
} as const;
