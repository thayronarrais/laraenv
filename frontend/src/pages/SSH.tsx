import { useEffect, useMemo, useRef, useState } from "react";
import { useDialogs } from "../components/dialogs/DialogProvider";
import { Plus, RefreshCw, Server, Edit3, Trash2, ArrowRight, Search, LayoutGrid, List, Clock3, Folder, User, Globe, KeyRound, MoreVertical, X, Zap } from "lucide-react";
import { ListSSHHosts, DeleteSSHHost } from "../../wailsjs/go/main/App";
import { EventsOn } from "../../wailsjs/runtime";
import SSHHostModal from "../components/SSHHostModal";
import PortalMenu from "../components/PortalMenu";
import { useT, useI18n } from "../lib/i18n";
import "./SSH.css";

export type SSHHost = {
  name: string; address: string; port: number; user: string; tags: string[];
  authMode: "password" | "key" | "both"; password?: string; passwordUnavailable?: boolean; keyName?: string;
  proxyJump?: string; proxyCommand?: string;
};
const cleanTags = (h: SSHHost) => [...new Set((h.tags ?? []).map(t => t.trim()).filter(Boolean))];
const isProduction = (h: SSHHost) => cleanTags(h).some(t => /^(prod|production)$/i.test(t));
const historyKey = "laraenv:ssh:recent";
function readHistory(): Record<string, number> {
  try {
    const value = JSON.parse(localStorage.getItem(historyKey) || "{}");
    return Object.fromEntries(Object.entries(value).filter(([, time]) => typeof time === "number" && Number.isFinite(time))) as Record<string, number>;
  } catch { return {}; }
}
function lastOpened(
  time: number | undefined,
  t: (key: string, vars?: Record<string, string | number>) => string,
  formatRelative: (d: Date | number) => string,
) {
  if (!time) return t("ssh.hostCard.noRecentConnection");
  return t("ssh.hostCard.lastOpened", { when: formatRelative(time) });
}
function HostCard({ host: h, last, connect, edit, remove, filter }: {
  host: SSHHost; last?: number; connect: () => void; edit: () => void;
  remove: () => void; filter: (tag: string) => void;
}) {
  const [menu, setMenu] = useState(false);
  const anchor = useRef<HTMLButtonElement>(null);
  const t = useT();
  const { formatRelative } = useI18n();
  return <article className="ssh-host">
    <div className="ssh-host-top">
      <span className="ssh-server-icon"><Server size={22} /></span>
      <div className="ssh-host-heading">
        <div className="ssh-host-title"><h3 title={h.name}>{h.name}</h3>{cleanTags(h).map(tag => <button key={tag} className={`ssh-tag ${/^(prod|production)$/i.test(tag) ? "is-prod" : ""}`} onClick={() => filter(tag)} title={t("ssh.hostCard.filterByTag", { tag })}>{tag}</button>)}</div>
        <p className="ssh-host-status"><span>{h.authMode === "password" ? t("ssh.hostCard.authPassword") : h.authMode === "both" ? t("ssh.hostCard.authKeyPassword") : t("ssh.hostCard.authSshKey")}</span><span>{lastOpened(last, t, formatRelative)}</span></p>
      </div>
      <button ref={anchor} className="ssh-icon-button ssh-more" aria-label={t("ssh.hostCard.actionsAria", { name: h.name })} aria-expanded={menu} onClick={() => setMenu(!menu)}><MoreVertical size={18} /></button>
      <PortalMenu open={menu} anchorRef={anchor} onClose={() => setMenu(false)}>
        <button className="ssh-menu-action" onClick={() => { setMenu(false); edit(); }}><Edit3 size={14} /> {t("ssh.hostCard.editHost")}</button>
        <button className="ssh-menu-action text-red-400" onClick={() => { setMenu(false); remove(); }}><Trash2 size={14} /> {t("ssh.hostCard.deleteHost")}</button>
      </PortalMenu>
    </div>
    <div className="ssh-host-bottom">
      <div className="ssh-details"><span title={h.user}><User />{h.user}</span><span title={h.address}><Globe />{h.address}</span><span title={t("ssh.hostCard.sshPortTooltip")}><KeyRound />:{h.port || 22}</span></div>
      <button className="ssh-connect" onClick={connect} aria-label={t("ssh.hostCard.connectAria", { name: h.name })}>{t("ssh.hostCard.connect")} <ArrowRight size={15} /></button>
    </div>
    {h.passwordUnavailable && h.authMode !== "key" && <button className="text-xs text-amber-300 mt-2 text-left" onClick={edit}>{t("ssh.hostModal.passwordUnavailable")}</button>}
    {(h.proxyJump || h.proxyCommand) && <p className="ssh-proxy" title={h.proxyCommand || h.proxyJump}>{t("ssh.hostCard.viaLabel", { target: h.proxyCommand ? "ProxyCommand" : h.proxyJump! })}</p>}
  </article>;
}
export default function SSH({ flash, onConnect }: { flash: (m: string) => void; onConnect: (host: string) => void }) {
  const [hosts, setHosts] = useState<SSHHost[]>([]);
  const dialogs = useDialogs();
  const t = useT();
  const { formatNumber } = useI18n();
  const [editing, setEditing] = useState<SSHHost | null>(null);
  const [creating, setCreating] = useState(false);
  const [tagFilter, setTagFilter] = useState("__all__");
  const [query, setQuery] = useState("");
  const [view, setView] = useState<"grid" | "list">("grid");
  const [history, setHistory] = useState(readHistory);
  const [loading, setLoading] = useState(false);
  const [tip, setTip] = useState(true);
  const search = useRef<HTMLInputElement>(null);
  const refresh = async () => {
    setLoading(true);
    try { setHosts((await ListSSHHosts() ?? []) as SSHHost[]); }
    catch (e) { flash(t("ssh.errors.loadHosts", { error: String(e) })); }
    finally { setLoading(false); }
  };
  useEffect(() => {
    void refresh();
    const off = EventsOn("ssh:hosts-changed", () => void refresh());
    const keydown = (e: KeyboardEvent) => {
      if (e.key !== "/" || e.ctrlKey || e.metaKey || e.altKey || !search.current?.getClientRects().length) return;
      const target = e.target as HTMLElement;
      if (target.closest("input, textarea, select, [contenteditable], [role=dialog]")) return;
      e.preventDefault(); search.current.focus();
    };
    document.addEventListener("keydown", keydown);
    return () => { off(); document.removeEventListener("keydown", keydown); };
  }, []);
  const tags = useMemo(() => [...new Set(hosts.flatMap(cleanTags))].sort((a, b) => a.localeCompare(b)), [hosts]);
  const untagged = hosts.filter(h => !cleanTags(h).length).length;
  useEffect(() => {
    if (tagFilter !== "__all__" && (tagFilter === "__untagged__" ? !untagged : !tags.includes(tagFilter))) setTagFilter("__all__");
  }, [tags, untagged, tagFilter]);
  const filtered = hosts.filter(h => (tagFilter === "__all__" || (tagFilter === "__untagged__" ? !cleanTags(h).length : cleanTags(h).includes(tagFilter))) && [h.name, h.user, h.address, String(h.port || 22), ...cleanTags(h)].some(v => v.toLowerCase().includes(query.trim().toLowerCase())));
  const recent = [...filtered].filter(h => history[h.name]).sort((a, b) => history[b.name] - history[a.name]).slice(0, 3);
  const searching = query.trim() !== "" || tagFilter !== "__all__";
  const sections = searching ? [{ title: t("ssh.page.sections.matchingTitle"), subtitle: t("ssh.page.sections.matchingSubtitle", { count: filtered.length }), icon: Search, hosts: filtered }] : [
    { title: t("ssh.page.sections.recentTitle"), subtitle: t("ssh.page.sections.recentSubtitle"), icon: Clock3, hosts: recent },
    { title: t("ssh.page.sections.productionTitle"), subtitle: t("ssh.page.sections.productionSubtitle"), icon: Server, hosts: filtered.filter(h => isProduction(h) && !recent.some(r => r.name === h.name)) },
    { title: t("ssh.page.sections.otherTitle"), subtitle: t("ssh.page.sections.otherSubtitle"), icon: Folder, hosts: filtered.filter(h => !isProduction(h) && !recent.some(r => r.name === h.name)) },
  ];
  const connect = (h: SSHHost) => {
    const next = { ...history, [h.name]: Date.now() };
    setHistory(next);
    try { localStorage.setItem(historyKey, JSON.stringify(next)); } catch { /* History is optional. */ }
    onConnect(h.name);
  };
  const remove = async (h: SSHHost) => {
    if (!(await dialogs.confirm({ title: t("ssh.deleteConfirm.title", { name: h.name }), message: t("ssh.deleteConfirm.message", { name: h.name }), confirmLabel: t("ssh.deleteLabel"), danger: true }))) return;
    try { await DeleteSSHHost(h.name); flash(t("ssh.flash.hostRemoved", { name: h.name })); void refresh(); }
    catch (e) { flash(t("ssh.errors.generic", { message: String(e) })); }
  };
  const chip = (value: string, label: string, count: number) => <button key={value} className={`ssh-filter ${tagFilter === value ? "is-active" : ""}`} aria-pressed={tagFilter === value} onClick={() => setTagFilter(value)}>{label}<span>{formatNumber(count)}</span></button>;
  return <div className="ssh-page">
    <header className="ssh-header"><div className="ssh-page-title"><h1>SSH</h1></div><div className="ssh-header-actions"><button className="ssh-reload" onClick={() => void refresh()} disabled={loading}><RefreshCw size={14} className={loading ? "animate-spin" : ""} /> {t("ssh.page.reloadButton")}</button><button className="ssh-new" onClick={() => setCreating(true)}><Plus size={14} /> {t("ssh.page.newHostButton")}</button></div></header>
    <div className="ssh-toolbar"><div className="ssh-search"><Search size={18} /><input ref={search} aria-label={t("ssh.page.searchAria")} placeholder={t("ssh.page.searchPlaceholder")} value={query} onChange={e => setQuery(e.target.value)} />{query ? <button className="ssh-icon-button" aria-label={t("ssh.page.clearSearchAria")} onClick={() => { setQuery(""); search.current?.focus(); }}><X size={15} /></button> : <kbd>/</kbd>}</div><div className="ssh-filters" aria-label={t("ssh.page.filterByTagAria")}>{chip("__all__", t("ssh.page.filters.all"), hosts.length)}{tags.map(tag => chip(tag, tag, hosts.filter(h => cleanTags(h).includes(tag)).length))}{untagged > 0 && chip("__untagged__", t("ssh.page.filters.untagged"), untagged)}</div><div className="ssh-view" aria-label={t("ssh.page.hostViewAria")}><button className={view === "grid" ? "is-active" : ""} aria-label={t("ssh.page.gridViewAria")} aria-pressed={view === "grid"} onClick={() => setView("grid")}><LayoutGrid size={18} /></button><button className={view === "list" ? "is-active" : ""} aria-label={t("ssh.page.listViewAria")} aria-pressed={view === "list"} onClick={() => setView("list")}><List size={18} /></button></div></div>
    <div className="ssh-content" aria-busy={loading}>
      {filtered.length === 0 && <div className="ssh-empty"><Server size={32} /><h2>{loading ? t("ssh.page.empty.loading") : hosts.length ? t("ssh.page.empty.noHostsFound") : t("ssh.page.empty.welcomeTitle")}</h2><p>{hosts.length ? t("ssh.page.empty.tryAnother") : t("ssh.page.empty.addFirstHost")}</p>{!loading && <button className="ssh-reload" onClick={() => { if (hosts.length) { setQuery(""); setTagFilter("__all__"); } else setCreating(true); }}>{hosts.length ? t("ssh.page.empty.clearFiltersButton") : t("ssh.page.empty.addHostButton")}</button>}</div>}
      {sections.filter(s => s.hosts.length > 0).map(s => <section className="ssh-section" key={s.title}><div className="ssh-section-heading"><s.icon size={21} /><div><h2>{s.title}<span>{formatNumber(s.hosts.length)}</span></h2><p>{s.subtitle}</p></div></div><div className={`ssh-hosts ${view === "list" ? "ssh-list" : ""}`}>{s.hosts.map(h => <HostCard key={h.name} host={h} last={history[h.name]} connect={() => connect(h)} edit={() => setEditing(h)} remove={() => void remove(h)} filter={setTagFilter} />)}</div></section>)}
      {tip && <aside className="ssh-tip"><Zap size={18} /><div><strong>{t("ssh.page.tip.title")}</strong><p>{t("ssh.page.tip.body")}</p></div><button className="ssh-icon-button" aria-label={t("ssh.page.tip.dismissAria")} onClick={() => setTip(false)}><X size={15} /></button></aside>}
    </div>
    <SSHHostModal open={creating || editing !== null} editing={editing} onClose={() => { setCreating(false); setEditing(null); }} onSaved={(_name, isNew) => { void refresh(); if (isNew) { setTagFilter("__all__"); setQuery(""); } }} flash={flash} />
  </div>;
}

