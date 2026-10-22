// @vitest-environment jsdom
// A pane whose PTY is gone used to stay on screen: `exit` in a shell, or
// stopping an agent session from the Agents page, left a window that looked
// alive and answered nothing. Both reach the frontend the same way — the
// backend emits term:exit:<id> when the pump's Read fails, which is what
// closing the PTY causes — and the component only painted "[pty exited: …]"
// into the scrollback. Nothing told the workspace to drop the pane.
import { describe, expect, it, vi } from "vitest";
import React, { act } from "react";
import { createRoot } from "react-dom/client";

const terminalWrites = vi.hoisted(() => vi.fn());
const listeners = new Map<string, (payload: string) => void>();

vi.mock("../../../wailsjs/runtime", () => ({
  EventsOn: (evt: string, cb: (payload: string) => void) => { listeners.set(evt, cb); },
  EventsOff: (evt: string) => { listeners.delete(evt); },
}));
vi.mock("../../../wailsjs/go/main/App", () => ({
  WritePty: async () => {},
  ResizePty: async () => {},
}));
vi.mock("xterm", () => ({
  Terminal: class {
    onData() { return { dispose() {} }; }
    loadAddon() {}
    open() {}
    write(value: string) { terminalWrites(value); }
    focus() {}
    dispose() {}
    attachCustomKeyEventHandler() {}
    getSelection() { return ""; }
    hasSelection() { return false; }
    clearSelection() {}
    element = document.createElement("div");
    textarea = document.createElement("textarea");
    cols = 80;
    rows = 24;
  },
}));
vi.mock("xterm-addon-fit", () => ({ FitAddon: class { fit() {} proposeDimensions() { return { cols: 80, rows: 24 }; } dispose() {} } }));
vi.mock("xterm-addon-webgl", () => ({ WebglAddon: class { dispose() {} onContextLoss() { return { dispose() {} }; } } }));
vi.mock("xterm/css/xterm.css", () => ({}));

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;
// jsdom ships no ResizeObserver, and the component observes its host to refit.
(globalThis as any).ResizeObserver = class {
  observe() {}
  unobserve() {}
  disconnect() {}
};
// It ships no FontFaceSet either; the component waits on it before open().
Object.defineProperty(document, "fonts", { value: { load: async () => [] }, configurable: true });

describe("Terminal", () => {
  it("tells its owner when the PTY behind it is gone", async () => {
    terminalWrites.mockClear();
    const { default: Terminal } = await import("../Terminal");
    const onExit = vi.fn();
    const host = document.createElement("div");
    document.body.appendChild(host);
    const root = createRoot(host);

    await act(async () => {
      root.render(<Terminal id="pty-7" visible onExit={onExit} />);
    });

    const fire = listeners.get("term:exit:pty-7");
    expect(fire, "the component must subscribe to its own exit channel").toBeTypeOf("function");

    const writesBeforeExit = terminalWrites.mock.calls.length;
    act(() => { fire!("The pipe has been ended."); });

    expect(onExit).toHaveBeenCalledTimes(1);
    expect(onExit).toHaveBeenCalledWith("The pipe has been ended.");
    expect(terminalWrites).toHaveBeenCalledTimes(writesBeforeExit);

    await act(async () => { root.unmount(); });
    host.remove();
  });
});
