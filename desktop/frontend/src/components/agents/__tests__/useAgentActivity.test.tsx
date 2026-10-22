// @vitest-environment jsdom
import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ActivitySnapshot } from "../activityTypes";

const bridge = vi.hoisted(() => ({
  get: vi.fn(), refresh: vi.fn(), set: vi.fn(), remove: vi.fn(),
  listeners: new Map<string, Set<(value?: ActivitySnapshot) => void>>(),
}));
vi.mock("../../../../wailsjs/go/main/App", () => ({
  GetAgentActivity: bridge.get, RefreshAgentActivity: bridge.refresh,
  SetAgentActivityInterest: bridge.set, RemoveAgentActivityInterest: bridge.remove,
}));
vi.mock("../../../../wailsjs/runtime", () => ({
  EventsOn: (name: string, listener: (value?: ActivitySnapshot) => void) => {
    const listeners = bridge.listeners.get(name) ?? new Set();
    bridge.listeners.set(name, listeners);
    listeners.add(listener);
    return () => listeners.delete(listener);
  },
}));
import { useAgentActivity } from "../useAgentActivity";
import { agentActivityApi, getAgentActivityWindowId } from "../agentActivityApi";

function snapshot(extra: Partial<ActivitySnapshot> = {}): ActivitySnapshot {
  return { sessionId: "a", sourceId: "source-z", runId: "run-z", revision: 10, rootId: "root", provider: "codex",
    observedSince: "2026-10-15T12:00:00Z", observedAt: "2026-10-15T12:00:05Z", status: "available", messageCode: "none",
    capabilities: {children: true, backgroundTasks: false, liveState: false, duration: false, model: false, tokens: false},
    nodes: [], backgroundTasks: [], truncated: false, stale: false, ...extra };
}
function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason: Error) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}
const options = { sessionId: "a", windowId: "window", purpose: "dialog" as const, enabled: true };
async function emit(value: ActivitySnapshot) {
  await act(async () => { for (const cb of bridge.listeners.get("agent:activity-changed") ?? []) cb(value); });
}

