import { useEffect, useRef } from 'react';
import { EventsOn } from '../../../wailsjs/runtime';
import { ResolveDocumentShutdown, SetDocumentWorkspaceState } from '../../../wailsjs/go/main/App';
import type { DocumentController } from './types';

/** Reports and replies share one lane; a rejected cancel retains its one-shot request ID. */
export function useDocumentShutdown(controller: DocumentController): void {
  const current = useRef(controller); current.current = controller;
  const report = useRef<() => void>(() => {});
  useEffect(() => {
    let alive = true;
    let pending: { id: string; canceling: boolean } | null = null;
    const handled = new Set<string>();
    let lane = Promise.resolve();
    let retry: ReturnType<typeof setTimeout> | undefined;
    const enqueue = (fn: () => Promise<void>) => {
      lane = lane.then(async () => { if (alive) await fn(); }).catch(() => {});
      return lane;
    };
    const cancel = (request: NonNullable<typeof pending>) => {
      request.canceling = true;
      current.current.endShutdownHandoff();
      void enqueue(async () => {
        if (pending !== request) return;
        try {
          const state = current.current.getWorkspaceState();
          await SetDocumentWorkspaceState(state);
          if (!alive || pending !== request) return;
          await ResolveDocumentShutdown({ requestId: request.id, decision: 'cancel', stateVersion: state.stateVersion });
          if (pending === request) { handled.add(request.id); pending = null; }
        } catch {
          // Even cancel is version-checked by Go. Never forget the ID on rejection.
          if (alive && pending === request) retry = setTimeout(() => cancel(request), 250);
        }
      });
    };
    const handle = async (request: NonNullable<typeof pending>) => {
      try {
        if (!await current.current.confirmCloseAll()) { if (alive) cancel(request); return; }
        if (!alive || pending !== request) return;
        const state = current.current.beginShutdownHandoff();
        if (!state) { cancel(request); return; }
        await enqueue(async () => {
          if (!alive || pending !== request) return;
          try {
            // Read after entering the lane: no render closure can regress the reported version.
            const frozen = current.current.getWorkspaceState();
            if (frozen.dirtyCount || !frozen.ready) { cancel(request); return; }
            await SetDocumentWorkspaceState(frozen);
            if (!alive || pending !== request) return;
            const outcome = await ResolveDocumentShutdown({ requestId: request.id, decision: 'allow', stateVersion: frozen.stateVersion });
            if (outcome?.released) current.current.endShutdownHandoff();
            handled.add(request.id); pending = null;
            // Accepted continuation may quit after a delay. Keep mutations frozen until teardown.
          } catch { cancel(request); }
        });
      } catch { if (alive) cancel(request); }
    };
    // Subscribe before the first ready report; the gate emits each request only once.
    const unsubscribe = EventsOn('documents:shutdown-request', (payload: { requestId: string }) => {
      if (!alive || !payload || typeof payload.requestId !== 'string' || !payload.requestId || handled.has(payload.requestId) || pending?.id === payload.requestId) return;
      if (pending && !pending.canceling) return;
      // A new server ID proves the earlier continuation/cancellation released the gate.
      if (pending) handled.add(pending.id);
      clearTimeout(retry); pending = { id: payload.requestId, canceling: false }; void handle(pending);
    });
    report.current = () => { void enqueue(async () => { try { await SetDocumentWorkspaceState(current.current.getWorkspaceState()); } catch { /* Fail closed; the next change/request retries. */ } }); };
    report.current();
    return () => { alive = false; clearTimeout(retry); unsubscribe(); report.current = () => {}; };
  }, [controller.getWorkspaceState]);
  useEffect(() => { report.current(); }, [controller.stateVersion]);
}
