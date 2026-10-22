import { describe, expect, it } from "vitest";
import { type AgentUsageView, usageWindows, visibleUsageSnapshots } from "../AgentUsagePopover";

describe("usageWindows", () => {
  it("treats null provider windows as an empty list", () => {
    const snapshot: AgentUsageView = {
      provider: "claude",
      status: "unsupported",
      readAt: "2026-09-05T12:00:00Z",
      stale: false,
      windows: null,
    };

    expect(usageWindows(snapshot)).toEqual([]);
  });

  it("keeps a real zero and hides missing or invalid quota values", () => {
    const snapshot: AgentUsageView = { provider: "claude", status: "available", readAt: "", stale: false, windows: [
      { id: "zero", label: "5 hour", usedPct: 0 },
      { id: "missing", label: "Weekly" },
      { id: "invalid", label: "Invalid", usedPct: Number.NaN },
    ] };
    expect(usageWindows(snapshot).map((window) => window.id)).toEqual(["zero"]);
  });

  it("only displays providers with reported limits, including explicitly stale readings", () => {
    const base = { readAt: "", stale: false, windows: null };
    const snapshots: AgentUsageView[] = [
      { ...base, provider: "kimi", status: "unsupported" },
      { ...base, provider: "opencode", status: "unavailable" },
      { ...base, provider: "empty", status: "available", windows: [] },
      { ...base, provider: "claude", status: "available", stale: true, windows: [{ id: "five_hour", label: "5 hour", usedPct: 0 }] },
      { ...base, provider: "codex", status: "error", windows: [{ id: "primary", label: "5 hour", usedPct: 70 }] },
    ];
    expect(visibleUsageSnapshots(snapshots).map((snapshot) => snapshot.provider)).toEqual(["claude"]);
  });
});
