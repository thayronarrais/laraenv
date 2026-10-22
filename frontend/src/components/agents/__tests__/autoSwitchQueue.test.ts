import { describe, it, expect } from "vitest";
import { autoSwitchReducer, initialAutoSwitchState, waitingCount, type AutoSwitchState } from "../autoSwitchQueue";

const waiting = (id: string, terminalId = `term-${id}`) => ({ type: "session" as const, id, state: "needs-input", terminalId });
const working = (id: string, terminalId = `term-${id}`) => ({ type: "session" as const, id, state: "working", terminalId });

function run(events: Parameters<typeof autoSwitchReducer>[1][], start: AutoSwitchState = initialAutoSwitchState) {
  return events.reduce(autoSwitchReducer, start);
}

describe("autoSwitchReducer", () => {
  it("queues waiting sessions in FIFO order without duplicates", () => {
    const s = run([waiting("a"), waiting("b"), waiting("a")]);
    expect(s.queue).toEqual(["a", "b"]);
    expect(waitingCount(s)).toBe(2);
    expect(s.terminalOf).toEqual({ a: "term-a", b: "term-b" });
  });

  it("ignores sessions without a terminal", () => {
    const s = run([{ type: "session", id: "a", state: "needs-input", terminalId: "" }]);
    expect(s.queue).toEqual([]);
  });

  it("advance promotes the head and asks for focus unless the user is typing", () => {
    let s = run([waiting("a"), waiting("b"), { type: "advance", typing: true, now: 1000 }]);
    expect(s.current).toBeNull();
    expect(s.focusRequest).toBeNull();
    s = autoSwitchReducer(s, { type: "advance", typing: false, now: 1000 });
    expect(s.current).toBe("a");
    expect(s.queue).toEqual(["b"]);
    expect(s.focusRequest).toBe("a");
    s = autoSwitchReducer(s, { type: "focused" });
    expect(s.focusRequest).toBeNull();
    // Nothing happens while a current session is unanswered.
    expect(autoSwitchReducer(s, { type: "advance", typing: false, now: 1000 })).toBe(s);
  });

  it("Enter in the current session's terminal releases it; other terminals do not", () => {
    let s = run([waiting("a"), waiting("b"), { type: "advance", typing: false, now: 1000 }, { type: "focused" }]);
    s = autoSwitchReducer(s, { type: "enter", terminalId: "term-b" });
    expect(s.current).toBe("a");
    s = autoSwitchReducer(s, { type: "enter", terminalId: "term-a" });
    expect(s.current).toBeNull();
    s = autoSwitchReducer(s, { type: "advance", typing: false, now: 1000 });
    expect(s.current).toBe("b");
    expect(s.focusRequest).toBe("b");
  });

  it("a session that stops waiting leaves the queue or clears current", () => {
    let s = run([waiting("a"), waiting("b"), { type: "advance", typing: false, now: 1000 }, { type: "focused" }]);
    s = autoSwitchReducer(s, working("b"));
    expect(s.queue).toEqual([]);
    s = autoSwitchReducer(s, working("a"));
    expect(s.current).toBeNull();
    // A stopped session with no terminal drops its terminal mapping.
    s = autoSwitchReducer(s, { type: "session", id: "a", state: "stopped", terminalId: "" });
    expect(s.terminalOf.a).toBeUndefined();
  });

  it("does not re-queue the current session while it keeps waiting", () => {
    let s = run([waiting("a"), { type: "advance", typing: false, now: 1000 }, { type: "focused" }]);
    s = autoSwitchReducer(s, waiting("a"));
    expect(s.queue).toEqual([]);
    expect(s.current).toBe("a");
  });

  it("reset clears everything", () => {
    const s = run([waiting("a"), waiting("b"), { type: "advance", typing: false, now: 1000 }, { type: "reset" }]);
    expect(s).toEqual(initialAutoSwitchState);
  });

  it("returns the same state object when nothing changes", () => {
    const s = run([waiting("a")]);
    expect(autoSwitchReducer(s, working("zzz"))).toBe(s);
    expect(autoSwitchReducer(s, { type: "enter", terminalId: "term-none" })).toBe(s);
  });

  it("drops the terminal mapping of the current session when Enter answers it", () => {
    let s = run([waiting("a"), { type: "advance", typing: false, now: 1000 }, { type: "focused" }]);
    expect(s.terminalOf.a).toBe("term-a");
    s = autoSwitchReducer(s, { type: "enter", terminalId: "term-a" });
    expect(s.current).toBeNull();
    expect(s.terminalOf.a).toBeUndefined();
  });

  it("updates the terminal mapping of a queued session that reports a new terminal without re-queuing it", () => {
    let s = run([waiting("a"), waiting("b")]);
    s = autoSwitchReducer(s, waiting("b", "term-b2"));
    expect(s.queue).toEqual(["a", "b"]);
    expect(s.terminalOf.b).toBe("term-b2");
  });

  it("input updates lastInputAt only when it targets the current session's terminal", () => {
    let s = run([waiting("a"), waiting("b"), { type: "advance", typing: false, now: 0 }]);
    const untouched = autoSwitchReducer(s, { type: "input", terminalId: "term-b", now: 500 });
    expect(untouched).toBe(s);
    const touched = autoSwitchReducer(s, { type: "input", terminalId: "term-a", now: 500 });
    expect(touched).not.toBe(s);
    expect(touched.lastInputAt).toBe(500);
    expect(touched.current).toBe("a");
  });

  it("releases current on a single-key answer (no Enter) after RELEASE_SILENCE_MS of no further input", () => {
    let s = run([waiting("a"), waiting("b")]);
    s = autoSwitchReducer(s, { type: "advance", typing: false, now: 0 });
    expect(s.current).toBe("a");
    s = autoSwitchReducer(s, { type: "input", terminalId: "term-a", now: 1000 });
    s = autoSwitchReducer(s, { type: "advance", typing: false, now: 3999 });
    expect(s.current).toBe("a");
    s = autoSwitchReducer(s, { type: "advance", typing: false, now: 4000 });
    expect(s.current).toBe("b");
    expect(s.focusRequest).toBe("b");
  });

  it("releases a stale current after STALE_CURRENT_MS, but only when something is queued", () => {
    let s = run([waiting("a")]);
    s = autoSwitchReducer(s, { type: "advance", typing: false, now: 0 });
    expect(s.current).toBe("a");
    // Nothing queued: staleness never fires, no matter how long current sits unanswered.
    expect(autoSwitchReducer(s, { type: "advance", typing: false, now: 1_000_000 }).current).toBe("a");

    s = autoSwitchReducer(s, waiting("b"));
    s = autoSwitchReducer(s, { type: "advance", typing: false, now: 59999 });
    expect(s.current).toBe("a");
    s = autoSwitchReducer(s, { type: "advance", typing: false, now: 60000 });
    expect(s.current).toBe("b");
  });
});
