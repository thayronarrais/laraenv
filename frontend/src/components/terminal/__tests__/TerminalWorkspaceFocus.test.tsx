// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { serializeWorkspace } from "../serialize";
import { group, leaf, tab, ws } from "./helpers";
import type { OpenProjectRequest } from '../types';

// Keep the workspace, groups, split panes and Terminal component real. Only
// the native PTY bridge and xterm renderer need substitutes in jsdom.
const bridge = vi.hoisted(() => ({
  load: vi.fn(),
  focusAgent: vi.fn(),
  shell: vi.fn(),
  stop: vi.fn(),
  projectCommand: vi.fn(),
  write: vi.fn(),
  ssh: vi.fn(),
  launch: vi.fn(),
  resume: vi.fn(),
  save: vi.fn(),
  drainAttach: vi.fn(),
  profiles: vi.fn(),
}));
const events = vi.hoisted(() => ({ listeners: new Map<string, (seed: string) => void>() }));
vi.mock("../../../../wailsjs/go/main/App", () => ({
  StartPtyKind: (...args: unknown[]) => bridge.shell(...args),
  StartPtyKindIn: vi.fn(), StartPtyKindRuntime: vi.fn(), StartPtyForProject: vi.fn(), StartPtyForSSH: (...args: unknown[]) => bridge.ssh(...args),
  StartPtyForProjectCommand: (...args: unknown[]) => bridge.projectCommand(...args),
  StopPty: (...args: unknown[]) => bridge.stop(...args), WritePty: (...args: unknown[]) => bridge.write(...args), ResizePty: vi.fn(),
  ListTerminals: (...args: unknown[]) => bridge.profiles(...args),
  ListPhpVersions: async () => [], ListNodeVersions: async () => [], SetDefaultTerminal: vi.fn(), SaveTerminalWorkspace: (...args: unknown[]) => bridge.save(...args),
  LoadTerminalWorkspace: (...args: unknown[]) => bridge.load(...args),
  DetachWorkspace: vi.fn(), GetDetachedSeed: vi.fn(), SendTerminalAttach: vi.fn(), DrainTerminalAttach: (...args: unknown[]) => bridge.drainAttach(...args),
  LaunchAgentSession: (...args: unknown[]) => bridge.launch(...args), ResumeAgentSession: (...args: unknown[]) => bridge.resume(...args),
  StopAgentSession: vi.fn(), ConfigureWorktreeRouting: vi.fn(), ListProjectRoutes: async () => [],
  SetFocusedAgentSession: (...args: unknown[]) => bridge.focusAgent(...args), ListAgentSessions: async () => [],
}));
vi.mock("../../../../wailsjs/runtime", () => ({
  EventsOn: (name: string, callback: (seed: string) => void) => { events.listeners.set(name, callback); return () => events.listeners.delete(name); },
  EventsOff: (name: string) => events.listeners.delete(name), Quit: vi.fn(),
  ClipboardGetText: async () => "", ClipboardSetText: async () => {},
}));
vi.mock("../../SFTPModal", () => ({ default: () => null }));
vi.mock("../../../lib/i18n", () => {
  const translate = (key: string, vars?: Record<string, string | number>) => key === "terminal.workspace.paneNumber" ? `Pane ${vars?.number}` : key;
  return { useT: () => translate };
});
vi.mock("../../../terminalPrefs", () => ({ getTerminalCopyMode: () => "keyboard", onTerminalCopyModeChange: () => () => {} }));

const terminals: Array<{ textarea: HTMLTextAreaElement }> = [];
vi.mock("xterm", () => ({
  Terminal: class {
    textarea = document.createElement("textarea");
    element: HTMLElement | undefined;
    cols = 80;
    rows = 24;
    constructor() { this.textarea.className = "xterm-helper-textarea"; terminals.push(this); }
    open(host: HTMLElement) {
      this.element = document.createElement("div");
      this.element.className = "xterm";
      this.element.appendChild(this.textarea);
      host.appendChild(this.element);
    }
    focus() { this.textarea.focus(); }
    blur() { this.textarea.blur(); }
    onData() { return { dispose() {} }; }
    loadAddon() {}
    write() {}
    dispose() {}
    attachCustomKeyEventHandler() {}
    getSelection() { return ""; }
    hasSelection() { return false; }
    clearSelection() {}
    paste() {}
  },
}));
vi.mock("xterm-addon-fit", () => ({ FitAddon: class { fit() {} } }));
vi.mock("xterm-addon-webgl", () => ({ WebglAddon: class { dispose() {} onContextLoss() {} } }));
vi.mock("xterm/css/xterm.css", () => ({}));

