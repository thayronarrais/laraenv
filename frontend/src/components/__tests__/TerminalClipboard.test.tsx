// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import React, { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import Terminal from "../Terminal";
import { setTerminalCopyMode } from "../../terminalPrefs";
import { resetTerminalActivity, subscribeInput } from "../agents/terminalActivity";

const bridge = vi.hoisted(() => ({
  read: vi.fn(), write: vi.fn(), writePty: vi.fn(), instances: [] as any[],
}));
vi.mock("../../../wailsjs/runtime", () => ({ EventsOn: vi.fn(), EventsOff: vi.fn() }));
vi.mock("../../../wailsjs/go/main/App", () => ({
  ReadClipboardText: bridge.read,
  WriteClipboardText: bridge.write,
  WritePty: bridge.writePty,
  ResizePty: vi.fn().mockResolvedValue(undefined),
}));
// jsdom cannot render xterm's canvas. This double preserves its input boundary:
// paste emits onData with the terminal's bracketed-paste and newline treatment.
vi.mock("xterm", () => ({
  Terminal: class {
    selection = "";
    textarea = document.createElement("textarea");
    cols = 80;
    rows = 24;
    keyHandler!: (event: KeyboardEvent) => boolean;
    dataHandler!: (data: string) => void;
    focus = vi.fn(() => this.textarea.focus());
    constructor() { bridge.instances.push(this); this.textarea.className = "xterm-helper-textarea"; }
    onData(cb: (data: string) => void) { this.dataHandler = cb; return { dispose() {} }; }
    attachCustomKeyEventHandler(cb: (event: KeyboardEvent) => boolean) { this.keyHandler = cb; }
    open(host: HTMLElement) { const surface = document.createElement("div"); surface.className = "xterm"; surface.append(this.textarea); host.append(surface); }
    paste(text: string) { this.dataHandler(`\x1b[200~${text.replace(/\r?\n/g, "\r")}\x1b[201~`); }
    getSelection() { return this.selection; }
    hasSelection() { return !!this.selection; }
    clearSelection() { this.selection = ""; }
    loadAddon() {}
    write() {}
    dispose() {}
  },
}));
vi.mock("xterm-addon-fit", () => ({ FitAddon: class { fit() {} } }));
vi.mock("xterm-addon-webgl", () => ({ WebglAddon: class { dispose() {} onContextLoss() {} } }));
vi.mock("xterm/css/xterm.css", () => ({}));

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;
vi.stubGlobal("ResizeObserver", class { observe() {} disconnect() {} });
Object.defineProperty(document, "fonts", { value: { load: async () => [] }, configurable: true });

let host: HTMLDivElement;
let root: Root;
let browserRead: ReturnType<typeof vi.fn>;
let browserWrite: ReturnType<typeof vi.fn>;
const term = () => bridge.instances[bridge.instances.length - 1];
async function render(props: Partial<React.ComponentProps<typeof Terminal>> = {}) {
  await act(async () => { root.render(<Terminal id="pty-local" visible {...props} />); });
}
async function rightClick() {
  await act(async () => { host.firstElementChild!.dispatchEvent(new MouseEvent("contextmenu", { bubbles: true, cancelable: true })); });
}
function key(key: string, shiftKey = true) {
  return new KeyboardEvent("keydown", { key, ctrlKey: true, shiftKey, cancelable: true });
}

beforeEach(() => {
  const storage = new Map<string, string>();
  vi.stubGlobal("localStorage", {
    getItem: (key: string) => storage.get(key) ?? null,
    setItem: (key: string, value: string) => storage.set(key, value),
    clear: () => storage.clear(),
  });
  vi.clearAllMocks();
  vi.useFakeTimers();
  bridge.instances.length = 0;
  bridge.read.mockResolvedValue("first\nsecond");
  bridge.write.mockResolvedValue(undefined);
  bridge.writePty.mockResolvedValue(undefined);
  browserRead = vi.fn().mockRejectedValue(new Error("browser clipboard unavailable"));
  browserWrite = vi.fn().mockRejectedValue(new Error("browser clipboard unavailable"));
  Object.defineProperty(navigator, "clipboard", { value: { readText: browserRead, writeText: browserWrite }, configurable: true });
  localStorage.clear();
  setTerminalCopyMode("mouse");
  resetTerminalActivity();
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});
afterEach(async () => {
  await act(async () => { root.unmount(); });
  host.remove();
  vi.useRealTimers();
  resetTerminalActivity();
});

