// Tracks whether the user is mid-command in any LaraEnv terminal so the
// agent auto-switch never yanks focus while they are typing.
export const TYPING_SILENCE_MS = 3000;

type EnterListener = (terminalId: string) => void;
type InputListener = (terminalId: string) => void;

let lastKeyAt = 0;
let pendingLine = false;
const enterListeners = new Set<EnterListener>();
const inputListeners = new Set<InputListener>();

const ESCAPE_PREFIX = "\x1b";
const BRACKETED_PASTE_START = "\x1b[200~";
const BRACKETED_PASTE_END = "\x1b[201~";

export function noteTerminalInput(terminalId: string, data: string, now: number = Date.now()): void {
  if (!data) return;
  lastKeyAt = now;
  for (const listener of inputListeners) listener(terminalId);
  if (data.startsWith(BRACKETED_PASTE_START) && data.endsWith(BRACKETED_PASTE_END)) {
    // The shell inserts bracketed paste as editable text, even when it has line breaks.
    if (data.length > BRACKETED_PASTE_START.length + BRACKETED_PASTE_END.length) pendingLine = true;
    return;
  }
  if (data.includes("\r") || data.includes("\n")) {
    pendingLine = false;
    for (const listener of enterListeners) listener(terminalId);
    return;
  }
  if (data.startsWith(ESCAPE_PREFIX)) return; // cursor keys, function keys: not text
  pendingLine = true;
}

export function isUserTyping(now: number = Date.now()): boolean {
  return pendingLine && now - lastKeyAt < TYPING_SILENCE_MS;
}

export function subscribeEnter(listener: EnterListener): () => void {
  enterListeners.add(listener);
  return () => { enterListeners.delete(listener); };
}

// Fires on every keystroke in any terminal, including Enter - unlike
// subscribeEnter, which only fires on line-ending input. Used to release a
// promoted-but-not-yet-answered auto-switch session on single-key answers
// (e.g. "y") that never produce Enter.
export function subscribeInput(listener: InputListener): () => void {
  inputListeners.add(listener);
  return () => { inputListeners.delete(listener); };
}

// True when the user is focused on a non-terminal input (search box, command
// palette, rename dialog, a Settings field, ...). xterm keeps its own hidden
// helper textarea inside a `.xterm` container to capture keystrokes; that one
// must not count, or the auto-switch would never advance while a terminal has
// focus.
export function isEditingElsewhere(doc: Document = document): boolean {
  const active = doc.activeElement as HTMLElement | null;
  if (!active) return false;
  const isEditable = active.tagName === "INPUT" || active.tagName === "TEXTAREA"
    || active.isContentEditable || active.getAttribute("contenteditable") === "true";
  if (!isEditable) return false;
  return active.closest(".xterm") === null;
}

export function resetTerminalActivity(): void {
  lastKeyAt = 0;
  pendingLine = false;
  enterListeners.clear();
  inputListeners.clear();
}