import TerminalWorkspace from "../TerminalWorkspace";

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;
(globalThis as any).ResizeObserver = class { observe() {} unobserve() {} disconnect() {} };
Object.defineProperty(document, "fonts", { value: { load: async () => [] }, configurable: true });

let host: HTMLDivElement;
let root: Root;

const agent = (id: string) => leaf(id, {
  kind: "agent:codex",
  spawn: { kind: "agent", projectName: "demo", agentId: "codex", laraSessionId: id },
});

type OpenTerminalView = { terminalId: string; label: string; hint: string; active: boolean };
type WorkspaceExtras = {
  activeSurface?: "terminal" | "documents" | "navigator";
  focusTerminalRequest?: { terminalId: string; seq: number };
  onOpenTerminalsChange?: (terminals: OpenTerminalView[]) => void;
  flash?: (message: string) => void;
  openRequest?: OpenProjectRequest;
  detached?: boolean;
  detachedSeed?: string;
};

async function render(active = true, focusAgentRequest?: { sessionId: string; seq: number }, extras: WorkspaceExtras = {}) {
  await act(async () => { root.render(<TerminalWorkspace active={active} focusAgentRequest={focusAgentRequest} {...extras} />); });
  await act(async () => { vi.advanceTimersByTime(100); });
}

beforeEach(() => {
  vi.useFakeTimers();
  vi.spyOn(document, "hasFocus").mockReturnValue(true);
  terminals.length = 0;
  bridge.focusAgent.mockReset();
  bridge.stop.mockReset();
  bridge.projectCommand.mockReset().mockResolvedValue('term-project-command');
  bridge.write.mockReset().mockResolvedValue(undefined);
  bridge.shell.mockReset().mockResolvedValue("term-background");
  bridge.ssh.mockReset().mockResolvedValue("term-ssh");
  bridge.launch.mockReset().mockResolvedValue({ id: "fresh", terminalId: "term-fresh" });
  bridge.resume.mockReset().mockImplementation(async (id: string) => ({ id, terminalId: `term-${id}` }));
  bridge.save.mockReset().mockResolvedValue(undefined);
  bridge.drainAttach.mockReset().mockResolvedValue([]);
  bridge.profiles.mockReset().mockResolvedValue({ profiles: [{ kind: "powershell", label: "PowerShell", available: true, path: "pwsh" }], defaultKind: "powershell", projectKinds: {} });
  events.listeners.clear();
  bridge.load.mockReset().mockResolvedValue(JSON.stringify(serializeWorkspace(ws([
    group("background", [tab("background-tab", leaf("background-shell"))]),
    group("front", [tab("split", { type: "split", direction: "horizontal", children: [agent("left"), agent("right")] }, "left")], { z: 2 }),
  ]))));
  host = document.createElement("div");
  document.body.appendChild(host);
  root = createRoot(host);
});

afterEach(async () => {
  await act(async () => { root.unmount(); });
  host.remove();
  vi.useRealTimers();
  vi.restoreAllMocks();
});

