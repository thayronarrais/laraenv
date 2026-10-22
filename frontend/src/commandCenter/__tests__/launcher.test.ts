import { describe, expect, it, vi } from "vitest";
import { computeLauncher, type LauncherData } from "../launcher";

const data = (): LauncherData => ({
  projects: ["demo-shop", "acme"],
  sshHosts: [],
  agentProviders: [
    { id: "claude", label: "Claude Code" },
    { id: "codex", label: "Codex" },
    { id: "kimi", label: "Kimi Code" },
  ],
  getProjectCommands: () => ({ artisan: [], npm: [] }),
  ensureProjectCommands: vi.fn(),
  runInTerminal: vi.fn(),
  openSSH: vi.fn(),
  launchAgent: vi.fn(),
  // Identity: this suite asserts on ids/labels built from data, not on
  // translated hint text, so resolving every key to itself keeps the
  // fixture readable while still exercising every d.t(...) call site.
  t: ((key: string) => key) as LauncherData["t"],
});

describe("command launcher agents", () => {
  it("turns project plus provider into a direct agent action", () => {
    const launcher = data();
    const state = computeLauncher("demo-shop codex", launcher);
    expect(state.mode).toBe("exclusive");
    expect(state.rows[0]).toMatchObject({ kind: "execute", label: "demo-shop + Codex", intent: { action: "launchAgent", project: "demo-shop", agentId: "codex" } });
    if (state.rows[0].kind === "execute") state.rows[0].run();
    expect(launcher.launchAgent).toHaveBeenCalledWith("demo-shop", "codex", "Codex");
  });

  it("suggests providers from a partial provider name", () => {
    const state = computeLauncher("acme cl", data());
    expect(state.rows).toEqual([expect.objectContaining({ kind: "suggest", label: "Claude Code", complete: "acme claude" })]);
  });
});
