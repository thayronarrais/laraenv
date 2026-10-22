// Tiny builders for tests.
import type { Group, Leaf, Tab, Workspace } from "../types";

export function leaf(id: string, over: Partial<Leaf> = {}): Leaf {
  return { type: "leaf", id, label: id, kind: "powershell", status: "ready", spawn: { kind: "local", terminalKind: "powershell" }, ...over };
}

export function tab(tabId: string, root: Leaf | Tab["tree"], activeLeafId?: string): Tab {
  const tree = root;
  const first = activeLeafId ?? (tree.type === "leaf" ? tree.id : firstLeafId(tree));
  return { tabId, label: tabId, tree, activeLeafId: first };
}

export function firstLeafId(node: Tab["tree"]): string {
  return node.type === "leaf" ? node.id : firstLeafId(node.children[0]);
}

export function group(id: string, tabs: Tab[], over: Partial<Group> = {}): Group {
  return { id, x: 0, y: 0, w: 600, h: 400, z: 1, snap: null, tabs, activeTabId: tabs[0]?.tabId ?? "", ...over };
}

export function ws(groups: Group[], over: Partial<Workspace> = {}): Workspace {
  return { groups, zCounter: groups.length, nextGroupNum: groups.length + 1, ...over };
}