describe("terminal workspace first use", () => {
  it.each(["default", "command"])("recovers a failed first profile lookup through an accessible retry for %s use", async use => {
    bridge.profiles.mockRejectedValueOnce(new Error("profiles temporarily unavailable"));
    bridge.load.mockResolvedValue("");
    const openRequest: OpenProjectRequest | undefined = use === "command"
      ? { kind: "command", project: "demo", command: "pnpm run build", seq: 1 }
      : undefined;
    await render(use === "default", undefined, { openRequest });
    expect(within(host).getByRole("alert").textContent).toContain("profiles temporarily unavailable");
    expect(bridge.shell).not.toHaveBeenCalled();
    expect(bridge.projectCommand).not.toHaveBeenCalled();
    expect(bridge.load).not.toHaveBeenCalled();
    expect(bridge.save).not.toHaveBeenCalled();
    await render(use === "default", undefined, { openRequest });
    await act(async () => { vi.advanceTimersByTime(1000); });
    expect(bridge.profiles).toHaveBeenCalledTimes(1);

    let finishRetry!: (value: unknown) => void;
    bridge.profiles.mockImplementationOnce(() => new Promise(resolve => { finishRetry = resolve; }));
    const retry = within(host).getByRole("button", { name: "documents.retry" });
    retry.focus();
    expect(document.activeElement).toBe(retry);
    await act(async () => { retry.click(); retry.click(); });
    expect(bridge.profiles).toHaveBeenCalledTimes(2);
    expect(within(host).queryByRole("alert")).toBeNull();
    await act(async () => { finishRetry({ profiles: [{ kind: "powershell", label: "PowerShell", available: true, path: "pwsh" }], defaultKind: "powershell", projectKinds: {} }); });
    expect(bridge.load).toHaveBeenCalledTimes(1);
    expect(host.querySelectorAll(".xterm")).toHaveLength(1);
    if (use === "command") {
      expect(bridge.shell).not.toHaveBeenCalled();
      expect(bridge.projectCommand.mock.calls).toEqual([["demo", "powershell", 100, 30]]);
      expect(bridge.write.mock.calls).toEqual([["term-project-command", "pnpm run build\r"]]);
    } else expect(bridge.shell.mock.calls).toEqual([["powershell", 100, 30]]);
    await render(false, undefined, { openRequest });
    await render(true, undefined, { openRequest });
    expect(bridge.profiles).toHaveBeenCalledTimes(2);
  });

  it("starts one default terminal on first activation and preserves it across page changes", async () => {
    bridge.load.mockResolvedValue("");
    await render(false);
    await act(async () => { vi.advanceTimersByTime(1000); });
    expect(bridge.load).not.toHaveBeenCalled();
    expect(bridge.shell).not.toHaveBeenCalled();
    expect(bridge.save).not.toHaveBeenCalled();
    expect(host.querySelectorAll(".xterm")).toHaveLength(0);

    await render();
    expect(bridge.shell.mock.calls).toEqual([["powershell", 100, 30]]);
    expect(host.querySelectorAll(".xterm")).toHaveLength(1);
    const pane = host.querySelector(".xterm");
    await render(false);
    await render();
    expect(host.querySelector(".xterm")).toBe(pane);
    expect(bridge.shell).toHaveBeenCalledTimes(1);
    expect(bridge.stop).not.toHaveBeenCalled();
  });

  it("defers saved shells and agents until first use and restores only once during navigation", async () => {
    let restore!: (value: string) => void;
    const saved = JSON.stringify(serializeWorkspace(ws([
      group("saved", [tab("local", leaf("shell")), tab("remote", leaf("ssh", { kind: "ssh", spawn: { kind: "ssh", sshHostName: "production" } })), tab("agent", agent("left"))]),
    ])));
    bridge.load.mockReturnValue(new Promise<string>(resolve => { restore = resolve; }));
    await render(false);
    expect(bridge.load).not.toHaveBeenCalled();
    expect(bridge.shell).not.toHaveBeenCalled();
    expect(bridge.ssh).not.toHaveBeenCalled();
    expect(bridge.resume).not.toHaveBeenCalled();

    await render();
    await render(false);
    await render();
    await act(async () => { restore(saved); });
    expect(bridge.load).toHaveBeenCalledTimes(1);
    expect(bridge.shell).toHaveBeenCalledTimes(1);
    expect(bridge.ssh).toHaveBeenCalledTimes(1);
    expect(bridge.resume.mock.calls).toEqual([["left"]]);
    expect(host.querySelectorAll(".xterm")).toHaveLength(3);
  });

  it("runs an explicit command while hidden without creating an unused default shell", async () => {
    bridge.load.mockResolvedValue("");
    await render(false, undefined, { openRequest: { kind: "command", project: "demo", shell: "gitbash", command: "pnpm run build", seq: 1 } });
    expect(bridge.shell).not.toHaveBeenCalled();
    expect(bridge.projectCommand.mock.calls).toEqual([["demo", "gitbash", 100, 30]]);
    expect(bridge.write.mock.calls).toEqual([["term-project-command", "pnpm run build\r"]]);
    expect(host.querySelectorAll(".xterm")).toHaveLength(1);
    expect(host.textContent).toContain("demo · gitbash");
  });

  it.each(["button", "shortcut"])("waits for layout loading before accepting the new-terminal %s", async action => {
    let restore!: (value: string) => void;
    bridge.load.mockReturnValue(new Promise<string>(resolve => { restore = resolve; }));
    await render();
    await act(async () => {
      if (action === "button") host.querySelector<HTMLButtonElement>('button[title="terminal.workspace.openNewTerminalTooltip"]')!.click();
      else document.dispatchEvent(new KeyboardEvent("keydown", { key: "t", ctrlKey: true, bubbles: true, cancelable: true }));
    });
    expect(bridge.shell).not.toHaveBeenCalled();
    await act(async () => { restore(""); });
    expect(bridge.shell.mock.calls).toEqual([["powershell", 100, 30]]);
    expect(host.querySelectorAll(".xterm")).toHaveLength(1);
  });

  it.each<OpenProjectRequest>([
    { kind: "ssh", host: "production", seq: 1 },
    { kind: "agent", project: "demo", agentId: "codex", agentLabel: "Codex", mode: "current", seq: 1 },
    { kind: "agent-resume", project: "demo", agentId: "codex", agentLabel: "Codex", sessionId: "history", seq: 1 },
  ])("preserves a first-use $kind request while saved layout loading is pending", async request => {
    let restore!: (value: string) => void;
    bridge.load.mockReturnValue(new Promise<string>(resolve => { restore = resolve; }));
    const publish = vi.fn<(terminals: OpenTerminalView[]) => void>();
    await render(false, undefined, { openRequest: request, onOpenTerminalsChange: publish });
    expect(bridge.ssh).not.toHaveBeenCalled();
    expect(bridge.launch).not.toHaveBeenCalled();
    expect(bridge.resume).not.toHaveBeenCalled();
    await act(async () => { restore(JSON.stringify(serializeWorkspace(ws([group("saved", [tab("saved tab", leaf("saved shell"))])])))); });
    const requestedID = request.kind === "ssh" ? "term-ssh" : request.kind === "agent" ? "term-fresh" : "term-history";
    expect(publish.mock.lastCall?.[0].map(pane => pane.terminalId)).toEqual(["term-background", requestedID]);
    expect(host.querySelectorAll(".xterm")).toHaveLength(2);
    expect(bridge.shell).toHaveBeenCalledTimes(1);
    if (request.kind === "ssh") expect(bridge.ssh.mock.calls).toEqual([["production", "1", 100, 30]]);
    if (request.kind === "agent") expect(bridge.launch).toHaveBeenCalledTimes(1);
    if (request.kind === "agent-resume") expect(bridge.resume.mock.calls).toEqual([["history"]]);
  });

  it("waits for restored panes before consuming the initial agent focus request", async () => {
    const flash = vi.fn();
    const publish = vi.fn<(terminals: OpenTerminalView[]) => void>();
    await render(false, { sessionId: "right", seq: 1 }, { flash, onOpenTerminalsChange: publish });
    expect(flash).not.toHaveBeenCalled();
    expect(publish.mock.lastCall?.[0].filter(pane => pane.active).map(pane => pane.terminalId)).toEqual(["term-right"]);
  });

  it("starts a detached window from its seed without waiting for main-page activation", async () => {
    const seed = JSON.stringify(serializeWorkspace(ws([group("detached", [tab("detached tab", leaf("detached shell"))])])));
    await render(false, undefined, { detached: true, detachedSeed: seed });
    expect(bridge.load).not.toHaveBeenCalled();
    expect(bridge.shell).toHaveBeenCalledTimes(1);
    expect(host.textContent).toContain("detached tab");
    await act(async () => { vi.advanceTimersByTime(1000); });
    expect(bridge.save).not.toHaveBeenCalled();
  });

  it("queues a reattached window until startup restore finishes without adding a default shell", async () => {
    let restore!: (value: string) => void;
    bridge.load.mockReturnValue(new Promise<string>(resolve => { restore = resolve; }));
    const seed = JSON.stringify(serializeWorkspace(ws([group("returned", [tab("returned tab", leaf("returned shell"))])])));
    await render(false);
    await act(async () => { events.listeners.get("terminal:attach")?.(seed); });
    expect(bridge.load).toHaveBeenCalledTimes(1);
    expect(bridge.shell).not.toHaveBeenCalled();
    await act(async () => { restore(""); });
    expect(bridge.shell).toHaveBeenCalledTimes(1);
    expect(host.textContent).toContain("returned tab");
    expect(host.querySelectorAll(".xterm")).toHaveLength(1);
  });
});

