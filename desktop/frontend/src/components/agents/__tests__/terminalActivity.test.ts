// @vitest-environment jsdom
import { describe, it, expect, beforeEach, vi } from "vitest";
import { noteTerminalInput, isUserTyping, isEditingElsewhere, subscribeEnter, subscribeInput, resetTerminalActivity, TYPING_SILENCE_MS } from "../terminalActivity";

describe("terminalActivity", () => {
  beforeEach(() => resetTerminalActivity());

  it("printable input marks the user as typing until Enter", () => {
    noteTerminalInput("term-1", "y", 1000);
    expect(isUserTyping(1500)).toBe(true);
    noteTerminalInput("term-1", "\r", 1600);
    expect(isUserTyping(1700)).toBe(false);
  });

  it("silence longer than the threshold releases the typing state", () => {
    noteTerminalInput("term-1", "abc", 1000);
    expect(isUserTyping(1000 + TYPING_SILENCE_MS - 1)).toBe(true);
    expect(isUserTyping(1000 + TYPING_SILENCE_MS)).toBe(false);
  });

  it("escape sequences alone do not start a pending line", () => {
    noteTerminalInput("term-1", "\x1b[A", 1000);
    expect(isUserTyping(1100)).toBe(false);
    noteTerminalInput("term-1", "\x7f", 1200); // backspace counts as editing
    expect(isUserTyping(1300)).toBe(true);
  });

  it.each(["echo hello", "first\rsecond\nthird\r\n"])(
    "keeps bracketed paste %j pending until an actual Enter",
    (text) => {
      const inputs: string[] = [];
      const enters: string[] = [];
      subscribeInput((id) => inputs.push(id));
      subscribeEnter((id) => enters.push(id));

      noteTerminalInput("term-1", `\x1b[200~${text}\x1b[201~`, 1000);

      expect(inputs).toEqual(["term-1"]);
      expect(enters).toEqual([]);
      expect(isUserTyping(1100)).toBe(true);

      noteTerminalInput("term-1", "\r", 1200);

      expect(isUserTyping(1300)).toBe(false);
      expect(inputs).toEqual(["term-1", "term-1"]);
      expect(enters).toEqual(["term-1"]);
    },
  );

  it("refreshes the typing timeout after a bracketed paste", () => {
    noteTerminalInput("term-1", "x", 1000);
    noteTerminalInput("term-1", "\x1b[200~hello\x1b[201~", 3500);
    expect(isUserTyping(3500 + TYPING_SILENCE_MS - 1)).toBe(true);
    expect(isUserTyping(3500 + TYPING_SILENCE_MS)).toBe(false);
  });

  it("does not start a pending line for empty bracketed paste", () => {
    noteTerminalInput("term-1", "\x1b[200~\x1b[201~", 1000);
    expect(isUserTyping(1100)).toBe(false);
  });

  it.each([
    "\x1b[200~hello",
    "\x1b[201~",
    "\x1b[200~hello\x1b[201~trailing",
  ])("ignores %j as an escape sequence without a complete paste envelope", (data) => {
    noteTerminalInput("term-1", data, 1000);
    expect(isUserTyping(1100)).toBe(false);
  });

  it("notifies Enter listeners with the terminal id and supports unsubscribe", () => {
    const seen: string[] = [];
    const off = subscribeEnter((id) => seen.push(id));
    noteTerminalInput("term-7", "ok\r", 1000);
    noteTerminalInput("term-8", "\n", 1001);
    off();
    noteTerminalInput("term-9", "\r", 1002);
    expect(seen).toEqual(["term-7", "term-8"]);
  });

  it("uses Date.now when no timestamp is given", () => {
    vi.spyOn(Date, "now").mockReturnValue(5000);
    noteTerminalInput("term-1", "x");
    expect(isUserTyping()).toBe(true);
    vi.restoreAllMocks();
  });

  it("reports editing elsewhere when a non-terminal input has focus", () => {
    const input = document.createElement("input");
    document.body.appendChild(input);
    input.focus();
    expect(isEditingElsewhere(document)).toBe(true);
    document.body.removeChild(input);
  });

  it("does not report editing elsewhere for xterm's own helper textarea", () => {
    const xterm = document.createElement("div");
    xterm.className = "xterm";
    const textarea = document.createElement("textarea");
    xterm.appendChild(textarea);
    document.body.appendChild(xterm);
    textarea.focus();
    expect(isEditingElsewhere(document)).toBe(false);
    document.body.removeChild(xterm);
  });

  it("treats contenteditable elements outside xterm as editing too", () => {
    const div = document.createElement("div");
    div.setAttribute("contenteditable", "true");
    div.tabIndex = 0;
    document.body.appendChild(div);
    div.focus();
    expect(isEditingElsewhere(document)).toBe(true);
    document.body.removeChild(div);
  });

  it("is false when nothing or a non-input element has focus", () => {
    expect(isEditingElsewhere(document)).toBe(false);
  });

  it("notifies input listeners on every keystroke, including Enter", () => {
    const seen: string[] = [];
    const off = subscribeInput((id) => seen.push(id));
    noteTerminalInput("term-1", "y", 1000);
    noteTerminalInput("term-1", "\r", 1001);
    off();
    noteTerminalInput("term-1", "z", 1002);
    expect(seen).toEqual(["term-1", "term-1"]);
  });
});
