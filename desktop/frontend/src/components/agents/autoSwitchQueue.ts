// Pure state machine behind the agent auto-switch: which sessions are waiting
// for the user, which one they were switched to, and whether a focus is due.

export type AutoSwitchState = {
  queue: string[];
  current: string | null;
  terminalOf: Record<string, string>;
  focusRequest: string | null;
  // When `current` was promoted, and the last time input was seen in its
  // terminal since that promotion - both null when nothing is current.
  promotedAt: number | null;
  lastInputAt: number | null;
};

export type AutoSwitchEvent =
  | { type: "session"; id: string; state: string; terminalId?: string }
  | { type: "enter"; terminalId: string }
  | { type: "input"; terminalId: string; now: number }
  | { type: "advance"; typing: boolean; now: number }
  | { type: "focused" }
  | { type: "reset" };

// A single-key answer ("y", "1", ...) never produces Enter, so `current`
// would otherwise stay set - and the whole queue frozen - until the agent's
// turn ends on its own. Once this much time passes with no further input in
// its terminal, treat it as answered and release it.
export const RELEASE_SILENCE_MS = 3000;

// A missing terminal (detached, closed) can leave `current` set forever with
// no input ever arriving. If something else is waiting, release a current
// this old even without seeing input, so the queue does not stall.
export const STALE_CURRENT_MS = 60000;

export const initialAutoSwitchState: AutoSwitchState = {
  queue: [],
  current: null,
  terminalOf: {},
  focusRequest: null,
  promotedAt: null,
  lastInputAt: null,
};

export function waitingCount(state: AutoSwitchState): number { return state.queue.length; }

// Only sessions the reducer is actively tracking (waiting with a terminal,
// already queued, or the current session) get a terminalOf entry. A session
// that is none of those - like an unrelated "working" update - must not gain
// a mapping, or the reducer would stop returning the same state object when
// nothing relevant changed. A tracked session that reports no terminal, or a
// session that is no longer tracked at all, has its mapping dropped.
function withTerminalMapping(state: AutoSwitchState, id: string, terminalId: string, tracked: boolean): AutoSwitchState {
  const mapped = state.terminalOf[id];
  if (tracked && terminalId && mapped !== terminalId) {
    return { ...state, terminalOf: { ...state.terminalOf, [id]: terminalId } };
  }
  if ((!tracked || !terminalId) && mapped !== undefined) {
    const { [id]: _dropped, ...rest } = state.terminalOf;
    return { ...state, terminalOf: rest };
  }
  return state;
}

export function autoSwitchReducer(state: AutoSwitchState, event: AutoSwitchEvent): AutoSwitchState {
  switch (event.type) {
    case "session": {
      const terminalId = event.terminalId ?? "";
      const waiting = event.state === "needs-input" && terminalId !== "";
      const inQueue = state.queue.includes(event.id);
      const isCurrent = state.current === event.id;
      const tracked = waiting || inQueue || isCurrent;
      let next = withTerminalMapping(state, event.id, terminalId, tracked);

      if (waiting) {
        if (next.current !== event.id && !next.queue.includes(event.id)) {
          next = { ...next, queue: [...next.queue, event.id] };
        }
        return next;
      }

      if (next.queue.includes(event.id)) {
        next = { ...next, queue: next.queue.filter((qid) => qid !== event.id) };
      }
      if (next.current === event.id) {
        next = {
          ...next,
          current: null,
          focusRequest: next.focusRequest === event.id ? null : next.focusRequest,
          promotedAt: null,
          lastInputAt: null,
        };
      }
      return next;
    }
    case "enter": {
      if (state.current && state.terminalOf[state.current] === event.terminalId) {
        const { [state.current]: _dropped, ...rest } = state.terminalOf;
        return { ...state, current: null, terminalOf: rest, promotedAt: null, lastInputAt: null };
      }
      return state;
    }
    case "input": {
      if (state.current !== null && state.terminalOf[state.current] === event.terminalId) {
        return { ...state, lastInputAt: event.now };
      }
      return state;
    }
    case "advance": {
      let next = state;
      if (next.current !== null) {
        const silentTooLong = next.lastInputAt !== null && event.now - next.lastInputAt >= RELEASE_SILENCE_MS;
        const staleWithWorkQueued = next.promotedAt !== null
          && event.now - next.promotedAt >= STALE_CURRENT_MS
          && next.queue.length > 0;
        if (silentTooLong || staleWithWorkQueued) {
          const { [next.current]: _dropped, ...rest } = next.terminalOf;
          next = { ...next, current: null, terminalOf: rest, promotedAt: null, lastInputAt: null };
        }
      }
      if (next.current !== null || next.queue.length === 0 || event.typing) return next;
      const [head, ...rest] = next.queue;
      return { ...next, queue: rest, current: head, focusRequest: head, promotedAt: event.now, lastInputAt: null };
    }
    case "focused":
      return state.focusRequest === null ? state : { ...state, focusRequest: null };
    case "reset":
      return state === initialAutoSwitchState ? state : initialAutoSwitchState;
    default:
      return state;
  }
}
