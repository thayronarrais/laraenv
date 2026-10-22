import { useEffect, useRef, useState } from "react";
import clsx from "clsx";
import { User, LogOut, Loader2, Check, AlertTriangle, Sparkles, Github, Cloud as CloudIcon, RefreshCw, Minus, KeyRound, ShieldCheck, ShieldAlert } from "lucide-react";
// @ts-ignore
import { CloudStatus, CloudSetEnabled, CloudLogin, CloudRegister, CloudLogout, CloudMe, CloudHealth, CloudSetBaseURL, CloudLoginWithGithub, CloudSyncStatus, CloudSetSyncEnabled, CloudSyncNow, CloudCheckout, CloudCheckoutPayPal, CloudPortal, CloudSetSyncPassphrase, CloudRekeySync } from "../../wailsjs/go/main/App";
import GlassCard from "../components/GlassCard";
import { useT } from "../lib/i18n";
import { EventsOn } from "../../wailsjs/runtime";

export type Status = {
  enabled: boolean;
  accountId?: number;
  loggedIn: boolean;
  baseUrl: string;
  email?: string;
  name?: string;
  githubId?: string;
  plan?: string;
  entitlements?: Record<string, boolean>;
};

const PLAN_FEATURES: { nameKey: string; free: boolean; pro: boolean }[] = [
  { nameKey: "account.plan.features.crossDeviceSync",  free: false, pro: true },
  { nameKey: "cloudOptIn.hostedDeployments",            free: false, pro: true },
  { nameKey: "account.plan.features.priorityUpdates",  free: false, pro: true },
];

type SyncStat = {
  enabled: boolean;
  hasKey: boolean;
  reachable: boolean;
  isPro: boolean;
  lastError?: string;
  resourceCount: number;
  encryption?: { cipher: string; kdf: string; keyOnDevice: boolean };
};

type Mode = "login" | "register";

type AccountProps = {
  flash: (m: string) => void;
  planSectionRef?: React.RefObject<HTMLDivElement>;
  onStatusChange?: (s: Status | undefined) => void;
};

export default function Account({ flash, planSectionRef, onStatusChange }: AccountProps) {
  const t = useT();
  const [status, setStatus] = useState<Status>({ enabled: false, loggedIn: false, baseUrl: "" });
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [syncSnapshot, setSyncSnapshot] = useState<SyncStat>();
  const revision = useRef(0);
  const mounted = useRef(false);
  const callbacks = useRef({ flash, onStatusChange });
  callbacks.current = { flash, onStatusChange };

  const publish = (next: Status) => {
    setStatus(next);
    callbacks.current.onStatusChange?.(next);
  };

  useEffect(() => {
    mounted.current = true;
    const request = revision.current;
    CloudStatus().then((next: Status) => {
      if (mounted.current && request === revision.current) publish(next);
    }).catch((error: unknown) => {
      if (mounted.current) callbacks.current.flash(String(error));
    }).finally(() => { if (mounted.current) setLoading(false); });
    return () => { mounted.current = false; revision.current++; };
  }, []);

  useEffect(() => {
    if (status.enabled !== true) return;
    const request = revision.current;
    let active = true;
    let eventReceived = false;
    const accept = (snapshot: SyncStat) => {
      if (active && mounted.current && request === revision.current) setSyncSnapshot(snapshot);
    };
    // Both this snapshot and subsequent events are local cached state.
    CloudSyncStatus().then((snapshot: SyncStat) => { if (!eventReceived) accept(snapshot); }).catch(() => {});
    const off = EventsOn("cloud:sync-status-changed", (snapshot: SyncStat) => {
      eventReceived = true;
      accept(snapshot);
    });
    return () => { active = false; off(); };
  }, [status.enabled]);

  const toggle = async () => {
    const next = status.enabled !== true;
    const request = ++revision.current;
    setSaving(true);
    // Unmount cloud actions immediately so their pending callbacks cannot
    // restore cached account state after the user disables the integration.
    if (!next) publish({ ...status, enabled: false });
    try {
      await CloudSetEnabled(next);
      const snapshot: Status = await CloudStatus();
      if (mounted.current && request === revision.current) publish(snapshot);
    } catch (error) {
      if (mounted.current && request === revision.current) {
        const snapshot: Status = await CloudStatus();
        publish(snapshot);
        callbacks.current.flash(String(error));
      }
    } finally {
      if (mounted.current && request === revision.current) setSaving(false);
    }
  };

  const activeRevision = revision.current;
  const active = () => mounted.current && activeRevision === revision.current;
  return (
    <div className="space-y-4">
      <GlassCard title={t("cloudOptIn.title")} icon={<CloudIcon className="w-4 h-4" />}>
        <p className="text-xs opacity-70 mb-3">{t("cloudOptIn.description")}</p>
        <div className="flex items-center justify-between gap-3">
          <span className="text-xs">{t(status.enabled === true ? "cloudOptIn.enabled" : "cloudOptIn.disabled")}</span>
          <button type="button" onClick={toggle} disabled={loading || saving} className="px-3 py-1.5 rounded-md text-xs border border-white/15 hover:bg-white/10 disabled:opacity-50">
            {t(status.enabled === true ? "cloudOptIn.disable" : "cloudOptIn.enable")}
          </button>
        </div>
        {status.enabled === true && syncSnapshot?.lastError && <div role="alert" className="mt-3 text-xs text-amber-300">{syncSnapshot.lastError}</div>}
      </GlassCard>
      {status.enabled === true && <CloudAccount key={activeRevision} initialStatus={status} planSectionRef={planSectionRef} syncSnapshot={syncSnapshot}
        flash={message => { if (active()) callbacks.current.flash(message); }}
        onStatusChange={next => { if (active() && next?.enabled === true) publish(next); }} />}
    </div>
  );
}

