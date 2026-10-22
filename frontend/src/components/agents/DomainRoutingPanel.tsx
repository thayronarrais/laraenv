import { useState } from "react";
import { ExternalLink, Globe2, Loader2, Network, RotateCcw } from "lucide-react";
import ProjectRouteBadge from "./ProjectRouteBadge";
import { useT } from "../../lib/i18n";

export type ProjectRouteView = {
  id: string;
  hostname: string;
  canonical: boolean;
  targetKind: string;
  targetPath: string;
};

export default function DomainRoutingPanel({
  canonicalHost,
  canonicalFromWorktree,
  onCanonicalFromWorktreeChange,
  previewEnabled,
  onPreviewEnabledChange,
  previewHostname,
  onPreviewHostnameChange,
  mainProxyPort,
  proxyPort,
  onProxyPortChange,
  routes,
  mainPath,
  restoring,
  onRestoreCanonical,
}: {
  canonicalHost: string;
  canonicalFromWorktree: boolean;
  onCanonicalFromWorktreeChange: (enabled: boolean) => void;
  previewEnabled: boolean;
  onPreviewEnabledChange: (enabled: boolean) => void;
  previewHostname: string;
  onPreviewHostnameChange: (hostname: string) => void;
  mainProxyPort: number;
  proxyPort: string;
  onProxyPortChange: (port: string) => void;
  routes: ProjectRouteView[];
  mainPath: string;
  restoring: boolean;
  onRestoreCanonical: () => void;
}) {
  const t = useT();
  const routingEnabled = canonicalFromWorktree || previewEnabled;
  const [confirmRestore, setConfirmRestore] = useState(false);
  const canonicalRoute = routes.find((route) => route.canonical);

  return (
    <div className="mt-2.5 rounded-lg border border-accent2/20 bg-accent2/[0.04] p-2.5">
      <div className="mb-2 flex items-start gap-2">
        <Globe2 className="mt-0.5 h-3.5 w-3.5 shrink-0 text-accent2" />
        <div>
          <div className="text-[10px] font-medium">{t("agents.domainRouting.heading")}</div>
          <div className="mt-0.5 text-[9px] leading-4 opacity-50">{t("agents.domainRouting.hint")}</div>
        </div>
      </div>

      {routes.length > 0 && (
        <div className="mb-2.5 rounded-md border border-white/10 bg-black/10 p-2">
          <div className="flex flex-wrap gap-1">
            {routes.map((route) => <ProjectRouteBadge key={route.id} canonical={route.canonical} hostname={route.hostname} />)}
          </div>
          {canonicalRoute && (
            <div className="mt-1.5 truncate font-mono text-[8px] opacity-45" title={canonicalRoute.targetPath}>
              {t("agents.domainRouting.primaryOwner", { owner: canonicalRoute.targetKind === "worktree" ? canonicalRoute.targetPath : t("agents.domainRouting.mainCheckoutLabel") })}
            </div>
          )}
          {canonicalRoute?.targetKind === "worktree" && (
            <div className="mt-2">
              {!confirmRestore ? (
                <button type="button" onClick={() => setConfirmRestore(true)} className="flex items-center gap-1.5 text-[9px] text-accent2 hover:text-white">
                  <RotateCcw className="h-3 w-3" /> {t("agents.domainRouting.restoreButton")}
                </button>
              ) : (
                <div className="rounded border border-amber-400/25 bg-amber-500/[0.08] p-2 text-[9px] leading-4 text-amber-100/75">
                  <div className="font-medium text-amber-100">{t("agents.domainRouting.routingChangeHeading")}</div>
                  <div className="truncate font-mono" title={canonicalRoute.targetPath}>{t("agents.domainRouting.fromLabel", { path: canonicalRoute.targetPath })}</div>
                  <div className="truncate font-mono" title={mainPath}>{t("agents.domainRouting.toLabel", { path: mainPath })}</div>
                  <div className="mt-1.5 flex gap-2">
                    <button type="button" disabled={restoring} onClick={onRestoreCanonical} className="rounded bg-amber-300/20 px-2 py-1 text-amber-50 hover:bg-amber-300/30 disabled:opacity-40">
                      {restoring ? <Loader2 className="h-3 w-3 animate-spin" /> : t("agents.domainRouting.confirmRestoreButton")}
                    </button>
                    <button type="button" disabled={restoring} onClick={() => setConfirmRestore(false)} className="px-1 py-1 opacity-60 hover:opacity-100">{t("common.cancel")}</button>
                  </div>
                </div>
              )}
            </div>
          )}
        </div>
      )}

      <div className="grid gap-1.5">
        <button
          type="button"
          onClick={() => onCanonicalFromWorktreeChange(false)}
          className={`rounded-md border px-2.5 py-2 text-left ${!canonicalFromWorktree ? "border-accent2/50 bg-accent2/10" : "border-white/10 bg-black/10 hover:bg-white/5"}`}
        >
          <span className="block text-[10px] font-medium">{t("agents.domainRouting.leaveUnchangedOption")}</span>
          <span className="mt-0.5 block font-mono text-[9px] opacity-50">{canonicalHost}</span>
        </button>
        <button
          type="button"
          onClick={() => onCanonicalFromWorktreeChange(true)}
          className={`rounded-md border px-2.5 py-2 text-left ${canonicalFromWorktree ? "border-accent2/50 bg-accent2/10" : "border-white/10 bg-black/10 hover:bg-white/5"}`}
        >
          <span className="block text-[10px] font-medium">{t("agents.domainRouting.serveFromWorktreeOption")}</span>
          <span className="mt-0.5 block text-[9px] opacity-50">{t("agents.domainRouting.serveFromWorktreeHint", { host: canonicalHost })}</span>
        </button>
      </div>

      <label className="mt-2.5 flex items-start gap-2 rounded-md border border-white/10 bg-black/10 px-2.5 py-2 text-[10px]">
        <input
          type="checkbox"
          checked={previewEnabled}
          onChange={(event) => onPreviewEnabledChange(event.target.checked)}
          className="mt-0.5"
        />
        <span>
          <span className="flex items-center gap-1.5 font-medium"><ExternalLink className="h-3 w-3" /> {t("agents.domainRouting.previewOption")}</span>
          <span className="mt-0.5 block text-[9px] opacity-50">{t("agents.domainRouting.previewHint")}</span>
        </span>
      </label>

      {previewEnabled && (
        <label className="mt-2 grid gap-1 text-[9px] uppercase tracking-wider opacity-60">
          {t("agents.domainRouting.previewHostnameLabel")}
          <input
            value={previewHostname}
            onChange={(event) => onPreviewHostnameChange(event.target.value.toLowerCase())}
            className="input h-8 font-mono text-[11px] normal-case tracking-normal opacity-100"
            placeholder={`preview.${canonicalHost}`}
          />
        </label>
      )}

      {mainProxyPort > 0 && routingEnabled && (
        <div className="mt-2 rounded-md border border-amber-400/25 bg-amber-500/[0.08] p-2">
          <label className="grid gap-1 text-[9px] uppercase tracking-wider text-amber-100/70">
            <span className="flex items-center gap-1.5"><Network className="h-3 w-3" /> {t("agents.domainRouting.devServerPortLabel")}</span>
            <input
              type="number"
              min={1}
              max={65535}
              value={proxyPort}
              onChange={(event) => onProxyPortChange(event.target.value)}
              className="input h-8 font-mono text-[11px] normal-case tracking-normal opacity-100"
              placeholder={String(mainProxyPort + 1)}
            />
          </label>
          <div className="mt-1 text-[9px] leading-4 text-amber-100/60">{t("agents.domainRouting.portHint", { port: mainProxyPort })}</div>
        </div>
      )}
    </div>
  );
}
