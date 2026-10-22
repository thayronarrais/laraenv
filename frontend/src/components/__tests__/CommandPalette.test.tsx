// @vitest-environment jsdom
// Regression: toggling the palette open after a closed render must not change
// the hook count ("Rendered more hooks than during the previous render"), which
// unmounts the whole React tree and blanks the window (Ctrl+K bug).
import { describe, it, expect, vi } from "vitest";
import { fireEvent, render, screen, cleanup } from "@testing-library/react";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import CommandPalette from "../CommandPalette";
import type { LauncherData } from "../../commandCenter/launcher";
import { I18nProvider } from "../../lib/i18n";
import { buildOpenTerminalCommands } from "../../commandCenter/builtins";

// CommandPalette now calls useT(), which needs I18nProvider's bindings mocked
// the same way every other i18n-aware component test does — otherwise
// GetLanguage() throws synchronously outside a real Wails window.
vi.mock("../../../wailsjs/go/main/App", () => ({
  GetLanguage: async () => "en",
  SetLanguage: async () => {},
  ListServices: vi.fn(), ListProjects: vi.fn(),
  StartService: vi.fn(), StopService: vi.fn(), ReloadService: vi.fn(),
  StartManagedAll: vi.fn(), StopManagedAll: vi.fn(), ReloadStack: vi.fn(),
  OpenProjectFolder: vi.fn(), OpenProjectInEditor: vi.fn(), OpenURL: vi.fn(), OpenProjectTerminal: vi.fn(),
}));
vi.mock("../../../wailsjs/runtime", () => ({ EventsOn: () => () => {} }));

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

const launcher: LauncherData = {
  projects: [],
  sshHosts: [],
  agentProviders: [],
  getProjectCommands: () => null,
  ensureProjectCommands: () => {},
  runInTerminal: () => {},
  openSSH: () => {},
  launchAgent: () => {},
};

describe("CommandPalette", () => {
  it("finds an open SSH terminal and switches to it with Enter", async () => {
    const focusTerminal = vi.fn();
    const onClose = vi.fn();
    const cmds = buildOpenTerminalCommands({
      t: (key) => key,
      focusTerminal,
      openTerminals: [
        { terminalId: "local", label: "demo · Git Bash", hint: "demo · Git Bash", active: true },
        { terminalId: "ssh", label: "ssh · contaboo", hint: "SSH · contaboo", active: false },
      ],
    });
    // jsdom lacks this browser method used by the palette's selection scroll.
    const previousScroll = Object.getOwnPropertyDescriptor(HTMLElement.prototype, "scrollIntoView");
    Object.defineProperty(HTMLElement.prototype, "scrollIntoView", { configurable: true, value: vi.fn() });
    const windowFocus = vi.spyOn(window, "focus").mockImplementation(() => {});
    try {
      render(<I18nProvider><CommandPalette open commands={cmds} launcher={{ ...launcher, t: (key) => key }} onClose={onClose} /></I18nProvider>);
      fireEvent.change(screen.getByRole("textbox"), { target: { value: "contaboo" } });
      expect(screen.getByRole("button", { name: /ssh · contaboo/ })).toBeTruthy();
      expect(screen.queryByRole("button", { name: /demo · Git Bash/ })).toBeNull();
      expect(screen.getByText("Enter Switch")).toBeTruthy();
      fireEvent.keyDown(window, { key: "Enter" });
      expect(onClose).toHaveBeenCalledOnce();
      expect(focusTerminal).toHaveBeenCalledWith("ssh");
    } finally {
      cleanup(); windowFocus.mockRestore();
      if (previousScroll) Object.defineProperty(HTMLElement.prototype, "scrollIntoView", previousScroll);
      else Reflect.deleteProperty(HTMLElement.prototype, "scrollIntoView");
    }
  });

  it("renders closed → open without violating hook order", () => {
    const host = document.createElement("div");
    document.body.appendChild(host);
    const root = createRoot(host);
    const render = (open: boolean) =>
      act(() => {
        root.render(
          <I18nProvider>
            <CommandPalette open={open} commands={[]} launcher={launcher} onClose={() => {}} />
          </I18nProvider>,
        );
      });

    render(false);
    expect(() => render(true)).not.toThrow();
    // and toggling back and forth stays stable
    expect(() => render(false)).not.toThrow();
    expect(() => render(true)).not.toThrow();
    act(() => root.unmount());
  });
});
