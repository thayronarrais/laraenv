// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { useAgentMap } from "../useAgentMap";

vi.mock("../../../lib/i18n", () => ({useI18n: () => ({t: (key: string) => key, formatNumber: String, formatDate: String})}));
vi.mock("../../../../wailsjs/runtime", () => ({EventsOn: () => () => {}}));
vi.mock("../../../../wailsjs/go/main/App", () => ({
  GetAgentActivity: async (sessionId: string) => ({sessionId, sourceId: "owner", runId: "run", revision: 1, rootId: "root", status: "available", messageCode: "none", capabilities: {}, nodes: [], backgroundTasks: []}),
  SetAgentActivityInterest: async () => {}, RemoveAgentActivityInterest: async () => {}, RefreshAgentActivity: vi.fn(),
}));
afterEach(cleanup);
let map: ReturnType<typeof useAgentMap>;
function Harness() {
  map = useAgentMap({enabled: true, selectedSessionId: "session", onOpenRoot: vi.fn()});
  return <><button onClick={() => map.openMap("session")}>Inspect</button>{map.dialog}</>;
}
it.each([true, false])("external closeMap restoreFocus=%s controls opener restoration", async restoreFocus => {
  render(<Harness/>); const opener = screen.getByRole("button", {name: "Inspect"}); opener.focus();
  fireEvent.click(opener); expect(screen.getByRole("dialog")).toBeTruthy(); expect(document.activeElement).not.toBe(opener);
  await act(async () => map.closeMap({restoreFocus}));
  if (restoreFocus) expect(document.activeElement).toBe(opener);
  else expect(document.activeElement).not.toBe(opener);
});
