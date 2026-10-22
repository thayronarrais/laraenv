// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { I18nProvider } from "../../../lib/i18n";
import type { ActivitySnapshot } from "../activityTypes";
import { useAgentMap } from "../useAgentMap";

const api = vi.hoisted(() => ({get: vi.fn(), interest: vi.fn(), remove: vi.fn()}));
vi.mock("../../../../wailsjs/go/main/App", () => ({
  GetLanguage: async () => "en", SetLanguage: async () => {},
  GetAgentActivity: (id: string) => api.get(id), RefreshAgentActivity: (id: string) => api.get(id),
  SetAgentActivityInterest: (interest: unknown) => api.interest(interest),
  RemoveAgentActivityInterest: (...args: unknown[]) => api.remove(...args),
}));
vi.mock("../../../../wailsjs/runtime", () => ({EventsOn: () => () => {}}));
const snapshot = (sessionId: string, extra: Partial<ActivitySnapshot> = {}): ActivitySnapshot => ({
  sessionId, sourceId: "owner", runId: "run", revision: 1, rootId: "root", provider: "claude",
  observedSince: "2026-10-16T12:00:00Z", observedAt: "2026-10-16T12:00:01Z",
  status: "available", messageCode: "none", truncated: false, stale: false,
  capabilities: {children: true, backgroundTasks: false, liveState: true, duration: false, model: false, tokens: false},
  nodes: [{id: "root", providerNodeId: "provider-root", kind: "agent", label: "Principal", state: "working",
    firstObservedAt: "2026-10-16T12:00:00Z", lastObservedAt: "2026-10-16T12:00:01Z", terminalId: "terminal"}],
  backgroundTasks: [], ...extra,
});
beforeEach(() => {
  api.get.mockReset().mockImplementation(async (id: string) => snapshot(id));
  api.interest.mockReset().mockResolvedValue(undefined); api.remove.mockReset().mockResolvedValue(undefined);
});
afterEach(cleanup);
function Harness({sessionId = ""}: {sessionId?: string}) {
  const map = useAgentMap({enabled: true, selectedSessionId: sessionId, onOpenRoot: () => {}});
  return <><button onClick={() => map.openMap("history")}>Inspect history</button><footer>{map.chip}</footer>{map.dialog}</>;
}
const show = (sessionId = "") => <I18nProvider><Harness sessionId={sessionId} /></I18nProvider>;

it("does not render a footer action or create an activity lease without an eligible session", () => {
  render(show());
  expect(document.querySelector("footer button")).toBeNull();
  expect(api.get).not.toHaveBeenCalled(); expect(api.interest).not.toHaveBeenCalled();
});
it("keeps the active principal count visible when no children have been observed", async () => {
  render(show("active"));
  expect(await screen.findByRole("button", {name: /1 agent/})).toBeTruthy();
});
it("keeps an active provider limitation visible", async () => {
  api.get.mockImplementation(async (id: string) => snapshot(id, {provider: "kimi", status: "unavailable", messageCode: "unsupported-provider",
    capabilities: {...snapshot(id).capabilities, children: false}}));
  render(show("active"));
  expect(await screen.findByRole("button", {name: "Child observation is unavailable for this provider."})).toBeTruthy();
});
it("hides ended automatic activity while preserving its notice in explicit history inspection", async () => {
  api.get.mockImplementation(async (id: string) => snapshot(id, {messageCode: "observation-ended"}));
  await act(async () => { render(show("ended")); });
  expect(document.querySelector("footer button")).toBeNull();
  fireEvent.click(screen.getByRole("button", {name: "Inspect history"}));
  expect(await screen.findByText("Observation ended. Child outcomes are unknown.")).toBeTruthy();
  expect(screen.getByRole("dialog")).toBeTruthy();
});
it("removes the previous counter and lease when its selected pane is no longer eligible", async () => {
  const view = render(show("active")); await screen.findByRole("button", {name: /1 agent/});
  view.rerender(show());
  expect(document.querySelector("footer button")).toBeNull();
  await waitFor(() => expect(api.remove).toHaveBeenCalledWith(expect.any(String), "counter"));
});
