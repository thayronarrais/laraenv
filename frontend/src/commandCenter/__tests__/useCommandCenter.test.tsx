// @vitest-environment jsdom
import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useCommandCenter } from "../useCommandCenter";
import type { CommandContext } from "../types";

const api = vi.hoisted(() => ({
  GetCommandCenter: vi.fn(), GetAgentSessionsEnabled: vi.fn(),
  ListAgentProfiles: vi.fn(), ListAgentSessions: vi.fn(),
  ListProjects: vi.fn(), ListSSHHosts: vi.fn(), ListServices: vi.fn(),
  ListProjectCommands: vi.fn(),
  events: new Map<string, (...args: any[]) => void>(),
}));
vi.mock("../../../wailsjs/go/main/App", () => ({
  ...api,
  StartService: vi.fn(), StopService: vi.fn(), ReloadService: vi.fn(),
  StartManagedAll: vi.fn(), StopManagedAll: vi.fn(), ReloadStack: vi.fn(),
  OpenProjectFolder: vi.fn(), OpenProjectInEditor: vi.fn(), OpenURL: vi.fn(), OpenProjectTerminal: vi.fn(),
}));
vi.mock("../../../wailsjs/runtime", () => ({ EventsOn: (name: string, callback: (...args: any[]) => void) => {
  api.events.set(name, callback);
  return () => { if (api.events.get(name) === callback) api.events.delete(name); };
} }));

const context = () => ({
  t: (key: string) => key,
  navigate: vi.fn(), newProject: vi.fn(), openTerminal: vi.fn(), runInTerminal: vi.fn(), openSSH: vi.fn(), flash: vi.fn(),
  launchAgent: vi.fn(), focusAgentSession: vi.fn(), resumeAgentSession: vi.fn(), focusTerminal: vi.fn(),
  openTerminals: [
    { terminalId: "local", label: "newracingtime · Git Bash", hint: "newracingtime · Git Bash", active: true },
    { terminalId: "ssh", label: "ssh · contaboo", hint: "SSH · contaboo", active: false },
    { terminalId: "agent", label: "Codex · demo", hint: "demo · Codex", active: false },
  ],
});

beforeEach(() => {
  vi.clearAllMocks();
  api.events.clear();
  api.ListProjectCommands.mockResolvedValue({ artisan: [], npmScripts: [] });
  api.GetCommandCenter.mockResolvedValue({ commands: [], paletteHotkey: "Ctrl+K" });
  api.GetAgentSessionsEnabled.mockResolvedValue(true);
  api.ListServices.mockResolvedValue([]);
  api.ListProjects.mockResolvedValue([]);
  api.ListSSHHosts.mockResolvedValue([]);
  api.ListAgentProfiles.mockResolvedValue([{ id: "codex", label: "Codex", available: true, capabilities: { launch: true, resume: true } }]);
  api.ListAgentSessions.mockResolvedValue([
    { id: "old", title: "Old session", projectName: "demo", agentId: "codex", state: "error", providerSessionId: "old-provider", updatedAt: "2026-10-01T10:00:00Z", lastPreview: "historical output" },
  ]);
});

describe('command discovery after runtime changes', () => {
  it('invalidates the changed project while retaining other cached vocabularies', async () => {
    api.ListProjectCommands.mockImplementation((name: string) => Promise.resolve({ artisan: [name === 'demo' ? 'old:runtime' : 'other:command'], npmScripts: [] }));
    const { result } = renderHook(() => useCommandCenter(context() as CommandContext));
    act(() => { result.current.ensureProjectCommands('demo'); result.current.ensureProjectCommands('other'); });
    await waitFor(() => expect(result.current.getProjectCommands('demo')?.artisan).toEqual(['old:runtime']));
    act(() => { api.events.get('project:runtime-changed')?.('demo'); });
    expect(result.current.getProjectCommands('demo')).toBeNull();
    expect(result.current.getProjectCommands('other')?.artisan).toEqual(['other:command']);
    api.ListProjectCommands.mockResolvedValue({ artisan: ['new:runtime'], npmScripts: ['dev'] });
    act(() => { result.current.ensureProjectCommands('demo'); });
    await waitFor(() => expect(result.current.getProjectCommands('demo')).toEqual({ artisan: ['new:runtime'], npm: ['dev'] }));
  });

  it('rejects a stale command response after a global runtime invalidation', async () => {
    let finishOld!: (value: any) => void;
    api.ListProjectCommands.mockImplementationOnce(() => new Promise(resolve => { finishOld = resolve; }))
      .mockResolvedValueOnce({ artisan: ['new:runtime'], npmScripts: [] });
    const { result } = renderHook(() => useCommandCenter(context() as CommandContext));
    act(() => { result.current.ensureProjectCommands('demo'); });
    act(() => { api.events.get('project:runtime-changed')?.(''); result.current.ensureProjectCommands('demo'); });
    await waitFor(() => expect(result.current.getProjectCommands('demo')?.artisan).toEqual(['new:runtime']));
    await act(async () => { finishOld({ artisan: ['old:runtime'], npmScripts: [] }); });
    expect(result.current.getProjectCommands('demo')?.artisan).toEqual(['new:runtime']);
  });
});

