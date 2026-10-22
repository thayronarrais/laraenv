import { describe, expect, it } from "vitest";
import { tabbyTheme, TERMINAL_BG, TERMINAL_FONT } from "../tabbyTheme";

describe("tabbyTheme", () => {
  it("matches the Tabby Default scheme from the design spec", () => {
    expect(TERMINAL_BG).toBe("#171717");
    expect(tabbyTheme.background).toBe("#171717");
    expect(tabbyTheme.foreground).toBe("#cacaca");
    expect(tabbyTheme.cursor).toBe("#bbbbbb");
    expect(tabbyTheme.cursorAccent).toBe("#171717");
    expect(tabbyTheme.selectionBackground).toBe("rgba(255,255,255,0.25)");
    expect([
      tabbyTheme.black, tabbyTheme.red, tabbyTheme.green, tabbyTheme.yellow,
      tabbyTheme.blue, tabbyTheme.magenta, tabbyTheme.cyan, tabbyTheme.white,
    ]).toEqual(["#000000", "#ff615a", "#b1e969", "#ebd99c", "#5da9f6", "#e86aff", "#82fff7", "#dedacf"]);
    expect([
      tabbyTheme.brightBlack, tabbyTheme.brightRed, tabbyTheme.brightGreen, tabbyTheme.brightYellow,
      tabbyTheme.brightBlue, tabbyTheme.brightMagenta, tabbyTheme.brightCyan, tabbyTheme.brightWhite,
    ]).toEqual(["#313131", "#f58c80", "#ddf88f", "#eee5b2", "#a5c7ff", "#ddaaff", "#b7fff9", "#ffffff"]);
  });

  it("uses Tabby metrics with the bundled font", () => {
    expect(TERMINAL_FONT.fontFamily).toContain("JetBrains Mono");
    expect(TERMINAL_FONT.fontSize).toBe(14);
    expect(TERMINAL_FONT.lineHeight).toBe(1.0);
    expect(TERMINAL_FONT.letterSpacing).toBe(0);
    expect(TERMINAL_FONT.fontWeight).toBe(400);
    expect(TERMINAL_FONT.fontWeightBold).toBe(700);
  });
});
