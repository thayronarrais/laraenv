// Pure reducer for the terminal workspace. All structural mutation lives here;
// async PTY work (spawn/stop) is done by the orchestrator, which dispatches the
// resulting actions. Because reducers can't return side data, the orchestrator
// reads the current workspace (via a ref) to collect PTY ids to StopPty BEFORE
// dispatching a close action.

import { findLeaf, firstLeaf, mapLeaves, removeLeaf, replaceLeaf, splitLeafInTree } from "./treeUtils";
import { applyAdoptedSession, type AdoptedSessionView } from "./adoptedLeaf";
import type { Group, Leaf, Pane, SnapState, Tab, Workspace } from "./types";

export type Geom = { x: number; y: number; w: number; h: number };

export type Action =
  | { type: "HYDRATE"; workspace: Workspace }
  | { type: "ADD_GROUP_WITH_TAB"; tab: Tab; geom: Geom; groupId?: string }
  | { type: "ADD_GROUPS"; groups: Group[] }
  | { type: "CLOSE_GROUP"; groupId: string }
  | { type: "FOCUS_GROUP"; groupId: string }
  | { type: "MOVE_GROUP"; groupId: string; x: number; y: number }
  | { type: "RESIZE_GROUP"; groupId: string; x: number; y: number; w: number; h: number }
  | { type: "SET_SNAP"; groupId: string; snap: SnapState }
  | { type: "ADD_TAB"; groupId: string; tab: Tab; activate?: boolean }
  | { type: "CLOSE_TAB"; groupId: string; tabId: string }
  | { type: "SET_ACTIVE_TAB"; groupId: string; tabId: string }
  | { type: "RENAME_TAB"; groupId: string; tabId: string; label: string }
  | { type: "REORDER_TAB"; groupId: string; srcTabId: string; targetTabId: string }
  | { type: "MOVE_TAB"; srcGroupId: string; tabId: string; destGroupId: string; atIndex?: number }
  | { type: "TEAR_OFF_TAB"; srcGroupId: string; tabId: string; geom: Geom }
  | { type: "SPLIT_LEAF"; groupId: string; tabId: string; targetLeafId: string; newLeaf: Leaf; direction: "horizontal" | "vertical"; side: "before" | "after" }
  | { type: "DROP_TAB_AS_SPLIT"; srcGroupId: string; tabId: string; destGroupId: string; destTabId: string; targetLeafId: string; direction: "horizontal" | "vertical"; side: "before" | "after" }
  | { type: "MOVE_PANE"; srcGroupId: string; srcTabId: string; leafId: string; destGroupId: string; destTabId: string; targetLeafId: string; direction: "horizontal" | "vertical"; side: "before" | "after" }
  | { type: "CLOSE_PANE"; groupId: string; tabId: string; leafId: string }
  | { type: "FOCUS_PANE"; groupId: string; tabId: string; leafId: string }
  | { type: "PATCH_LEAF"; groupId: string; tabId: string; leafId: string; patch: Partial<Leaf> }
  | { type: "REPLACE_LEAF"; groupId: string; tabId: string; leafId: string; replacement: Leaf }
  | { type: "APPLY_ADOPTED_SESSION"; session: AdoptedSessionView };

export const emptyWorkspace: Workspace = { groups: [], zCounter: 1, nextGroupNum: 1 };

// ---------- small immutable helpers ----------

function mapGroup(ws: Workspace, groupId: string, fn: (g: Group) => Group): Workspace {
  return { ...ws, groups: ws.groups.map((g) => (g.id === groupId ? fn(g) : g)) };
}

function mapTab(g: Group, tabId: string, fn: (t: Tab) => Tab): Group {
  return { ...g, tabs: g.tabs.map((t) => (t.tabId === tabId ? fn(t) : t)) };
}

// Removes a tab from a group, fixing activeTabId. Returns null if the group is
// left empty (caller drops it).
function removeTab(g: Group, tabId: string): Group | null {
  const tabs = g.tabs.filter((t) => t.tabId !== tabId);
  if (tabs.length === 0) return null;
  const activeTabId = g.tabs.find((t) => t.tabId === g.activeTabId && t.tabId !== tabId)
    ? g.activeTabId
    : tabs[tabs.length - 1].tabId;
  return { ...g, tabs, activeTabId };
}

