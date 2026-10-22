import { describe, expect, it } from "vitest";
import type { AgentSessionView } from "../AgentSessionCard";
import {
  SESSION_AGE_NOW_KEY,
  filterAgentSessions,
  groupAgentSessions,
  locationLabel,
  sessionAge,
  sessionAgeLabel,
  sessionProjectNames,
} from "../sessionTree";

const session = (overrides: Partial<AgentSessionView>): AgentSessionView => ({
  id: "session-1",
  projectName: "lara4ever",
  projectPath: "C:\\code\\lara4ever",
  agentId: "codex",
  launchMode: "current",
  title: "Fix navigation",
  state: "working",
  updatedAt: "2026-09-05T11:59:00Z",
  unread: false,
  archived: false,
  ...overrides,
});

describe("agent session tree", () => {
  it("groups multiple sessions below their project and working copy", () => {
    const groups = groupAgentSessions([
      session({ id: "current", gitBranch: "main" }),
      session({ id: "wt-1", launchMode: "worktree", gitBranch: "feature/ui", worktreePath: "C:\\worktrees\\ui" }),
      session({ id: "wt-2", launchMode: "worktree", gitBranch: "feature/ui", worktreePath: "C:\\worktrees\\ui", updatedAt: "2026-09-05T12:00:00Z" }),
    ], ["empty-project"]);

    expect(groups.map((group) => group.name)).toEqual(["empty-project", "lara4ever"]);
    expect(groups[0].locations).toEqual([]);
    expect(groups[1].locations.map((location) => [location.kind, location.label, location.sessions.map((item) => item.id)])).toEqual([
      ["current", "main", ["current"]],
      ["worktree", "feature/ui", ["wt-2", "wt-1"]],
    ]);
  });

  it("keeps live sessions in launch order even when their activity timestamps change", () => {
    const live = (id: string, startedAt: string, updatedAt: string) =>
      session({ id, terminalId: `term-${id}`, state: "working", gitBranch: "main", startedAt, updatedAt });
    const before = groupAgentSessions([
      live("first", "2026-09-07T10:00:00Z", "2026-09-07T10:05:00Z"),
      live("second", "2026-09-07T10:01:00Z", "2026-09-07T10:04:00Z"),
      live("third", "2026-09-07T10:02:00Z", "2026-09-07T10:03:00Z"),
    ]);
    // "third" was just clicked / marked read, so its updatedAt is now the newest.
    const after = groupAgentSessions([
      live("first", "2026-09-07T10:00:00Z", "2026-09-07T10:05:00Z"),
      live("second", "2026-09-07T10:01:00Z", "2026-09-07T10:04:00Z"),
      live("third", "2026-09-07T10:02:00Z", "2026-09-07T10:09:00Z"),
    ]);

    expect(before[0].locations[0].sessions.map((item) => item.id)).toEqual(["first", "second", "third"]);
    expect(after[0].locations[0].sessions.map((item) => item.id)).toEqual(["first", "second", "third"]);
  });

  it("lists live sessions before stopped ones, then the stopped ones by recency", () => {
    const groups = groupAgentSessions([
      session({ id: "old-stopped", state: "stopped", gitBranch: "main", updatedAt: "2026-09-07T09:00:00Z" }),
      session({ id: "live", terminalId: "term-1", state: "working", gitBranch: "main", startedAt: "2026-09-07T08:00:00Z", updatedAt: "2026-09-07T08:30:00Z" }),
      session({ id: "new-stopped", state: "done", gitBranch: "main", updatedAt: "2026-09-07T09:30:00Z" }),
    ]);

    expect(groups[0].locations[0].sessions.map((item) => item.id)).toEqual(["live", "new-stopped", "old-stopped"]);
  });

  it("uses the worktree folder when no branch is available", () => {
    expect(locationLabel(session({ launchMode: "worktree", worktreePath: "C:\\worktrees\\preview-one" })).label).toBe("preview-one");
  });

  it("returns a catalogue key instead of literal English when there is no branch to show", () => {
    expect(locationLabel(session({ launchMode: "branch" }))).toEqual({ label: "", labelKey: "agents.page.locationSelectedBranch" });
    expect(locationLabel(session({ launchMode: "current" }))).toEqual({ label: "", labelKey: "agents.page.locationCurrentCheckout" });
  });

  it("formats compact relative ages", () => {
    const now = new Date("2026-09-05T12:00:00Z").getTime();
    expect(sessionAge("2026-09-05T11:59:00Z", now)).toBe("1m");
    expect(sessionAge("2026-09-02T12:00:00Z", now)).toBe("3d");
  });

  it("returns a catalogue key instead of literal English for a sub-minute age", () => {
    const now = new Date("2026-09-05T12:00:00Z").getTime();
    expect(sessionAge("2026-09-05T11:59:30Z", now)).toBe(SESSION_AGE_NOW_KEY);
  });

  it("resolves the sub-minute sentinel through t() but leaves compact ages untouched", () => {
    const now = new Date("2026-09-05T12:00:00Z").getTime();
    const t = (key: string) => (key === SESSION_AGE_NOW_KEY ? "now" : key);
    expect(sessionAgeLabel(t, "2026-09-05T11:59:30Z", now)).toBe("now");
    expect(sessionAgeLabel(t, "2026-09-05T11:59:00Z", now)).toBe("1m");
  });

  it("shows only attached live sessions in the active scope", () => {
    const sessions = [
      session({ id: "active", terminalId: "terminal-1" }),
      session({ id: "stopped", state: "stopped" }),
      session({ id: "detached", state: "working", terminalId: undefined }),
      session({ id: "archived", terminalId: "terminal-2", archived: true }),
    ];

    expect(filterAgentSessions(sessions, "active").map((item) => item.id)).toEqual(["active"]);
    expect(filterAgentSessions(sessions, "recent").map((item) => item.id)).toEqual(["active", "stopped", "detached"]);
    expect(filterAgentSessions(sessions, "archived").map((item) => item.id)).toEqual(["archived"]);
  });

  it("filters by a session project and keeps the project picker focused", () => {
    const sessions = [
      session({ id: "lara", terminalId: "terminal-1" }),
      session({ id: "shop", projectName: "shop", title: "Checkout", terminalId: "terminal-2" }),
    ];

    expect(sessionProjectNames(sessions)).toEqual(["lara4ever", "shop"]);
    expect(filterAgentSessions(sessions, "active", "check", "shop").map((item) => item.id)).toEqual(["shop"]);
  });
});