function CloudAccount({
  flash,
  planSectionRef,
  onStatusChange,
  initialStatus,
  syncSnapshot,
}: {
  flash: (m: string) => void;
  planSectionRef?: React.RefObject<HTMLDivElement>;
  onStatusChange?: (s: Status | undefined) => void;
  initialStatus: Status;
  syncSnapshot?: SyncStat;
}) {
  const t = useT();
  const [status, setStatus] = useState<Status>(initialStatus);
  const mounted = useRef(true);
  const [sync, setSync] = useState<SyncStat>({ enabled: false, hasKey: false, reachable: false, isPro: false, resourceCount: 0 });
  useEffect(() => { if (syncSnapshot) setSync(syncSnapshot); }, [syncSnapshot]);
  const [mode, setMode] = useState<Mode>("login");
  const [name, setName] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [serverUrl, setServerUrl] = useState("");
  const [busy, setBusy] = useState(false);
  const [editServer, setEditServer] = useState(false);
  const [showPassphrase, setShowPassphrase] = useState(false);
  const [passphrase, setPassphrase] = useState("");
  const [passphrase2, setPassphrase2] = useState("");
  const [showRekey, setShowRekey] = useState(false);
  const [rekeyPw, setRekeyPw] = useState("");

  const refresh = async () => {
    const s: Status = await CloudStatus();
    if (!mounted.current || s.enabled !== true) return;
    setStatus(s);
    onStatusChange?.(s);
    setServerUrl(s.baseUrl);
    if (s.loggedIn) {
      const ss: SyncStat = await CloudSyncStatus();
      if (mounted.current) setSync(ss);
    }
  };

  const toggleSync = async () => {
    if (!sync.isPro) { flash(t("account.syncRequiresProFlash")); return; }
    if (!sync.hasKey) { setShowPassphrase(true); return; }
    try {
      await CloudSetSyncEnabled(!sync.enabled);
      await refresh();
    } catch (e) { flash(t("account.errors.generic", { message: humanize(e) })); }
  };

  const submitPassphrase = async () => {
    if (passphrase.length < 8) { flash(t("account.passphraseTooShortFlash")); return; }
    if (passphrase !== passphrase2) { flash(t("account.passphraseMismatchFlash")); return; }
    setBusy(true);
    try {
      await CloudSetSyncPassphrase(passphrase);
      await CloudSetSyncEnabled(true);
      flash(t("account.passphraseSetFlash"));
      setShowPassphrase(false);
      setPassphrase("");
      setPassphrase2("");
      await refresh();
    } catch (e) { flash(t("account.errors.generic", { message: humanize(e) })); }
    finally { setBusy(false); }
  };

  const submitRekey = async () => {
    if (rekeyPw.length < 1) { flash(t("account.rekeyPasswordRequiredFlash")); return; }
    setBusy(true);
    try {
      const summary: string = await CloudRekeySync(rekeyPw);
      flash(summary);
      setShowRekey(false);
      setRekeyPw("");
      await refresh();
    } catch (e) { flash(t("account.errors.generic", { message: humanize(e) })); }
    finally { setBusy(false); }
  };

  const upgrade = async (provider: "stripe" | "paypal") => {
    setBusy(true);
    try {
      if (provider === "paypal") await CloudCheckoutPayPal();
      else await CloudCheckout();
      flash(t("account.checkingSubscriptionFlash"));
      await refresh();
    } catch (e) { flash(t("account.errors.generic", { message: humanize(e) })); }
    finally { setBusy(false); }
  };

  const managePlan = async () => {
    setBusy(true);
    try {
      await CloudPortal();
      await refresh();
    } catch (e) { flash(t("account.errors.generic", { message: humanize(e) })); }
    finally { setBusy(false); }
  };

  const syncNow = async () => {
    setBusy(true);
    try {
      const summary: string = await CloudSyncNow();
      flash(summary);
      await refresh();
    } catch (e) { flash(t("account.errors.generic", { message: humanize(e) })); }
    finally { setBusy(false); }
  };

  useEffect(() => {
    mounted.current = true;
    refresh();
    if (initialStatus.loggedIn) CloudMe().then(() => { if (mounted.current) return refresh(); }).catch(() => {});
    return () => { mounted.current = false; };
  }, []);

  const submit = async () => {
    if (!email || !password || (mode === "register" && !name)) {
      flash(t("account.fillAllFieldsFlash"));
      return;
    }
    setBusy(true);
    try {
      if (mode === "register") await CloudRegister(name, email, password);
      else await CloudLogin(email, password);
      flash(mode === "register" ? t("account.accountCreatedFlash") : t("account.loggedInFlash"));
      setPassword("");
      await refresh();
    } catch (e) {
      flash(t("account.errors.generic", { message: humanize(e) }));
    } finally {
      setBusy(false);
    }
  };

  const logout = async () => {
    setBusy(true);
    try {
      await CloudLogout();
      flash(t("account.loggedOutFlash"));
      await refresh();
    } catch (e) { flash(t("account.errors.generic", { message: humanize(e) })); }
    finally { setBusy(false); }
  };

  const githubLogin = async () => {
    setBusy(true);
    try {
      await CloudLoginWithGithub();
      flash(t("account.signedInWithGithubFlash"));
      await refresh();
    } catch (e) { flash(t("account.errors.generic", { message: humanize(e) })); }
    finally { setBusy(false); }
  };

  const checkServer = async () => {
    setBusy(true);
    try {
      await CloudSetBaseURL(serverUrl);
      await CloudHealth();
      flash(t("account.serverReachableFlash"));
      setEditServer(false);
      await refresh();
    } catch (e) { flash(t("account.errors.unreachable", { message: humanize(e) })); }
    finally { setBusy(false); }
  };

  if (status.loggedIn) {
    const isPro = status.plan === "pro";
    return (
      <div className="space-y-4">
        <GlassCard title={t("account.accountCardTitle")} icon={<User className="w-4 h-4" />}>
          <div className="flex items-center justify-between gap-3">
            <div>
              <div className="text-sm font-medium">{status.name}</div>
              <div className="text-[11px] opacity-60">{status.email}</div>
            </div>
            <button
              onClick={logout}
              disabled={busy}
              className="flex items-center gap-1.5 px-3 py-1.5 rounded-md text-xs border border-white/10 hover:bg-white/10"
            >
              {busy ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <LogOut className="w-3.5 h-3.5" />}
              {t("account.logoutButton")}
            </button>
          </div>
        </GlassCard>

        <GlassCard title={t("account.syncCardTitle")} icon={<CloudIcon className="w-4 h-4" />}>
          <div className="flex items-center justify-between gap-3">
            <div className="flex-1">
              <div className="flex items-center gap-2 text-sm">
                <span className={clsx("font-medium", sync.enabled ? "text-emerald-300" : "")}>
                  {sync.enabled ? t("account.sync.enabledLabel") : t("account.sync.disabledLabel")}
                </span>
                {!sync.isPro && <span className="text-[10px] uppercase tracking-wider text-accent2 bg-accent2/10 border border-accent2/30 px-1.5 py-0.5 rounded">{t("account.tierNames.pro")}</span>}
                {!sync.hasKey && sync.isPro && <span className="text-[10px] uppercase tracking-wider text-amber-300 bg-amber-500/10 border border-amber-500/30 px-1.5 py-0.5 rounded">{t("account.sync.noKeyBadge")}</span>}
              </div>
              <div className="text-[11px] opacity-60 mt-0.5">
                {!sync.isPro
                  ? t("account.sync.upsellPro")
                  : !sync.hasKey
                  ? t("account.sync.setPassphrasePrompt")
                  : sync.enabled
                  ? t("account.sync.hostsWatching", { count: sync.resourceCount })
                  : t("account.sync.toggleOnPrompt")}
              </div>
            </div>
            <div className="flex items-center gap-2">
              {sync.isPro && sync.hasKey && (
                <button
                  onClick={() => setShowRekey(true)}
                  disabled={busy}
                  className="p-1.5 rounded-md border border-white/10 hover:bg-white/10"
                  title={t("account.sync.rekeyTooltip")}
                >
                  <KeyRound className="w-3.5 h-3.5" />
                </button>
              )}
              <button
                onClick={syncNow}
                disabled={busy || !sync.enabled || !sync.isPro}
                className={clsx(
                  "p-1.5 rounded-md border",
                  busy || !sync.enabled || !sync.isPro
                    ? "border-white/10 opacity-40 cursor-not-allowed"
                    : "border-white/10 hover:bg-white/10",
                )}
                title={t("account.sync.syncNowTooltip")}
              >
                {busy ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <RefreshCw className="w-3.5 h-3.5" />}
              </button>
              <button
                onClick={toggleSync}
                disabled={!sync.isPro}
                className={clsx(
                  "relative inline-flex h-5 w-9 shrink-0 items-center rounded-full transition-colors",
                  sync.enabled ? "bg-accent" : "bg-white/10",
                  !sync.isPro && "opacity-40 cursor-not-allowed",
                )}
                title={!sync.hasKey ? t("account.sync.setPassphraseTooltip") : ""}
              >
                <span className={clsx("inline-block h-3.5 w-3.5 transform rounded-full bg-white transition-transform", sync.enabled ? "translate-x-5" : "translate-x-1")} />
              </button>
            </div>
          </div>

          {sync.isPro && (
            <div
              data-testid="sync-encryption"
              className={clsx(
                "mt-4 flex items-start gap-3 rounded-lg border px-3 py-2.5",
                sync.hasKey ? "border-emerald-500/25 bg-emerald-500/[0.06]" : "border-amber-500/25 bg-amber-500/[0.06]",
              )}
            >
              {sync.hasKey
                ? <ShieldCheck className="mt-0.5 h-4 w-4 shrink-0 text-emerald-300" />
                : <ShieldAlert className="mt-0.5 h-4 w-4 shrink-0 text-amber-300" />}
              <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-center gap-2 text-sm">
                  <span className={clsx("font-medium", sync.hasKey ? "text-emerald-200" : "text-amber-200")}>
                    {sync.hasKey ? t("account.sync.encryptionEnabledLabel") : t("account.sync.encryptionMissingLabel")}
                  </span>
                  <span className="rounded border border-white/15 px-1.5 py-0.5 font-mono text-[10px] uppercase tracking-wider text-white/60">
                    {sync.encryption?.cipher ?? "AES-256-GCM"}
                  </span>
                  <span className="rounded border border-white/15 px-1.5 py-0.5 font-mono text-[10px] uppercase tracking-wider text-white/60">
                    {t("account.sync.kdfKeyLabel", { kdf: sync.encryption?.kdf ?? "Argon2id" })}
                  </span>
                </div>
                {sync.hasKey ? (
                  <ul className="mt-1.5 space-y-0.5 text-[11px] leading-relaxed opacity-65">
                    <li>{t("account.sync.encryptedDetail1")}</li>
                    <li>{t("account.sync.encryptedDetail2")}</li>
                    <li>{t("account.sync.encryptedDetail3")}</li>
                  </ul>
                ) : (
                  <div className="mt-1.5 text-[11px] leading-relaxed opacity-65">
                    {t("account.sync.noKeyDetail")}
                  </div>
                )}
              </div>
            </div>
          )}

          {showPassphrase && (
            <div className="mt-4 pt-4 border-t border-white/10 space-y-3">
              <div className="text-[11px] opacity-70 leading-relaxed">
                <strong className="text-amber-200">{t("account.sync.passphraseModal.title")}</strong> {t("account.sync.passphraseModal.body")} <strong className="text-red-200">{t("account.sync.passphraseModal.warning")}</strong>
              </div>
              <div className="space-y-2">
                <input
                  type="password"
                  value={passphrase}
                  onChange={(e) => setPassphrase(e.target.value)}
                  placeholder={t("account.sync.passphraseModal.passphrasePlaceholder")}
                  className="input-style"
                  autoFocus
                />
                <input
                  type="password"
                  value={passphrase2}
                  onChange={(e) => setPassphrase2(e.target.value)}
                  onKeyDown={(e) => e.key === "Enter" && submitPassphrase()}
                  placeholder={t("account.sync.passphraseModal.confirmPlaceholder")}
                  className="input-style"
                />
              </div>
              <div className="flex justify-end gap-2">
                <button
                  onClick={() => { setShowPassphrase(false); setPassphrase(""); setPassphrase2(""); }}
                  className="px-3 py-1.5 rounded-md text-xs border border-white/10 hover:bg-white/10"
                >
                  {t("common.cancel")}
                </button>
                <button
                  onClick={submitPassphrase}
                  disabled={busy}
                  className="px-3 py-1.5 rounded-md text-xs border bg-accent/30 border-accent hover:bg-accent/50"
                >
                  {busy ? <Loader2 className="w-3.5 h-3.5 animate-spin inline" /> : t("account.sync.passphraseModal.submitButton")}
                </button>
              </div>
            </div>
          )}

          {showRekey && (
            <div className="mt-4 pt-4 border-t border-white/10 space-y-3">
              <div className="text-[11px] opacity-70 leading-relaxed">
                <strong className="text-amber-200">{t("account.sync.rekeyModal.title")}</strong> {t("account.sync.rekeyModal.intro")} <strong>{t("account.sync.rekeyModal.accountPassword")}</strong>{t("account.sync.rekeyModal.middle")} <strong>{t("account.sync.rekeyModal.canStillSync")}</strong> {t("account.sync.rekeyModal.afterwards")} <em>"{t("account.sync.rekeyModal.differentPassphrase")}"</em>{t("account.sync.rekeyModal.warningSuffix")}
              </div>
              <input
                type="password"
                value={rekeyPw}
                onChange={(e) => setRekeyPw(e.target.value)}
                onKeyDown={(e) => e.key === "Enter" && submitRekey()}
                placeholder={t("account.sync.rekeyModal.passwordPlaceholder")}
                className="input-style"
                autoFocus
              />
              <div className="flex justify-end gap-2">
                <button
                  onClick={() => { setShowRekey(false); setRekeyPw(""); }}
                  className="px-3 py-1.5 rounded-md text-xs border border-white/10 hover:bg-white/10"
                >
                  {t("common.cancel")}
                </button>
                <button
                  onClick={submitRekey}
                  disabled={busy}
                  className="px-3 py-1.5 rounded-md text-xs border bg-accent/30 border-accent hover:bg-accent/50"
                >
                  {busy ? <Loader2 className="w-3.5 h-3.5 animate-spin inline" /> : t("account.sync.rekeyModal.submitButton")}
                </button>
              </div>
            </div>
          )}
        </GlassCard>

        <div ref={planSectionRef}>
        <GlassCard title={t("cloudOptIn.subscription")} icon={isPro ? <Sparkles className="w-4 h-4" /> : <User className="w-4 h-4" />}>
          <p className="text-xs opacity-60 mb-3">{t("cloudOptIn.subscriptionHint")}</p>
          <div className="flex items-center justify-between mb-4">
            <div>
              <div className={clsx("text-sm font-semibold", isPro ? "text-accent2" : "")}>
                {isPro ? t("account.plan.proTitle") : t("account.tierNames.free")}
              </div>
              <div className="text-[11px] opacity-60 mt-0.5">
                {isPro
                  ? t("account.plan.proSubtitle")
                  : t("account.plan.freeSubtitle")}
              </div>
            </div>
            {isPro ? (
              <button
                onClick={managePlan}
                disabled={busy}
                className="px-3 py-1.5 rounded-md text-xs border border-white/10 hover:bg-white/10"
              >
                {busy ? <Loader2 className="w-3.5 h-3.5 animate-spin inline mr-1" /> : null}
                {t("account.plan.manageSubscriptionButton")}
              </button>
            ) : null}
          </div>

          <PlanComparison currentPro={isPro} />

          {!isPro && (
            <div className="flex flex-col sm:flex-row gap-2 mt-4 pt-4 border-t border-white/10">
              <button
                onClick={() => upgrade("stripe")}
                disabled={busy}
                className="flex-1 px-3 py-2 rounded-md text-xs border bg-accent/30 border-accent hover:bg-accent/50 flex items-center justify-center gap-1.5"
                title={t("account.plan.payWithStripeTooltip")}
              >
                {busy ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Sparkles className="w-3.5 h-3.5" />}
                {t("account.plan.payWithStripeButton")}
              </button>
              <button
                onClick={() => upgrade("paypal")}
                disabled={busy}
                className="flex-1 px-3 py-2 rounded-md text-xs border border-[#003087]/60 bg-[#003087]/20 hover:bg-[#003087]/40 flex items-center justify-center gap-1.5"
                title={t("account.plan.payWithPaypalTooltip")}
              >
                {busy ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : null}
                {t("account.plan.payWithPaypalButton")}
              </button>
            </div>
          )}
        </GlassCard>
        </div>

        <FieldStyles />
      </div>
    );
  }

  return (
    <GlassCard title={mode === "register" ? t("account.signIn.createAccountLabel") : t("account.signIn.signInLabel")} icon={<User className="w-4 h-4" />}>
      <div className="flex gap-1 mb-4">
        <button onClick={() => setMode("login")} className={clsx("text-xs px-3 py-1 rounded-md", mode === "login" ? "bg-accent/25 border border-accent/60" : "text-white/60 hover:bg-white/5")}>{t("account.signIn.signInLabel")}</button>
        <button onClick={() => setMode("register")} className={clsx("text-xs px-3 py-1 rounded-md", mode === "register" ? "bg-accent/25 border border-accent/60" : "text-white/60 hover:bg-white/5")}>{t("account.signIn.createAccountLabel")}</button>
      </div>

      <button
        onClick={githubLogin}
        disabled={busy}
        className="w-full px-3 py-2 rounded-md text-sm border border-white/15 bg-white/[0.03] hover:bg-white/[0.08] flex items-center justify-center gap-2 mb-3"
      >
        {busy ? <Loader2 className="w-4 h-4 animate-spin" /> : <Github className="w-4 h-4" />}
        {t("account.signIn.continueWithGithubButton")}
      </button>

      <div className="flex items-center gap-3 my-3">
        <div className="flex-1 h-px bg-white/10" />
        <span className="text-[10px] uppercase opacity-40">{t("account.signIn.orDivider")}</span>
        <div className="flex-1 h-px bg-white/10" />
      </div>

      <div className="space-y-3">
        {mode === "register" && (
          <Field label={t("account.signIn.nameLabel")}>
            <input className="input-style" value={name} onChange={(e) => setName(e.target.value)} placeholder={t("account.signIn.namePlaceholder")} />
          </Field>
        )}
        <Field label={t("account.signIn.emailLabel")}>
          <input className="input-style" type="email" value={email} onChange={(e) => setEmail(e.target.value)} placeholder="you@example.com" />
        </Field>
        <Field label={t("account.signIn.passwordLabel")} hint={mode === "register" ? t("account.signIn.passwordHint") : undefined}>
          <input className="input-style" type="password" value={password} onChange={(e) => setPassword(e.target.value)} onKeyDown={(e) => e.key === "Enter" && submit()} placeholder="••••••••" />
        </Field>

        <button
          onClick={submit}
          disabled={busy}
          className={clsx("w-full px-3 py-2 rounded-md text-sm border transition-colors", busy ? "opacity-60 cursor-wait bg-white/5 border-white/10" : "bg-accent/30 border-accent hover:bg-accent/50")}
        >
          {busy ? <Loader2 className="w-4 h-4 animate-spin inline" /> : mode === "register" ? t("account.signIn.createAccountLabel") : t("account.signIn.signInLabel")}
        </button>

      </div>
      <FieldStyles />
    </GlassCard>
  );
}

