import { describe, expect, it } from "vitest";
import { activityCounts, activityDurationMs, activityTree } from "../activityModel";
import type { ActivityNode, ActivitySnapshot } from "../activityTypes";

const node = (id: string, extra: Partial<ActivityNode> = {}): ActivityNode => ({
  id, providerNodeId: id, kind: "agent", label: id, state: "unknown",
  firstObservedAt: "2026-10-15T12:00:00Z", lastObservedAt: "2026-10-15T12:00:05Z", ...extra,
});
const snapshot = (extra: Partial<ActivitySnapshot> = {}): ActivitySnapshot => ({
  sessionId: "session", sourceId: "owner", runId: "run", revision: 1, rootId: "root", provider: "claude",
  observedSince: "2026-10-15T12:00:00Z", observedAt: "2026-10-15T12:00:05Z",
  status: "available", messageCode: "none", capabilities: { children: true, backgroundTasks: false, liveState: true, duration: false, model: false, tokens: false },
  nodes: [], backgroundTasks: [], truncated: false, stale: false, ...extra,
});

describe("activity selectors", () => {
  it("counts root and descendants but not tasks", () => {
    const value = snapshot({ nodes: [node("root", {state: "working"}), node("a", {parentId: "root", state: "needs-input"}), node("b", {parentId: "a"}), node("task", {kind: "background-task", state: "working"})], backgroundTasks: [node("task", {kind: "background-task"})] });
    expect(activityCounts(value).agents).toBe(3);
    expect(activityCounts(value)).toEqual({ agents: 3, working: 1, waiting: 1, unknown: 1, lowerBound: false });
  });

  it("counts starting as working and preserves unknown without inferring liveness", () => {
    expect(activityCounts(snapshot({ nodes: [node("root", { state: "starting" }), node("child")] })))
      .toEqual({ agents: 2, working: 1, waiting: 0, unknown: 1, lowerBound: false });
  });

  it("marks truncated counts as lower bounds", () => {
    expect(activityCounts(snapshot({ truncated: true })).lowerBound).toBe(true);
  });

  it("distinguishes missing metrics from zero", () => {
    const unknownStart = node("root");
    const now = Date.parse("2026-10-15T12:00:05Z");
    expect(activityDurationMs(unknownStart, now, snapshot())).toBeNull();
    expect(unknownStart.tokens).toBeUndefined();
    const zero = node("zero", { state: "working", startedAt: "2026-10-15T12:00:05Z", tokens: { total: 0 }, model: {name: "configured", kind: "configured"} });
    expect(activityDurationMs(zero, now, snapshot())).toBe(0);
    expect(zero.tokens?.total).toBe(0);
    expect(zero.tokens?.input).toBeUndefined();
  });

  it("uses explicit end rather than last observation and rejects invalid timestamps", () => {
    const now = Date.parse("2026-10-15T12:01:00Z");
    expect(activityDurationMs(node("root", { state: "working", startedAt: "2026-10-15T12:00:00Z" }), now, snapshot())).toBe(60000);
    expect(activityDurationMs(node("root", { startedAt: "2026-10-15T12:00:00Z", endedAt: "2026-10-15T12:00:03Z" }), now, snapshot())).toBe(3000);
    expect(activityDurationMs(node("root", { state: "working", startedAt: "bad" }), now, snapshot())).toBeNull();
    expect(activityDurationMs(node("root", { startedAt: "2026-10-15T12:00:00Z", endedAt: "bad" }), now, snapshot())).toBeNull();
  });

  it("does not extrapolate unended intervals after observation ends but preserves observed completion", () => {
    const value = snapshot({stale: true, status: "partial", messageCode: "observation-ended"});
    const child = node("child", {startedAt: "2026-10-15T12:00:00Z", state: "unknown"});
    const completed = node("completed", {startedAt: "2026-10-15T12:00:00Z", endedAt: "2026-10-15T12:00:03Z", state: "reply-finished"});
    for (const now of [Date.parse("2026-10-15T12:01:00Z"), Date.parse("2026-10-15T12:02:00Z")]) {
      expect(activityDurationMs(child, now, value)).toBeNull();
      expect(activityDurationMs(completed, now, value)).toBe(3000);
    }
    expect(child.endedAt).toBeUndefined();
  });

  it("requires fresh observation for open intervals even when the last state was working", () => {
    const child = node("child", {startedAt: "2026-10-15T12:00:00Z", state: "working"});
    const now = Date.parse("2026-10-15T12:01:00Z");
    expect(activityDurationMs(child, now, snapshot({stale: true}))).toBeNull();
    expect(activityDurationMs(child, now, snapshot({messageCode: "observation-ended"}))).toBeNull();
    expect(activityDurationMs(child, now, snapshot())).toBe(60000);
    expect(activityDurationMs(child, now + 1000, snapshot())).toBe(61000);
  });

  it("requires confirmed state and enabled observation for an unended interval", () => {
    const child = node("child", {startedAt: "2026-10-15T12:00:00Z", state: "working"});
    const now = Date.parse("2026-10-15T12:01:00Z");
    expect(activityDurationMs({...child, state: "unknown"}, now, snapshot())).toBeNull();
    expect(activityDurationMs(child, now, snapshot({messageCode: "hooks-disabled"}))).toBeNull();
    expect(activityDurationMs({...child, state: "unknown", endedAt: "2026-10-15T12:00:03Z"}, now, snapshot({messageCode: "hooks-disabled"}))).toBe(3000);
  });

  it.each(["truncated", "unsupported-protocol", "hook-trust-required"] as const)("does not let the %s notice override unavailable live observation", messageCode => {
    const value = snapshot({status: "partial", messageCode, capabilities: {...snapshot().capabilities, liveState: false}});
    const child = node("child", {state: "working", startedAt: "2026-10-15T12:00:00Z"});
    for (const now of [Date.parse("2026-10-15T12:01:00Z"), Date.parse("2026-10-15T12:02:00Z")]) {
      expect(activityDurationMs(child, now, value)).toBeNull();
      expect(activityDurationMs({...child, endedAt: "2026-10-15T12:00:03Z"}, now, value)).toBe(3000);
    }
  });

  it("builds a root-first tree with orphans and each cyclic node represented once", () => {
    const value = snapshot({ nodes: [node("child", {parentId: "root"}), node("root"), node("orphan", {parentId: "missing"}), node("a", {parentId: "b"}), node("b", {parentId: "a"})], backgroundTasks: [node("task", {kind: "background-task", parentId: "root"})] });
    const tree = activityTree(value);
    expect(tree[0].node.id).toBe("root");
    expect(tree[0].children.map(item => item.node.id)).toEqual(["child", "task"]);
    const ids = (items: ReturnType<typeof activityTree>): string[] => items.flatMap(item => [item.node.id, ...ids(item.children)]);
    expect(ids(tree).sort()).toEqual(["a", "b", "child", "orphan", "root", "task"]);
    expect(value.nodes[0].parentId).toBe("root");
  });
});
