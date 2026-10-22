import { describe, it, expect } from "vitest";
import { workspaceReducer } from "../workspaceReducer";
import { allLeaves } from "../treeUtils";
import { group, leaf, tab, ws } from "./helpers";
import type { Workspace } from "../types";

describe("workspaceReducer", () => {
  it("ADD_GROUP_WITH_TAB appends a focused group and bumps counters", () => {
    const out = workspaceReducer(ws([]), {
      type: "ADD_GROUP_WITH_TAB", groupId: "g1", tab: tab("t1", leaf("a")), geom: { x: 1, y: 2, w: 3, h: 4 },
    });
    expect(out.groups.length).toBe(1);
    expect(out.groups[0].id).toBe("g1");
    expect(out.groups[0].z).toBe(out.zCounter);
    expect(out.nextGroupNum).toBe(2);
  });

  it("FOCUS_GROUP raises z above all others", () => {
    let s: Workspace = ws([group("g1", [tab("t1", leaf("a"))], { z: 1 }), group("g2", [tab("t2", leaf("b"))], { z: 2 })], { zCounter: 2 });
    s = workspaceReducer(s, { type: "FOCUS_GROUP", groupId: "g1" });
    expect(s.groups.find((g) => g.id === "g1")!.z).toBeGreaterThan(s.groups.find((g) => g.id === "g2")!.z);
  });

  it("SPLIT_LEAF then CLOSE_PANE collapses back to a single leaf", () => {
    let s: Workspace = ws([group("g1", [tab("t1", leaf("a"))])]);
    s = workspaceReducer(s, { type: "SPLIT_LEAF", groupId: "g1", tabId: "t1", targetLeafId: "a", newLeaf: leaf("b"), direction: "horizontal", side: "after" });
    expect(allLeaves(s.groups[0].tabs[0].tree).length).toBe(2);
    s = workspaceReducer(s, { type: "CLOSE_PANE", groupId: "g1", tabId: "t1", leafId: "b" });
    expect(allLeaves(s.groups[0].tabs[0].tree).map((l) => l.id)).toEqual(["a"]);
  });

  it("CLOSE_PANE on the last pane drops the tab, and last tab drops the group", () => {
    let s: Workspace = ws([group("g1", [tab("t1", leaf("a"))])]);
    s = workspaceReducer(s, { type: "CLOSE_PANE", groupId: "g1", tabId: "t1", leafId: "a" });
    expect(s.groups.length).toBe(0);
  });

  it("REORDER_TAB moves a tab to the target slot", () => {
    let s: Workspace = ws([group("g1", [tab("t1", leaf("a")), tab("t2", leaf("b")), tab("t3", leaf("c"))])]);
    s = workspaceReducer(s, { type: "REORDER_TAB", groupId: "g1", srcTabId: "t3", targetTabId: "t1" });
    expect(s.groups[0].tabs.map((t) => t.tabId)).toEqual(["t3", "t1", "t2"]);
  });

  it("MOVE_TAB relocates a tab between groups and removes an emptied source", () => {
    let s: Workspace = ws([group("g1", [tab("t1", leaf("a"))]), group("g2", [tab("t2", leaf("b"))])]);
    s = workspaceReducer(s, { type: "MOVE_TAB", srcGroupId: "g1", tabId: "t1", destGroupId: "g2" });
    expect(s.groups.map((g) => g.id)).toEqual(["g2"]);
    expect(s.groups[0].tabs.map((t) => t.tabId)).toEqual(["t2", "t1"]);
    expect(s.groups[0].activeTabId).toBe("t1");
  });

  it("TEAR_OFF_TAB spins a multi-tab group's tab into a new group", () => {
    let s: Workspace = ws([group("g1", [tab("t1", leaf("a")), tab("t2", leaf("b"))])]);
    s = workspaceReducer(s, { type: "TEAR_OFF_TAB", srcGroupId: "g1", tabId: "t2", geom: { x: 5, y: 5, w: 640, h: 420 } });
    expect(s.groups.length).toBe(2);
    expect(s.groups[0].tabs.map((t) => t.tabId)).toEqual(["t1"]);
    expect(s.groups[1].tabs.map((t) => t.tabId)).toEqual(["t2"]);
    expect(s.groups[1].x).toBe(5);
  });

  it("TEAR_OFF_TAB on a lone tab just relocates the group", () => {
    let s: Workspace = ws([group("g1", [tab("t1", leaf("a"))], { x: 0, y: 0 })]);
    s = workspaceReducer(s, { type: "TEAR_OFF_TAB", srcGroupId: "g1", tabId: "t1", geom: { x: 99, y: 88, w: 640, h: 420 } });
    expect(s.groups.length).toBe(1);
    expect(s.groups[0].x).toBe(99);
    expect(s.groups[0].snap).toBeNull();
  });

  it("MOVE_PANE moves a leaf into another tab's split and prunes the source tab", () => {
    let s: Workspace = ws([
      group("g1", [tab("t1", leaf("a"))]),
      group("g2", [tab("t2", leaf("b"))]),
    ]);
    s = workspaceReducer(s, { type: "MOVE_PANE", srcGroupId: "g1", srcTabId: "t1", leafId: "a", destGroupId: "g2", destTabId: "t2", targetLeafId: "b", direction: "horizontal", side: "after" });
    // g1 emptied → dropped; g2.t2 now has a + b
    expect(s.groups.map((g) => g.id)).toEqual(["g2"]);
    expect(allLeaves(s.groups[0].tabs[0].tree).map((l) => l.id).sort()).toEqual(["a", "b"]);
  });

  it("SET_SNAP / MOVE_GROUP toggle snap state", () => {
    let s: Workspace = ws([group("g1", [tab("t1", leaf("a"))])]);
    s = workspaceReducer(s, { type: "SET_SNAP", groupId: "g1", snap: "left" });
    expect(s.groups[0].snap).toBe("left");
    s = workspaceReducer(s, { type: "MOVE_GROUP", groupId: "g1", x: 30, y: 40 });
    expect(s.groups[0].snap).toBeNull();
    expect(s.groups[0].x).toBe(30);
  });

  it("REPLACE_LEAF swaps a pending leaf for a ready one and updates activeLeafId", () => {
    let s: Workspace = ws([group("g1", [tab("t1", leaf("pending-1", { status: "pending" }), "pending-1")])]);
    s = workspaceReducer(s, { type: "REPLACE_LEAF", groupId: "g1", tabId: "t1", leafId: "pending-1", replacement: leaf("term-9") });
    expect(s.groups[0].tabs[0].activeLeafId).toBe("term-9");
    expect(allLeaves(s.groups[0].tabs[0].tree).map((l) => l.id)).toEqual(["term-9"]);
  });

  it("APPLY_ADOPTED_SESSION promotes only the matching leaf", () => {
    const a = { ...leaf("term-1"), kind: "gitbash", label: "demo · git bash", spawn: { kind: "project" as const, projectName: "demo", terminalKind: "gitbash" } };
    const b = { ...leaf("term-2"), kind: "gitbash", label: "demo · git bash", spawn: { kind: "project" as const, projectName: "demo", terminalKind: "gitbash" } };
    let s: Workspace = ws([group("g1", [tab("t1", a), tab("t2", b)])]);
    s = workspaceReducer(s, { type: "APPLY_ADOPTED_SESSION", session: { id: "s1", terminalId: "term-2", agentId: "claude", projectName: "demo", state: "idle", adopted: true } });
    const leaves = s.groups[0].tabs.map((t) => allLeaves(t.tree)[0]);
    expect(leaves[0].kind).toBe("gitbash");
    expect(leaves[1].kind).toBe("agent:claude");
    expect(s.groups[0].tabs[1].label).toBe("demo · Claude Code");
  });
});
