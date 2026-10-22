export type TerminalCopyMode = "keyboard" | "mouse";

const KEY = "l4e:terminal:copyMode";
const EVT = "l4e:terminal:copyMode:change";

export function getTerminalCopyMode(): TerminalCopyMode {
  const v = localStorage.getItem(KEY);
  return v === "mouse" ? "mouse" : "keyboard";
}

export function setTerminalCopyMode(mode: TerminalCopyMode): void {
  localStorage.setItem(KEY, mode);
  window.dispatchEvent(new CustomEvent<TerminalCopyMode>(EVT, { detail: mode }));
}

export function onTerminalCopyModeChange(
  cb: (m: TerminalCopyMode) => void
): () => void {
  const handler = (e: Event) => cb((e as CustomEvent<TerminalCopyMode>).detail);
  window.addEventListener(EVT, handler);
  return () => window.removeEventListener(EVT, handler);
}
