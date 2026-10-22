import { useEffect, useRef, useState } from "react";
import { X, ScrollText } from "lucide-react";
import clsx from "clsx";
// @ts-ignore
import { ListLoggedServices, GetServiceLogs, ServiceStatus } from "../../wailsjs/go/main/App";
// @ts-ignore
import { EventsOn } from "../../wailsjs/runtime";
import { useT } from "../lib/i18n";

function labelFor(name: string): string {
  const m = /^php-fpm:(.+)$/.exec(name);
  if (m) return `PHP-FPM ${m[1]}`;
  return name.charAt(0).toUpperCase() + name.slice(1);
}

export default function ServiceLogsModal({ open, onClose }: { open: boolean; onClose: () => void }) {
  const t = useT();
  const [services, setServices] = useState<string[]>([]);
  const [statusMap, setStatusMap] = useState<Record<string, string>>({});
  const [selected, setSelected] = useState<string | null>(null);
  const [lines, setLines] = useState<string[]>([]);
  const boxRef = useRef<HTMLDivElement>(null);
  const selectedRef = useRef<string | null>(null);
  selectedRef.current = selected;

  // Load the service list + statuses, pick an initial selection.
  const loadList = async () => {
    const list: string[] = (await ListLoggedServices()) ?? [];
    setServices(list);
    const entries = await Promise.all(list.map(async (n) => [n, await ServiceStatus(n)] as const));
    setStatusMap(Object.fromEntries(entries));
    setSelected((cur) => cur && list.includes(cur) ? cur : (entries.find(([, s]) => s === "running")?.[0] ?? list[0] ?? null));
  };

  useEffect(() => {
    if (!open) return;
    loadList();
    // EventsOn returns a canceler that removes ONLY this listener.
    // EventsOff("service:status") would nuke every listener for the event
    // app-wide (Dashboard cards, Projects, Downloads, PhpFpmStatus) — and
    // since pages are kept alive and never remount, they'd stay deaf until
    // app restart. That's exactly the "Stop all but cards stay RUNNING" bug.
    const off = EventsOn("service:status", () => loadList());
    return () => off();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  // Poll the selected service's logs while the modal is open.
  useEffect(() => {
    if (!open || !selected) { setLines([]); return; }
    let alive = true;
    const pull = () => GetServiceLogs(selected).then((ls: string[]) => { if (alive) setLines(ls ?? []); });
    pull();
    const id = setInterval(pull, 1200);
    return () => { alive = false; clearInterval(id); };
  }, [open, selected]);

  useEffect(() => {
    if (boxRef.current) boxRef.current.scrollTop = boxRef.current.scrollHeight;
  }, [lines]);

  if (!open) return null;

  return (
    <div className="fixed inset-0 z-[100] bg-black/60 backdrop-blur-sm flex items-center justify-center p-4">
      <div className="glass-strong w-[920px] max-w-full h-[78vh] flex flex-col rounded-2xl overflow-hidden">
        <div className="flex items-center justify-between px-4 py-3 border-b border-white/10">
          <div className="text-sm font-semibold flex items-center gap-2"><ScrollText className="w-4 h-4" /> {t("terminal.serviceLogs.title")}</div>
          <button type="button" onClick={onClose} className="opacity-60 hover:opacity-100" title={t("common.closeTooltip")}><X className="w-4 h-4" /></button>
        </div>

        <div className="flex-1 min-h-0 flex">
          {/* service list */}
          <div className="w-48 shrink-0 border-r border-white/10 overflow-y-auto p-2 flex flex-col gap-1">
            {services.length === 0 && <div className="text-[11px] opacity-50 px-2 py-1">{t("terminal.serviceLogs.noServicesYet")}</div>}
            {services.map((n) => {
              const running = statusMap[n] === "running";
              return (
                <button
                  type="button"
                  key={n}
                  onClick={() => setSelected(n)}
                  className={clsx(
                    "flex items-center gap-2 px-2 py-1.5 rounded-md text-[11px] text-left transition-colors",
                    selected === n ? "bg-accent/25 text-white" : "hover:bg-white/10 text-white/70",
                  )}
                >
                  <span className={clsx("w-1.5 h-1.5 rounded-full shrink-0", running ? "bg-emerald-400" : "bg-zinc-600")} />
                  <span className="truncate">{labelFor(n)}</span>
                </button>
              );
            })}
          </div>

          {/* log output */}
          <div ref={boxRef} className="flex-1 overflow-y-auto p-3 font-mono text-[11px] leading-relaxed bg-black/40">
            {!selected ? (
              <div className="opacity-50">{t("terminal.serviceLogs.selectService")}</div>
            ) : lines.length === 0 ? (
              <div className="opacity-50">{t("terminal.serviceLogs.noOutputYet")}</div>
            ) : (
              lines.map((l, i) => <div key={i} className="whitespace-pre-wrap break-words">{l}</div>)
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
