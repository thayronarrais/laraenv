import { useCallback, useEffect, useRef, useState } from "react";
import { agentActivityApi } from "./agentActivityApi";
import type { ActivityInterestPurpose, ActivitySnapshot } from "./activityTypes";
export interface UseAgentActivityOptions { sessionId: string; windowId: string; purpose: ActivityInterestPurpose; enabled: boolean }
export type AgentActivityError = "refresh-failed";
export interface AgentActivityResult { snapshot: ActivitySnapshot | null; loading: boolean; refreshing: boolean; error: AgentActivityError | null; refresh(): Promise<void> }
type View = Omit<AgentActivityResult, "refresh">;
const empty: View = { snapshot: null, loading: false, refreshing: false, error: null };
const epoch = (value: ActivitySnapshot) => JSON.stringify([value.sourceId, value.runId]);

export function useAgentActivity({sessionId, windowId, purpose, enabled}: UseAgentActivityOptions): AgentActivityResult {
  const [view, setView] = useState<View>(empty);
  const refreshRef = useRef<() => Promise<void>>(async () => {});
  const refresh = useCallback(() => refreshRef.current(), []);

  useEffect(() => {
    setView(empty);
    if (!enabled || !sessionId || !windowId) { refreshRef.current = async () => {}; return; }
    let active = true;
    let current: ActivitySnapshot | null = null;
    let candidate: ActivitySnapshot | null = null;
    let getId = 0;
    let getPending = false;
    let refreshPromise: Promise<void> | null = null;
    const retired = new Set<string>();
    const failures = new Set<"get" | "refresh" | "interest">();
    const displayed = () => failures.size && current ? {...current, stale: true, messageCode: "refresh-failed" as const} : current;
    const updateErrors = () => setView(previous => ({...previous, snapshot: displayed(), error: failures.size ? "refresh-failed" : null}));
    const publish = (next: ActivitySnapshot) => {
      if (current && epoch(current) !== epoch(next)) retired.add(epoch(current));
      current = next;
      failures.delete("get");
      failures.delete("refresh");
      setView(previous => ({...previous, snapshot: displayed(), error: failures.size ? "refresh-failed" : null, loading: false}));
    };
    const valid = (next: ActivitySnapshot) => active && next.sessionId === sessionId
      && Number.isSafeInteger(next.revision) && next.revision >= 0 && !retired.has(epoch(next));
    const older = (next: ActivitySnapshot) => current !== null && next.sourceId === current.sourceId
      && (next.revision < current.revision || (epoch(next) !== epoch(current) && next.revision === current.revision));
    const failed = (kind: "get" | "refresh" | "interest") => {
      if (!active) return;
      failures.add(kind);
      setView(previous => ({...previous, snapshot: displayed(), error: "refresh-failed", loading: false}));
    };
    const get = async () => {
      const requestId = ++getId;
      getPending = true;
      try {
        const next = await agentActivityApi.get(sessionId);
        if (!active || requestId !== getId) return;
        if (!valid(next) || older(next)) return;
        // Events can advance the revision while a confirmation Get is pending.
        // The Get confirms identity; the newest revision of that identity wins.
        const confirmed = candidate && epoch(candidate) === epoch(next) && candidate.revision > next.revision ? candidate : next;
        candidate = null;
        publish(confirmed);
      } catch {
        if (requestId === getId) { candidate = null; failed("get"); }
      } finally {
        if (active && requestId === getId) {
          getPending = false;
          setView(previous => ({...previous, loading: false}));
        }
      }
    };
    const receive = (next: ActivitySnapshot, fromRefresh = false) => {
      if (!valid(next) || older(next)) return;
      if (current && epoch(next) === epoch(current)) {
        if (next.revision > current.revision || fromRefresh) publish(next);
        return;
      }
      if (candidate && epoch(candidate) === epoch(next)) {
        if (next.revision > candidate.revision) candidate = next;
        // A retained candidate can outlive a discarded confirmation response.
        if (!getPending) void get();
        return;
      }
      candidate = next;
      // Never infer epoch order from opaque UUIDs or compare owners' revisions.
      void get();
    };
    const renew = () => {
      void agentActivityApi.setInterest({windowId, purpose, sessionId}).then(() => {
        if (active && failures.delete("interest")) updateErrors();
      }).catch(() => failed("interest"));
    };
    const timer = setInterval(renew, 10000);
    let removed = false;
    const remove = () => {
      if (removed) return;
      removed = true;
      clearInterval(timer);
      void agentActivityApi.removeInterest(windowId, purpose).catch(() => {});
    };
    const offChanged = agentActivityApi.onChanged(next => receive(next));
    const offReset = agentActivityApi.onReset(() => {
      active = false;
      ++getId;
      current = null;
      candidate = null;
      remove();
      setView(empty);
    });
    refreshRef.current = () => {
      if (!active) return Promise.resolve();
      if (refreshPromise) return refreshPromise;
      if (failures.has("interest")) renew();
      const startedEpoch = current && epoch(current);
      setView(previous => ({...previous, refreshing: true}));
      refreshPromise = (async () => {
        try { receive(await agentActivityApi.refresh(sessionId), true); }
        catch { if (startedEpoch === (current && epoch(current))) failed("refresh"); }
        finally {
          refreshPromise = null;
          if (active) setView(previous => ({...previous, refreshing: false}));
        }
      })();
      return refreshPromise;
    };
    setView({...empty, loading: true});
    renew();
    void get();
    return () => {
      active = false;
      ++getId;
      offChanged();
      offReset();
      remove();
    };
  }, [sessionId, windowId, purpose, enabled]);

  return {...view, refresh};
}
