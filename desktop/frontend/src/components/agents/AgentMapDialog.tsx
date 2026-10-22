import { useEffect, useId, useLayoutEffect, useMemo, useRef, useState } from "react";
import type { RefObject } from "react";
import { createPortal } from "react-dom";
import { Bot, Clock3, Network, RefreshCw, Terminal, X } from "lucide-react";
import { useI18n } from "../../lib/i18n";
import { activityDurationMs, activityTree, type ActivityTreeNode } from "./activityModel";
import { activityCountLabel, activityNotice, activityStateKeys } from "./AgentActivityChip";
import type { ActivityNode, ActivitySnapshot } from "./activityTypes";
import type { AgentActivityError } from "./useAgentActivity";

export type AgentMapDialogProps = {
  sessionId: string;
  snapshot: ActivitySnapshot | null;
  loading: boolean;
  refreshing: boolean;
  error?: AgentActivityError | null;
  onRefresh(): void;
  onClose(options: {restoreFocus: boolean}): void;
  onOpenRoot(sessionId: string): void;
  restoreFocusRef?: RefObject<boolean>;
  openerRef?: RefObject<HTMLElement | null>;
};
function visible(element: HTMLElement): boolean {
  if (!element.isConnected || element.closest('[hidden], [inert], [aria-hidden="true"]')) return false;
  for (let current: HTMLElement | null = element; current; current = current.parentElement) {
    const style = getComputedStyle(current);
    if (style.display === "none" || style.visibility === "hidden" || style.opacity === "0") return false;
  }
  return !element.matches(":disabled");
}
function restoreFocus(opener: HTMLElement | null) {
  if (opener && visible(opener)) { opener.focus(); if (document.activeElement === opener) return; }
  // A removed/hidden opener has a deterministic fallback without selecting a session.
  const body = document.body; const previous = body.getAttribute("tabindex");
  body.setAttribute("tabindex", "-1"); body.focus();
  if (previous === null) body.removeAttribute("tabindex"); else body.setAttribute("tabindex", previous);
}
function sortTree(forest: ActivityTreeNode[], rootId: string): ActivityTreeNode[] {
  return forest.map(item => ({...item, children: sortTree(item.children, rootId)})).sort((a, b) =>
    Number(b.node.id === rootId) - Number(a.node.id === rootId)
    || a.node.firstObservedAt.localeCompare(b.node.firstObservedAt) || a.node.id.localeCompare(b.node.id));
}

