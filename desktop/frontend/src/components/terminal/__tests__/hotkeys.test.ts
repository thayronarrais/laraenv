// @vitest-environment jsdom
import { describe, expect, it } from "vitest";
import { isTypingInFormField, terminalHotkey } from "../hotkeys";

// Ctrl+T and Ctrl+W were wired to a bubble-phase listener on window. xterm
// turns both into byte sequences (\x14 and \x17) and calls stopPropagation on
// the keydown, so while a terminal had focus — which is always, that is where
// you are typing — the event died at xterm's textarea and neither shortcut
// ever reached the workspace.

const ctrl = (key: string, over: Partial<KeyboardEvent> = {}) =>
  ({ key, ctrlKey: true, altKey: false, metaKey: false, shiftKey: false, ...over });

describe("terminalHotkey", () => {
  it("maps Ctrl+T to a new tab and Ctrl+W to closing one", () => {
    expect(terminalHotkey(ctrl("t"), false)).toBe("new-tab");
    expect(terminalHotkey(ctrl("w"), false)).toBe("close-tab");
  });

  it("accepts the uppercase key a shell may report", () => {
    expect(terminalHotkey(ctrl("T"), false)).toBe("new-tab");
  });

  it("ignores other modifiers so shell and app bindings stay distinct", () => {
    expect(terminalHotkey(ctrl("w", { shiftKey: true }), false)).toBeNull();
    expect(terminalHotkey(ctrl("w", { altKey: true }), false)).toBeNull();
    expect(terminalHotkey({ key: "w", ctrlKey: false, altKey: false, metaKey: false, shiftKey: false }, false)).toBeNull();
  });

  it("stays out of the way while text is being typed into a field", () => {
    // Renaming a tab is an <input> on top of the terminal: Ctrl+W there must
    // not close the very tab being renamed.
    expect(terminalHotkey(ctrl("w"), true)).toBeNull();
    expect(terminalHotkey(ctrl("t"), true)).toBeNull();
  });
});

describe("isTypingInFormField", () => {
  const el = (html: string) => {
    const host = document.createElement("div");
    host.innerHTML = html;
    return host.firstElementChild as HTMLElement;
  };

  it("recognises the fields a shortcut must not steal from", () => {
    expect(isTypingInFormField(el("<input />"))).toBe(true);
    expect(isTypingInFormField(el("<textarea></textarea>"))).toBe(true);
    expect(isTypingInFormField(el("<select></select>"))).toBe(true);
    const editable = el("<div></div>");
    Object.defineProperty(editable, "isContentEditable", { value: true });
    expect(isTypingInFormField(editable)).toBe(true);
  });

  it("does not count xterm's own textarea — that IS the terminal", () => {
    expect(isTypingInFormField(el('<textarea class="xterm-helper-textarea"></textarea>'))).toBe(false);
  });

  it("treats anything else, and nothing at all, as not typing", () => {
    expect(isTypingInFormField(el("<div></div>"))).toBe(false);
    expect(isTypingInFormField(null)).toBe(false);
  });
});