describe("useAgentActivity", () => {
  beforeEach(() => {
    vi.useFakeTimers(); bridge.listeners.clear(); vi.resetAllMocks();
    bridge.get.mockResolvedValue(snapshot()); bridge.refresh.mockResolvedValue(snapshot());
    bridge.set.mockResolvedValue(undefined); bridge.remove.mockResolvedValue(undefined);
  });
  afterEach(() => { vi.useRealTimers(); });

  it("ignores previous selection responses", async () => {
    const previous = deferred<ActivitySnapshot>();
    bridge.get.mockReturnValueOnce(previous.promise).mockResolvedValueOnce(snapshot({sessionId: "b"}));
    const { result, rerender } = renderHook(props => useAgentActivity(props), {initialProps: options});
    await act(async () => { rerender({...options, sessionId: "b"}); });
    await act(async () => { previous.resolve(snapshot({revision: 999})); });
    expect(result.current.snapshot?.sessionId).toBe("b");
    expect(result.current.loading).toBe(false);
    expect(bridge.get).toHaveBeenLastCalledWith("b");
  });

  it("keeps a newer event when the initial Get resolves later", async () => {
    const initial = deferred<ActivitySnapshot>(); const confirm = deferred<ActivitySnapshot>();
    bridge.get.mockReturnValueOnce(initial.promise).mockReturnValueOnce(confirm.promise);
    const { result } = renderHook(() => useAgentActivity(options));
    await emit(snapshot({revision: 12}));
    await act(async () => { initial.resolve(snapshot({revision: 10})); confirm.resolve(snapshot({revision: 11})); });
    expect(result.current.snapshot?.revision).toBe(12);
  });

  it("requires fresh owner confirmation when an event changes epoch before initial Get resolves", async () => {
    const initial = deferred<ActivitySnapshot>(); const confirm = deferred<ActivitySnapshot>();
    bridge.get.mockReturnValueOnce(initial.promise).mockReturnValueOnce(confirm.promise);
    const { result } = renderHook(() => useAgentActivity(options));
    await emit(snapshot({sourceId: "source-a", runId: "run-a", revision: 1}));
    expect(result.current.snapshot).toBeNull();
    await act(async () => { confirm.resolve(snapshot({sourceId: "source-a", runId: "run-a", revision: 1})); });
    await act(async () => { initial.resolve(snapshot({revision: 999})); });
    expect(result.current.snapshot).toMatchObject({sourceId: "source-a", runId: "run-a", revision: 1});
  });

  it("keeps newest revision across runs and confirms run changes with a fresh Get", async () => {
    const { result } = renderHook(() => useAgentActivity(options));
    await act(async () => {});
    const confirm = deferred<ActivitySnapshot>(); bridge.get.mockReturnValueOnce(confirm.promise);
    await emit(snapshot({runId: "run-a", revision: 11}));
    expect(result.current.snapshot?.runId).toBe("run-z");
    await act(async () => { confirm.resolve(snapshot({runId: "run-a", revision: 11})); });
    expect(result.current.snapshot?.runId).toBe("run-a");
    await emit(snapshot({runId: "run-z", revision: 10}));
    expect(result.current.snapshot?.revision).toBe(11);
    expect(result.current.snapshot?.runId).toBe("run-a");
    expect(bridge.get).toHaveBeenCalledTimes(2);
  });

  it("confirms changed owners without comparing their revisions or ordering UUIDs", async () => {
    const { result } = renderHook(() => useAgentActivity(options)); await act(async () => {});
    const confirm = deferred<ActivitySnapshot>(); bridge.get.mockReturnValueOnce(confirm.promise);
    await emit(snapshot({sourceId: "source-a", runId: "run-a", revision: 1}));
    expect(result.current.snapshot?.sourceId).toBe("source-z");
    await act(async () => { confirm.resolve(snapshot({sourceId: "source-a", runId: "run-a", revision: 1})); });
    await emit(snapshot({sourceId: "source-z", revision: 999}));
    expect(result.current.snapshot?.sourceId).toBe("source-a");
    expect(result.current.snapshot?.revision).toBe(1);
    expect(bridge.get).toHaveBeenCalledTimes(2);
  });

  it("a superseded confirmation Get cannot undo a newer confirmed epoch", async () => {
    const { result } = renderHook(() => useAgentActivity(options)); await act(async () => {});
    const old = deferred<ActivitySnapshot>(); const fresh = deferred<ActivitySnapshot>();
    bridge.get.mockReturnValueOnce(old.promise).mockReturnValueOnce(fresh.promise);
    await emit(snapshot({sourceId: "source-b", runId: "run-b", revision: 1}));
    await emit(snapshot({sourceId: "source-a", runId: "run-a", revision: 1}));
    await act(async () => { fresh.resolve(snapshot({sourceId: "source-a", runId: "run-a", revision: 2})); });
    await act(async () => { old.resolve(snapshot({sourceId: "source-b", runId: "run-b", revision: 1000})); });
    expect(result.current.snapshot?.sourceId).toBe("source-a");
    expect(result.current.snapshot?.revision).toBe(2);
  });

  it.each(["event", "manual refresh"] as const)("retries discarded owner confirmation through a later %s without losing newer revisions", async recovery => {
    const { result } = renderHook(() => useAgentActivity(options)); await act(async () => {});
    const discarded = deferred<ActivitySnapshot>();
    bridge.get.mockReturnValueOnce(discarded.promise);
    await emit(snapshot({sourceId: "source-a", runId: "run-a", revision: 3}));
    await emit(snapshot({revision: 12}));
    await act(async () => { discarded.resolve(snapshot({revision: 11})); });
    expect(result.current.snapshot).toMatchObject({sourceId: "source-z", revision: 12});

    const confirmation = deferred<ActivitySnapshot>();
    bridge.get.mockReturnValueOnce(confirmation.promise);
    const later = snapshot({sourceId: "source-a", runId: "run-a", revision: 2});
    if (recovery === "event") await emit(later);
    else {
      bridge.refresh.mockResolvedValueOnce(later);
      await act(async () => { await result.current.refresh(); });
    }
    expect(bridge.get).toHaveBeenCalledTimes(3);
    expect(result.current.snapshot).toMatchObject({sourceId: "source-z", revision: 12});
    await act(async () => { confirmation.resolve(snapshot({sourceId: "source-a", runId: "run-a", revision: 1})); });
    expect(result.current.snapshot).toMatchObject({sourceId: "source-a", runId: "run-a", revision: 3});
    await emit(snapshot({revision: 999}));
    expect(result.current.snapshot).toMatchObject({sourceId: "source-a", runId: "run-a", revision: 3});
    expect(bridge.get).toHaveBeenCalledTimes(3);
    expect(result.current.error).toBeNull();
  });

  it("clears on sessions reset and rejects pending Gets and events", async () => {
    const pending = deferred<ActivitySnapshot>(); bridge.refresh.mockReturnValueOnce(pending.promise);
    const { result } = renderHook(() => useAgentActivity(options)); await act(async () => {});
    let refresh!: Promise<void>; act(() => { refresh = result.current.refresh(); });
    await act(async () => { for (const cb of bridge.listeners.get("agent:sessions-reset") ?? []) cb(); });
    await act(async () => { pending.resolve(snapshot({revision: 12})); await refresh; });
    await emit(snapshot({revision: 13}));
    expect(result.current).toMatchObject({ snapshot: null, loading: false, refreshing: false });
    expect(bridge.remove).toHaveBeenCalledWith("window", "dialog");
  });

  it("rejects the pending initial Get after a sessions reset", async () => {
    const initial = deferred<ActivitySnapshot>(); bridge.get.mockReturnValueOnce(initial.promise);
    const { result } = renderHook(() => useAgentActivity(options));
    await act(async () => { for (const cb of bridge.listeners.get("agent:sessions-reset") ?? []) cb(); });
    await act(async () => { initial.resolve(snapshot()); vi.advanceTimersByTime(10000); });
    expect(result.current).toMatchObject({snapshot: null, loading: false, refreshing: false, error: null});
    expect(bridge.set).toHaveBeenCalledTimes(1);
  });

  it("renews independent counter and dialog interests", async () => {
    const counter = renderHook(() => useAgentActivity({...options, purpose: "counter"}));
    const dialog = renderHook(() => useAgentActivity(options)); await act(async () => {});
    expect(bridge.set).toHaveBeenCalledWith({windowId: "window", purpose: "counter", sessionId: "a"});
    expect(bridge.set).toHaveBeenCalledWith({windowId: "window", purpose: "dialog", sessionId: "a"});
    await act(async () => { vi.advanceTimersByTime(10000); });
    expect(bridge.set).toHaveBeenCalledTimes(4);
    dialog.unmount();
    expect(bridge.remove.mock.calls).toEqual([["window", "dialog"]]);
    await act(async () => { vi.advanceTimersByTime(10000); });
    expect(bridge.set).toHaveBeenCalledTimes(5);
    counter.unmount();
    expect(bridge.remove.mock.calls).toEqual([["window", "dialog"], ["window", "counter"]]);
  });

  it("preserves stale snapshot after refresh error and deduplicates manual refresh", async () => {
    const { result } = renderHook(() => useAgentActivity(options)); await act(async () => {});
    const pending = deferred<ActivitySnapshot>(); bridge.refresh.mockReturnValueOnce(pending.promise);
    let one!: Promise<void>; let two!: Promise<void>;
    act(() => { one = result.current.refresh(); two = result.current.refresh(); });
    expect(result.current.refreshing).toBe(true);
    expect(bridge.refresh).toHaveBeenCalledTimes(1);
    await act(async () => { pending.reject(new Error("offline")); await Promise.all([one, two]); });
    expect(result.current.snapshot).toMatchObject({sessionId: "a", revision: 10, stale: true, messageCode: "refresh-failed"});
    expect(result.current.refreshing).toBe(false);
    expect(bridge.remove).not.toHaveBeenCalled();
  });

  it("ignores a refresh from the old run after confirmation of a new run", async () => {
    const { result } = renderHook(() => useAgentActivity(options)); await act(async () => {});
    const old = deferred<ActivitySnapshot>(); bridge.refresh.mockReturnValueOnce(old.promise);
    let refresh!: Promise<void>; act(() => { refresh = result.current.refresh(); });
    bridge.get.mockResolvedValueOnce(snapshot({runId: "run-a", revision: 11}));
    await emit(snapshot({runId: "run-a", revision: 11}));
    await act(async () => { old.resolve(snapshot({revision: 999})); await refresh; });
    expect(result.current.snapshot?.runId).toBe("run-a");
    expect(result.current.snapshot?.revision).toBe(11);
  });

  it("clears a local stale marker after a successful same-revision manual refresh", async () => {
    const { result } = renderHook(() => useAgentActivity(options)); await act(async () => {});
    bridge.refresh.mockRejectedValueOnce(new Error("offline"));
    await act(async () => { await result.current.refresh(); });
    expect(result.current.snapshot?.stale).toBe(true);
    await act(async () => { await result.current.refresh(); });
    expect(result.current.snapshot).toMatchObject({revision: 10, stale: false, messageCode: "none"});
  });

  it("exposes initial Get errors and recovers through a manual retry", async () => {
    bridge.get.mockRejectedValueOnce(new Error("offline"));
    const { result } = renderHook(() => useAgentActivity(options)); await act(async () => {});
    expect(result.current).toMatchObject({snapshot: null, loading: false, error: "refresh-failed"});
    await act(async () => { await result.current.refresh(); });
    expect(result.current.error).toBeNull();
    expect(result.current.snapshot?.revision).toBe(10);
  });

  it("exposes registration errors and clears only the interest error when renewal recovers", async () => {
    bridge.set.mockRejectedValueOnce(new Error("offline"));
    const { result } = renderHook(() => useAgentActivity(options)); await act(async () => {});
    expect(result.current.error).toBe("refresh-failed");
    expect(result.current.snapshot?.stale).toBe(true);
    await act(async () => { vi.advanceTimersByTime(10000); });
    expect(result.current.error).toBeNull();
    expect(result.current.snapshot?.stale).toBe(false);
  });

  it("manual retry immediately retries a failed interest without replacing either purpose", async () => {
    bridge.set.mockRejectedValueOnce(new Error("offline"));
    const { result } = renderHook(() => useAgentActivity(options)); await act(async () => {});
    await act(async () => { await result.current.refresh(); });
    expect(bridge.set.mock.calls).toEqual([
      [{windowId: "window", purpose: "dialog", sessionId: "a"}],
      [{windowId: "window", purpose: "dialog", sessionId: "a"}],
    ]);
    expect(result.current.error).toBeNull();
    expect(bridge.remove).not.toHaveBeenCalled();
  });

  it("successful renewal does not hide a failed snapshot request", async () => {
    bridge.get.mockRejectedValueOnce(new Error("offline"));
    const { result } = renderHook(() => useAgentActivity(options)); await act(async () => {});
    await act(async () => { vi.advanceTimersByTime(10000); });
    expect(result.current.error).toBe("refresh-failed");
    expect(result.current.snapshot).toBeNull();
  });

  it("never promotes an older same-owner revision even when its run differs", async () => {
    const { result } = renderHook(() => useAgentActivity(options)); await act(async () => {});
    await emit(snapshot({runId: "other-run", revision: 9}));
    expect(result.current.snapshot?.runId).toBe("run-z");
    expect(bridge.get).toHaveBeenCalledTimes(1);
  });

  it("rejects old initial Get failures after selection changes", async () => {
    const first = deferred<ActivitySnapshot>(); bridge.get.mockReturnValueOnce(first.promise).mockResolvedValueOnce(snapshot({sessionId: "b"}));
    const { result, rerender } = renderHook(props => useAgentActivity(props), {initialProps: options});
    await act(async () => { rerender({...options, sessionId: "b"}); });
    await act(async () => { first.reject(new Error("old request")); });
    expect(result.current.snapshot).toMatchObject({sessionId: "b", stale: false, messageCode: "none"});
  });

  it("does not subscribe while disabled or accept callbacks after unmount", async () => {
    const pending = deferred<ActivitySnapshot>(); bridge.get.mockReturnValueOnce(pending.promise);
    const { result, rerender, unmount } = renderHook(props => useAgentActivity(props), {initialProps: {...options, enabled: false}});
    expect(bridge.get).not.toHaveBeenCalled(); expect(bridge.set).not.toHaveBeenCalled();
    rerender({...options, enabled: true}); const before = result.current;
    unmount();
    await act(async () => { pending.resolve(snapshot()); });
    expect(result.current).toBe(before);
    expect([...bridge.listeners.values()].every(listeners => listeners.size === 0)).toBe(true);
  });

  it("uses one opaque window identity for independently mounted consumers", () => {
    expect(getAgentActivityWindowId()).toBe(getAgentActivityWindowId());
    expect(getAgentActivityWindowId().length).toBeGreaterThan(0);
  });

  it("maps refresh requests and preserves zero versus absent metrics and inventory coverage", async () => {
    const raw = snapshot({backgroundTasks: null as unknown as [], capabilities: {...snapshot().capabilities, backgroundTasks: false}, nodes: [{
      id: "root", providerNodeId: "root", kind: "agent", state: "unknown", label: "root",
      firstObservedAt: "2026-10-15T12:00:00Z", lastObservedAt: "2026-10-15T12:00:05Z", tokens: {total: 0}, model: {name: "example", kind: "configured"},
    }]});
    bridge.refresh.mockResolvedValueOnce(raw);
    const value = await agentActivityApi.refresh("specific-session");
    expect(bridge.refresh).toHaveBeenCalledWith("specific-session");
    expect(value.backgroundTasks).toEqual([]);
    expect(value.capabilities.backgroundTasks).toBe(false);
    expect(value.nodes[0].tokens).toEqual({total: 0});
    expect(value.nodes[0].startedAt).toBeUndefined();
    expect(value.nodes[0].model).toEqual({name: "example", kind: "configured"});
    expect(raw.backgroundTasks).toBeNull();
  });

  it("accepts native broad string enums and an unobserved root without an owner or run", async () => {
    const native: Record<string, unknown> = {...snapshot(), sourceId: "", runId: "", revision: 0, status: "unavailable", messageCode: "hooks-unobserved", nodes: null, backgroundTasks: null};
    bridge.get.mockResolvedValueOnce(native);
    expect(await agentActivityApi.get("a")).toMatchObject({sourceId: "", runId: "", revision: 0, provider: "codex", status: "unavailable", messageCode: "hooks-unobserved", nodes: [], backgroundTasks: []});
  });

  it("rejects malformed native providers through the request error path", async () => {
    bridge.get.mockResolvedValueOnce({...snapshot(), provider: "invalid-provider"});
    await expect(agentActivityApi.get("a")).rejects.toThrow("Invalid agent activity snapshot");
  });

  it("silently ignores malformed runtime enums and accepts the next valid snapshot", () => {
    const received = vi.fn(); const off = agentActivityApi.onChanged(received);
    const node = {id: "root", providerNodeId: "root", label: "root", firstObservedAt: "2026-10-15T12:00:00Z", lastObservedAt: "2026-10-15T12:00:05Z", kind: "agent", state: "unknown"};
    const dispatch = (raw: unknown) => {
      for (const listener of bridge.listeners.get("agent:activity-changed") ?? []) listener(raw as ActivitySnapshot);
    };
    expect(() => {
      dispatch({...snapshot(), provider: "invalid-provider"});
      dispatch({...snapshot(), status: "invalid-status"});
      dispatch({...snapshot(), messageCode: "invalid-message"});
      dispatch({...snapshot(), nodes: [{...node, state: "invalid-state"}]});
      dispatch({...snapshot(), nodes: [{...node, kind: "invalid-kind"}]});
      dispatch({...snapshot(), nodes: [{...node, model: {name: "model", kind: "invalid-kind"}}]});
    }).not.toThrow();
    expect(received).not.toHaveBeenCalled();
    dispatch(snapshot());
    expect(received).toHaveBeenCalledTimes(1);
    expect(received.mock.calls[0][0]).toMatchObject({sourceId: "source-z", provider: "codex"});
    off();
  });
});