export default function AgentMapDialog({sessionId, snapshot, loading, refreshing, error = null, onRefresh, onClose, onOpenRoot, restoreFocusRef, openerRef}: AgentMapDialogProps) {
  const {t, formatNumber, formatDate} = useI18n(); const titleId = useId(), treeId = useId(), tasksId = useId(), detailsId = useId();
  const card = useRef<HTMLDivElement>(null), closeButton = useRef<HTMLButtonElement>(null);
  // Capture once before layout focus, including StrictMode's effect replay.
  const [fallbackOpener] = useState(() => document.activeElement instanceof HTMLElement ? document.activeElement : null);
  const restore = useRef(true); const focusedNode = useRef<string | null>(null); const nodeButtons = useRef(new Map<string, HTMLButtonElement>());
  const close = useRef(onClose); close.current = onClose;
  const [selection, setSelection] = useState<{epoch: string; id: string} | null>(null);
  const [now, setNow] = useState(Date.now);
  const epoch = JSON.stringify([sessionId, snapshot?.sourceId, snapshot?.runId]);
  const displayedSnapshot = useMemo(() => snapshot && !snapshot.capabilities.children
    ? {...snapshot, nodes: snapshot.nodes.filter(node => node.id === snapshot.rootId)} : snapshot, [snapshot]);
  const nodes = useMemo(() => [...(displayedSnapshot?.nodes ?? []), ...(displayedSnapshot?.backgroundTasks ?? [])], [displayedSnapshot]);
  const selected = nodes.find(node => node.id === (selection?.epoch === epoch ? selection.id : snapshot?.rootId))
    ?? nodes.find(node => node.id === snapshot?.rootId);
  const forest = useMemo(() => displayedSnapshot ? sortTree(activityTree({...displayedSnapshot, backgroundTasks: []}), displayedSnapshot.rootId) : [], [displayedSnapshot]);
  const tasks = useMemo(() => sortTree((snapshot?.backgroundTasks ?? []).map(node => ({node, children: []})), ""), [snapshot]);
  const notice = activityNotice(snapshot, loading, error, t);
  const ticking = snapshot !== null && nodes.some(node => node.startedAt && !node.endedAt && activityDurationMs(node, Date.now(), snapshot) !== null);

  useLayoutEffect(() => {
    const opener = openerRef ? openerRef.current : fallbackOpener;
    const element = card.current; closeButton.current?.focus();
    return () => {
      // Sibling inert attributes are removed after layout cleanup, during this
      // same commit. Restore only after that commit and recheck focus ownership.
      queueMicrotask(() => {
        // This live boolean deliberately changes immediately before unmount.
        // eslint-disable-next-line react-hooks/exhaustive-deps
        if (!restore.current || restoreFocusRef?.current === false || element?.isConnected) return;
        const active = document.activeElement;
        if (active !== document.body && active && !element?.contains(active)) return;
        if (document.querySelector('[role="dialog"][aria-modal="true"]')) return;
        restoreFocus(opener);
      });
    };
  }, [restoreFocusRef, openerRef, fallbackOpener]);
  useLayoutEffect(() => {
    if (selection?.epoch === epoch && selection.id !== snapshot?.rootId && !nodes.some(node => node.id === selection.id)) {
      setSelection(snapshot ? {epoch, id: snapshot.rootId} : null);
    }
    if (focusedNode.current && !nodes.some(node => node.id === focusedNode.current)) {
      if (document.activeElement === document.body || card.current?.contains(document.activeElement)) {
        (nodeButtons.current.get(snapshot?.rootId ?? "") ?? closeButton.current)?.focus();
      }
      focusedNode.current = null;
    }
  }, [nodes, snapshot, epoch, selection]);
  useEffect(() => {
    if (!ticking) return;
    setNow(Date.now()); const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, [ticking]);
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (!card.current?.contains(document.activeElement)) return;
      if (event.key === "Escape") { event.preventDefault(); event.stopImmediatePropagation(); close.current({restoreFocus: true}); }
      if (event.key === "Tab") {
        const controls = [...card.current.querySelectorAll<HTMLButtonElement>("button:not(:disabled)")].filter(visible);
        const first = controls[0], last = controls[controls.length - 1];
        if (event.shiftKey && document.activeElement === first) {event.preventDefault(); last?.focus();}
        else if (!event.shiftKey && document.activeElement === last) {event.preventDefault(); first?.focus();}
      }
    };
    window.addEventListener("keydown", onKey, true);
    return () => window.removeEventListener("keydown", onKey, true);
  }, []);

  const timestamp = (value: string | undefined) => {
    if (!value || !Number.isFinite(Date.parse(value))) return t("agents.map.unknownMetric");
    return formatDate(new Date(value), {month: "short", day: "numeric", hour: "2-digit", minute: "2-digit", second: "2-digit"});
  };
  const renderNode = (node: ActivityNode) => <button type="button" ref={element => {if (element) nodeButtons.current.set(node.id, element); else nodeButtons.current.delete(node.id);}}
      data-node-id={node.id} aria-pressed={selected?.id === node.id} aria-controls={detailsId}
      onClick={() => setSelection({epoch, id: node.id})}
      className={`flex w-full items-start gap-2 rounded-md border px-2.5 py-2 text-left text-[11px] focus-visible:outline focus-visible:outline-2 focus-visible:outline-accent2 ${selected?.id === node.id ? "border-accent2/40 bg-accent2/[0.08]" : "border-transparent hover:bg-white/5"}`}>
      {node.kind === "background-task" ? <Terminal aria-hidden="true" className="mt-0.5 h-3.5 w-3.5 shrink-0 text-white/60" /> : <Bot aria-hidden="true" className="mt-0.5 h-3.5 w-3.5 shrink-0 text-accent2" />}
      <span className="min-w-0 flex-1 break-words">{node.label}<span className="mt-0.5 block text-[10px] text-white/55">{t(activityStateKeys[node.state])}</span>
        {node.kind === "background-task" && <><span className="mt-0.5 block text-[10px] text-white/55">{t("agents.map.kind")}: {node.agentType || t("agents.map.backgroundTask")}</span>
          <span className="mt-0.5 block text-[10px] text-white/55">{t("agents.map.lastObserved")}: {timestamp(node.lastObservedAt)}</span></>}
      </span>
    </button>;
  const renderTree = (items: ActivityTreeNode[]) => <ul className="space-y-1">{items.map(({node, children}) => <li key={node.id}>
    {renderNode(node)}
    {children.length > 0 && <div className="ml-4 border-l border-white/15 pl-2">{renderTree(children)}</div>}
  </li>)}</ul>;
  const duration = selected && snapshot ? activityDurationMs(selected, now, snapshot) : null;
  const tokens = selected?.tokens;
  const tokenRows = tokens ? [
    ["agents.map.inputTokens", tokens.input], ["agents.map.outputTokens", tokens.output], ["agents.map.totalTokens", tokens.total],
  ] as const : [];
  return createPortal(<div className="modal-backdrop z-[80]" onClick={event => {if (event.target === event.currentTarget) onClose({restoreFocus: true});}}>
    <div ref={card} role="dialog" aria-modal="true" aria-labelledby={titleId}
      className="modal-card flex max-h-[80vh] w-[850px] max-w-[94vw] flex-col overflow-hidden text-white/90"
      onFocusCapture={event => {focusedNode.current = (event.target as HTMLElement).closest<HTMLElement>("[data-node-id]")?.dataset.nodeId ?? null;}}>
      <header className="flex shrink-0 items-start gap-3 border-b border-white/10 p-4">
        <Network aria-hidden="true" className="mt-0.5 h-5 w-5 text-accent2" />
        <div className="min-w-0 flex-1"><h2 id={titleId} className="text-sm font-semibold">{t("agents.map.title")}</h2>
          {snapshot?.capabilities.children && snapshot.status !== "loading" && snapshot.status !== "unavailable" && <p className="mt-1 text-[11px] text-white/65">{activityCountLabel(snapshot, t)}</p>}
        </div>
        <button type="button" ref={closeButton} aria-label={t("agents.map.close")} onClick={() => onClose({restoreFocus: true})}
          className="rounded-md p-1.5 text-white/60 hover:bg-white/10 hover:text-white focus-visible:outline focus-visible:outline-2 focus-visible:outline-accent2"><X aria-hidden="true" className="h-4 w-4" /></button>
      </header>
      <div className="min-h-0 overflow-y-auto">
        <div className="space-y-1 border-b border-white/10 px-4 py-3 text-[11px] leading-relaxed text-white/60">
          {notice && <p role={error || snapshot?.status === "error" ? "alert" : "status"} className="text-amber-200/90">{notice}</p>}
          {snapshot?.truncated && snapshot.messageCode !== "truncated" && <p>{t("agents.map.messages.truncated")}</p>}
          {snapshot?.stale && <p>{t("agents.map.stale")}</p>}
          {snapshot && <><p>{t(snapshot.status === "available" ? "agents.map.coverageAvailable" : "agents.map.coveragePartial")}</p>
            <p>{t("agents.map.observedSince", {when: timestamp(snapshot.observedSince)})}</p></>}
          <p>{t("agents.map.memoryOnly")}</p>
        </div>
        <div className="grid min-h-0 grid-cols-1 min-[640px]:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
          <section role="region" aria-labelledby={treeId} className="max-h-[38vh] overflow-auto p-4">
            <h3 id={treeId} className="mb-2 text-xs font-medium text-white/75">{t("agents.map.tree")}</h3>
            {renderTree(forest)}
            {!displayedSnapshot?.nodes.length && !loading && <p className="text-[11px] text-white/55">{t("agents.map.noAgents")}</p>}
          </section>
          <section id={detailsId} aria-label={t("agents.map.details")} className="min-w-0 border-t border-white/10 p-4 min-[640px]:border-l min-[640px]:border-t-0">
            {selected && <><h3 className="break-words text-sm font-semibold">{selected.label}</h3>
              <p className="mt-1 text-[11px] text-accent2">{t(activityStateKeys[selected.state])}</p>
              <dl className="mt-4 grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-2 break-words text-[11px]">
                <dt className="text-white/50">{t("agents.map.kind")}</dt><dd>{t(selected.id === snapshot?.rootId ? "agents.map.principal" : selected.kind === "agent" ? "agents.map.child" : "agents.map.backgroundTask")}</dd>
                {selected.agentType && <><dt className="text-white/50">{t("agents.map.agentType")}</dt><dd>{selected.agentType}</dd></>}
                {selected.projectName && <><dt className="text-white/50">{t("agents.map.project")}</dt><dd>{selected.projectName}</dd></>}
                {selected.projectPath && <><dt className="text-white/50">{t("agents.map.projectPath")}</dt><dd>{selected.projectPath}</dd></>}
                {selected.worktreePath && <><dt className="text-white/50">{t("agents.map.worktreePath")}</dt><dd>{selected.worktreePath}</dd></>}
                <dt className="text-white/50">{t("agents.map.model")}</dt><dd>{selected.model ? selected.model.kind === "configured" ? t("agents.map.configuredModel", {name: selected.model.name}) : selected.model.name : t("agents.map.modelUnavailable")}</dd>
                <dt className="flex items-center gap-1 text-white/50"><Clock3 aria-hidden="true" className="h-3 w-3" />{t("agents.map.duration")}</dt><dd>{duration === null ? t("agents.map.durationUnavailable") : t("agents.map.seconds", {count: Math.floor(duration / 1000)})}</dd>
                <dt className="text-white/50">{t("agents.map.started")}</dt><dd>{timestamp(selected.startedAt)}</dd>
                {!selected.startedAt && <><dt className="text-white/50">{t("agents.map.firstObserved")}</dt><dd>{timestamp(selected.firstObservedAt)}</dd></>}
                {selected.endedAt && <><dt className="text-white/50">{t("agents.map.ended")}</dt><dd>{timestamp(selected.endedAt)}</dd></>}
                <dt className="text-white/50">{t("agents.map.lastObserved")}</dt><dd>{timestamp(selected.lastObservedAt)}</dd>
                {tokenRows.some(([, count]) => count !== undefined) ? tokenRows.map(([key, count]) => count === undefined ? null : <div key={key} className="contents"><dt className="text-white/50">{t(key)}</dt><dd>{formatNumber(count)}</dd></div>) : <><dt className="text-white/50">{t("agents.map.tokens")}</dt><dd>{t("agents.map.tokensUnavailable")}</dd></>}
              </dl></>}
          </section>
        </div>
        <section role="region" aria-labelledby={tasksId} className="max-h-[30vh] overflow-auto border-t border-white/10 p-4">
          <h3 id={tasksId} className="mb-2 text-xs font-medium text-white/75">{t("agents.map.backgroundTasks")}</h3>
          {snapshot && <p className="mb-2 text-[10px] leading-relaxed text-white/50">{t(!snapshot.capabilities.backgroundTasks ? "agents.map.tasksUnavailable" : snapshot.backgroundTasks.length === 0 ? "agents.map.tasksEmpty" : "agents.map.taskCount", {count: snapshot.backgroundTasks.length})}</p>}
          {renderTree(tasks)}
        </section>
      </div>
      <footer className="flex shrink-0 justify-end gap-2 border-t border-white/10 p-4">
        <button type="button" onClick={onRefresh} disabled={refreshing || loading}
          className="flex items-center gap-1.5 rounded-md border border-white/15 px-3 py-1.5 text-xs text-white/75 hover:bg-white/10 disabled:opacity-40 focus-visible:outline focus-visible:outline-2 focus-visible:outline-accent2"><RefreshCw aria-hidden="true" className="h-3.5 w-3.5" />{t(refreshing ? "agents.map.refreshing" : "agents.map.refresh")}</button>
        <button type="button" disabled={!snapshot?.nodes.some(node => node.id === snapshot.rootId && node.terminalId)}
          onClick={() => {restore.current = false; onClose({restoreFocus: false}); onOpenRoot(sessionId);}}
          className="flex items-center gap-1.5 rounded-md bg-accent px-3 py-1.5 text-xs font-medium text-white hover:bg-accent/85 disabled:opacity-40 focus-visible:outline focus-visible:outline-2 focus-visible:outline-accent2"><Terminal aria-hidden="true" className="h-3.5 w-3.5" />{t("agents.map.openTerminal")}</button>
      </footer>
    </div>
  </div>, document.body);
}
