// Pure helpers over the split tree (Pane = Leaf | Split). Lifted verbatim from
// the old TerminalTabs so they can be unit-tested in isolation.

import type { Leaf, Pane, Split } from "./types";

export function findLeaf(node: Pane, id: string): Leaf | null {
  if (node.type === "leaf") return node.id === id ? node : null;
  return findLeaf(node.children[0], id) ?? findLeaf(node.children[1], id);
}

export function firstLeaf(node: Pane): Leaf {
  return node.type === "leaf" ? node : firstLeaf(node.children[0]);
}

export function allLeaves(node: Pane): Leaf[] {
  if (node.type === "leaf") return [node];
  return [...allLeaves(node.children[0]), ...allLeaves(node.children[1])];
}

export function mapLeaves(node: Pane, fn: (l: Leaf) => Pane): Pane {
  if (node.type === "leaf") return fn(node);
  return {
    ...node,
    children: [mapLeaves(node.children[0], fn), mapLeaves(node.children[1], fn)] as [Pane, Pane],
  };
}

export function replaceLeaf(node: Pane, id: string, replacement: Pane): Pane {
  return mapLeaves(node, (l) => (l.id === id ? replacement : l));
}

// Removes the leaf from the tree. If its parent split would be left with a
// single child, that child takes the parent's place (splits never have one
// child). Returns null when the whole tree is gone.
export function removeLeaf(node: Pane, id: string): Pane | null {
  if (node.type === "leaf") return node.id === id ? null : node;
  const a = removeLeaf(node.children[0], id);
  const b = removeLeaf(node.children[1], id);
  if (a && b) return { ...node, children: [a, b] };
  return a ?? b ?? null;
}

// Splits `targetLeafId` into a Split containing the old leaf and `newLeaf`.
// side="before" puts newLeaf first (top/left drop), "after" puts it second
// (bottom/right drop).
export function splitLeafInTree(
  tree: Pane,
  targetLeafId: string,
  newLeaf: Leaf,
  direction: "horizontal" | "vertical",
  side: "before" | "after",
): Pane {
  const target = findLeaf(tree, targetLeafId) ?? firstLeaf(tree);
  const children: [Pane, Pane] =
    side === "before" ? [newLeaf, { ...target }] : [{ ...target }, newLeaf];
  const split: Split = { type: "split", direction, children };
  return replaceLeaf(tree, target.id, split);
}

// Smallest positive integer not already taken — used to label "powershell 1",
// "powershell 2", … so reused numbers fill gaps.
export function nextNumber(taken: Set<number>): number {
  let n = 1;
  while (taken.has(n)) n++;
  return n;
}
