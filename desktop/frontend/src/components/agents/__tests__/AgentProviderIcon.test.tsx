// @vitest-environment jsdom
import { act } from "react";
import { createRoot } from "react-dom/client";
import { describe, expect, it } from "vitest";
import AgentProviderIcon from "../AgentProviderIcon";

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

describe("AgentProviderIcon", () => {
  it.each(["claude", "codex", "kimi", "opencode", "unknown"])("renders %s without requesting vendor artwork", (provider) => {
    const host = document.createElement("div");
    const root = createRoot(host);
    act(() => root.render(<AgentProviderIcon provider={provider} />));
    expect(host.querySelector("img")).toBeNull();
    expect(host.querySelector("svg")).toBeTruthy();
    expect(host.firstElementChild?.getAttribute("aria-hidden")).toBe("true");
    expect(host.firstElementChild?.classList.contains("h-8")).toBe(true);
    act(() => root.unmount());
  });

  it("preserves the compact decorative tile size", () => {
    const host = document.createElement("div");
    const root = createRoot(host);
    act(() => root.render(<AgentProviderIcon provider="claude" compact />));
    expect(host.firstElementChild?.classList.contains("h-5")).toBe(true);
    expect(host.firstElementChild?.classList.contains("w-5")).toBe(true);
    act(() => root.unmount());
  });
});
