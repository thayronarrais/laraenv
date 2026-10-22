import { describe, expect, it } from "vitest";
import { collectOpenTerminals } from "../openTerminals";
import { group, leaf, tab, ws } from "./helpers";

const translate = (key: string, vars?: Record<string, string | number>) => key === "terminal.workspace.paneNumber" ? `Panel ${vars?.number}` : key;

describe("collectOpenTerminals", () => {
  it("includes pending and failed panes in inactive tabs without agent-history data", () => {
    const workspace = ws([
      group("front", [
        tab("selected", leaf("live")),
        tab("remote", { type: "split", direction: "vertical", children: [
          leaf("connecting", { label: "same", kind: "ssh", status: "pending", spawn: { kind: "ssh", sshHostName: "server" } }),
          leaf("failed", { label: "same", kind: "ssh", status: "error", errorMsg: "Previous output must not enter the palette", spawn: { kind: "ssh", sshHostName: "server" } }),
        ] }),
      ], { z: 9 }),
      group("background", [tab("shell", leaf("other"))], { z: 1 }),
    ]);

    expect(collectOpenTerminals(workspace, translate)).toEqual([
      { terminalId: "live", label: "selected", hint: "powershell", active: true },
      { terminalId: "connecting", label: "remote · same · Panel 1", hint: "ssh · server", active: false },
      { terminalId: "failed", label: "remote · same · Panel 2", hint: "ssh · server", active: false },
      { terminalId: "other", label: "shell", hint: "powershell", active: false },
    ]);
  });

  it("uses the renamed tab title and current checkout metadata for an agent pane", () => {
    const workspace = ws([group("g", [tab("tab", leaf("pty-current", {
      kind: "agent:codex", label: "Provider default title",
      spawn: { kind: "agent", agentId: "codex", projectName: "demo", laraSessionId: "history-id", worktreePath: "C:\\worktrees\\feature", cwd: "C:\\project" },
    }))])]);
    workspace.groups[0].tabs[0].label = "Current task";

    expect(collectOpenTerminals(workspace, translate)).toEqual([
      { terminalId: "pty-current", laraSessionId: "history-id", label: "Current task", hint: "codex · demo · C:\\worktrees\\feature", active: true },
    ]);
  });
});