function Field({ label, hint, children }: { label: string; hint?: string; children: React.ReactNode }) {
  return (
    <div>
      <div className="text-[11px] uppercase tracking-wider opacity-60 mb-1">{label}</div>
      {children}
      {hint && <div className="text-[10px] opacity-50 mt-1 leading-relaxed">{hint}</div>}
    </div>
  );
}

function FieldStyles() {
  return <style>{`.input-style{width:100%;background:rgba(255,255,255,0.04);border:1px solid rgba(255,255,255,0.1);border-radius:6px;padding:6px 10px;font-size:12px;color:inherit;outline:none}.input-style:focus{border-color:var(--c-accent)}`}</style>;
}

function PlanComparison({ currentPro }: { currentPro: boolean }) {
  const t = useT();
  return (
    <div className="rounded-lg border border-white/10 overflow-hidden">
      <div className="grid grid-cols-[1fr_90px_90px] text-[11px]">
        <div className="px-3 py-2 bg-white/[0.04] uppercase tracking-wider opacity-60 font-medium">
          {t("account.plan.table.featureHeader")}
        </div>
        <div
          className={clsx(
            "px-3 py-2 text-center uppercase tracking-wider font-medium border-l border-white/10",
            !currentPro ? "bg-accent/15 text-white" : "bg-white/[0.04] opacity-60"
          )}
        >
          {t("account.tierNames.free")}
          {!currentPro && (
            <div className="text-[9px] mt-0.5 opacity-80 normal-case tracking-normal">{t("account.plan.table.currentBadge")}</div>
          )}
        </div>
        <div
          className={clsx(
            "px-3 py-2 text-center uppercase tracking-wider font-medium border-l border-white/10",
            currentPro ? "bg-accent2/15 text-accent2" : "bg-white/[0.04] text-accent2/80"
          )}
        >
          {t("account.tierNames.pro")}
          {currentPro && (
            <div className="text-[9px] mt-0.5 opacity-80 normal-case tracking-normal">{t("account.plan.table.currentBadge")}</div>
          )}
        </div>

        {PLAN_FEATURES.map((f, i) => (
          <FeatureRow key={f.nameKey} feature={f} currentPro={currentPro} odd={i % 2 === 1} />
        ))}
      </div>
    </div>
  );
}

