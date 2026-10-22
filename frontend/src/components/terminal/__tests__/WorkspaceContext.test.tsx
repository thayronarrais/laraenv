// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { serializeWorkspace } from "../serialize";
import { group, leaf, tab, ws } from "./helpers";

// Keep the workspace, groups, split panes and Terminal component real. Only
// the native PTY bridge and xterm renderer need substitutes in jsdom.
const bridge = vi.hoisted(() => ({
  load: vi.fn(),
  xtermFocus: vi.fn(),
  focusAgent: vi.fn(),
  shell: vi.fn(),
  shellIn: vi.fn(),
  runtimeShell: vi.fn(),
  stop: vi.fn(),
  agentShell: vi.fn(),
  ssh: vi.fn(),
  project: vi.fn(),
  resume: vi.fn(),
  save: vi.fn(),
}));
const events = vi.hoisted(() => ({ listeners: new Map<string, (stage: string) => void>() }));
vi.mock("../../../../wailsjs/go/main/App", () => ({
  StartPtyKind: (...args: unknown[]) => bridge.shell(...args),
  StartPtyKindIn: (...args: unknown[]) => bridge.shellIn(...args), StartPtyKindRuntime: (...args: unknown[]) => bridge.runtimeShell(...args), StartPtyForProject: (...args: unknown[]) => bridge.project(...args), StartPtyForSSH: (...args: unknown[]) => bridge.ssh(...args),
  StartPtyForAgentSession: (...args: unknown[]) => bridge.agentShell(...args),
  StopPty: (...args: unknown[]) => bridge.stop(...args), WritePty: vi.fn(), ResizePty: vi.fn(),
  ListTerminals: async () => ({ profiles: [{ kind: "powershell", label: "PowerShell", available: true, path: "pwsh" }], defaultKind: "powershell", projectKinds: {} }),
  ListPhpVersions: async () => [], ListNodeVersions: async () => [], SetDefaultTerminal: vi.fn(), SaveTerminalWorkspace: (...args: unknown[]) => bridge.save(...args),
  LoadTerminalWorkspace: (...args: unknown[]) => bridge.load(...args),
  DetachWorkspace: vi.fn(), GetDetachedSeed: vi.fn(), SendTerminalAttach: vi.fn(), DrainTerminalAttach: async () => [],
  LaunchAgentSession: vi.fn(), ResumeAgentSession: (...args: unknown[]) => bridge.resume(...args),
  StopAgentSession: vi.fn(), ConfigureWorktreeRouting: vi.fn(), ListProjectRoutes: async () => [],
  SetFocusedAgentSession: (...args: unknown[]) => bridge.focusAgent(...args), ListAgentSessions: async () => [],
}));
vi.mock("../../../../wailsjs/runtime", () => ({
  EventsOn: (name: string, callback: (stage: string) => void) => { events.listeners.set(name, callback); return () => events.listeners.delete(name); },
  EventsOff: (name: string) => events.listeners.delete(name), Quit: vi.fn(),
  ClipboardGetText: async () => "", ClipboardSetText: async () => {},
}));
vi.mock("../../SFTPModal", () => ({ default: () => <div data-testid="sftp" /> }));
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
    focus() { bridge.xtermFocus(); this.textarea.focus(); }
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
import TerminalGroup from "../TerminalGroup";

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
  onActiveSurfaceChange?: (surface: "terminal" | "documents" | "navigator") => void;
  onWorkspaceSelectionChange?: (context: any) => void;
  onOpenWorkspaceFiles?: (context: any) => void;
  openFilesRequest?: { seq: number };
  focusTerminalRequest?: { terminalId: string; seq: number };
  onOpenTerminalsChange?: (terminals: OpenTerminalView[]) => void;
  flash?: (message: string) => void;
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
  bridge.save.mockReset();
  bridge.agentShell.mockReset().mockResolvedValue({ termId: "term-scoped", rootKey: "key-1", label: "C:\\worktrees\\demo" });
  bridge.ssh.mockReset().mockResolvedValue("term-ssh");
  bridge.project.mockReset().mockResolvedValue("term-project");
  bridge.resume.mockReset().mockImplementation(async (id: string) => ({ id, terminalId: `term-${id}` }));
  bridge.xtermFocus.mockReset();
  bridge.shell.mockReset().mockResolvedValue("term-background");
  bridge.shellIn.mockReset();
  bridge.runtimeShell.mockReset();
  events.listeners.clear();
  bridge.load.mockResolvedValue(JSON.stringify(serializeWorkspace(ws([
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

describe("workspace context in terminal splits and restore", () => {
  it("splits an agent pane into a scoped shell without adopting the agent PTY", async () => {
    await render();
    const split = host.querySelectorAll<HTMLButtonElement>('button[title="terminal.group.splitRightTooltip"]')[1];
    await act(async () => { split.click(); });
    expect(bridge.agentShell).toHaveBeenCalledWith("left", "powershell", 100, 30, "");
    expect(bridge.project).not.toHaveBeenCalled();
    await act(async () => { vi.advanceTimersByTime(600); });
    const saved = JSON.parse(bridge.save.mock.lastCall![0]);
    const all = (node: any): any[] => node.type === "leaf" ? [node] : [...all(node.children[0]), ...all(node.children[1])];
    const shell = all(saved.groups[1].tabs[0].tree).find((node) => node.spawn.workspaceContext);
    expect(shell.spawn.workspaceContext).toEqual({ kind: "agent", sessionId: "left" });
    expect(shell.spawn.workspaceRootKey).toBe("key-1");
    expect(shell.spawn.laraSessionId).toBeUndefined();
    expect(host.querySelector<HTMLElement>('[title*="worktrees"]')?.title).toContain("C:\\worktrees\\demo");
    expect(bridge.focusAgent).not.toHaveBeenLastCalledWith("term-scoped");
  });

  it("restores a scoped shell with its saved key and without resuming an agent PTY", async () => {
    bridge.load.mockResolvedValue(JSON.stringify(serializeWorkspace(ws([
      group("scoped", [tab("tab", leaf("old", {
        spawn: { kind: "local", terminalKind: "powershell", workspaceContext: { kind: "agent", sessionId: "a1" }, workspaceRootKey: "saved-key" },
      }))]),
    ]))));
    await render();
    expect(bridge.agentShell).toHaveBeenCalledWith("a1", "powershell", 100, 30, "saved-key");
    expect(bridge.resume).not.toHaveBeenCalled();
    expect(bridge.project).not.toHaveBeenCalled();
    bridge.agentShell.mockResolvedValueOnce({ termId: "term-second", rootKey: "key-1", label: "C:\\worktrees\\demo" });
    await act(async () => { host.querySelector<HTMLButtonElement>('button[title="terminal.group.splitRightTooltip"]')!.click(); });
    expect(bridge.agentShell).toHaveBeenCalledTimes(2);
    expect(bridge.agentShell).toHaveBeenLastCalledWith("a1", "powershell", 100, 30, "key-1");
  });

  it("keeps a failed scoped restore in error instead of replacing it with a local shell", async () => {
    bridge.load.mockResolvedValue(JSON.stringify(serializeWorkspace(ws([
      group("scoped", [tab("tab", leaf("old", {
        spawn: { kind: "local", terminalKind: "powershell", workspaceContext: { kind: "agent", sessionId: "a1" }, workspaceRootKey: "old-key" },
      }))]),
    ]))));
    bridge.agentShell.mockRejectedValueOnce(new Error("agent workspace root changed"));
    await render();
    expect(host.textContent).toContain("agent workspace root changed");
    expect(bridge.shell).not.toHaveBeenCalled();
    expect(bridge.shellIn).not.toHaveBeenCalled();
    expect(bridge.project).not.toHaveBeenCalled();
  });

  it("closes a plain shell runtime menu when focus changes to a scoped pane without changing its descriptor", async () => {
    bridge.load.mockResolvedValue(JSON.stringify(serializeWorkspace(ws([
      group("mixed", [tab("tab", { type: "split", direction: "horizontal", children: [
        leaf("plain"),
        leaf("scoped", { spawn: { kind: "local", terminalKind: "powershell", workspaceContext: { kind: "agent", sessionId: "a1" }, workspaceRootKey: "saved-key" } }),
      ] }, "plain")]),
    ]))));
    await render(true, undefined, { focusTerminalRequest: { terminalId: "outside", seq: 1 } });
    const runtimeButton = host.querySelector<HTMLButtonElement>('button[title="terminal.group.runtimeTooltip"]')!;
    await act(async () => { runtimeButton.click(); });
    expect(document.querySelector(".glass-strong")).not.toBeNull();
    await act(async () => { terminals[1].textarea.focus(); });
    const staleOption = document.querySelector<HTMLButtonElement>(".glass-strong button");
    if (staleOption) await act(async () => { staleOption.click(); });
    expect(document.querySelector(".glass-strong")).toBeNull();
    expect(bridge.runtimeShell).not.toHaveBeenCalled();
    expect(bridge.stop).not.toHaveBeenCalledWith("term-scoped");
    await act(async () => { vi.advanceTimersByTime(600); });
    const saved = JSON.parse(bridge.save.mock.lastCall![0]);
    const scoped = saved.groups[0].tabs[0].tree.children[1];
    expect(scoped.spawn.workspaceContext).toEqual({ kind: "agent", sessionId: "a1" });
    expect(scoped.spawn.workspaceRootKey).toBe("key-1");
  });

  it("splits SSH with a fresh request and never launches a local shell on failure", async () => {
    bridge.load.mockResolvedValue(JSON.stringify(serializeWorkspace(ws([
      group("remote", [tab("tab", leaf("ssh", { kind: "ssh", spawn: { kind: "ssh", sshHostName: "host-a" } }))]),
    ]))));
    await render();
    const existing = bridge.ssh.mock.calls[0]?.[1];
    let rejectSplit!: (error: Error) => void;
    bridge.ssh.mockImplementationOnce(() => new Promise((_, reject) => { rejectSplit = reject; }));
    await act(async () => { host.querySelector<HTMLButtonElement>('button[title="terminal.group.splitRightTooltip"]')!.click(); });
    expect(bridge.ssh).toHaveBeenCalledTimes(2);
    expect(bridge.ssh).toHaveBeenLastCalledWith("host-a", expect.any(String), 100, 30);
    expect(bridge.ssh.mock.lastCall?.[1]).not.toBe(existing);
    const requestId = bridge.ssh.mock.lastCall![1] as string;
    expect(events.listeners.has(`ssh:progress:${requestId}`)).toBe(true);
    await act(async () => { events.listeners.get(`ssh:progress:${requestId}`)?.("handshake"); });
    expect(host.textContent).toContain("terminal.sshTimeline.inProgress");
    await act(async () => { rejectSplit(new Error("offline")); });
    expect(host.textContent).toContain("offline");
    expect(events.listeners.has(`ssh:progress:${requestId}`)).toBe(false);
    expect(bridge.shell).not.toHaveBeenCalled();
    expect(bridge.shellIn).not.toHaveBeenCalled();
    expect(bridge.project).not.toHaveBeenCalled();
  });
});


describe("document focus integration", () => {
  it("reports selected origin independently of ownership and clears agent focus for documents", async () => {
    const context = vi.fn();
    await render(true, undefined, { onWorkspaceSelectionChange: context });
    expect(context).toHaveBeenLastCalledWith({ kind: "local", context: { kind: "agent", sessionId: "left" } });
    expect(bridge.focusAgent).toHaveBeenLastCalledWith("left");
    await render(true, undefined, { activeSurface: "documents", onWorkspaceSelectionChange: context });
    expect(bridge.focusAgent).toHaveBeenLastCalledWith("");
    expect(context).toHaveBeenLastCalledWith({ kind: "local", context: { kind: "agent", sessionId: "left" } });
  });

  it("same-pane pointer returns terminal ownership; passive native focus and page changes do not", async () => {
    const claim = vi.fn();
    await render(true, undefined, { activeSurface: "documents", onActiveSurfaceChange: claim });
    expect(claim).not.toHaveBeenCalled();
    const pane = terminals[1].textarea;
    await act(async () => { pane.dispatchEvent(new MouseEvent("mousedown", { bubbles: true })); });
    expect(claim).toHaveBeenLastCalledWith("terminal");
    claim.mockClear();
    await act(async () => { pane.focus(); });
    expect(claim).not.toHaveBeenCalled();
    claim.mockClear();
    await render(false, undefined, { activeSurface: "documents", onActiveSurfaceChange: claim });
    expect(claim).not.toHaveBeenCalled();
  });

  it("explicit found session requests claim terminal while missing requests retain documents", async () => {
    const claim = vi.fn();
    await render(true, undefined, { activeSurface: "documents", onActiveSurfaceChange: claim });
    await render(true, { sessionId: "absent", seq: 1 }, { activeSurface: "documents", onActiveSurfaceChange: claim });
    expect(claim).not.toHaveBeenCalled();
    await render(true, { sessionId: "right", seq: 2 }, { activeSurface: "documents", onActiveSurfaceChange: claim });
    expect(claim).toHaveBeenLastCalledWith("terminal");
    claim.mockClear();
    await render(true, undefined, { activeSurface: "documents", onActiveSurfaceChange: claim, focusTerminalRequest: { terminalId: "absent", seq: 3 } });
    expect(claim).not.toHaveBeenCalled();
    await render(true, undefined, { activeSurface: "documents", onActiveSurfaceChange: claim, focusTerminalRequest: { terminalId: "term-left", seq: 4 } });
    expect(claim).toHaveBeenLastCalledWith("terminal");
  });

  it("plain shell opens the catalog chooser and reports no eligible origin", async () => {
    bridge.load.mockResolvedValue(JSON.stringify(serializeWorkspace(ws([group("plain", [tab("tab", leaf("plain"))])]))));
    const context = vi.fn(), files = vi.fn();
    await render(true, undefined, { onWorkspaceSelectionChange: context, onOpenWorkspaceFiles: files });
    expect(context).toHaveBeenLastCalledWith({ kind: "local", context: null });
    await render(true, undefined, { onWorkspaceSelectionChange: context, onOpenWorkspaceFiles: files, openFilesRequest: { seq: 1 } });
    expect(files).toHaveBeenCalledWith(null);
  });

  it("scoped local shells supply origin but cannot match an agent focus request", async () => {
    bridge.load.mockResolvedValue(JSON.stringify(serializeWorkspace(ws([group("scoped", [tab("tab", leaf("old", { spawn: { kind: "local", terminalKind: "powershell", workspaceContext: { kind: "agent", sessionId: "a1" }, workspaceRootKey: "key-1" } }))])]))));
    const context = vi.fn(), claim = vi.fn();
    await render(true, undefined, { activeSurface: "documents", onWorkspaceSelectionChange: context, onActiveSurfaceChange: claim });
    expect(context).toHaveBeenLastCalledWith({ kind: "local", context: { kind: "agent", sessionId: "a1" } });
    await render(true, { sessionId: "a1", seq: 1 }, { activeSurface: "documents", onActiveSurfaceChange: claim });
    expect(claim).not.toHaveBeenCalled();
    expect(bridge.focusAgent).not.toHaveBeenCalledWith("a1");
  });

  it("SSH files route to SFTP only when ready and never select a local project", async () => {
    bridge.load.mockResolvedValue(JSON.stringify(serializeWorkspace(ws([group("ssh", [tab("tab", leaf("ssh", { kind: "ssh", spawn: { kind: "ssh", sshHostName: "host" } }))])]))));
    const context = vi.fn(), files = vi.fn();
    await render(true, undefined, { onWorkspaceSelectionChange: context, onOpenWorkspaceFiles: files });
    expect(context).toHaveBeenLastCalledWith({ kind: "ssh", context: null });
    await render(true, undefined, { onOpenWorkspaceFiles: files, openFilesRequest: { seq: 1 } });
    expect(host.querySelector('[data-testid="sftp"]')).not.toBeNull();
    expect(files).not.toHaveBeenCalled();
  });

  it("SSH error files never fall back to the catalog", async () => {
    bridge.ssh.mockRejectedValueOnce(new Error("offline"));
    bridge.load.mockResolvedValue(JSON.stringify(serializeWorkspace(ws([group("ssh", [tab("tab", leaf("ssh", { kind: "ssh", spawn: { kind: "ssh", sshHostName: "host" } }))])]))));
    const files = vi.fn();
    await render(true, undefined, { onOpenWorkspaceFiles: files });
    await render(true, undefined, { onOpenWorkspaceFiles: files, openFilesRequest: { seq: 1 } });
    expect(files).not.toHaveBeenCalled();
    expect(host.querySelector('[data-testid="sftp"]')).toBeNull();
  });

  it("does not dereference an absent request while a leaf is pending", async () => {
    const pendingTab = { ...tab("pending", leaf("pending", { status: "pending" })), activeLeafId: undefined } as any;
    await act(async () => { root.render(<TerminalGroup group={group("pending", [pendingTab])} isFocused={false} canvasW={800} canvasH={600} getCanvasRect={() => null} terminals={{profiles:[],defaultKind:"",projectKinds:{}}} api={{phpVersions:[],nodeVersions:[]} as any} />); });
    expect(host.textContent).toContain("pending");
  });
});

it("preview ownership prevents xterm focus and workspace hotkeys through passive group removal", async () => {
 const claim = vi.fn(), context = vi.fn();
 await render(true, undefined, {activeSurface:"documents",onActiveSurfaceChange:claim,onWorkspaceSelectionChange:context});
 const preview = document.createElement('button');document.body.appendChild(preview);preview.focus();
 bridge.xtermFocus.mockClear();
 const shortcut = new KeyboardEvent('keydown',{key:'w',ctrlKey:true,bubbles:true,cancelable:true});
 await act(async () => {preview.dispatchEvent(shortcut);events.listeners.get('agent:sessions-reset')?.('');vi.advanceTimersByTime(100);});
 expect(shortcut.defaultPrevented).toBe(false);expect(bridge.stop).not.toHaveBeenCalled();
 expect(claim).not.toHaveBeenCalled();expect(bridge.xtermFocus).not.toHaveBeenCalled();
 expect(context).toHaveBeenLastCalledWith({ kind: "local", context: null });expect(document.activeElement).toBe(preview);preview.remove();
});
it("page and surface toggles keep the original PTYs without stop or respawn", async () => {
 await render(); const spawnCount = bridge.shell.mock.calls.length + bridge.resume.mock.calls.length;
 await render(false,undefined,{activeSurface:"documents"});
 await render(true,undefined,{activeSurface:"documents"});
 await render(true,undefined,{activeSurface:"terminal"});
 expect(bridge.shell.mock.calls.length+bridge.resume.mock.calls.length).toBe(spawnCount);expect(bridge.stop).not.toHaveBeenCalled();
});
it("clicking the selected tab explicitly returns ownership to terminal", async () => {
 const claim=vi.fn();await render(true,undefined,{activeSurface:"documents",onActiveSurfaceChange:claim});
 await act(async () => {host.querySelectorAll<HTMLElement>('[title="terminal.group.tabTooltip"]')[1].click();});
 expect(claim).toHaveBeenLastCalledWith("terminal");
});

it("passive programmatic/native focus never claims ownership from documents", async () => {
  const claim = vi.fn();
  await render(true, undefined, { activeSurface: "documents", onActiveSurfaceChange: claim });
  const preview = document.createElement("button");document.body.appendChild(preview);preview.focus();
  await act(async () => {terminals[2].textarea.focus();});
  expect(claim).not.toHaveBeenCalled();
  expect(bridge.focusAgent).toHaveBeenLastCalledWith("");
  const close = new KeyboardEvent("keydown",{key:"w",ctrlKey:true,bubbles:true,cancelable:true});
  await act(async () => {terminals[2].textarea.dispatchEvent(close);});
  expect(close.defaultPrevented).toBe(false);expect(bridge.stop).not.toHaveBeenCalled();
  preview.remove();
});

it.each([false,true])("Tab navigation (shift=%s) claims terminal once; unrelated later focus stays passive", async shiftKey => {
  const claim = vi.fn();
  await render(true, undefined, { activeSurface: "documents", onActiveSurfaceChange: claim });
  const preview = document.createElement("button");document.body.appendChild(preview);preview.focus();
  await act(async () => {
    const tab = new KeyboardEvent("keydown",{key:"Tab",shiftKey,bubbles:true,cancelable:true});
    preview.dispatchEvent(tab);
    // jsdom has no sequential-navigation default action. Model that action only
    // after the real keydown path has run. Ownership waits for completed dispatch.
    expect(tab.defaultPrevented).toBe(false);
    terminals[1].textarea.focus();
    expect(claim).not.toHaveBeenCalled();
    terminals[1].textarea.dispatchEvent(new KeyboardEvent("keyup",{key:"Tab",shiftKey,bubbles:true}));
    vi.advanceTimersByTime(1);
  });
  expect(claim).toHaveBeenCalledWith("terminal");claim.mockClear();
  await act(async () => {terminals[2].textarea.focus();});
  expect(claim).not.toHaveBeenCalled();preview.remove();
});

it("a canceled or expired Tab cannot authorize a later passive focus restoration", async () => {
  const claim = vi.fn();
  await render(true, undefined, { activeSurface: "documents", onActiveSurfaceChange: claim });
  const preview = document.createElement("button");document.body.appendChild(preview);preview.focus();
  const cancelTab = (event:KeyboardEvent) => { if(event.key === "Tab") event.preventDefault(); };
  preview.addEventListener("keydown",cancelTab);
  await act(async () => {
    preview.dispatchEvent(new KeyboardEvent("keydown",{key:"Tab",bubbles:true,cancelable:true}));
    terminals[1].textarea.focus();
  });
  expect(claim).not.toHaveBeenCalled();
  preview.removeEventListener("keydown",cancelTab);preview.focus();
  await act(async () => {
    preview.dispatchEvent(new KeyboardEvent("keydown",{key:"Tab",bubbles:true,cancelable:true}));
    vi.advanceTimersByTime(1);
    terminals[2].textarea.focus();
  });
  expect(claim).not.toHaveBeenCalled();preview.remove();
});

it.each(["documents", "navigator"] as const)("focus followed by preventDefault in a later Tab handler retains document ownership (%s)", async owner => {
  const claim = vi.fn();
  await render(true, undefined, { activeSurface: owner, onActiveSurfaceChange: claim });
  const preview = document.createElement("button");document.body.appendChild(preview);preview.focus();
  preview.addEventListener("keydown", event => {
    if (event.key === "Tab") {
      terminals[2].textarea.focus();
      event.preventDefault();
    }
  });
  const tab = new KeyboardEvent("keydown",{key:"Tab",bubbles:true,cancelable:true});
  await act(async () => {preview.dispatchEvent(tab);vi.advanceTimersByTime(1);});
  expect(tab.defaultPrevented).toBe(true);expect(claim).not.toHaveBeenCalled();
  expect(bridge.focusAgent).toHaveBeenLastCalledWith("");
  const close = new KeyboardEvent("keydown",{key:"w",ctrlKey:true,bubbles:true,cancelable:true});
  await act(async () => {terminals[2].textarea.dispatchEvent(close);});
  expect(close.defaultPrevented).toBe(false);expect(bridge.stop).not.toHaveBeenCalled();preview.remove();
});

it.each(["documents", "navigator"] as const)("a Tab destination removed before completed-dispatch checking cannot claim ownership (%s)", async owner => {
  const claim = vi.fn();
  await render(true, undefined, { activeSurface: owner, onActiveSurfaceChange: claim });
  const preview = document.createElement("button");document.body.appendChild(preview);preview.focus();
  await act(async () => {
    preview.dispatchEvent(new KeyboardEvent("keydown",{key:"Tab",bubbles:true,cancelable:true}));
    terminals[1].textarea.focus();
    root.render(<div />);
  });
  await act(async () => {vi.advanceTimersByTime(1);});
  expect(claim).not.toHaveBeenCalled();preview.remove();
});

it.each(["documents", "navigator"] as const)("focus elsewhere after a Tab destination invalidates its deferred ownership claim (%s)", async owner => {
  const claim = vi.fn();
  await render(true, undefined, { activeSurface: owner, onActiveSurfaceChange: claim });
  const preview = document.createElement("button");document.body.appendChild(preview);preview.focus();
  await act(async () => {
    preview.dispatchEvent(new KeyboardEvent("keydown",{key:"Tab",bubbles:true,cancelable:true}));
    terminals[1].textarea.focus();
    terminals[1].textarea.dispatchEvent(new KeyboardEvent("keyup",{key:"Tab",bubbles:true}));
    preview.focus();
    vi.advanceTimersByTime(1);
  });
  expect(claim).not.toHaveBeenCalled();preview.remove();
});

it('navigator passive focus cannot retarget the selected leaf, but completed Tab and pointer can', async () => {
 const context=vi.fn(), claim=vi.fn(); await render(true,undefined,{activeSurface:'navigator',onWorkspaceSelectionChange:context,onActiveSurfaceChange:claim});
 expect(bridge.xtermFocus).not.toHaveBeenCalled();
 await act(async () => terminals[2].textarea.focus());expect(context).toHaveBeenLastCalledWith({kind:'local',context:{kind:'agent',sessionId:'left'}});expect(claim).not.toHaveBeenCalled();
 const input=document.createElement('button');document.body.appendChild(input);input.focus();
 await act(async () => {input.dispatchEvent(new KeyboardEvent('keydown',{key:'Tab',bubbles:true,cancelable:true}));terminals[2].textarea.focus();vi.advanceTimersByTime(1);});
 expect(claim).toHaveBeenLastCalledWith('terminal');expect(context).toHaveBeenLastCalledWith({kind:'local',context:{kind:'agent',sessionId:'right'}});input.remove();
});
