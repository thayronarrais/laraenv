// Workspace-level terminal shortcuts.
//
// These have to be read in the capture phase. xterm turns Ctrl+T and Ctrl+W
// into byte sequences (\x14, \x17) and cancels the keydown — preventDefault
// *and* stopPropagation — so a listener that waits for the event to bubble to
// window never sees either one while a terminal has focus.

export type TerminalHotkey = "new-tab" | "close-tab";

type Chord = Pick<KeyboardEvent, "key" | "ctrlKey" | "altKey" | "metaKey" | "shiftKey">;

// xterm's own input surface. A keystroke landing here is someone typing at a
// terminal, which is exactly when these shortcuts should work — unlike the
// app's real text fields below.
const XTERM_INPUT = "xterm-helper-textarea";

/**
 * Is this keystroke going into a text field of the app itself (renaming a tab,
 * the command palette, a dialog)? Shortcuts must leave those alone.
 */
export function isTypingInFormField(target: EventTarget | null): boolean {
  const el = target as HTMLElement | null;
  if (!el || typeof el.tagName !== "string") return false;
  if (el.classList?.contains(XTERM_INPUT)) return false;
  if (el.isContentEditable) return true;
  return el.tagName === "INPUT" || el.tagName === "TEXTAREA" || el.tagName === "SELECT";
}

/** Which workspace action a keydown stands for, if any. */
export function terminalHotkey(e: Chord, typingInFormField: boolean): TerminalHotkey | null {
  if (typingInFormField) return null;
  if (!e.ctrlKey || e.altKey || e.metaKey || e.shiftKey) return null;
  switch (e.key.toLowerCase()) {
    case "t": return "new-tab";
    case "w": return "close-tab";
    default: return null;
  }
}
