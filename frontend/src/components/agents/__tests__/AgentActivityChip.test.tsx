// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "../../../lib/i18n";
import AgentActivityChip from "../AgentActivityChip";
import type { ActivitySnapshot } from "../activityTypes";

vi.mock("../../../../wailsjs/go/main/App", () => ({ GetLanguage: async () => "en", SetLanguage: async () => {} }));
vi.mock("../../../../wailsjs/runtime", () => ({ EventsOn: () => () => {} }));
afterEach(cleanup);
function snapshot(extra: Partial<ActivitySnapshot> = {}): ActivitySnapshot {
  return { sessionId: "session", runId: "run", sourceId: "source", revision: 1, rootId: "root", provider: "claude",
    observedSince: "2026-10-15T12:00:00Z", observedAt: "2026-10-15T12:00:05Z", status: "available", messageCode: "none",
    capabilities: { children: true, backgroundTasks: false, liveState: true, duration: false, model: false, tokens: false },
    nodes: ["root", "child-a", "child-b"].map((id, i) => ({ id, providerNodeId: id, parentId: i ? "root" : undefined, kind: "agent", label: id,
      state: i === 2 ? "unknown" : "working", firstObservedAt: "2026-10-15T12:00:00Z", lastObservedAt: "2026-10-15T12:00:05Z" })),
    backgroundTasks: [], truncated: false, stale: false, ...extra };
}
function show(value: ActivitySnapshot | null, loading = false, error: "refresh-failed" | null = null) {
  const onOpen = vi.fn(), onSelectSession = vi.fn();
  render(<I18nProvider><AgentActivityChip snapshot={value} loading={loading} error={error} onOpen={onOpen} onSelectSession={onSelectSession} /></I18nProvider>);
  return {onOpen, onSelectSession};
}
describe("AgentActivityChip", () => {
  it("counts the principal and children, states unknown coverage, and opens the selected map", () => {
    const {onOpen, onSelectSession} = show(snapshot());
    fireEvent.click(screen.getByRole("button", {name: /3 agents/}));
    expect(screen.getByText(/1 unknown/)).toBeTruthy();
    expect(onOpen).toHaveBeenCalledOnce(); expect(onSelectSession).not.toHaveBeenCalled();
  });
  it("reports a real empty inventory as zero", () => { show(snapshot({nodes: []})); expect(screen.getByText("0 agents")).toBeTruthy(); });
  it("uses a lower bound for retained observations", () => { show(snapshot({truncated: true})); expect(screen.getByText("At least 3 agents")).toBeTruthy(); });
  it("does not claim a complete count for an unsupported provider", () => {
    show(snapshot({provider: "kimi", status: "unavailable", messageCode: "unsupported-provider", capabilities: {...snapshot().capabilities, children: false}}));
    expect(screen.getByRole("button").textContent).toContain("Child observation is unavailable for this provider.");
    expect(screen.queryByText("3 agents")).toBeNull();
  });
  it("opens the session navigator when there is no selection", () => {
    const {onOpen, onSelectSession} = show(null); fireEvent.click(screen.getByRole("button", {name: /Select an agent session/}));
    expect(onSelectSession).toHaveBeenCalledOnce(); expect(onOpen).not.toHaveBeenCalled();
  });
  it("distinguishes collecting and a failed initial request", () => {
    show(null, true); expect(screen.getByText("Loading agent activity…")).toBeTruthy(); cleanup();
    show(null, false, "refresh-failed"); expect(screen.getByText("Activity could not be refreshed. Try again.")).toBeTruthy();
  });
  it("keeps the observed count visible after a refresh failure", () => {
    show(snapshot({stale: true}), false, "refresh-failed"); expect(screen.getByText("3 agents")).toBeTruthy();
    expect(screen.getByRole("button").textContent).toContain("Activity could not be refreshed. Try again.");
  });
  it.each([
    {value: snapshot(), error: null, notice: ""},
    {value: snapshot({status: "partial", messageCode: "hooks-unobserved"}), error: null, notice: "No live hook activity has been observed. Agent states may be unknown."},
    {value: snapshot({stale: true}), error: "refresh-failed" as const, notice: "Activity could not be refreshed. Try again."},
  ])("preserves complete accessible activity and notice text when the footer truncates the visible label ($notice)", ({value, error, notice}) => {
    show(value, false, error);
    const label = ["3 agents", "2 working · 0 waiting · 1 unknown", notice].filter(Boolean).join(" · ");
    const button = screen.getByRole("button", {name: label});
    expect(button.title).toBe(label); expect(button.textContent).toContain("1 unknown");
    if (notice) expect(button.textContent).toContain(notice);
  });
});