describe("command center open terminals", () => {
  it("puts every open terminal first without loading session history", async () => {
    const ctx = context();
    const { result } = renderHook(() => useCommandCenter(ctx as CommandContext));
    await waitFor(() => expect(result.current.commands.some((command) => command.id === "nav:terminal")).toBe(true));
    expect(result.current.commands.slice(0, 3).map((command) => command.id)).toEqual(["terminal:focus:local", "terminal:focus:ssh", "terminal:focus:agent"]);
    expect(result.current.commands.some((command) => command.id.startsWith("agent:session:"))).toBe(false);
    expect(api.ListAgentSessions).not.toHaveBeenCalled();
  });

  it("focuses an existing SSH or agent pane without launching another session", async () => {
    const ctx = context();
    const { result } = renderHook(() => useCommandCenter(ctx as CommandContext));
    await waitFor(() => expect(result.current.commands.length).toBeGreaterThan(0));
    act(() => {
      result.current.commands.find((command) => command.id === "terminal:focus:ssh")?.run();
      result.current.commands.find((command) => command.id === "terminal:focus:agent")?.run();
    });
    expect(ctx.focusTerminal.mock.calls).toEqual([["ssh"], ["agent"]]);
    expect(ctx.launchAgent).not.toHaveBeenCalled();
    expect(ctx.resumeAgentSession).not.toHaveBeenCalled();
    expect(ctx.openSSH).not.toHaveBeenCalled();
    expect(ctx.openTerminal).not.toHaveBeenCalled();
  });

  it("updates renamed and closed panes immediately from the workspace", async () => {
    const ctx = context();
    const { result, rerender } = renderHook(({ value }) => useCommandCenter(value as CommandContext), { initialProps: { value: ctx } });
    await waitFor(() => expect(result.current.commands.length).toBeGreaterThan(0));
    rerender({ value: { ...ctx, openTerminals: [{ ...ctx.openTerminals[0], label: "API work" }] } });
    const terminals = result.current.commands.filter((command) => command.id.startsWith("terminal:focus:"));
    expect(terminals.map((command) => command.label)).toEqual(["API work"]);
    expect(terminals[0].intent).toEqual({ action: "focusTerminal", terminalId: "local" });
  });

  it("forwards an explicit terminal focus intent to the current workspace", () => {
    const ctx = context();
    const { result } = renderHook(() => useCommandCenter(ctx as CommandContext));
    act(() => { result.current.runIntent({ action: "focusTerminal", terminalId: "ssh" }); });
    expect(ctx.focusTerminal).toHaveBeenCalledWith("ssh");
    expect(ctx.openSSH).not.toHaveBeenCalled();
  });

  it("keeps open terminals available when agent sessions are disabled", async () => {
    api.GetAgentSessionsEnabled.mockResolvedValue(false);
    const ctx = context();
    const { result } = renderHook(() => useCommandCenter(ctx as CommandContext));
    await waitFor(() => expect(result.current.commands.some((command) => command.id === "nav:terminal")).toBe(true));
    expect(result.current.commands.filter((command) => command.id.startsWith("terminal:focus:"))).toHaveLength(3);
    expect(result.current.agentProviders).toEqual([]);
  });
});
