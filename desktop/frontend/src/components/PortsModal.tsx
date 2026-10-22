import { Fragment, useEffect, useMemo, useState } from "react";
import { X, Network, Skull, Search } from "lucide-react";
import clsx from "clsx";
// @ts-ignore
import { ListPorts, KillPort } from "../../wailsjs/go/main/App";
import { useT } from "../lib/i18n";

type PortEntry = {
  owner: string;
  kind: string; // "dev" | "service" | "orphan"
  port: number;
  pid: number;
  process: string;
  listening: boolean;
  orphan: boolean;
};

type Group = { owner: string; kind: string; rows: PortEntry[] };

function kindBadge(kind: string, t: (key: string) => string) {
  switch (kind) {
    case "dev":
      return { label: t("projects.portsModal.badgeDev"), cls: "bg-accent/25 text-accent2" };
    case "service":
      return { label: t("projects.portsModal.badgeService"), cls: "bg-emerald-500/20 text-emerald-300" };
    case "orphan":
      return { label: t("projects.portsModal.badgeOrphan"), cls: "bg-amber-500/20 text-amber-300" };
    default:
      return { label: kind.toUpperCase(), cls: "bg-white/10 text-white/70" };
  }
}

export default function PortsModal({ open, onClose }: { open: boolean; onClose: () => void }) {
  const t = useT();
  const [rows, setRows] = useState<PortEntry[]>([]);
  const [killing, setKilling] = useState<number | null>(null);
  const [query, setQuery] = useState("");

  const load = async () => {
    const list: PortEntry[] = (await ListPorts()) ?? [];
    setRows(list);
  };

  useEffect(() => {
    if (!open) return;
    let alive = true;
    const pull = () => { if (alive) load(); };
    pull();
    const id = setInterval(pull, 1500);
    return () => { alive = false; clearInterval(id); };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  // Filter by free-text query, then group the matching rows by owner (keeping
  // first-seen order, which is port-ascending from the backend).
  const groups = useMemo<Group[]>(() => {
    const q = query.trim().toLowerCase();
    const filtered = !q
      ? rows
      : rows.filter((r) =>
          r.owner.toLowerCase().includes(q) ||
          r.process.toLowerCase().includes(q) ||
          r.kind.toLowerCase().includes(q) ||
          String(r.port).includes(q),
        );
    const byOwner = new Map<string, Group>();
    for (const r of filtered) {
      const g = byOwner.get(r.owner);
      if (g) g.rows.push(r);
      else byOwner.set(r.owner, { owner: r.owner, kind: r.kind, rows: [r] });
    }
    return [...byOwner.values()];
  }, [rows, query]);

  const kill = async (port: number) => {
    setKilling(port);
    try {
      await KillPort(port);
      await load();
    } finally {
      setKilling(null);
    }
  };

  if (!open) return null;

  const total = rows.length;

  return (
    <div className="fixed inset-0 z-[100] bg-black/60 backdrop-blur-sm flex items-center justify-center p-4">
      <div className="glass-strong w-[720px] max-w-full h-[70vh] flex flex-col rounded-2xl overflow-hidden">
        <div className="flex items-center justify-between px-4 py-3 border-b border-white/10">
          <div className="text-sm font-semibold flex items-center gap-2"><Network className="w-4 h-4" /> {t("projects.portsModal.title")}</div>
          <button type="button" onClick={onClose} className="opacity-60 hover:opacity-100" title={t("common.closeTooltip")}><X className="w-4 h-4" /></button>
        </div>

        {/* search */}
        <div className="px-4 py-2 border-b border-white/10">
          <div className="flex items-center gap-2 px-2 py-1.5 rounded-md bg-black/30 border border-white/10 focus-within:border-accent/50">
            <Search className="w-3.5 h-3.5 opacity-40 shrink-0" />
            <input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder={t("projects.portsModal.searchPlaceholder")}
              className="bg-transparent outline-none text-[12px] w-full placeholder:text-white/30"
            />
            {query && (
              <button type="button" onClick={() => setQuery("")} className="opacity-40 hover:opacity-100" title={t("projects.portsModal.clearTooltip")}>
                <X className="w-3.5 h-3.5" />
              </button>
            )}
          </div>
        </div>

        <div className="flex-1 min-h-0 overflow-y-auto">
          {total === 0 ? (
            <div className="opacity-50 text-[12px] p-6 text-center">{t("projects.portsModal.empty")}</div>
          ) : groups.length === 0 ? (
            <div className="opacity-50 text-[12px] p-6 text-center">{t("projects.portsModal.noMatch", { query })}</div>
          ) : (
            <table className="w-full text-[12px]">
              <thead className="sticky top-0 bg-midnight/80 backdrop-blur text-white/50 text-[10px] uppercase tracking-wide">
                <tr>
                  <th className="text-left font-medium px-4 py-2">{t("projects.portsModal.portColumn")}</th>
                  <th className="text-left font-medium px-2 py-2">{t("projects.portsModal.processColumn")}</th>
                  <th className="text-left font-medium px-2 py-2">{t("projects.portsModal.pidColumn")}</th>
                  <th className="px-2 py-2"><span className="sr-only">{t("projects.portsModal.actionsColumn")}</span></th>
                </tr>
              </thead>
              <tbody>
                {groups.map((g) => {
                  const b = kindBadge(g.kind, t);
                  return (
                    <Fragment key={`g-${g.owner}`}>
                      <tr className="bg-white/[0.03]">
                        <td colSpan={4} className="px-4 py-1.5">
                          <div className="flex items-center gap-2">
                            <span className="font-semibold text-white/90 truncate">{g.owner}</span>
                            <span className={clsx("px-1.5 py-0.5 rounded text-[9px] font-semibold", b.cls)}>{b.label}</span>
                            <span className="text-[10px] opacity-40">{t("projects.portsModal.portCount", { count: g.rows.length })}</span>
                          </div>
                        </td>
                      </tr>
                      {g.rows.map((r) => (
                        <tr key={`${r.port}-${r.pid}`} className="border-t border-white/5 hover:bg-white/5">
                          <td className="px-4 py-2 font-mono text-accent2 pl-6">:{r.port}</td>
                          <td className="px-2 py-2 font-mono opacity-70">{r.process || "—"}</td>
                          <td className="px-2 py-2 font-mono opacity-50">{r.pid}</td>
                          <td className="px-2 py-2 text-right">
                            <button
                              type="button"
                              onClick={() => kill(r.port)}
                              disabled={killing === r.port}
                              title={t("projects.portsModal.killTooltip")}
                              className="inline-flex items-center gap-1 px-2 py-1 rounded-md text-[10px] bg-rose-500/15 text-rose-300 hover:bg-rose-500/30 disabled:opacity-50 transition-colors"
                            >
                              <Skull className="w-3 h-3" /> {killing === r.port ? t("projects.portsModal.killing") : t("projects.portsModal.kill")}
                            </button>
                          </td>
                        </tr>
                      ))}
                    </Fragment>
                  );
                })}
              </tbody>
            </table>
          )}
        </div>

        <div className="px-4 py-2 border-t border-white/10 text-[10px] opacity-40">
          {t("projects.portsModal.footer", { killWord: t("projects.portsModal.kill") })}
        </div>
      </div>
    </div>
  );
}