describe("terminal clipboard", () => {
  it("copies a right-click selection through the native bridge when browser access fails", async () => {
    await render(); term().selection = "agent output";
    await rightClick();
    expect(bridge.write).toHaveBeenCalledWith("agent output");
    expect(term().selection).toBe("");
  });
  it("keeps selected text and reports a failed copy so it can be retried", async () => {
    bridge.write.mockRejectedValue(new Error("clipboard busy"));
    const onError = vi.fn();
    await render({ onError } as any); term().selection = "keep this";
    await rightClick();
    expect(term().selection).toBe("keep this");
    expect(onError).toHaveBeenCalledWith("copy", expect.anything());
  });
  it.each(["pty-local", "pty-ssh"])("pastes into %s using terminal transformations and activity tracking", async (id) => {
    const input = vi.fn(); subscribeInput(input);
    await render({ id });
    await rightClick();
    expect(bridge.writePty).toHaveBeenCalledTimes(1);
    expect(bridge.writePty).toHaveBeenCalledWith(id, "\x1b[200~first\rsecond\x1b[201~");
    expect(input).toHaveBeenCalledWith(id);
  });
  it("falls back to browser clipboard when the native read is unavailable", async () => {
    bridge.read.mockRejectedValue(new Error("not running in desktop"));
    browserRead.mockResolvedValue("browser text");
    await render(); await rightClick();
    expect(bridge.writePty).toHaveBeenCalledTimes(1);
    expect(bridge.writePty).toHaveBeenCalledWith("pty-local", "\x1b[200~browser text\x1b[201~");
  });
  it("falls back to browser copy and clears the selection only after success", async () => {
    bridge.write.mockRejectedValue(new Error("native clipboard busy"));
    browserWrite.mockResolvedValue(undefined);
    await render(); term().selection = "copied by browser"; await rightClick();
    expect(browserWrite).toHaveBeenCalledWith("copied by browser");
    expect(term().selection).toBe("");
  });
  it("does not erase a new selection made while the previous copy was pending", async () => {
    let finish!: () => void;
    bridge.write.mockReturnValue(new Promise<void>((resolve) => { finish = resolve; }));
    await render(); term().selection = "old selection"; await rightClick();
    term().selection = "new selection";
    await act(async () => { finish(); });
    expect(term().selection).toBe("new selection");
  });
  it("does not send terminal input for an empty clipboard", async () => {
    bridge.read.mockResolvedValue("");
    await render(); await rightClick();
    expect(bridge.writePty).not.toHaveBeenCalled();
  });
  it("reports a session write failure instead of silently losing pasted text", async () => {
    bridge.writePty.mockRejectedValue(new Error("session disconnected"));
    const onError = vi.fn(); await render({ onError }); await rightClick();
    expect(onError).toHaveBeenCalledWith("input", expect.anything());
  });
  it("does not send a paste after the terminal has been disposed", async () => {
    let finish!: (text: string) => void;
    bridge.read.mockReturnValue(new Promise<string>((resolve) => { finish = resolve; }));
    await render(); await rightClick();
    await act(async () => { root.unmount(); });
    await act(async () => { finish("late clipboard"); });
    expect(bridge.writePty).not.toHaveBeenCalled();
  });
  it("keeps clipboard keyboard shortcuts working in mouse mode and cancels the browser default", async () => {
    await render(); const event = key("V");
    await act(async () => { expect(term().keyHandler(event)).toBe(false); });
    expect(event.defaultPrevented).toBe(true);
    expect(bridge.writePty).toHaveBeenCalledTimes(1);
    expect(bridge.writePty).toHaveBeenCalledWith("pty-local", "\x1b[200~first\rsecond\x1b[201~");
  });
  it("leaves plain Ctrl+C available to interrupt the running command", async () => {
    await render(); term().selection = "output";
    expect(term().keyHandler(key("c", false))).toBe(true);
    expect(bridge.write).not.toHaveBeenCalled();
  });
  it("reports a clipboard read failure", async () => {
    bridge.read.mockRejectedValue(new Error("busy")); const onError = vi.fn();
    await render({ onError } as any); await rightClick();
    expect(onError).toHaveBeenCalledWith("paste", expect.anything());
    expect(bridge.writePty).not.toHaveBeenCalled();
  });
});

describe("terminal focus", () => {
  it("does not focus a background pane after fonts finish loading or timers run", async () => {
    await render({ focused: false } as any);
    await act(async () => { await vi.runAllTimersAsync(); });
    expect(term().focus).not.toHaveBeenCalled();
  });
  it("focuses the selected pane when selection changes inside the same tab", async () => {
    await render({ focused: false } as any); await act(async () => { await vi.runAllTimersAsync(); });
    term().focus.mockClear();
    await render({ focused: true } as any); await act(async () => { await vi.runAllTimersAsync(); });
    expect(term().focus).toHaveBeenCalled();
  });
  it("does not steal focus from a field being edited", async () => {
    await render({ focused: false } as any); await act(async () => { await vi.runAllTimersAsync(); });
    term().focus.mockClear();
    const field = document.createElement("input"); document.body.append(field); field.focus();
    await render({ focused: true } as any); await act(async () => { await vi.runAllTimersAsync(); });
    expect(document.activeElement).toBe(field);
    expect(term().focus).not.toHaveBeenCalled(); field.remove();
  });
});