function getGroup(ws: Workspace, id: string): Group | undefined {
  return ws.groups.find((g) => g.id === id);
}
function getTab(g: Group, id: string): Tab | undefined {
  return g.tabs.find((t) => t.tabId === id);
}

function focusGeom(ws: Workspace, groupId: string): Workspace {
  const z = ws.zCounter + 1;
  return { ...mapGroup(ws, groupId, (g) => ({ ...g, z })), zCounter: z };
}

// ---------- reducer ----------

export function workspaceReducer(ws: Workspace, action: Action): Workspace {
  switch (action.type) {
    case "HYDRATE":
      return action.workspace;

    case "ADD_GROUP_WITH_TAB": {
      const z = ws.zCounter + 1;
      const group: Group = {
        id: action.groupId ?? `grp-${ws.nextGroupNum}-${rand()}`,
        x: action.geom.x, y: action.geom.y, w: action.geom.w, h: action.geom.h,
        z, snap: null,
        tabs: [action.tab],
        activeTabId: action.tab.tabId,
      };
      return { ...ws, groups: [...ws.groups, group], zCounter: z, nextGroupNum: ws.nextGroupNum + 1 };
    }

    case "ADD_GROUPS": {
      // Append pre-built groups (used by re-attach), assigning fresh z so they
      // come to the front in order.
      let z = ws.zCounter;
      const groups = action.groups.map((g) => ({ ...g, z: ++z }));
      return { ...ws, groups: [...ws.groups, ...groups], zCounter: z, nextGroupNum: ws.nextGroupNum + groups.length };
    }

    case "CLOSE_GROUP":
      return { ...ws, groups: ws.groups.filter((g) => g.id !== action.groupId) };

    case "FOCUS_GROUP":
      return focusGeom(ws, action.groupId);

    case "MOVE_GROUP":
      return mapGroup(ws, action.groupId, (g) => ({ ...g, x: action.x, y: action.y, snap: null }));

    case "RESIZE_GROUP":
      return mapGroup(ws, action.groupId, (g) => ({ ...g, x: action.x, y: action.y, w: action.w, h: action.h, snap: null }));

    case "SET_SNAP":
      return mapGroup(ws, action.groupId, (g) => ({ ...g, snap: action.snap }));

    case "ADD_TAB":
      return mapGroup(ws, action.groupId, (g) => ({
        ...g,
        tabs: [...g.tabs, action.tab],
        activeTabId: action.activate === false ? g.activeTabId : action.tab.tabId,
      }));

    case "CLOSE_TAB": {
      const g = getGroup(ws, action.groupId);
      if (!g) return ws;
      const next = removeTab(g, action.tabId);
      if (!next) return { ...ws, groups: ws.groups.filter((x) => x.id !== g.id) };
      return mapGroup(ws, g.id, () => next);
    }

    case "SET_ACTIVE_TAB":
      return mapGroup(ws, action.groupId, (g) => ({ ...g, activeTabId: action.tabId }));

    case "RENAME_TAB":
      return mapGroup(ws, action.groupId, (g) =>
        mapTab(g, action.tabId, (t) => ({ ...t, label: action.label.trim() || "term" })));

    case "REORDER_TAB":
      return mapGroup(ws, action.groupId, (g) => {
        if (action.srcTabId === action.targetTabId) return g;
        const src = g.tabs.find((t) => t.tabId === action.srcTabId);
        if (!src) return g;
        const without = g.tabs.filter((t) => t.tabId !== action.srcTabId);
        const idx = without.findIndex((t) => t.tabId === action.targetTabId);
        if (idx < 0) return g;
        return { ...g, tabs: [...without.slice(0, idx), src, ...without.slice(idx)] };
      });

    case "MOVE_TAB": {
      if (action.srcGroupId === action.destGroupId) return ws;
      const src = getGroup(ws, action.srcGroupId);
      const tab = src && getTab(src, action.tabId);
      if (!src || !tab) return ws;
      let groups = ws.groups;
      const trimmed = removeTab(src, action.tabId);
      groups = trimmed
        ? groups.map((g) => (g.id === src.id ? trimmed : g))
        : groups.filter((g) => g.id !== src.id);
      groups = groups.map((g) => {
        if (g.id !== action.destGroupId) return g;
        const at = action.atIndex ?? g.tabs.length;
        const tabs = [...g.tabs.slice(0, at), tab, ...g.tabs.slice(at)];
        return { ...g, tabs, activeTabId: tab.tabId };
      });
      return { ...ws, groups };
    }

    case "TEAR_OFF_TAB": {
      const src = getGroup(ws, action.srcGroupId);
      const tab = src && getTab(src, action.tabId);
      if (!src || !tab) return ws;
      if (src.tabs.length === 1) {
        // Already a lone tab — just relocate the group rather than spawn a new one.
        return focusGeom(mapGroup(ws, src.id, (g) => ({ ...g, ...action.geom, snap: null })), src.id);
      }
      const trimmed = removeTab(src, action.tabId)!;
      const z = ws.zCounter + 1;
      const group: Group = {
        id: `grp-${ws.nextGroupNum}-${rand()}`,
        ...action.geom, z, snap: null,
        tabs: [tab], activeTabId: tab.tabId,
      };
      return {
        ...ws,
        groups: [...ws.groups.map((g) => (g.id === src.id ? trimmed : g)), group],
        zCounter: z,
        nextGroupNum: ws.nextGroupNum + 1,
      };
    }

    case "SPLIT_LEAF":
      return mapGroup(ws, action.groupId, (g) =>
        mapTab(g, action.tabId, (t) => ({
          ...t,
          tree: splitLeafInTree(t.tree, action.targetLeafId, action.newLeaf, action.direction, action.side),
          activeLeafId: action.newLeaf.id,
        })));

    case "DROP_TAB_AS_SPLIT": {
      const src = getGroup(ws, action.srcGroupId);
      const tab = src && getTab(src, action.tabId);
      const dest = getGroup(ws, action.destGroupId);
      if (!src || !tab || !dest) return ws;
      // Self-drop guard: dropping a tab onto a leaf inside itself is a no-op.
      if (action.srcGroupId === action.destGroupId && action.tabId === action.destTabId) return ws;
      const moved = tab.tree; // the whole subtree becomes one side of the split
      let groups = ws.groups.map((g) =>
        g.id === action.destGroupId
          ? mapTab(g, action.destTabId, (t) => ({
              ...t,
              tree: splitSubtree(t.tree, action.targetLeafId, moved, action.direction, action.side),
              activeLeafId: firstLeaf(moved).id,
            }))
          : g);
      const trimmed = removeTab(src, action.tabId);
      groups = trimmed
        ? groups.map((g) => (g.id === src.id ? trimmed : g))
        : groups.filter((g) => g.id !== src.id);
      return { ...ws, groups };
    }

    case "MOVE_PANE": {
      const src = getGroup(ws, action.srcGroupId);
      const srcTab = src && getTab(src, action.srcTabId);
      if (!src || !srcTab) return ws;
      const leaf = findLeaf(srcTab.tree, action.leafId);
      if (!leaf) return ws;
      // No-op: dropping a leaf onto itself.
      if (action.leafId === action.targetLeafId) return ws;

      // 1) insert into destination tab.
      let groups = ws.groups.map((g) =>
        g.id === action.destGroupId
          ? mapTab(g, action.destTabId, (t) => ({
              ...t,
              tree: splitLeafInTree(t.tree, action.targetLeafId, { ...leaf }, action.direction, action.side),
              activeLeafId: leaf.id,
            }))
          : g);

      // 2) remove from source tab (which may now equal the just-edited dest).
      groups = groups.flatMap((g) => {
        if (g.id !== action.srcGroupId) return [g];
        const tabs: Tab[] = [];
        for (const t of g.tabs) {
          if (t.tabId !== action.srcTabId) { tabs.push(t); continue; }
          const newTree = removeLeaf(t.tree, action.leafId);
          if (!newTree) continue; // tab emptied → drop it
          const activeLeafId = findLeaf(newTree, t.activeLeafId) ? t.activeLeafId : firstLeaf(newTree).id;
          tabs.push({ ...t, tree: newTree, activeLeafId });
        }
        if (tabs.length === 0) return []; // group emptied → drop it
        const activeTabId = tabs.find((t) => t.tabId === g.activeTabId) ? g.activeTabId : tabs[tabs.length - 1].tabId;
        return [{ ...g, tabs, activeTabId }];
      });
      return { ...ws, groups };
    }

    case "CLOSE_PANE": {
      const g = getGroup(ws, action.groupId);
      const tab = g && getTab(g, action.tabId);
      if (!g || !tab) return ws;
      const newTree = removeLeaf(tab.tree, action.leafId);
      if (newTree) {
        const activeLeafId = findLeaf(newTree, tab.activeLeafId) ? tab.activeLeafId : firstLeaf(newTree).id;
        return mapGroup(ws, g.id, (gg) => mapTab(gg, tab.tabId, (t) => ({ ...t, tree: newTree, activeLeafId })));
      }
      // tab emptied → drop tab (and group if it was the last).
      const next = removeTab(g, tab.tabId);
      if (!next) return { ...ws, groups: ws.groups.filter((x) => x.id !== g.id) };
      return mapGroup(ws, g.id, () => next);
    }

    case "FOCUS_PANE":
      return mapGroup(ws, action.groupId, (g) =>
        mapTab(g, action.tabId, (t) => ({ ...t, activeLeafId: action.leafId })));

    case "PATCH_LEAF":
      return mapGroup(ws, action.groupId, (g) =>
        mapTab(g, action.tabId, (t) => ({
          ...t,
          tree: mapLeafById(t.tree, action.leafId, (l) => ({ ...l, ...action.patch })),
        })));

    case "REPLACE_LEAF":
      return mapGroup(ws, action.groupId, (g) =>
        mapTab(g, action.tabId, (t) => {
          const tree = replaceLeaf(t.tree, action.leafId, action.replacement);
          const activeLeafId = t.activeLeafId === action.leafId ? action.replacement.id : t.activeLeafId;
          return { ...t, tree, activeLeafId };
        }));

    case "APPLY_ADOPTED_SESSION": {
      let changed = false;
      const groups = ws.groups.map((g) => ({
        ...g,
        tabs: g.tabs.map((t) => {
          const tree = mapLeaves(t.tree, (l) => {
            const next = applyAdoptedSession(l, action.session);
            if (next !== l) changed = true;
            return next;
          });
          if (tree === t.tree) return t;
          const active = findLeaf(tree, t.activeLeafId);
          return { ...t, tree, label: active?.label ?? t.label };
        }),
      }));
      return changed ? { ...ws, groups } : ws;
    }

    default:
      return ws;
  }
}

// ---------- local tree helpers ----------

function mapLeafById(node: Pane, id: string, fn: (l: Leaf) => Leaf): Pane {
  if (node.type === "leaf") return node.id === id ? fn(node) : node;
  return {
    ...node,
    children: [mapLeafById(node.children[0], id, fn), mapLeafById(node.children[1], id, fn)] as [Pane, Pane],
  };
}

// Like splitLeafInTree but the new side is an arbitrary subtree (not a fresh leaf).
function splitSubtree(
  tree: Pane,
  targetLeafId: string,
  subtree: Pane,
  direction: "horizontal" | "vertical",
  side: "before" | "after",
): Pane {
  const target = findLeaf(tree, targetLeafId) ?? firstLeaf(tree);
  const children: [Pane, Pane] = side === "before" ? [subtree, { ...target }] : [{ ...target }, subtree];
  return replaceLeaf(tree, target.id, { type: "split", direction, children });
}

function rand(): string {
  return Math.random().toString(36).slice(2, 7);
}
