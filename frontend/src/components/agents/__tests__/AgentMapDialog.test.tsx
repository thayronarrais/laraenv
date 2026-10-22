// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { StrictMode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "../../../lib/i18n";
import AgentMapDialog from "../AgentMapDialog";
import type { ActivityNode, ActivitySnapshot } from "../activityTypes";

vi.mock("../../../../wailsjs/go/main/App", () => ({ GetLanguage: async () => "en", SetLanguage: async () => {} }));
vi.mock("../../../../wailsjs/runtime", () => ({ EventsOn: () => () => {} }));
afterEach(() => { cleanup(); vi.useRealTimers(); document.body.innerHTML = ""; });
function node(id: string, extra: Partial<ActivityNode> = {}): ActivityNode {
  return {id, providerNodeId: id, parentId: "root", kind: "agent", label: id, state: "working", firstObservedAt: "2026-10-15T12:00:00Z", lastObservedAt: "2026-10-15T12:00:05Z", ...extra};
}
function snapshot(extra: Partial<ActivitySnapshot> = {}): ActivitySnapshot {
  return {sessionId: "session", runId: "run", sourceId: "source", revision: 1, rootId: "root", provider: "codex", observedSince: "2026-10-15T12:00:00Z", observedAt: "2026-10-15T12:00:05Z", status: "available", messageCode: "none",
    capabilities: {children: true, backgroundTasks: false, liveState: true, duration: true, model: true, tokens: false},
    nodes: [node("root", {parentId: undefined, projectName: "Project", projectPath: "C:/Project", terminalId: "terminal"}), node("Alpha", {state: "unknown"}), node("Beta", {state: "reply-finished"})], backgroundTasks: [], truncated: false, stale: false, ...extra};
}
function setup(value: ActivitySnapshot | null = snapshot(), options: {loading?: boolean; refreshing?: boolean; error?: "refresh-failed" | null} = {}) {
  const onClose = vi.fn(), onRefresh = vi.fn(), onOpenRoot = vi.fn();
  const props = {sessionId: "session", snapshot: value, loading: false, refreshing: false, onClose, onRefresh, onOpenRoot, ...options};
  const view = render(<I18nProvider><AgentMapDialog {...props} /></I18nProvider>);
  return {...view, props, onClose, onRefresh, onOpenRoot, update(next: ActivitySnapshot) {view.rerender(<I18nProvider><AgentMapDialog {...props} snapshot={next} /></I18nProvider>);}};
}
describe("AgentMapDialog", () => {
  it("starts on the principal; inspecting children only updates details and exposes states in text", () => {
    const {onOpenRoot} = setup(); const dialog = screen.getByRole("dialog", {name: "Agent map"});
    expect(within(dialog).getByRole("heading", {name: "root"})).toBeTruthy();
    expect(screen.getByRole("button", {name: /Close agent map/})).toBe(document.activeElement);
    fireEvent.click(screen.getByRole("button", {name: /Alpha.*Unknown/}));
    expect(screen.getByRole("heading", {name: "Alpha"})).toBeTruthy(); expect(screen.getByText("Reply finished")).toBeTruthy();
    expect(onOpenRoot).not.toHaveBeenCalled();
  });
  it("preserves selection, scroll, focus and sibling order during snapshot events; removed selection returns to principal", () => {
    const {update, onOpenRoot} = setup(); const alpha = screen.getByRole("button", {name: /Alpha.*Unknown/});
    fireEvent.click(alpha); alpha.focus(); const tree = screen.getByRole("region", {name: "Agents"}); tree.scrollTop = 120;
    update(snapshot({revision: 2, nodes: [...snapshot().nodes].reverse()}));
    expect(screen.getByRole("heading", {name: "Alpha"})).toBeTruthy(); expect(document.activeElement).toBe(alpha); expect(tree.scrollTop).toBe(120);
    const buttons = within(tree).getAllByRole("button"); expect(buttons.map(b => b.textContent)).toEqual([expect.stringContaining("root"), expect.stringContaining("Alpha"), expect.stringContaining("Beta")]);
    update(snapshot({revision: 3, nodes: snapshot().nodes.filter(n => n.id !== "Alpha"), truncated: true}));
    expect(screen.getByRole("heading", {name: "root"})).toBeTruthy(); expect(document.activeElement).toBe(screen.getByRole("button", {name: /root.*Working/}));
    update(snapshot({revision: 4})); expect(screen.getByRole("heading", {name: "root"})).toBeTruthy();
    expect(onOpenRoot).not.toHaveBeenCalled();
  });
  it("renders children with missing intermediate parents and distinguishes missing from empty task inventory", () => {
    const {update} = setup(snapshot({nodes: [...snapshot().nodes, node("Orphan", {parentId: "missing"})]}));
    expect(screen.getByRole("button", {name: /Orphan/})).toBeTruthy(); expect(screen.getByText("Background task inventory is unavailable.")).toBeTruthy();
    update(snapshot({capabilities: {...snapshot().capabilities, backgroundTasks: true}})); expect(screen.getByText("No background tasks observed.")).toBeTruthy();
  });
  it("shows task details, configured model, absent tokens and known durations without inventing metrics", () => {
    vi.useFakeTimers(); vi.setSystemTime(new Date("2026-10-15T12:00:05Z"));
    setup(snapshot({backgroundTasks: [node("Build", {kind: "background-task", startedAt: "2026-10-15T12:00:00Z", model: {name: "test-model", kind: "configured"}})]}));
    fireEvent.click(screen.getByRole("button", {name: /Build/})); expect(screen.getByText("test-model (configured)")).toBeTruthy(); expect(screen.getByText("Tokens unavailable")).toBeTruthy();
    expect(screen.getByText("5 seconds")).toBeTruthy(); act(() => { vi.advanceTimersByTime(1000); }); expect(screen.getByText("6 seconds")).toBeTruthy();
  });
  it("groups tasks outside the agent hierarchy with type and last observation while preserving task inspection", () => {
    const task = node("Build", {kind: "background-task", agentType: "shell", parentId: "root", lastObservedAt: "2026-10-15T12:03:15Z"});
    const value = snapshot({capabilities: {...snapshot().capabilities, backgroundTasks: true}, backgroundTasks: [task]});
    const {onOpenRoot, update} = setup(value);
    const agents = screen.getByRole("region", {name: "Agents"}); const tasks = screen.getByRole("region", {name: "Background tasks"});
    expect(within(agents).queryByRole("button", {name: /Build/})).toBeNull();
    const taskButton = within(tasks).getByRole("button", {name: /Build/});
    expect(taskButton.textContent).toContain("Kind: shell"); expect(taskButton.textContent).toContain("Last observed:");
    expect(taskButton.textContent).toContain(new Intl.DateTimeFormat("en", {month: "short", day: "numeric", hour: "2-digit", minute: "2-digit", second: "2-digit"}).format(new Date(task.lastObservedAt)));
    fireEvent.click(taskButton); taskButton.focus(); tasks.scrollTop = 80;
    expect(screen.getByRole("heading", {name: "Build"})).toBeTruthy(); expect(onOpenRoot).not.toHaveBeenCalled();
    update({...value, revision: 2}); expect(screen.getByRole("heading", {name: "Build"})).toBeTruthy(); expect(document.activeElement).toBe(taskButton); expect(tasks.scrollTop).toBe(80);
    update({...value, revision: 3, backgroundTasks: []});
    expect(screen.getByRole("heading", {name: "root"})).toBeTruthy(); expect(within(tasks).getByText("No background tasks observed.")).toBeTruthy(); expect(onOpenRoot).not.toHaveBeenCalled();
  });
  it("keeps unavailable and confirmed empty inventory messages inside the background task section", () => {
    const {update} = setup(); const tasks = screen.getByRole("region", {name: "Background tasks"});
    expect(within(tasks).getByText("Background task inventory is unavailable.")).toBeTruthy();
    update(snapshot({capabilities: {...snapshot().capabilities, backgroundTasks: true}}));
    expect(within(tasks).getByText("No background tasks observed.")).toBeTruthy(); expect(within(tasks).queryByText("Background task inventory is unavailable.")).toBeNull();
  });
  it("labels the first observation separately from unknown execution start and duration", () => {
    const first = "2026-10-15T12:00:00Z", last = "2026-10-15T12:03:15Z";
    setup(snapshot({nodes: [snapshot().nodes[0], node("Metadata child", {firstObservedAt: first, lastObservedAt: last, state: "unknown"})]}));
    fireEvent.click(screen.getByRole("button", {name: /Metadata child/}));
    const details = screen.getByRole("region", {name: "Selected agent or task details"});
    const format = (value: string) => new Intl.DateTimeFormat("en", {month: "short", day: "numeric", hour: "2-digit", minute: "2-digit", second: "2-digit"}).format(new Date(value));
    expect(within(details).getByText("First observed").nextElementSibling?.textContent).toBe(format(first));
    expect(within(details).getByText("Last observed").nextElementSibling?.textContent).toBe(format(last));
    expect(within(details).getByText("Started").nextElementSibling?.textContent).toBe("Unavailable"); expect(within(details).getByText("Duration unavailable")).toBeTruthy();
  });
  it("traps Tab and Shift+Tab, and closes via Escape without leaking it to underlying listeners", () => {
    const {onClose} = setup(); const firstControl = screen.getByRole("button", {name: "Close agent map"}); const lastControl = screen.getByRole("button", {name: "Open terminal"});
    lastControl.focus(); fireEvent.keyDown(lastControl, {key: "Tab"}); expect(document.activeElement).toBe(firstControl);
    fireEvent.keyDown(firstControl, {key: "Tab", shiftKey: true}); expect(document.activeElement).toBe(lastControl);
    const underlying = vi.fn(); window.addEventListener("keydown", underlying);
    fireEvent.keyDown(lastControl, {key: "Escape"}); expect(onClose).toHaveBeenCalledWith({restoreFocus: true}); expect(underlying).not.toHaveBeenCalled(); window.removeEventListener("keydown", underlying);
  });
  it("backdrop closes; card clicks do not", () => {
    const {onClose} = setup(); fireEvent.click(screen.getByRole("dialog")); expect(onClose).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("dialog").parentElement!); expect(onClose).toHaveBeenCalledWith({restoreFocus: true});
  });
  it("restores a visible opener, falls back when removed, and respects another dialog's focus", async () => {
    const opener = document.createElement("button"); document.body.append(opener); opener.focus(); const first = setup(); expect(document.activeElement).toBe(screen.getByRole("button", {name: "Close agent map"})); first.unmount(); await act(async () => {}); expect(document.activeElement).toBe(opener);
    opener.focus(); const second = setup(); opener.remove(); second.unmount(); await act(async () => {}); expect(document.activeElement).toBe(document.body);
    const third = setup(); const dirty = document.createElement("button"); document.body.append(dirty); dirty.focus(); third.unmount(); await act(async () => {}); expect(document.activeElement).toBe(dirty);
  });
  it("does not restore focus into a hidden opener", async () => {
    const opener = document.createElement("button"); document.body.append(opener); opener.focus(); const view = setup(); opener.hidden = true; view.unmount(); await act(async () => {}); expect(document.activeElement).toBe(document.body);
  });
  it.each([false, true])("preserves the original opener across StrictMode replay with explicit ref=%s", async explicitRef => {
    const opener = document.createElement("button"); document.body.append(opener); opener.focus();
    const view = render(<StrictMode><I18nProvider><AgentMapDialog sessionId="session" snapshot={snapshot()} loading={false} refreshing={false}
      onRefresh={vi.fn()} onClose={vi.fn()} onOpenRoot={vi.fn()} openerRef={explicitRef ? {current: opener} : undefined}/></I18nProvider></StrictMode>);
    await act(async () => {});
    expect(document.activeElement).toBe(screen.getByRole("button", {name: "Close agent map"}));
    view.unmount(); await act(async () => {});
    expect(document.activeElement).toBe(opener);
  });
  it("does not steal focus claimed after cleanup by a newer dialog or terminal", async () => {
    for (const kind of ["dialog", "terminal"]) {
      const opener = document.createElement("button"); document.body.append(opener); opener.focus(); const view = setup(); view.unmount();
      const owner = document.createElement("div"), input = document.createElement("textarea"); owner.append(input); document.body.append(owner);
      if (kind === "dialog") {owner.setAttribute("role", "dialog"); owner.setAttribute("aria-modal", "true");} else owner.className = "xterm";
      input.focus(); await act(async () => {}); expect(document.activeElement).toBe(input); owner.remove(); opener.remove();
    }
  });
  it("opening the root terminal suppresses restoration so terminal focus wins", () => {
    const opener = document.createElement("button"), terminal = document.createElement("button"); document.body.append(opener, terminal); opener.focus();
    const view = setup(); view.onOpenRoot.mockImplementation(() => terminal.focus()); fireEvent.click(screen.getByRole("button", {name: "Open terminal"}));
    expect(view.onClose).toHaveBeenCalledWith({restoreFocus: false}); expect(view.onOpenRoot).toHaveBeenCalledWith("session"); view.unmount(); expect(document.activeElement).toBe(terminal);
  });
  it("shows loading/error/unsupported states, retains stale content and allows refresh", () => {
    setup(null, {loading: true}); expect(screen.getByText("Loading agent activity…")).toBeTruthy(); cleanup();
    const failed = setup(snapshot({stale: true}), {error: "refresh-failed"}); expect(screen.getByText("Activity could not be refreshed. Try again.")).toBeTruthy(); expect(screen.getByRole("heading", {name: "root"})).toBeTruthy();
    fireEvent.click(screen.getByRole("button", {name: "Refresh activity"})); expect(failed.onRefresh).toHaveBeenCalledOnce(); cleanup();
    setup(snapshot({provider: "opencode", status: "unavailable", messageCode: "unsupported-provider", capabilities: {...snapshot().capabilities, children: false}}));
    expect(screen.getByText("Child observation is unavailable for this provider.")).toBeTruthy(); expect(screen.queryByText("3 agents")).toBeNull();
    expect(screen.queryByRole("button", {name: /Alpha/})).toBeNull();
  });
  it("distinguishes a failed initial request, a confirmed zero, and truncated counts", () => {
    setup(null, {error: "refresh-failed"}); expect(screen.getByRole("alert").textContent).toBe("Activity could not be refreshed. Try again."); cleanup();
    setup(snapshot({nodes: []})); expect(screen.getByText("0 agents")).toBeTruthy(); expect(screen.getByText("No agents observed.")).toBeTruthy(); cleanup();
    setup(snapshot({truncated: true})); expect(screen.getByText("At least 3 agents")).toBeTruthy(); expect(screen.getByText(/Counts are minimums/)).toBeTruthy();
  });
  it("keeps a keyboard focus target if an update temporarily has no retained nodes", () => {
    const {update} = setup(); const alpha = screen.getByRole("button", {name: /Alpha/}); fireEvent.click(alpha); alpha.focus();
    update(snapshot({nodes: []})); expect(screen.getByText("0 agents")).toBeTruthy(); expect(document.activeElement).toBe(screen.getByRole("button", {name: "Close agent map"}));
    update(snapshot()); expect(screen.getByRole("heading", {name: "root"})).toBeTruthy();
  });
  it("keeps the map and children when root observation ends without inferring a child outcome", () => {
    const {update, onClose, onOpenRoot} = setup();
    update(snapshot({messageCode: "observation-ended", status: "partial", nodes: snapshot().nodes.map(n => n.id === "root" ? {...n, terminalId: undefined, state: "unknown"} : {...n, state: "unknown"})}));
    expect(screen.getByRole("dialog")).toBeTruthy(); expect(screen.getByText("Observation ended. Child outcomes are unknown.")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", {name: /Alpha.*Unknown/})); expect(screen.getByRole("heading", {name: "Alpha"})).toBeTruthy();
    expect((screen.getByRole("button", {name: "Open terminal"}) as HTMLButtonElement).disabled).toBe(true); expect(onClose).not.toHaveBeenCalled(); expect(onOpenRoot).not.toHaveBeenCalled();
  });
  it("stops duration ticking for unended children and tasks when observation ends, including after reopening", () => {
    vi.useFakeTimers({toFake: ["Date", "setInterval", "clearInterval"]}); vi.setSystemTime(new Date("2026-10-15T12:00:05Z"));
    const startedAt = "2026-10-15T12:00:00Z";
    const live = snapshot({nodes: [node("root", {parentId: undefined, startedAt}), node("Alpha", {startedAt}),
      node("Completed", {startedAt, endedAt: "2026-10-15T12:00:03Z", state: "reply-finished"})],
      backgroundTasks: [node("Build", {kind: "background-task", startedAt})]});
    const view = setup(live);
    const select = (name: string) => fireEvent.click(screen.getByRole("button", {name: new RegExp(name)}));
    select("Alpha"); expect(screen.getByText("5 seconds")).toBeTruthy();
    act(() => {vi.advanceTimersByTime(1000);}); expect(screen.getByText("6 seconds")).toBeTruthy();
    expect(vi.getTimerCount()).toBe(1);

    // Store.End retains real start/end timestamps and final child states.
    const ended = snapshot({...live, revision: 2, stale: true, status: "partial", messageCode: "observation-ended", observedAt: "2026-10-15T12:00:06Z",
      nodes: [ {...live.nodes[0], state: "stopped", lastObservedAt: "2026-10-15T12:00:06Z"}, {...live.nodes[1], state: "unknown"}, live.nodes[2]],
      backgroundTasks: [{...live.backgroundTasks[0], state: "unknown"}]});
    view.update(ended);
    expect(screen.getByText("Duration unavailable")).toBeTruthy(); expect(vi.getTimerCount()).toBe(0);
    act(() => {vi.advanceTimersByTime(60000);}); expect(screen.getByText("Duration unavailable")).toBeTruthy();
    select("Build"); expect(screen.getByText("Duration unavailable")).toBeTruthy();
    select("Completed"); expect(screen.getByText("3 seconds")).toBeTruthy();
    view.unmount(); act(() => {vi.advanceTimersByTime(60000);});
    setup(ended); select("Alpha");
    expect(screen.getByText("Duration unavailable")).toBeTruthy(); expect(vi.getTimerCount()).toBe(0);
    act(() => {vi.advanceTimersByTime(60000);}); expect(screen.getByText("Duration unavailable")).toBeTruthy();
    select("Completed"); expect(screen.getByText("3 seconds")).toBeTruthy();
    expect(ended.nodes[1].endedAt).toBeUndefined();
  });
  it.each(["unknown", "hooks-disabled", "truncated", "unsupported-protocol", "hook-trust-required"] as const)("stops an open interval when observation becomes %s and resumes after confirmation", reason => {
    vi.useFakeTimers({toFake: ["Date", "setInterval", "clearInterval"]}); vi.setSystemTime(new Date("2026-10-15T12:00:05Z"));
    const live = snapshot({nodes: [node("root", {parentId: undefined}), node("Alpha", {startedAt: "2026-10-15T12:00:00Z"})]});
    const {update} = setup(live);
    fireEvent.click(screen.getByRole("button", {name: /Alpha/}));
    expect(screen.getByText("5 seconds")).toBeTruthy(); expect(vi.getTimerCount()).toBe(1);
    update({...live, revision: 2, messageCode: reason === "unknown" ? "none" : reason,
      capabilities: {...live.capabilities, liveState: reason === "unknown" || reason === "hooks-disabled"},
      nodes: [live.nodes[0], {...live.nodes[1], state: reason === "unknown" ? "unknown" : "working"}]});
    expect(screen.getByText("Duration unavailable")).toBeTruthy(); expect(vi.getTimerCount()).toBe(0);
    act(() => {vi.advanceTimersByTime(60000);}); expect(screen.getByText("Duration unavailable")).toBeTruthy();
    update({...live, revision: 3});
    expect(screen.getByText("65 seconds")).toBeTruthy(); expect(vi.getTimerCount()).toBe(1);
    act(() => {vi.advanceTimersByTime(1000);}); expect(screen.getByText("66 seconds")).toBeTruthy();
  });
  it("resets selection for a new run and keeps completed durations fixed while formatting known token values only", () => {
    vi.useFakeTimers(); vi.setSystemTime(new Date("2026-10-15T12:00:05Z"));
    const value = snapshot({nodes: [node("root", {parentId: undefined, terminalId: "terminal", startedAt: "2026-10-15T12:00:00Z", endedAt: "2026-10-15T12:00:05Z", tokens: {input: 0, total: 1234}}), node("Alpha")]});
    const {update} = setup(value); expect(screen.getByText("1,234")).toBeTruthy(); expect(screen.getByText("0")).toBeTruthy(); expect(screen.queryByText("Output tokens")).toBeNull();
    act(() => {vi.advanceTimersByTime(3000);}); expect(screen.getByText("5 seconds")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", {name: /Alpha/})); update({...value, runId: "new-run"}); expect(screen.getByRole("heading", {name: "root"})).toBeTruthy();
  });
});
