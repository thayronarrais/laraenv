import { allLeaves } from "./treeUtils";
import type { Group, OpenTerminal, Workspace } from "./types";

type Translate = (key: string, vars?: Record<string, string | number>) => string;

// The mounted workspace is the source of truth. Neither saved layouts nor
// agent history can tell us which terminal panes are currently open.
export function collectOpenTerminals(workspace: Workspace, translate: Translate): OpenTerminal[] {
  const topGroup = workspace.groups.reduce<Group | null>((top, group) => !top || group.z > top.z ? group : top, null);
  return workspace.groups.flatMap((group) => group.tabs.flatMap((tab) => {
    const leaves = allLeaves(tab.tree);
    return leaves.map((leaf, index) => {
      const spawn = leaf.spawn;
      const shell = leaf.kind.startsWith("agent:") ? leaf.kind.slice(6) : spawn.terminalKind || leaf.kind;
      const context = [shell, spawn.sshHostName, spawn.projectName, spawn.worktreePath || spawn.cwd]
        .filter((part): part is string => !!part);
      return {
        terminalId: leaf.id,
        ...(spawn.laraSessionId ? {laraSessionId: spawn.laraSessionId} : {}),
        label: leaves.length === 1 ? tab.label : `${tab.label} · ${leaf.label} · ${translate("terminal.workspace.paneNumber", { number: index + 1 })}`,
        hint: [...new Set(context)].join(" · "),
        active: group.id === topGroup?.id && tab.tabId === group.activeTabId && leaf.id === tab.activeLeafId,
      };
    });
  }));
}
