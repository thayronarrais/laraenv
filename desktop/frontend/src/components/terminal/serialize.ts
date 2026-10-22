// Layout persistence: serialize the live workspace to a compact, id-free shape
// (only structure + spawn descriptors), and hydrate it back with fresh ids and
// every leaf marked "pending" so the startup respawn loop can fill real PTY ids.

import type { Group, Leaf, Pane, SnapState, Tab, Workspace } from "./types";

type PLeaf = { type: "leaf"; label: string; kind: string; spawn: Leaf["spawn"] };
type PSplit = { type: "split"; direction: "horizontal" | "vertical"; children: [PPane, PPane] };
type PPane = PLeaf | PSplit;
type PTab = { label: string; tree: PPane; activeLeafIndex: number };
type PGroup = { x: number; y: number; w: number; h: number; snap: SnapState; activeTabIndex: number; tabs: PTab[] };
export type PersistedWorkspace = { version: 1 | 2; groups: PGroup[] };

export function persistedWorkspaceContainsAgent(workspace: PersistedWorkspace | null | undefined) {
  const containsAgent = (pane: PPane): boolean => pane.type === "leaf"
    ? pane.spawn.kind === "agent"
    : containsAgent(pane.children[0]) || containsAgent(pane.children[1]);
  return !!workspace?.groups?.some((group) => group.tabs.some((tab) => containsAgent(tab.tree)));
}

// ---------- serialize ----------

function sPane(node: Pane): PPane {
  if (node.type === "leaf") return { type: "leaf", label: node.label, kind: node.kind, spawn: node.spawn };
  return { type: "split", direction: node.direction, children: [sPane(node.children[0]), sPane(node.children[1])] };
}

function leafOrder(node: Pane): Leaf[] {
  return node.type === "leaf" ? [node] : [...leafOrder(node.children[0]), ...leafOrder(node.children[1])];
}

function sTab(t: Tab): PTab {
  const order = leafOrder(t.tree);
  const activeLeafIndex = Math.max(0, order.findIndex((l) => l.id === t.activeLeafId));
  return { label: t.label, tree: sPane(t.tree), activeLeafIndex };
}

export function serializeWorkspace(ws: Workspace): PersistedWorkspace {
  return {
    version: 2,
    groups: ws.groups.map((g) => ({
      x: g.x, y: g.y, w: g.w, h: g.h, snap: g.snap,
      activeTabIndex: Math.max(0, g.tabs.findIndex((t) => t.tabId === g.activeTabId)),
      tabs: g.tabs.map(sTab),
    })),
  };
}

// ---------- hydrate ----------

function rand(): string {
  return Math.random().toString(36).slice(2, 8);
}

// Rebuilds a Pane with fresh ids; collects the created leaves in document order
// so the caller can map an activeLeafIndex back to a concrete id.
function hPane(node: PPane, sink: Leaf[]): Pane {
  if (node.type === "leaf") {
    const leaf: Leaf = {
      type: "leaf",
      id: `leaf-${rand()}`,
      label: node.label,
      kind: node.kind,
      status: "pending",
      spawn: node.spawn,
    };
    sink.push(leaf);
    return leaf;
  }
  return { type: "split", direction: node.direction, children: [hPane(node.children[0], sink), hPane(node.children[1], sink)] };
}

function hTab(t: PTab): Tab {
  const leaves: Leaf[] = [];
  const tree = hPane(t.tree, leaves);
  const active = leaves[Math.min(t.activeLeafIndex, leaves.length - 1)] ?? leaves[0];
  return { tabId: `tab-${rand()}`, label: t.label, tree, activeLeafId: active.id };
}

export function hydrateWorkspace(pw: PersistedWorkspace): Workspace {
  let z = 1;
  const groups: Group[] = pw.groups.map((pg, i) => {
    const tabs = pg.tabs.map(hTab);
    const activeTab = tabs[Math.min(pg.activeTabIndex, tabs.length - 1)] ?? tabs[0];
    return {
      id: `grp-${i + 1}-${rand()}`,
      x: pg.x, y: pg.y, w: pg.w, h: pg.h,
      z: ++z, snap: pg.snap ?? null,
      tabs, activeTabId: activeTab?.tabId ?? "",
    };
  }).filter((g) => g.tabs.length > 0);
  return { groups, zCounter: z, nextGroupNum: groups.length + 1 };
}

// Every leaf in the workspace paired with its group/tab, in respawn order.
// Used by the orchestrator's startup loop.
export function leavesToRespawn(ws: Workspace): { groupId: string; tabId: string; leaf: Leaf }[] {
  const out: { groupId: string; tabId: string; leaf: Leaf }[] = [];
  for (const g of ws.groups) {
    for (const t of g.tabs) {
      for (const l of leafOrder(t.tree)) out.push({ groupId: g.id, tabId: t.tabId, leaf: l });
    }
  }
  return out;
}
