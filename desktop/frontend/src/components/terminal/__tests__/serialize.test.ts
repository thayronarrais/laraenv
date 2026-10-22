import { describe, it, expect } from "vitest";
import { serializeWorkspace, hydrateWorkspace, leavesToRespawn, persistedWorkspaceContainsAgent } from "../serialize";
import { splitLeafInTree } from "../treeUtils";
import { group, leaf, tab, ws } from "./helpers";

describe("serialize", () => {
  const tree = splitLeafInTree(leaf("a", { label: "php", spawn: { kind: "project", projectName: "aioke", terminalKind: "gitbash" } }), "a", leaf("b", { label: "vite" }), "horizontal", "after");
  const workspace = ws([
    group("g1", [tab("t1", tree, "b")], { x: 10, y: 20, w: 700, h: 500, snap: "left" }),
    group("g2", [tab("t2", leaf("c", { spawn: { kind: "ssh", sshHostName: "contaboo" } }))]),
  ]);

  it("round-trip preserves structure, labels, spawn; drops live ids + status", () => {
    const pw = serializeWorkspace(workspace);
    const hy = hydrateWorkspace(pw);

    expect(hy.groups.length).toBe(2);
    expect(hy.groups[0].snap).toBe("left");
    expect(hy.groups[0].x).toBe(10);

    const respawn = leavesToRespawn(hy);
    expect(respawn.length).toBe(3);
    // every hydrated leaf is pending with a fresh id and carries its spawn
    for (const r of respawn) {
      expect(r.leaf.status).toBe("pending");
      expect(r.leaf.id).not.toBe("a");
      expect(r.leaf.id).not.toBe("b");
      expect(r.leaf.id).not.toBe("c");
    }
    const labels = respawn.map((r) => r.leaf.label).sort();
    expect(labels).toEqual(["c", "php", "vite"]);
    const projectLeaf = respawn.find((r) => r.leaf.label === "php")!;
    expect(projectLeaf.leaf.spawn).toEqual({ kind: "project", projectName: "aioke", terminalKind: "gitbash" });
    const sshLeaf = respawn.find((r) => r.leaf.label === "c")!;
    expect(sshLeaf.leaf.spawn.sshHostName).toBe("contaboo");
  });

  it("preserves per-terminal PHP/Node runtime overrides on local leaves", () => {
    const local = ws([
      group("g1", [tab("t1", leaf("a", { label: "gitbash", spawn: { kind: "local", terminalKind: "gitbash", cwd: "C:\\proj", phpVersion: "8.4", nodeVersion: "22.11.0" } }))]),
    ]);
    const hy = hydrateWorkspace(serializeWorkspace(local));
    const l = leavesToRespawn(hy)[0].leaf;
    expect(l.spawn).toEqual({ kind: "local", terminalKind: "gitbash", cwd: "C:\\proj", phpVersion: "8.4", nodeVersion: "22.11.0" });
  });

  it("persists agent session descriptors in schema version 2", () => {
    const agent = ws([
      group("g1", [tab("t1", leaf("agent", { label: "demo · Codex", kind: "agent:codex", spawn: { kind: "agent", projectName: "demo", agentId: "codex", laraSessionId: "lara-1" } }))]),
    ]);
    const persisted = serializeWorkspace(agent);
    expect(persisted.version).toBe(2);
    expect(persistedWorkspaceContainsAgent(persisted)).toBe(true);
    expect(persistedWorkspaceContainsAgent(serializeWorkspace(workspace))).toBe(false);
    const restored = leavesToRespawn(hydrateWorkspace(persisted))[0].leaf;
    expect(restored.spawn).toEqual({ kind: "agent", projectName: "demo", agentId: "codex", laraSessionId: "lara-1" });
  });

  it("keeps scoped shell authority distinct from an agent terminal across restore", () => {
    const scoped = ws([group("g", [tab("t", leaf("shell", {
      spawn: { kind: "local", terminalKind: "gitbash", workspaceContext: { kind: "agent", sessionId: "a1" }, workspaceRootKey: "physical-root" },
    }))])]);
    const restored = leavesToRespawn(hydrateWorkspace(serializeWorkspace(scoped)))[0].leaf;
    expect(restored.spawn.workspaceContext).toEqual({ kind: "agent", sessionId: "a1" });
    expect(restored.spawn.workspaceRootKey).toBe("physical-root");
    expect(restored.spawn.laraSessionId).toBeUndefined();
  });

  it.each([1, 2] as const)("hydrates old v%d descriptors without inventing context", (version) => {
    const old = serializeWorkspace(workspace);
    old.version = version;
    const restored = leavesToRespawn(hydrateWorkspace(JSON.parse(JSON.stringify(old))));
    expect(restored).toHaveLength(3);
    expect(restored[0].leaf.spawn.workspaceContext).toBeUndefined();
    expect(restored[2].leaf.spawn.sshHostName).toBe("contaboo");
  });

  it("hydrate restores activeLeafId by index", () => {
    const pw = serializeWorkspace(workspace);
    const hy = hydrateWorkspace(pw);
    const t1 = hy.groups[0].tabs[0];
    // original active was leaf "b" (index 1) → second leaf in document order
    const order = leavesToRespawn(hy).filter((r) => r.tabId === t1.tabId).map((r) => r.leaf.id);
    expect(t1.activeLeafId).toBe(order[1]);
  });
});
