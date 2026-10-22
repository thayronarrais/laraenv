// Hotkey parsing/formatting. Hotkeys are stored as canonical strings like
// "Ctrl+Shift+K". formatHotkey turns a live KeyboardEvent into that string (used
// by the "press a key" capture input); matchesHotkey compares an event against a
// stored hotkey, order- and case-insensitively.

function normKey(key: string): string {
  if (key === " " || key === "Spacebar") return "Space";
  if (key.length === 1) return key.toUpperCase();
  return key; // "Enter", "ArrowUp", "F5", etc.
}

// Canonicalize any "+"-joined hotkey string into a stable modifier order.
export function canonicalHotkey(h: string): string {
  if (!h) return "";
  const mods = { Ctrl: false, Alt: false, Shift: false, Meta: false };
  let key = "";
  for (const raw of h.split("+")) {
    const p = raw.trim();
    if (!p) continue;
    const lp = p.toLowerCase();
    if (lp === "ctrl" || lp === "control") mods.Ctrl = true;
    else if (lp === "alt") mods.Alt = true;
    else if (lp === "shift") mods.Shift = true;
    else if (lp === "meta" || lp === "cmd" || lp === "command" || lp === "win") mods.Meta = true;
    else key = normKey(p);
  }
  const out: string[] = [];
  if (mods.Ctrl) out.push("Ctrl");
  if (mods.Alt) out.push("Alt");
  if (mods.Shift) out.push("Shift");
  if (mods.Meta) out.push("Meta");
  if (key) out.push(key);
  return out.join("+");
}

// Turn a KeyboardEvent into a canonical hotkey string, or null if only a
// modifier key was pressed (so capture inputs keep waiting for a real key).
export function formatHotkey(e: KeyboardEvent): string | null {
  const key = e.key;
  if (key === "Control" || key === "Shift" || key === "Alt" || key === "Meta") return null;
  const parts: string[] = [];
  if (e.ctrlKey) parts.push("Ctrl");
  if (e.altKey) parts.push("Alt");
  if (e.shiftKey) parts.push("Shift");
  if (e.metaKey) parts.push("Meta");
  parts.push(normKey(key));
  return parts.join("+");
}

export function matchesHotkey(e: KeyboardEvent, hotkey: string): boolean {
  if (!hotkey) return false;
  const fmt = formatHotkey(e);
  if (!fmt) return false;
  return canonicalHotkey(fmt) === canonicalHotkey(hotkey);
}
