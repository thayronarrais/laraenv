// @vitest-environment jsdom
import { act } from "react";
import { createRoot } from "react-dom/client";
import { describe, expect, it, vi } from "vitest";
import AgentSessionCard, { type AgentSessionView } from "../AgentSessionCard";
import { I18nProvider } from "../../../lib/i18n";

vi.mock("../../../../wailsjs/go/main/App", () => ({
  GetLanguage: () => Promise.resolve("en"),
  SetLanguage: () => Promise.resolve(),
}));
vi.mock("../../../../wailsjs/runtime", () => ({ EventsOn: () => () => {} }));

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

const noop = () => {};

const baseSession = (overrides: Partial<AgentSessionView>): AgentSessionView => ({
  id: "session-1",
  projectName: "lara4ever",
  projectPath: "C:\\code\\lara4ever",
  agentId: "claude",
  launchMode: "current",
  title: "Fix navigation",
  state: "working",
  updatedAt: "2026-09-05T11:59:00Z",
  unread: false,
  archived: false,
  ...overrides,
});

function renderCard(session: AgentSessionView) {
  const host = document.createElement("div");
  const root = createRoot(host);
  act(() => {
    root.render(
      <I18nProvider>
        <AgentSessionCard
          session={session}
          providerLabel="Claude Code"
          canResume={false}
          onOpen={noop}
          onResume={noop}
          onFresh={noop}
          onRename={noop}
          onStop={noop}
          onArchive={noop}
          onMarkRead={noop}
        />
      </I18nProvider>,
    );
  });
  return { host, root };
}

describe("AgentSessionCard", () => {
  it("shows the terminal badge and adopted stop tooltip for an adopted live session", () => {
    const session = baseSession({ state: "working", terminalId: "term-3", adopted: true });
    const { host, root } = renderCard(session);

    const badge = host.querySelector('[title="Started by hand in a LaraEnv terminal"]');
    expect(badge?.textContent).toBe("Terminal");

    const stopButton = Array.from(host.querySelectorAll("button")).find((button) => button.textContent?.includes("Stop"));
    expect(stopButton?.getAttribute("title")).toBe("Stop closes this terminal tab");

    act(() => root.unmount());
  });

  it("hides the terminal badge and uses the plain stop tooltip for a launched live session", () => {
    const session = baseSession({ state: "working", terminalId: "term-4" });
    const { host, root } = renderCard(session);

    expect(host.querySelector('[title="Started by hand in a LaraEnv terminal"]')).toBeNull();

    const stopButton = Array.from(host.querySelectorAll("button")).find((button) => button.textContent?.includes("Stop"));
    expect(stopButton?.getAttribute("title")).toBe("Stop this agent");

    act(() => root.unmount());
  });
});
