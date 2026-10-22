// @vitest-environment jsdom
import { act } from "react";
import { createRoot } from "react-dom/client";
import { describe, expect, it, vi } from "vitest";
import AgentOnboarding from "../AgentOnboarding";
import type { AgentProfile } from "../../AgentLauncherMenu";
import { I18nProvider } from "../../../lib/i18n";

vi.mock("../../../../wailsjs/go/main/App", () => ({
  GetLanguage: () => Promise.resolve("en"),
  SetLanguage: () => Promise.resolve(),
}));
vi.mock("../../../../wailsjs/runtime", () => ({ EventsOn: () => () => {} }));

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

const profiles: AgentProfile[] = [
  { id: "codex", label: "Codex", available: true, version: "1.2.3", capabilities: { launch: true, resume: true, richStatus: true, history: true, usage: true } },
  { id: "kimi", label: "Kimi", available: false, reason: "CLI not found", capabilities: { launch: true, resume: true, richStatus: false, history: true, usage: false } },
];

describe("AgentOnboarding", () => {
  it("explains the project-first flow and starts a session", () => {
    const host = document.createElement("div");
    const root = createRoot(host);
    const onStart = vi.fn();
    act(() => root.render(<I18nProvider><AgentOnboarding projectName="lara4ever" profiles={profiles} hasProjects onStart={onStart} /></I18nProvider>));

    expect(host.textContent).toContain("lara4ever");
    expect(host.textContent).toContain("Current · Branch · Worktree");
    expect(host.textContent).toContain("Codex · ready");
    expect(host.textContent).toContain("Kimi · not found");
    const button = host.querySelector("button");
    act(() => button?.click());
    expect(onStart).toHaveBeenCalledOnce();

    act(() => root.unmount());
  });
});
