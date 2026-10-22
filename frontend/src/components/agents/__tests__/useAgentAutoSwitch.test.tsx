// @vitest-environment jsdom
// This repo does not have @testing-library/react installed (renderHook is
// unavailable), so this test drives the hook through a tiny host component
// using the same createRoot/act pattern as AgentLauncherContext.test.tsx.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";

const listeners: Record<string, (payload: any) => void> = {};
vi.mock("../../../../wailsjs/runtime", () => ({
  EventsOn: (name: string, cb: (payload: any) => void) => {
    listeners[name] = cb;
    return () => { delete listeners[name]; };
  },
}));
const listSessions = vi.fn();
vi.mock("../../../../wailsjs/go/main/App", () => ({ ListAgentSessions: (...args: unknown[]) => listSessions(...args) }));

import { useAgentAutoSwitch, AUTO_SWITCH_TICK_MS } from "../useAgentAutoSwitch";
import { noteTerminalInput, resetTerminalActivity } from "../terminalActivity";

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

const session = (id: string, state: string, terminalId = `term-${id}`) => ({ id, state, terminalId, agentId: "claude", projectName: "demo" });

let host: HTMLDivElement;
let root: Root;
let latest: { waiting: number } = { waiting: 0 };

function Harness({ enabled, focus, blocked = false }: { enabled: boolean; focus: (id: string) => void; blocked?: boolean }) {
  latest = useAgentAutoSwitch({ enabled, blocked, focusSession: focus });
  return null;
}

function mount(enabled: boolean, focus: (id: string) => void) {
  host = document.createElement("div");
  document.body.appendChild(host);
  root = createRoot(host);
  act(() => { root.render(<Harness enabled={enabled} focus={focus} />); });
}

function rerender(enabled: boolean, focus: (id: string) => void) {
  act(() => { root.render(<Harness enabled={enabled} focus={focus} />); });
}

describe("useAgentAutoSwitch", () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ["setInterval", "clearInterval", "setTimeout", "clearTimeout", "Date"] });
    resetTerminalActivity();
    listSessions.mockResolvedValue([]);
    for (const key of Object.keys(listeners)) delete listeners[key];
  });

  afterEach(() => {
    act(() => { root?.unmount(); });
    host?.remove();
    vi.useRealTimers();
    vi.clearAllMocks();
  });

  it("focuses waiting sessions one at a time, advancing after Enter", async () => {
    const focus = vi.fn();
    mount(true, focus);
    await act(async () => { await Promise.resolve(); });
    act(() => {
      listeners["agent:session-changed"](session("a", "needs-input"));
      listeners["agent:session-changed"](session("b", "needs-input"));
    });
    act(() => { vi.advanceTimersByTime(AUTO_SWITCH_TICK_MS); });
    expect(focus).toHaveBeenCalledWith("a");
    expect(latest.waiting).toBe(1);
    act(() => { noteTerminalInput("term-a", "y\r"); vi.advanceTimersByTime(AUTO_SWITCH_TICK_MS); });
    expect(focus).toHaveBeenLastCalledWith("b");
    expect(latest.waiting).toBe(0);
  });

  it("waits while the user is typing and releases after silence", () => {
    const focus = vi.fn();
    mount(true, focus);
    act(() => {
      noteTerminalInput("term-x", "git st");
      listeners["agent:session-changed"](session("a", "needs-input"));
      vi.advanceTimersByTime(AUTO_SWITCH_TICK_MS * 2);
    });
    expect(focus).not.toHaveBeenCalled();
    act(() => { vi.advanceTimersByTime(3500); });
    expect(focus).toHaveBeenCalledWith("a");
  });

  it("does nothing while disabled and seeds from the session list when enabled", async () => {
    const focus = vi.fn();
    listSessions.mockResolvedValue([session("seed", "needs-input")]);
    mount(false, focus);
    act(() => {
      listeners["agent:session-changed"]?.(session("a", "needs-input"));
      vi.advanceTimersByTime(AUTO_SWITCH_TICK_MS);
    });
    expect(focus).not.toHaveBeenCalled();
    rerender(true, focus);
    await act(async () => { await Promise.resolve(); vi.advanceTimersByTime(AUTO_SWITCH_TICK_MS); });
    expect(focus).toHaveBeenCalledWith("seed");
  });

  it("never steals focus from a non-terminal input; releases once it blurs", async () => {
    const focus = vi.fn();
    const input = document.createElement("input");
    document.body.appendChild(input);
    input.focus();
    mount(true, focus);
    await act(async () => { await Promise.resolve(); });
    act(() => {
      listeners["agent:session-changed"](session("a", "needs-input"));
      vi.advanceTimersByTime(AUTO_SWITCH_TICK_MS * 3);
    });
    expect(focus).not.toHaveBeenCalled();
    input.blur();
    act(() => { vi.advanceTimersByTime(AUTO_SWITCH_TICK_MS); });
    expect(focus).toHaveBeenCalledWith("a");
    input.remove();
  });

  it("releases current on a single-key answer with no Enter, after a silence", () => {
    const focus = vi.fn();
    mount(true, focus);
    act(() => {
      listeners["agent:session-changed"](session("a", "needs-input"));
      listeners["agent:session-changed"](session("b", "needs-input"));
      vi.advanceTimersByTime(AUTO_SWITCH_TICK_MS);
    });
    expect(focus).toHaveBeenCalledWith("a");
    act(() => { noteTerminalInput("term-a", "y"); });
    act(() => { vi.advanceTimersByTime(3500); });
    expect(focus).toHaveBeenLastCalledWith("b");
  });

  it("runs the tick timer only while there is work (a queued session or an unanswered current)", async () => {
    const focus = vi.fn();
    mount(true, focus);
    await act(async () => { await Promise.resolve(); });
    expect(vi.getTimerCount()).toBe(0);
    act(() => { listeners["agent:session-changed"](session("a", "needs-input")); });
    expect(vi.getTimerCount()).toBeGreaterThan(0);
    act(() => { noteTerminalInput("term-a", "y\r"); });
    expect(vi.getTimerCount()).toBe(0);
  });
});

it("auto-switch queue survives document blocking in original order", async () => {
  vi.useFakeTimers(); resetTerminalActivity(); listSessions.mockResolvedValue([]);
  const focus = vi.fn(); mount(true, focus);
  await act(async () => { await Promise.resolve(); root.render(<Harness enabled focus={focus} blocked />); });
  act(() => {
    listeners["agent:session-changed"](session("a", "needs-input"));
    listeners["agent:session-changed"](session("b", "needs-input"));
    vi.advanceTimersByTime(65000);
  });
  expect(focus).not.toHaveBeenCalled(); expect(latest.waiting).toBe(2);
  act(() => { root.render(<Harness enabled focus={focus} blocked={false} />); });
  act(() => { vi.advanceTimersByTime(AUTO_SWITCH_TICK_MS); });
  expect(focus.mock.calls.map(call => call[0])).toEqual(["a"]);
  act(() => { noteTerminalInput("term-a", "y\r"); vi.advanceTimersByTime(AUTO_SWITCH_TICK_MS); });
  expect(focus.mock.calls.map(call => call[0])).toEqual(["a", "b"]);
  act(() => { root.unmount(); }); host.remove(); vi.useRealTimers();
});
