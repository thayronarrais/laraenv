import { beforeEach, describe, expect, it, vi } from "vitest";

const api = vi.hoisted(() => ({
  ListAgentProfiles: vi.fn(),
  ListAgentSessions: vi.fn(),
  ListProjects: vi.fn(),
  ListServices: vi.fn(),
}));

vi.mock("../../../wailsjs/go/main/App", () => ({
  ...api,
  ListServices: api.ListServices, StartService: vi.fn(), StopService: vi.fn(), ReloadService: vi.fn(),
  StartManagedAll: vi.fn(), StopManagedAll: vi.fn(), ReloadStack: vi.fn(),
  OpenProjectFolder: vi.fn(), OpenProjectInEditor: vi.fn(), OpenURL: vi.fn(), OpenProjectTerminal: vi.fn(),
}));

import { buildOpenTerminalCommands, buildBuiltinCommands } from "../builtins";
import type { CommandContext } from "../types";

// Identity `t`: this suite asserts on ids/behavior, not on rendered label
// text, so resolving every key to itself keeps the fixtures readable while
// still exercising every ctx.t(...) call site in builtins.ts.
const context = (): CommandContext => ({
  t: ((key: string) => key) as CommandContext["t"],
  navigate: vi.fn(), newProject: vi.fn(), openTerminal: vi.fn(), runInTerminal: vi.fn(), openSSH: vi.fn(), flash: vi.fn(),
  launchAgent: vi.fn(), focusAgentSession: vi.fn(), resumeAgentSession: vi.fn(), focusTerminal: vi.fn(),
});

describe("buildOpenTerminalCommands", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    api.ListServices.mockResolvedValue([]);
    api.ListProjects.mockResolvedValue([{ name: "lara4ever" }]);
    api.ListAgentProfiles.mockResolvedValue([
      { id: "codex", label: "Codex", available: true, version: "1.0", capabilities: { launch: true, resume: true } },
      { id: "claude", label: "Claude Code", available: false, capabilities: { launch: true, resume: true } },
    ]);
  });

  it("uses only mounted terminals and marks the current pane", () => {
    const ctx = context();
    ctx.openTerminals = [
      { terminalId: "ssh", label: "ssh · contaboo", hint: "SSH · contaboo", active: true },
      { terminalId: "local", label: "demo · Git Bash", hint: "Git Bash · demo", active: false },
    ];
    const commands = buildOpenTerminalCommands(ctx);
    expect(commands.map((command) => command.id)).toEqual(["terminal:focus:ssh", "terminal:focus:local"]);
    expect(commands[0].hint).toBe("commandCenter.builtins.currentTerminal · SSH · contaboo");
    expect(commands[1].hint).toBe("Git Bash · demo");
    expect(commands[0].intent).toEqual({ action: "focusTerminal", terminalId: "ssh" });
    expect(api.ListAgentSessions).not.toHaveBeenCalled();
  });

  it("offers no terminal entries when no workspace is mounted", () => {
    expect(buildOpenTerminalCommands(context())).toEqual([]);
  });

  it("switches by terminal ID instead of resuming or restarting an agent", () => {
    const ctx = context();
    ctx.openTerminals = [{ terminalId: "agent-pty", label: "Codex · demo", hint: "demo", active: false }];
    buildOpenTerminalCommands(ctx)[0].run();
    expect(ctx.focusTerminal).toHaveBeenCalledWith("agent-pty");
    expect(ctx.resumeAgentSession).not.toHaveBeenCalled();
    expect(ctx.launchAgent).not.toHaveBeenCalled();
  });
});

describe("experimental agent gate", () => {
  it("keeps Agents inside Terminal instead of creating duplicate navigation", async () => {
    api.ListProjects.mockResolvedValue([]);
    const commands = await buildBuiltinCommands(context(), false);
    expect(commands.some((command) => command.id === "nav:agents")).toBe(false);
    expect(commands.some((command) => command.id === "nav:terminal")).toBe(true);
  });
});