describe("terminal workspace keyboard focus", () => {
  it('runs a project command in a fresh terminal without selecting a new terminal preference', async () => {
    await render(true, undefined, { openRequest: { kind: 'command', project: 'demo', shell: 'gitbash', command: 'pnpm run build', seq: 1 } });
    expect(bridge.projectCommand).toHaveBeenCalledWith('demo', 'gitbash', 100, 30);
    expect(bridge.write).toHaveBeenCalledWith('term-project-command', 'pnpm run build\r');
    expect(host.textContent).toContain('demo · gitbash');
  });

  it('restores a project command terminal without saving its inherited shell as a project preference', async () => {
    bridge.load.mockResolvedValue(JSON.stringify(serializeWorkspace(ws([
      group('restored-project', [tab('command-terminal', leaf('saved-shell', {
        kind: 'gitbash', spawn: { kind: 'project', projectName: 'demo', terminalKind: 'gitbash' },
      }))]),
    ]))));
    await render();
    expect(bridge.projectCommand).toHaveBeenCalledWith('demo', 'gitbash', 100, 30);
  });
  it("restores focus to the selected split pane rather than the last pane spawned", async () => {
    await render();
    expect(terminals).toHaveLength(3);
    expect(document.activeElement).toBe(terminals[1].textarea);
    expect(host.querySelectorAll(".ring-accent\\/60")).toHaveLength(1);
  });

  it("keeps the current app input focused while the terminal page is inactive", async () => {
    const input = document.createElement("input");
    document.body.appendChild(input);
    input.focus();
    try {
      await render(false);
      expect(document.activeElement).toBe(input);
      expect(host.querySelectorAll(".ring-accent\\/60")).toHaveLength(0);
    } finally { input.remove(); }
  });

  it("preserves a modal opener restored after passive terminal reactivation while honoring a later explicit request", async () => {
    await render();
    await render(true, undefined, {activeSurface: "navigator"});
    const opener = document.createElement("button"); document.body.appendChild(opener);
    try {
      await act(async () => {root.render(<TerminalWorkspace active activeSurface="terminal" />);});
      // Modal layout cleanup restores its opener in a microtask, after the
      // terminal reactivation has scheduled its delayed fit/focus callback.
      await act(async () => {queueMicrotask(() => opener.focus()); vi.runAllTicks();});
      expect(document.activeElement).toBe(opener);
      await act(async () => {vi.advanceTimersByTime(100);});
      expect(document.activeElement).toBe(opener);
      await render(true, {sessionId: "left", seq: 1});
      expect(document.activeElement).toBe(terminals[1].textarea);
    } finally {opener.remove();}
  });

  it("focuses the requested agent within a split and honors a repeated explicit request", async () => {
    await render();
    await render(true, { sessionId: "right", seq: 1 });
    expect(document.activeElement).toBe(terminals[2].textarea);
    const button = document.createElement("button");
    document.body.appendChild(button);
    button.focus();
    try {
      await render(true, { sessionId: "right", seq: 2 });
      expect(document.activeElement).toBe(terminals[2].textarea);
    } finally { button.remove(); }
  });

  it("synchronizes native keyboard focus with the selected agent pane", async () => {
    await render();
    act(() => { terminals[2].textarea.focus(); });
    expect(bridge.focusAgent).toHaveBeenLastCalledWith("right");
    act(() => { terminals[0].textarea.focus(); });
    expect(bridge.focusAgent).toHaveBeenLastCalledWith("");
  });

  it("leaves focus on the shell picker when opening it in a background group", async () => {
    await render();
    const picker = host.querySelector<HTMLButtonElement>('button[title="terminal.pickTerminalTypeTooltip"]')!;
    act(() => {
      picker.focus();
      picker.dispatchEvent(new MouseEvent("pointerdown", { bubbles: true, button: 0 }));
    });
    // A real mouse press can last longer than the terminal's focus timer.
    await act(async () => { vi.advanceTimersByTime(100); });
    expect(document.activeElement).toBe(picker);
    act(() => { picker.click(); });
    await act(async () => { vi.advanceTimersByTime(100); });
    expect(document.activeElement).toBe(picker);
    expect(host.querySelectorAll(".ring-accent\\/60")).toHaveLength(0);
  });

  it("publishes local, SSH and agent panes in inactive tabs and updates renamed or closed tabs", async () => {
    bridge.load.mockResolvedValue(JSON.stringify(serializeWorkspace(ws([
      group("background", [tab("local", leaf("shell")), tab("remote", leaf("ssh", { kind: "ssh", spawn: { kind: "ssh", sshHostName: "production" } }))]),
      group("front", [tab("agents", { type: "split", direction: "horizontal", children: [agent("left"), agent("right")] }, "left")], { z: 2 }),
    ]))));
    const publish = vi.fn<(terminals: OpenTerminalView[]) => void>();
    await render(true, undefined, { onOpenTerminalsChange: publish });
    const listed = publish.mock.lastCall?.[0];
    expect(listed?.map((entry) => entry.terminalId)).toEqual(["term-background", "term-ssh", "term-left", "term-right"]);
    expect(listed?.map((entry) => entry.active)).toEqual([false, false, true, false]);
    expect(listed?.[0].label).toBe("local");
    expect(listed?.[0].hint).toContain("powershell");
    expect(listed?.[1].hint).toContain("production");
    expect(listed?.[2].hint).toContain("demo");
    expect(listed?.[2].label).toContain("Pane 1");
    expect(listed?.[3].label).toContain("Pane 2");
    expect(new Set(listed?.map((entry) => entry.label)).size).toBe(4);

    const tabTitle = host.querySelector<HTMLElement>('[title="terminal.group.tabTooltip"]')!;
    act(() => { tabTitle.dispatchEvent(new MouseEvent("dblclick", { bubbles: true })); });
    const rename = host.querySelector<HTMLInputElement>("input")!;
    await act(async () => {
      rename.focus();
      rename.value = "My local terminal";
      rename.blur();
    });
    expect(publish.mock.lastCall?.[0].find((entry) => entry.terminalId === "term-background")?.label).toBe("My local terminal");

    const close = host.querySelector<HTMLButtonElement>('button[title="terminal.group.closeTabTooltip"]')!;
    await act(async () => { close.click(); });
    expect(publish.mock.lastCall?.[0].map((entry) => entry.terminalId)).toEqual(["term-ssh", "term-left", "term-right"]);
  });

  it("focuses an inactive SSH tab or selected agent split by terminal id without launching a process", async () => {
    bridge.load.mockResolvedValue(JSON.stringify(serializeWorkspace(ws([
      group("background", [tab("local", leaf("shell")), tab("remote", leaf("ssh", { kind: "ssh", spawn: { kind: "ssh", sshHostName: "production" } }))]),
      group("front", [tab("agents", { type: "split", direction: "horizontal", children: [agent("left"), agent("right")] }, "left")], { z: 2 }),
    ]))));
    await render();
    const shellStarts = bridge.shell.mock.calls.length;
    await render(true, undefined, { focusTerminalRequest: { terminalId: "term-ssh", seq: 1 } });
    expect(document.activeElement).toBe(terminals[1].textarea);
    const button = document.createElement("button");
    document.body.appendChild(button);
    button.focus();
    try {
      await render(true, undefined, { focusTerminalRequest: { terminalId: "term-ssh", seq: 2 } });
      expect(document.activeElement).toBe(terminals[1].textarea);
      await render(true, undefined, { focusTerminalRequest: { terminalId: "term-right", seq: 3 } });
      expect(document.activeElement).toBe(terminals[3].textarea);
      expect(bridge.focusAgent).toHaveBeenLastCalledWith("right");
      expect(bridge.shell.mock.calls.length).toBe(shellStarts);
    } finally { button.remove(); }
  });

  it("reports a closed terminal instead of creating or resuming a replacement", async () => {
    const flash = vi.fn();
    await render();
    const selected = document.activeElement;
    const shellStarts = bridge.shell.mock.calls.length;
    await render(true, undefined, { focusTerminalRequest: { terminalId: "closed-terminal", seq: 1 }, flash });
    expect(flash).toHaveBeenCalledWith("terminal.workspace.terminalNotInWorkspace");
    expect(document.activeElement).toBe(selected);
    expect(bridge.shell.mock.calls.length).toBe(shellStarts);
  });

  it.each([
    ["shell", "terminal.pickTerminalTypeTooltip"],
    ["runtime", "terminal.group.runtimeTooltip"],
  ])("closes the destination %s picker for a new explicit terminal focus request", async (_pickerKind, title) => {
    await render();
    const picker = host.querySelector<HTMLButtonElement>(`button[title="${title}"]`)!;
    act(() => { picker.focus(); picker.click(); });
    expect(document.querySelectorAll(".glass-strong")).toHaveLength(1);
    await render();
    expect(document.querySelectorAll(".glass-strong")).toHaveLength(1);

    const request = { terminalId: "term-background", seq: 1 };
    await render(true, undefined, { focusTerminalRequest: request });
    expect(document.activeElement).toBe(terminals[0].textarea);
    expect(document.querySelectorAll(".glass-strong")).toHaveLength(0);

    // Re-rendering the same request or selecting an unavailable pane must
    // leave a subsequently opened menu intact.
    act(() => { picker.focus(); picker.click(); });
    await render(true, undefined, { focusTerminalRequest: request });
    expect(document.querySelectorAll(".glass-strong")).toHaveLength(1);
    await render(true, undefined, { focusTerminalRequest: { terminalId: "missing", seq: 2 } });
    expect(document.querySelectorAll(".glass-strong")).toHaveLength(1);
  });

  it("closes a destination shell picker for an explicit agent focus request", async () => {
    await render();
    const picker = host.querySelectorAll<HTMLButtonElement>('button[title="terminal.pickTerminalTypeTooltip"]')[1];
    act(() => { picker.focus(); picker.click(); });
    expect(document.querySelectorAll(".glass-strong")).toHaveLength(1);
    await render(true, { sessionId: "left", seq: 1 });
    expect(document.activeElement).toBe(terminals[1].textarea);
    expect(document.querySelectorAll(".glass-strong")).toHaveLength(0);
  });

  it.each(["w", "t"])("leaves Ctrl+%s to an app shortcut intercepted at window capture", async (key) => {
    const publish = vi.fn<(terminals: OpenTerminalView[]) => void>();
    await render(true, undefined, { onOpenTerminalsChange: publish });
    act(() => { terminals[0].textarea.focus(); });
    const listedIds = publish.mock.lastCall?.[0].map((entry) => entry.terminalId);
    const shellStarts = bridge.shell.mock.calls.length;
    const appShortcut = vi.fn((event: KeyboardEvent) => {
      if (event.ctrlKey && event.key === key) {
        event.preventDefault();
        event.stopPropagation();
      }
    });
    window.addEventListener("keydown", appShortcut, true);
    try {
      await act(async () => {
        terminals[0].textarea.dispatchEvent(new KeyboardEvent("keydown", { key, ctrlKey: true, bubbles: true, cancelable: true }));
      });
      expect(appShortcut).toHaveBeenCalledTimes(1);
      expect(publish.mock.lastCall?.[0].map((entry) => entry.terminalId)).toEqual(listedIds);
      expect(bridge.stop).not.toHaveBeenCalled();
      expect(bridge.shell.mock.calls.length).toBe(shellStarts);
    } finally { window.removeEventListener("keydown", appShortcut, true); }
  });

  it("still closes a terminal with ordinary Ctrl+W from the xterm input", async () => {
    const publish = vi.fn<(terminals: OpenTerminalView[]) => void>();
    await render(true, undefined, { onOpenTerminalsChange: publish });
    act(() => { terminals[0].textarea.focus(); });
    await act(async () => {
      terminals[0].textarea.dispatchEvent(new KeyboardEvent("keydown", { key: "w", ctrlKey: true, bubbles: true, cancelable: true }));
    });
    expect(bridge.stop).toHaveBeenCalledWith("term-background");
    expect(publish.mock.lastCall?.[0].map((entry) => entry.terminalId)).toEqual(["term-left", "term-right"]);
  });

  it("still creates a terminal with ordinary Ctrl+T from the xterm input", async () => {
    const publish = vi.fn<(terminals: OpenTerminalView[]) => void>();
    await render(true, undefined, { onOpenTerminalsChange: publish });
    act(() => { terminals[0].textarea.focus(); });
    const shellStarts = bridge.shell.mock.calls.length;
    bridge.shell.mockResolvedValueOnce("term-new");
    await act(async () => {
      terminals[0].textarea.dispatchEvent(new KeyboardEvent("keydown", { key: "t", ctrlKey: true, bubbles: true, cancelable: true }));
    });
    expect(bridge.shell.mock.calls.length).toBe(shellStarts + 1);
    expect(publish.mock.lastCall?.[0].map((entry) => entry.terminalId)).toEqual(["term-background", "term-new", "term-left", "term-right"]);
  });
});
