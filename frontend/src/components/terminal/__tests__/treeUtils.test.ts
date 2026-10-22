import { describe, it, expect } from "vitest";
import { allLeaves, findLeaf, removeLeaf, splitLeafInTree } from "../treeUtils";
import { leaf } from "./helpers";
import type { Split } from "../types";

describe("treeUtils", () => {
  it("splitLeafInTree wraps the target in a split (side=after)", () => {
    const tree = leaf("a");
    const out = splitLeafInTree(tree, "a", leaf("b"), "horizontal", "after") as Split;
    expect(out.type).toBe("split");
    expect(out.direction).toBe("horizontal");
    expect(out.children.map((c) => (c.type === "leaf" ? c.id : "?"))).toEqual(["a", "b"]);
  });

  it("splitLeafInTree side=before puts the new leaf first", () => {
    const out = splitLeafInTree(leaf("a"), "a", leaf("b"), "vertical", "before") as Split;
    expect(out.children.map((c) => (c.type === "leaf" ? c.id : "?"))).toEqual(["b", "a"]);
  });

  it("removeLeaf collapses a split when one child is removed", () => {
    const tree = splitLeafInTree(leaf("a"), "a", leaf("b"), "horizontal", "after");
    const out = removeLeaf(tree, "b");
    expect(out).toEqual(leaf("a"));
  });

  it("removeLeaf returns null when the whole tree is gone", () => {
    expect(removeLeaf(leaf("a"), "a")).toBeNull();
  });

  it("allLeaves / findLeaf traverse nested splits", () => {
    const inner = splitLeafInTree(leaf("a"), "a", leaf("b"), "horizontal", "after");
    const tree = splitLeafInTree(inner, "b", leaf("c"), "vertical", "after");
    expect(allLeaves(tree).map((l) => l.id).sort()).toEqual(["a", "b", "c"]);
    expect(findLeaf(tree, "c")?.id).toBe("c");
    expect(findLeaf(tree, "zz")).toBeNull();
  });
});
