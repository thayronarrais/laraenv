import { useEffect, useState } from "react";
import clsx from "clsx";
import { LayoutDashboard, Download, Globe, TerminalSquare, Network, Settings, Heart, Github, Clock, Server, PanelLeftOpen, PanelLeftClose } from "lucide-react";
import logoUrl from "../../icon.png";
// @ts-ignore
import { OpenURL, AppVersion } from "../../wailsjs/go/main/App";
import type { Status } from "../pages/Account";
import { useT } from "../lib/i18n";

const PAYPAL_URL = "https://www.paypal.com/donate/?business=AQNPEX7PVKRHN&no_recurring=0&currency_code=USD";
const GITHUB_URL = "https://github.com/thayronarrais/laraenv";

export type Page = "dashboard" | "projects" | "terminal" | "ssh" | "cron" | "deployments" | "downloads" | "settings";

export default function Sidebar({
  current,
  onChange,
  updateAvailable,
  status,
  onGoToPlan,
  collapsed = false,
  onToggleCollapsed,
}: {
  current: Page;
  onChange: (p: Page) => void;
  updateAvailable?: boolean;
  status?: Status;
  onGoToPlan?: () => void;
  collapsed?: boolean;
  onToggleCollapsed?: () => void;
}) {
  const t = useT();
  const [version, setVersion] = useState<string>("");
  const [avatarError, setAvatarError] = useState(false);
  useEffect(() => {
    AppVersion().then((v: string) => setVersion(v ?? ""));
  }, []);
  useEffect(() => { setAvatarError(false); }, [status?.githubId]);
  const showAccount = status?.enabled === true && status.loggedIn && status.name;
  const items: { id: Page; label: string; icon: React.ReactNode }[] = [
    { id: "dashboard",   label: t("sidebar.nav.dashboard"),   icon: <LayoutDashboard className="w-4 h-4" /> },
    { id: "projects",    label: t("sidebar.nav.projects"),    icon: <Globe className="w-4 h-4" /> },
    { id: "terminal",    label: t("sidebar.nav.terminal"),    icon: <TerminalSquare className="w-4 h-4" /> },
    { id: "ssh",         label: "SSH",         icon: <Network className="w-4 h-4" /> },
    { id: "cron",        label: t("sidebar.nav.cron"), icon: <Clock className="w-4 h-4" /> },
    { id: "deployments", label: t("sidebar.nav.deployments"), icon: <Server className="w-4 h-4" /> },
    { id: "downloads",   label: t("sidebar.nav.downloads"),   icon: <Download className="w-4 h-4" /> },
    { id: "settings",    label: t("sidebar.nav.settings"),    icon: <Settings className="w-4 h-4" /> },
  ];
  return (
    <aside id="primary-navigation" style={{ width: collapsed ? 56 : 192 }} className="glass-strong primary-navigation flex-shrink-0 flex flex-col gap-1 min-h-0 p-2">
      {onToggleCollapsed && <div className="flex h-8 shrink-0 items-center"><button type="button" aria-label={t(collapsed ? 'sidebar.expand' : 'sidebar.collapse')} title={t(collapsed ? 'sidebar.expand' : 'sidebar.collapse')} aria-expanded={!collapsed} aria-controls="primary-navigation" className="workspace-icon-button navigation-toggle" onClick={onToggleCollapsed}>{collapsed ? <PanelLeftOpen aria-hidden="true" className="h-4 w-4"/> : <PanelLeftClose aria-hidden="true" className="h-4 w-4"/>}</button></div>}
      <div className={clsx('flex h-16 shrink-0 items-center gap-2.5 mb-1', collapsed ? 'justify-center' : 'px-1')}>
        <img
          src={logoUrl}
          alt="LaraEnv"
          className="h-9 w-9 shrink-0 rounded object-contain shadow-[0_0_24px_rgba(31,107,255,0.6)]"
        />
        <div className={collapsed ? 'sr-only' : undefined}>
          <div className="text-sm font-semibold tracking-wide leading-none">
            Lara<span className="brand-env">Env</span>
          </div>
          <div className="text-[9px] opacity-50 mt-0.5">{version ? `v${version}` : ""}</div>
        </div>
      </div>
      {items.map((it) => (
        <button
          key={it.id}
          aria-label={it.label}
          title={collapsed ? it.label : undefined}
          onClick={() => onChange(it.id)}
          className={clsx(
            "relative flex h-9 shrink-0 items-center gap-2 rounded text-sm transition-colors text-left border",
            collapsed ? 'justify-center px-2' : 'px-3',
            current === it.id
              ? "bg-accent/25 border border-accent/60 text-white"
              : "border-transparent hover:bg-white/5 text-white/70"
          )}
        >
          {it.icon}
          <span className={collapsed ? 'sr-only' : undefined}>{it.label}</span>
          {it.id === "settings" && updateAvailable && (
            <span role="status" aria-label={t('sidebar.updateAvailable')} className={clsx('w-1.5 h-1.5 rounded-full bg-accent2 shadow-[0_0_6px_var(--c-accent2)]', collapsed ? 'absolute right-1 top-1' : 'ml-auto')} />
          )}
        </button>
      ))}

      {showAccount && status && (
        <button
          onClick={onGoToPlan}
          title={t("cloudOptIn.title")}
          aria-label={t("cloudOptIn.title")}
          className={clsx('mt-auto mb-2 w-full rounded glass hover:bg-white/[0.08] border border-white/10 transition-colors text-left flex items-center gap-2.5', collapsed ? 'justify-center py-2' : 'p-2')}
        >
          {status.githubId && !avatarError ? (
            <img
              src={`https://avatars.githubusercontent.com/u/${status.githubId}`}
              alt=""
              referrerPolicy="no-referrer"
              onError={() => setAvatarError(true)}
              className="w-9 h-9 rounded-full flex-shrink-0 bg-white/10 object-cover"
            />
          ) : (
            <div className="w-9 h-9 rounded-full bg-accent/40 flex items-center justify-center text-sm font-semibold flex-shrink-0">
              {status.name?.charAt(0).toUpperCase()}
            </div>
          )}
          <div className={collapsed ? 'sr-only' : 'flex-1 min-w-0'}>
            <div className="text-[12px] font-medium truncate">{status.name}</div>
            <div className="flex items-center gap-1 mt-0.5">
              <span className="text-[10px] opacity-60">{t("cloudOptIn.title")}</span>
            </div>
          </div>
        </button>
      )}

      <div className={clsx("pt-3 border-t border-white/5 flex flex-col gap-1.5", !showAccount && "mt-auto")}>
        <button
          onClick={() => OpenURL(PAYPAL_URL)}
          title={t("sidebar.footer.supportTooltip")}
          aria-label={t("sidebar.footer.donate")}
          className="flex items-center justify-center gap-1.5 px-2 py-1.5 rounded text-[11px] font-medium bg-pink-500/10 hover:bg-pink-500/20 border border-pink-500/30 text-pink-300 transition-colors"
        >
          <Heart className="w-3.5 h-3.5" />
          <span className={collapsed ? 'sr-only' : undefined}>{t("sidebar.footer.donate")}</span>
        </button>
        <button
          onClick={() => OpenURL(GITHUB_URL)}
          title={t("sidebar.footer.githubTooltip")}
          aria-label={t("sidebar.footer.github")}
          className="flex items-center justify-center gap-1.5 px-2 py-1.5 rounded text-[11px] hover:bg-white/5 text-white/60 hover:text-white/90 transition-colors"
        >
          <Github className="w-3.5 h-3.5" />
          <span className={collapsed ? 'sr-only' : undefined}>{t("sidebar.footer.github")}</span>
        </button>
        <div className={collapsed ? 'sr-only' : 'text-center text-[9px] text-white/30 leading-tight pt-0.5'}>
          {t("sidebar.footer.madeWithLove")} <Heart className="inline w-2.5 h-2.5 text-pink-400/70 -mt-0.5" fill="currentColor" /> {t("sidebar.footer.hateForAds")}
        </div>
      </div>
    </aside>
  );
}
