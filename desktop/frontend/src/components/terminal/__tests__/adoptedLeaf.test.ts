import { describe, it, expect } from "vitest";
import { applyAdoptedSession } from "../adoptedLeaf";
import type { Leaf } from "../types";

const shell = (): Leaf => ({
  type: "leaf", id: "term-3", label: "demo · git bash", kind: "gitbash", status: "ready",
  spawn: { kind: "project", projectName: "demo", terminalKind: "gitbash" },
});

describe("applyAdoptedSession", () => {
  it("promotes the shell leaf bound to a live adopted session", () => {
    const out = applyAdoptedSession(shell(), { id: "s1", terminalId: "term-3", agentId: "claude", projectName: "demo", state: "idle", adopted: true });
    expect(out.kind).toBe("agent:claude");
    expect(out.label).toBe("demo · Claude Code");
    expect(out.spawn.kind).toBe("project");
    expect(out.spawn.laraSessionId).toBe("s1");
    expect(out.spawn.agentId).toBe("claude");
    expect(out.spawn.adoptedShell).toEqual({ kind: "gitbash", label: "demo · git bash" });
  });

  it("returns the same leaf when the terminal does not match or the session is not adopted", () => {
    const leaf = shell();
    expect(applyAdoptedSession(leaf, { id: "s1", terminalId: "term-9", agentId: "claude", projectName: "demo", state: "working", adopted: true })).toBe(leaf);
    expect(applyAdoptedSession(leaf, { id: "s1", terminalId: "term-3", agentId: "claude", projectName: "demo", state: "working", adopted: false })).toBe(leaf);
    const agentLeaf: Leaf = { ...leaf, spawn: { kind: "agent", projectName: "demo", agentId: "codex", laraSessionId: "launched" } };
    expect(applyAdoptedSession(agentLeaf, { id: "s1", terminalId: "term-3", agentId: "claude", projectName: "demo", state: "working", adopted: true })).toBe(agentLeaf);
  });

  it("is idempotent while the session stays live", () => {
    const session = { id: "s1", terminalId: "term-3", agentId: "codex" as const, projectName: "demo", state: "working", adopted: true };
    const once = applyAdoptedSession(shell(), session);
    expect(applyAdoptedSession(once, { ...session, state: "needs-input" })).toBe(once);
  });

  it("reverts to the shell when the session stops", () => {
    const session = { id: "s1", terminalId: "term-3", agentId: "kimi" as const, projectName: "demo", state: "idle", adopted: true };
    const promoted = applyAdoptedSession(shell(), session);
    const reverted = applyAdoptedSession(promoted, { ...session, terminalId: "", state: "stopped" });
    expect(reverted.kind).toBe("gitbash");
    expect(reverted.label).toBe("demo · git bash");
    expect(reverted.spawn).toEqual({ kind: "project", projectName: "demo", terminalKind: "gitbash" });
  });

  it("promotes and stays promoted when the backend reports an error state", () => {
    const errored = { id: "s1", terminalId: "term-3", agentId: "codex" as const, projectName: "demo", state: "error", adopted: true };
    const promoted = applyAdoptedSession(shell(), errored);
    expect(promoted.kind).toBe("agent:codex");
    expect(promoted.spawn.laraSessionId).toBe("s1");
    // A working session that later errors out (still bound to the terminal)
    // must stay an agent leaf rather than reverting to a plain shell.
    const stillPromoted = applyAdoptedSession(promoted, { ...errored, state: "error" });
    expect(stillPromoted).toBe(promoted);
  });

  it("ignores stop events for a different session than the one bound", () => {
    const promoted = applyAdoptedSession(shell(), { id: "s1", terminalId: "term-3", agentId: "claude", projectName: "demo", state: "idle", adopted: true });
    expect(applyAdoptedSession(promoted, { id: "other", terminalId: "", agentId: "claude", projectName: "demo", state: "stopped", adopted: true })).toBe(promoted);
  });
});
