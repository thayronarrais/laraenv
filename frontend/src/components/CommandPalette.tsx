// Ctrl+K command palette. Two behaviors share one input:
//   • Normal mode  — fuzzy-filter the built-in/custom command list (Enter runs).
//   • Launcher mode — type "<project> <provider>", "<project> <command…>" or
//     "ssh:<host>"; provider/artisan/npm/host autocompletions appear (Tab
//     completes the token), and an "execute" row runs it on Enter.

import { useEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import clsx from "clsx";
import { Search, CornerDownLeft } from "lucide-react";
import type { CommandIntent, RuntimeCommand } from "../commandCenter/types";
import { computeLauncher, type LauncherData } from "../commandCenter/launcher";
import { useT } from "../lib/i18n";

type Row =
  | { type: "cmd"; cmd: RuntimeCommand }
  | { type: "exec"; label: string; hint?: string; intent?: CommandIntent; run: () => void; altRun?: () => void }
  | { type: "suggest"; label: string; hint?: string; complete: string };

// Actions that support an alternate "open in xterm.js" target on Shift+Enter.
const ALT_ACTIONS = new Set(["openTerminal", "terminal", "ssh"]);
const rowIntent = (row: Row): CommandIntent | undefined =>
  row.type === "cmd" ? row.cmd.intent : row.type === "exec" ? row.intent : undefined;
const rowSupportsAlt = (row: Row): boolean => {
  const a = rowIntent(row)?.action;
  return !!a && ALT_ACTIONS.has(a);
};

export default function CommandPalette({
  open,
  commands,
  launcher,
  onClose,
  bar = false,
  onPick,
}: {
  open: boolean;
  commands: RuntimeCommand[];
  launcher: LauncherData;
  onClose: () => void;
  // bar = standalone command-bar window: fills the window (no overlay) and
  // forwards the chosen command's intent via onPick instead of running run().
  bar?: boolean;
  // alt = Shift held → open in xterm.js (in-app) instead of native external.
  onPick?: (intent: CommandIntent | undefined, alt: boolean) => void;
}) {
  const t = useT();
  const [q, setQ] = useState("");
  const [sel, setSel] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const listRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    setQ("");
    setSel(0);
    // Focus the input persistently: a brand-new window (the command bar) may not
    // have OS keyboard focus yet, so retry across the first few hundred ms and
    // re-grab whenever the window regains focus.
    const focus = () => { try { window.focus(); } catch {} inputRef.current?.focus(); };
    const timers = [0, 40, 120, 250, 400, 600].map((t) => setTimeout(focus, t));
    window.addEventListener("focus", focus);
    return () => {
      timers.forEach(clearTimeout);
      window.removeEventListener("focus", focus);
    };
  }, [open]);

  // Normal-mode filtered commands. Token-based AND match so separators in the
  // label (e.g. the "·" in "Open terminal · aioke") don't block multi-word
  // queries like "open terminal aioke".
  const filtered = useMemo(() => {
    const tokens = q.trim().toLowerCase().split(/\s+/).filter(Boolean);
    if (tokens.length === 0) return commands;
    return commands.filter((c) => {
      const hay = (c.label + " " + c.group + " " + (c.hint ?? "")).toLowerCase();
      return tokens.every((t) => hay.includes(t));
    });
  }, [q, commands]);

  const launcherState = useMemo(() => computeLauncher(q, launcher), [q, launcher]);

  // Build the rendered sections (with a flat index per actionable row) plus a
  // parallel flat list used to resolve keyboard selection.
  const { sections, flat } = useMemo(() => {
    const sections: { group: string; items: { row: Row; idx: number }[] }[] = [];
    const flat: Row[] = [];
    const push = (group: string, rows: Row[]) => {
      if (!rows.length) return;
      sections.push({ group, items: rows.map((row) => ({ row, idx: flat.push(row) - 1 })) });
    };

    if (launcherState.mode === "exclusive") {
      const rows: Row[] = launcherState.rows.map((r) =>
        r.kind === "execute"
          ? { type: "exec", label: r.label, hint: r.hint, intent: r.intent, run: r.run, altRun: r.altRun }
          : { type: "suggest", label: r.label, hint: r.hint, complete: r.complete },
      );
      push(t("commandCenter.groups.run"), rows);
    } else {
      const order: string[] = [];
      const map = new Map<string, Row[]>();
      for (const cmd of filtered) {
        if (!map.has(cmd.group)) { map.set(cmd.group, []); order.push(cmd.group); }
        map.get(cmd.group)!.push({ type: "cmd", cmd });
      }
      for (const g of order) push(g, map.get(g)!);
    }

    return { sections, flat };
  }, [launcherState, filtered, t]);

  useEffect(() => { setSel(0); }, [q]);
  useEffect(() => {
    if (sel >= flat.length) setSel(Math.max(0, flat.length - 1));
  }, [flat, sel]);
  useEffect(() => {
    const el = listRef.current?.querySelector(`[data-idx="${sel}"]`);
    (el as HTMLElement | null)?.scrollIntoView({ block: "nearest" });
  }, [sel, open]);

  // alt (Shift) opens terminal/ssh rows in the in-app xterm.js instead of the
  // native external terminal. Other rows ignore alt.
  const activate = (row: Row, alt = false) => {
    if (row.type === "suggest") { setQ(row.complete); setSel(0); return; }
    // Command-bar window: forward the intent to the main process, don't run here.
    if (onPick) {
      onPick(rowIntent(row), alt);
      return;
    }
    onClose();
    const altRun = row.type === "cmd" ? row.cmd.altRun : row.altRun;
    if (alt && altRun) { void altRun(); return; }
    if (row.type === "cmd") void row.cmd.run();
    else void row.run();
  };

  // Keyboard navigation is handled on `window` (not the modal's onKeyDown) so
  // arrows/Enter/Tab work even if focus drifts off the input. Current sel/flat/
  // activate are read through refs to avoid stale closures.
  const selRef = useRef(sel); selRef.current = sel;
  const flatRef = useRef(flat); flatRef.current = flat;
  const activateRef = useRef(activate); activateRef.current = activate;
  useEffect(() => {
    if (!open) return;
    const handler = (e: KeyboardEvent) => {
      if (e.key === "ArrowDown") { e.preventDefault(); setSel((s) => Math.min(s + 1, flatRef.current.length - 1)); }
      else if (e.key === "ArrowUp") { e.preventDefault(); setSel((s) => Math.max(s - 1, 0)); }
      else if (e.key === "Enter") { e.preventDefault(); const row = flatRef.current[selRef.current]; if (row) activateRef.current(row, e.shiftKey); }
      else if (e.key === "Tab") {
        e.preventDefault();
        const row = flatRef.current[selRef.current];
        if (row?.type === "suggest") { setQ(row.complete); setSel(0); }
        else if (row?.type === "cmd" && row.cmd.complete) { setQ(row.cmd.complete); setSel(0); }
      } else if (e.key === "Escape") { e.preventDefault(); onClose(); }
    };
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, [open, onClose]);

  // NOTE: keep every hook above this line — an early return before a hook makes
  // the open-toggle change the hook count and React tears down the whole tree.
  if (!open) return null;

  const selectedRow = flat[sel];
  const switchesTerminal = selectedRow && rowIntent(selectedRow)?.action === "focusTerminal";

  const box = (
      <div
        className={clsx(
          "relative flex flex-col overflow-hidden bg-[var(--c-bg-2)]",
          bar
            ? "w-screen h-screen"
            : "mt-[12vh] w-[min(640px,92vw)] rounded-xl border border-white/15 shadow-2xl",
        )}
        onMouseDown={(e) => e.stopPropagation()}
      >
        <div className="flex items-center gap-2 px-4 py-3 border-b border-white/10">
          <Search className="w-4 h-4 text-white/40" />
          <input
            ref={inputRef}
            value={q}
            onChange={(e) => setQ(e.target.value)}
            placeholder={t("commandCenter.palette.searchPlaceholder", { examples: "“project codex” · “project php artisan migrate” · “ssh:host”" })}
            className="flex-1 bg-transparent outline-none text-sm text-white placeholder:text-white/35"
          />
          <span className="text-[10px] px-1.5 py-0.5 rounded border border-white/15 text-white/40">ESC</span>
        </div>

        <div ref={listRef} className={clsx("overflow-y-auto py-2", bar ? "flex-1" : "max-h-[50vh]")}>
          {flat.length === 0 && (
            <div className="px-4 py-8 text-center text-xs text-white/40">{t("commandCenter.palette.noCommandsFound")}</div>
          )}
          {sections.map((sec) => (
            <div key={sec.group} className="mb-1">
              <div className="px-4 py-1 text-[10px] uppercase tracking-wider text-white/35">{sec.group}</div>
              {sec.items.map(({ row, idx }) => {
                const label = row.type === "cmd" ? row.cmd.label : row.label;
                const hint = row.type === "cmd" ? row.cmd.hint : row.hint;
                const hotkey = row.type === "cmd" ? row.cmd.hotkey : undefined;
                const key = row.type === "cmd" ? row.cmd.id : `${row.type}:${label}:${idx}`;
                const supportsAlt = rowSupportsAlt(row);
                return (
                  <button
                    key={key}
                    type="button"
                    data-idx={idx}
                    onMouseEnter={() => setSel(idx)}
                    onClick={(e) => activate(row, e.shiftKey)}
                    className={clsx(
                      "w-full flex items-center gap-3 px-4 py-2 text-left border-l-2 transition-colors",
                      idx === sel
                        ? "bg-accent/30 border-accent"
                        : "border-transparent hover:bg-white/5",
                    )}
                  >
                    <div className="flex-1 min-w-0">
                      <div className={clsx("text-sm truncate", idx === sel ? "text-white font-medium" : "text-white/80")}>{label}</div>
                      {hint && <div className="text-[11px] text-white/45 truncate">{hint}</div>}
                    </div>
                    {((row.type === "suggest") || (row.type === "cmd" && row.cmd.complete)) && idx === sel && (
                      <span className="text-[10px] px-1.5 py-0.5 rounded border border-white/15 text-white/45 font-mono flex-shrink-0">Tab</span>
                    )}
                    {supportsAlt && idx === sel && (
                      <span className="text-[10px] px-1.5 py-0.5 rounded border border-white/15 text-white/45 font-mono flex-shrink-0">⇧↵ xterm</span>
                    )}
                    {row.type === "exec" && idx === sel && (
                      <CornerDownLeft className="w-3.5 h-3.5 text-white/45 flex-shrink-0" />
                    )}
                    {hotkey && (
                      <span className="text-[10px] px-1.5 py-0.5 rounded border border-white/15 text-white/45 font-mono flex-shrink-0">{hotkey}</span>
                    )}
                  </button>
                );
              })}
            </div>
          ))}
        </div>

        <div className="flex items-center gap-3 px-4 py-2 border-t border-white/10 text-[10px] text-white/40">
          <span>↑↓ {t("commandCenter.palette.hints.navigate")}</span>
          <span>Tab {t("commandCenter.palette.hints.complete")}</span>
          <span>Enter {t(switchesTerminal ? "commandCenter.palette.hints.switch" : "commandCenter.palette.hints.run")}</span>
          {selectedRow && rowSupportsAlt(selectedRow) && <span>⇧↵ xterm.js</span>}
          <span>Esc {t("common.close")}</span>
        </div>
      </div>
  );

  // Standalone command-bar window: the OS window IS the bar, no overlay/portal.
  if (bar) return box;

  return createPortal(
    <div className="fixed inset-0 z-[9999] flex items-start justify-center" onMouseDown={onClose}>
      {/* Solid (no backdrop-filter): WebView2 paints backdrop-blur as a black
          frame when the palette opens right after the global hotkey changes
          window focus, so we avoid GPU-composited blur here entirely. */}
      <div className="absolute inset-0 bg-black/60" />
      {box}
    </div>,
    document.body,
  );
}