function FeatureRow({
  feature,
  currentPro,
  odd,
}: {
  feature: { nameKey: string; free: boolean; pro: boolean };
  currentPro: boolean;
  odd: boolean;
}) {
  const t = useT();
  return (
    <>
      <div className={clsx("px-3 py-2 text-[12px] border-t border-white/5", odd && "bg-white/[0.02]")}>
        {t(feature.nameKey)}
      </div>
      <div
        className={clsx(
          "px-3 py-2 text-center border-t border-l border-white/5",
          !currentPro && "bg-accent/10",
          odd && currentPro && "bg-white/[0.02]"
        )}
      >
        {feature.free ? (
          <Check className="w-4 h-4 text-emerald-400 inline" />
        ) : (
          <Minus className="w-3.5 h-3.5 text-white/25 inline" />
        )}
      </div>
      <div
        className={clsx(
          "px-3 py-2 text-center border-t border-l border-white/5",
          currentPro && "bg-accent2/10",
          odd && !currentPro && "bg-white/[0.02]"
        )}
      >
        {feature.pro ? (
          <Check className="w-4 h-4 text-accent2 inline" />
        ) : (
          <Minus className="w-3.5 h-3.5 text-white/25 inline" />
        )}
      </div>
    </>
  );
}

function humanize(e: any): string {
  if (e?.Message) return e.Message;
  if (typeof e === "string") return e;
  return String(e);
}
