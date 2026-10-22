import { useEffect, useRef, useState } from "react";
// @ts-ignore
import { ListAgentSessions } from "../../../wailsjs/go/main/App";
// @ts-ignore
import { EventsOn } from "../../../wailsjs/runtime";
import { autoSwitchReducer, initialAutoSwitchState, waitingCount, type AutoSwitchEvent, type AutoSwitchState } from "./autoSwitchQueue";
import { isEditingElsewhere, isUserTyping, subscribeEnter, subscribeInput } from "./terminalActivity";

export const AUTO_SWITCH_TICK_MS = 500;

type SessionLike = { id: string; state: string; terminalId?: string };

// Switches the visible terminal to whichever agent is waiting for input,
// one at a time, never while the user is mid-command.
export function useAgentAutoSwitch({ enabled, blocked = false, focusSession }: { enabled: boolean; blocked?: boolean; focusSession: (sessionId: string) => void }): { waiting: number } {
  const blockedRef = useRef(blocked);
  blockedRef.current = blocked;
  const stateRef = useRef<AutoSwitchState>(initialAutoSwitchState);
  const focusRef = useRef(focusSession);
  useEffect(() => { focusRef.current = focusSession; });
  const [waiting, setWaiting] = useState(0);

  useEffect(() => {
    if (!enabled) {
      stateRef.current = autoSwitchReducer(stateRef.current, { type: "reset" });
      setWaiting(0);
      return;
    }
    const typing = () => isUserTyping() || isEditingElsewhere();
    // There is nothing to advance towards - and nothing that can go stale -
    // once the queue is empty and no session is current, so the tick only
    // needs to run while one of those holds.
    const hasWork = (s: AutoSwitchState) => s.queue.length > 0 || s.current !== null;
    let timer: ReturnType<typeof setInterval> | null = null;
    const dispatch = (event: AutoSwitchEvent) => {
      if (blockedRef.current && event.type === "advance") return;
      const next = autoSwitchReducer(stateRef.current, event);
      if (next === stateRef.current) return;
      stateRef.current = next;
      setWaiting(waitingCount(next));
      if (hasWork(next)) {
        if (timer === null) timer = setInterval(() => dispatch({ type: "advance", typing: typing(), now: Date.now() }), AUTO_SWITCH_TICK_MS);
      } else if (timer !== null) {
        clearInterval(timer);
        timer = null;
      }
      if (next.focusRequest) {
        const target = next.focusRequest;
        stateRef.current = autoSwitchReducer(stateRef.current, { type: "focused" });
        focusRef.current(target);
      }
    };
    const onSession = (session: SessionLike) => {
      if (!session?.id) return;
      dispatch({ type: "session", id: session.id, state: session.state, terminalId: session.terminalId });
      dispatch({ type: "advance", typing: typing(), now: Date.now() });
    };
    const off = EventsOn("agent:session-changed", onSession);
    const offEnter = subscribeEnter((terminalId) => {
      dispatch({ type: "enter", terminalId });
      dispatch({ type: "advance", typing: typing(), now: Date.now() });
    });
    const offInput = subscribeInput((terminalId) => {
      dispatch({ type: "input", terminalId, now: Date.now() });
      dispatch({ type: "advance", typing: typing(), now: Date.now() });
    });
    let cancelled = false;
    ListAgentSessions("", false).then((sessions: SessionLike[]) => { if (!cancelled) (sessions ?? []).forEach(onSession); }).catch(() => {});
    return () => { cancelled = true; off(); offEnter(); offInput(); if (timer !== null) clearInterval(timer); };
  }, [enabled]);

  return { waiting };
}
