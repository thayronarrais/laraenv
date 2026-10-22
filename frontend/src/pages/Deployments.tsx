import { useEffect, useRef, useState } from "react";
import { useDialogs } from "../components/dialogs/DialogProvider";
import clsx from "clsx";
import { Server, Plus, RefreshCw, Trash2, Check, AlertTriangle, Loader2, X, Sparkles, Search, Rocket, Activity as ActivityIcon, GitCommit, FileCode2, Eye, Play, RotateCcw, Pencil } from "lucide-react";
// @ts-ignore
import { CloudListDeployments, CloudCreateDeployment, CloudDeleteDeployment, ListSSHHosts, CloudDeploymentDetect, CloudDeploymentApply, CloudDeploymentDeploy, CloudGetDeployment, CloudDeploymentDestroy, CloudDeploymentActivity, ListDeployPresets, OpenURL } from "../../wailsjs/go/main/App";
// @ts-ignore
import * as AppApi from "../../wailsjs/go/main/App";
// @ts-ignore
import { EventsOn, EventsOff } from "../../wailsjs/runtime";
import Editor from "@monaco-editor/react";
import GlassCard from "../components/GlassCard";
import DeployPresetsModal, { DeployPreset } from "../components/DeployPresetsModal";
import DeployScriptPreviewModal from "../components/DeployScriptPreviewModal";
import type { Status as CloudStatusType } from "./Account";
import { useT, useI18n, type Vars } from "../lib/i18n";

// A deploy-plan step's label. Go sends both: `title` is the plain-English
// text (also what cmd/deploy-tui prints — it has no loaded catalogue) and
// `titleKey`/`titleVars` are the catalogue key + interpolation vars the
// desktop GUI renders instead. `titleKey` can be absent (an older backend,
// or a step type this build doesn't know about yet) — falling back to
// `title` means a missing key degrades to English, never to a blank label.
type DeployStepInfo = { title: string; titleKey?: string; titleVars?: Vars };
type DeployStepItem = DeployStepInfo & { status: "pending" | "running" | "ok" | "error" | "ignored" };

function stepLabel(s: DeployStepInfo, t: (key: string, vars?: Vars) => string): string {
  return s.titleKey ? t(s.titleKey, s.titleVars) : s.title;
}

// Every deployments.steps.* key internal/deploy/apply_plan.go (and app.go's
// inline "Deploy" step) can send as a Step.TitleKey. stepLabel above never
// references these by literal string — the key arrives at runtime from Go —
// so this list exists purely to keep them visible to
// catalogueKeys.test.ts's "every UI key in en.json is actually used" check,
// which scans this file's source text for the literal key strings rather
// than call sites. Keep in sync with apply_plan.go's TitleKey values.
const DEPLOY_STEP_TITLE_KEYS = [
  "deployments.steps.aptUpdate",
  "deployments.steps.addSuryRepo",
  "deployments.steps.installMissingPackages",
  "deployments.steps.verifyPhpInstalled",
  "deployments.steps.createDeployDir",
  "deployments.steps.cloneRepo",
  "deployments.steps.bootstrapMysqlDb",
  "deployments.steps.bootstrapPostgresDb",
  "deployments.steps.dropManagedPostgresDb",
  "deployments.steps.ensureEnvExists",
  "deployments.steps.configureDatabaseAccess",
  "deployments.steps.revokePreviousDatabaseAccess",
  "deployments.steps.runBuildScript",
  "deployments.steps.runDeployPipeline",
  "deployments.steps.setLaravelPermissions",
  "deployments.steps.writePhpFpmPool",
  "deployments.steps.disableDefaultPhpFpmPool",
  "deployments.steps.systemdDropinFpm",
  "deployments.steps.systemdDropinNginx",
  "deployments.steps.reloadSystemdRestartFpm",
  "deployments.steps.enableApacheModules",
  "deployments.steps.writeApacheVhost",
  "deployments.steps.reloadApache",
  "deployments.steps.writeNginxVhost",
  "deployments.steps.reloadNginx",
  "deployments.steps.installCertbot",
  "deployments.steps.obtainCertificate",
  "deployments.steps.installAutopullScript",
  "deployments.steps.scheduleAutopull",
  "deployments.steps.removeAutopullCron",
  "deployments.steps.removeAutopullScript",
  "deployments.steps.disableRemoveApacheVhost",
  "deployments.steps.removeNginxVhost",
  "deployments.steps.removePhpFpmPoolConfig",
  "deployments.steps.reloadPhpFpm",
  "deployments.steps.revokeCertificate",
  "deployments.steps.dropMysqlDb",
  "deployments.steps.wipeDeployDir",
  "deployments.steps.deployProject",
] as const;
void DEPLOY_STEP_TITLE_KEYS; // referenced for the completeness check above, not at runtime

type DeploymentSource = "local" | "cloud";
type DeploymentID = string | number;
type RunSummary = { id: DeploymentID; kind: string; status: string; error_message?: string; started_at: string; has_log: boolean; truncated?: boolean; interrupted?: boolean };
type Deployment = {
  id: DeploymentID;
  source?: DeploymentSource;
  profileKey?: string;
  name: string;
  ssh_host_external_id: string;
  os_family?: string;
  os_version?: string;
  project_kind?: string;
  last_status?: string;
  last_run_at?: string;
  config?: Record<string, any>;
  runs?: RunSummary[];
  secretsUnavailable?: boolean;
  origin?: { source: { namespace?: string; service_url?: string; account_id?: string }; id: string };
};

type SSHHost = { name: string; address: string };

type Props = { flash: (m: string) => void; status?: CloudStatusType & { enabled?: boolean; accountId?: string | number } };

type ImportResult = { inserted: string[]; skipped: string[]; conflicts: { id: string; existing_id: string; reason: string }[] };
type DeploymentAPI = {
  list: () => Promise<Deployment[]>;
  get: (id: DeploymentID) => Promise<Deployment>;
  create: (name: string, host: string) => Promise<unknown>;
  remove: (id: DeploymentID) => Promise<unknown>;
  detect: (id: DeploymentID) => Promise<unknown>;
  apply: (id: DeploymentID, cfg: any) => Promise<unknown>;
  deploy: (id: DeploymentID) => Promise<unknown>;
  destroy: (id: DeploymentID) => Promise<unknown>;
  activity: (id: DeploymentID) => Promise<ActivityEntry[]>;
};
const localBridge = AppApi;
function localID(id: DeploymentID): string {
  if (typeof id !== "string") throw new Error("Expected a local deployment ID");
  return id;
}
function cloudID(id: DeploymentID): number {
  if (typeof id !== "number") throw new Error("Expected a hosted deployment ID");
  return id;
}
function deploymentAPI(source: DeploymentSource): DeploymentAPI {
  return source === "local" ? {
    list: () => localBridge.ListDeployments(), get: id => localBridge.GetDeployment(localID(id)),
    create: (name, host) => localBridge.CreateDeployment(name, host), remove: id => localBridge.DeleteDeployment(localID(id)),
    detect: id => localBridge.DetectDeployment(localID(id)), apply: (id, cfg) => localBridge.ApplyDeployment(localID(id), cfg),
    deploy: id => localBridge.DeployDeployment(localID(id)), destroy: id => localBridge.DestroyDeployment(localID(id)),
    activity: id => localBridge.DeploymentActivity(localID(id)),
  } : {
    list: CloudListDeployments, get: id => CloudGetDeployment(cloudID(id)), create: CloudCreateDeployment,
    remove: id => CloudDeleteDeployment(cloudID(id)), detect: id => CloudDeploymentDetect(cloudID(id)),
    apply: (id, cfg) => CloudDeploymentApply(cloudID(id), cfg), deploy: id => CloudDeploymentDeploy(cloudID(id)),
    destroy: id => CloudDeploymentDestroy(cloudID(id)), activity: id => CloudDeploymentActivity(cloudID(id)),
  };
}

export default function Deployments({ flash, status }: Props) {
  const t = useT();
  const dialogs = useDialogs();
  const cloudEnabled = status?.enabled === true;
  const [selectedSource, setSelectedSource] = useState<DeploymentSource>("local");
  const source = cloudEnabled ? selectedSource : "local";
  const profileIdentity = JSON.stringify([status?.baseUrl ?? "", status?.email ?? "", status?.githubId ?? "", status?.accountId ?? ""]);
  const profileRevision = useRef({ identity: profileIdentity, revision: 0 });
  if (profileRevision.current.identity !== profileIdentity) {
    profileRevision.current = { identity: profileIdentity, revision: profileRevision.current.revision + 1 };
  }
  // A revision prevents an old A request becoming current after A → B → A.
  const profileKey = `${profileRevision.current.revision}:${profileIdentity}`;
  const activeProfileKey = source === "cloud" ? profileKey : "";
  const cloudLoggedIn = source === "cloud" && !!status?.loggedIn;
  const cloudPro = source === "cloud" && status?.entitlements?.deployments === true;
  const flashRef = useRef(flash);
  flashRef.current = flash;
  const context = useRef({ source, profileKey, cloudEnabled, loggedIn: !!status?.loggedIn, pro: status?.entitlements?.deployments === true });
  context.current = { source, profileKey, cloudEnabled, loggedIn: !!status?.loggedIn, pro: status?.entitlements?.deployments === true };
  const request = useRef(0);
  const subscriptions = useRef(new Map<() => void, { source: DeploymentSource; profileKey?: string }>());
  const [deployments, setDeployments] = useState<Deployment[]>([]);
  const [hosts, setHosts] = useState<SSHHost[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState("");
  const [showNew, setShowNew] = useState<{ source: DeploymentSource; profileKey?: string } | null>(null);
  const [detecting, setDetecting] = useState<{ id: DeploymentID; source: DeploymentSource; profileKey?: string; lines: string[]; done: boolean; error?: string } | null>(null);
  const [applying, setApplying] = useState<(ApplyLogState & { source: DeploymentSource; profileKey?: string; kind: "apply" | "destroy" | "deploy" }) | null>(null);
  const [destroyConfirm, setDestroyConfirm] = useState<Deployment | null>(null);
  const [activity, setActivity] = useState<{ deployment: Deployment; entries: ActivityEntry[]; loading: boolean; error?: string } | null>(null);
  const [applyForm, setApplyForm] = useState<{ deployment: Deployment; detection?: any } | null>(null);
  const [showPresets, setShowPresets] = useState(false);
  const [importing, setImporting] = useState<"json" | Deployment | null>(null);
  const [history, setHistory] = useState<Deployment | null>(null);
  const canUseSource = source === "local" || (!!status?.loggedIn && status?.entitlements?.deployments === true);
  const currentProfile = (origin: DeploymentSource, capturedProfile?: string) => origin === "local" || (
    context.current.cloudEnabled && context.current.loggedIn && context.current.pro && capturedProfile === context.current.profileKey
  );
  const currentDeployment = (d: Deployment) => currentProfile(d.source ?? "local", d.profileKey);

  const refresh = async (origin: DeploymentSource = context.current.source, capturedProfile: string | undefined = context.current.profileKey) => {
    if (origin !== context.current.source || (origin === "cloud" && (!context.current.cloudEnabled || capturedProfile !== context.current.profileKey))) return;
    const ticket = ++request.current;
    const current = () => ticket === request.current && origin === context.current.source && (origin === "local" || (context.current.cloudEnabled && capturedProfile === context.current.profileKey));
    setLoading(true);
    setLoadError("");
    try {
      if (origin === "cloud" && (!context.current.cloudEnabled || !context.current.loggedIn || !context.current.pro)) {
        if (current()) setDeployments([]);
        return;
      }
      const list = await deploymentAPI(origin).list();
      if (current()) setDeployments((list ?? []).map(d => ({ ...d, source: origin, profileKey: origin === "cloud" ? capturedProfile : undefined })));
    } catch (e) {
      if (current()) { setDeployments([]); setLoadError(humanize(e)); }
    } finally {
      if (current()) setLoading(false);
    }
  };

  useEffect(() => {
    void refresh(source);
    setShowNew(null);
    setApplyForm(null);
    setDestroyConfirm(null);
    setActivity(null);
    setHistory(null);
    setImporting(null);
  }, [source, cloudLoggedIn, cloudPro, activeProfileKey]);
  useEffect(() => {
    setDeployments(cur => cur.filter(d => d.source !== "cloud" || d.profileKey === profileKey));
    setShowNew(cur => cur?.source === "cloud" && cur.profileKey !== profileKey ? null : cur);
    setApplyForm(cur => cur?.deployment.source === "cloud" && cur.deployment.profileKey !== profileKey ? null : cur);
    setDestroyConfirm(cur => cur?.source === "cloud" && cur.profileKey !== profileKey ? null : cur);
    setActivity(cur => cur?.deployment.source === "cloud" && cur.deployment.profileKey !== profileKey ? null : cur);
    setHistory(cur => cur?.source === "cloud" && cur.profileKey !== profileKey ? null : cur);
    setImporting(cur => cur !== null && cur !== "json" && cur.source === "cloud" && cur.profileKey !== profileKey ? null : cur);
    setDetecting(cur => cur?.source === "cloud" && cur.profileKey !== profileKey ? null : cur);
    setApplying(cur => cur?.source === "cloud" && cur.profileKey !== profileKey ? null : cur);
    subscriptions.current.forEach((scope, cleanup) => { if (scope.source === "cloud" && scope.profileKey !== profileKey) cleanup(); });
  }, [profileKey]);
  useEffect(() => {
    let mounted = true;
    const handlers = subscriptions.current;
    const cancelRequests = () => { request.current++; };
    ListSSHHosts().then((h: SSHHost[]) => { if (mounted) setHosts(h ?? []); }).catch(e => { if (mounted) flashRef.current(humanize(e)); });
    return () => { mounted = false; cancelRequests(); handlers.forEach((_, cleanup) => cleanup()); };
  }, []);
  useEffect(() => { if (!cloudEnabled) setSelectedSource("local"); }, [cloudEnabled]);

  const onDetect = async (d: Deployment) => {
    if (!currentDeployment(d)) return;
    const { id } = d;
    const origin = d.source ?? "local";
    const capturedProfile = d.profileKey;
    setDetecting({ id, source: origin, profileKey: capturedProfile, lines: [], done: false });
    const logEvt = `deployment:detect:log:${id}`;
    const doneEvt = `deployment:detect:done:${id}`;
    const errEvt = `deployment:detect:error:${id}`;
    const cleanup = () => {
      if (!subscriptions.current.has(cleanup)) return;
      subscriptions.current.delete(cleanup);
      EventsOff(logEvt); EventsOff(doneEvt); EventsOff(errEvt);
    };
    subscriptions.current.set(cleanup, { source: origin, profileKey: capturedProfile });
    const update = (transform: (cur: NonNullable<typeof detecting>) => NonNullable<typeof detecting>) => {
      if (!currentProfile(origin, capturedProfile)) return;
      setDetecting(cur => cur && cur.id === id && cur.source === origin && cur.profileKey === capturedProfile ? transform(cur) : cur);
    };
    EventsOn(logEvt, (line: string) => update(cur => ({ ...cur, lines: [...cur.lines, line].slice(-4000) })));
    EventsOn(doneEvt, () => {
      update(cur => ({ ...cur, done: true }));
      cleanup(); void refresh(origin, capturedProfile);
    });
    EventsOn(errEvt, (error: string) => {
      update(cur => ({ ...cur, done: true, error }));
      cleanup();
    });
    try { await deploymentAPI(origin).detect(id); }
    catch (e) {
      update(cur => ({ ...cur, done: true, error: humanize(e) }));
      cleanup();
    }
  };

  const onApplyClick = async (d: Deployment) => {
    if (!currentDeployment(d)) return;
    const origin = d.source ?? "local";
    const ticket = request.current;
    try {
      const full = await deploymentAPI(origin).get(d.id);
      if (ticket !== request.current || context.current.source !== origin || !currentDeployment(d)) return;
      setApplyForm({ deployment: { ...(full ?? d), source: origin, profileKey: d.profileKey }, detection: full?.config?.detection });
    } catch (e) { if (ticket === request.current && context.current.source === origin && currentDeployment(d)) flash(t("deployments.errors.generic", { message: humanize(e) })); }
  };

  const startRun = async (kind: "apply" | "deploy" | "destroy", d: Deployment, cfg?: any) => {
    const { id } = d;
    const origin = d.source ?? "local";
    const capturedProfile = d.profileKey;
    if (!currentDeployment(d)) return;
    setApplyForm(null); setDestroyConfirm(null);
    setApplying({ id, source: origin, profileKey: capturedProfile, kind, name: d.name, host: d.ssh_host_external_id, steps: [], total: 0, currentIdx: 0, lines: [], done: false });
    const prefix = `deployment:${kind}`;
    const logEvt = `${prefix}:log:${id}`, planEvt = `${prefix}:plan:${id}`, stepEvt = `${prefix}:step:${id}`;
    const doneEvt = `${prefix}:done:${id}`, errEvt = `${prefix}:error:${id}`;
    const cleanup = () => {
      if (!subscriptions.current.has(cleanup)) return;
      subscriptions.current.delete(cleanup);
      [logEvt, planEvt, stepEvt, doneEvt, errEvt].forEach(name => EventsOff(name));
    };
    subscriptions.current.set(cleanup, { source: origin, profileKey: capturedProfile });
    const update = (transform: (cur: NonNullable<typeof applying>) => NonNullable<typeof applying>) => {
      if (!currentProfile(origin, capturedProfile)) return;
      setApplying(cur => cur && cur.id === id && cur.source === origin && cur.profileKey === capturedProfile ? transform(cur) : cur);
    };
    EventsOn(planEvt, (steps: DeployStepInfo[]) => update(cur => ({ ...cur, steps: steps.map(step => ({ ...step, status: "pending" })), total: steps.length })));
    EventsOn(stepEvt, (step: { idx: number; total: number; title: string; status: DeployStepItem["status"] }) => update(cur => {
      const steps = [...cur.steps];
      if (steps[step.idx]) steps[step.idx] = { ...steps[step.idx], title: step.title, status: step.status };
      return { ...cur, steps, total: step.total, currentIdx: step.idx };
    }));
    EventsOn(logEvt, (line: string) => update(cur => ({ ...cur, lines: [...cur.lines, line].slice(-4000) })));
    EventsOn(doneEvt, (domain: string) => { update(cur => ({ ...cur, done: true, domain })); cleanup(); void refresh(origin, capturedProfile); });
    EventsOn(errEvt, (error: string) => { update(cur => ({ ...cur, done: true, error })); cleanup(); void refresh(origin, capturedProfile); });
    try {
      const api = deploymentAPI(origin);
      if (kind === "apply") await api.apply(id, cfg);
      else if (kind === "deploy") await api.deploy(id);
      else await api.destroy(id);
    } catch (e) { update(cur => ({ ...cur, done: true, error: humanize(e) })); cleanup(); }
  };

  const onActivity = async (d: Deployment) => {
    if (!currentDeployment(d)) return;
    const origin = d.source ?? "local";
    const ticket = request.current;
    setActivity({ deployment: d, entries: [], loading: true });
    try {
      const entries = await deploymentAPI(origin).activity(d.id);
      if (ticket !== request.current || context.current.source !== origin || !currentDeployment(d)) return;
      setActivity(cur => cur?.deployment.id === d.id ? { deployment: d, entries: entries ?? [], loading: false } : cur);
    } catch (e) { if (ticket === request.current && context.current.source === origin && currentDeployment(d)) setActivity(cur => cur?.deployment.id === d.id ? { ...cur, loading: false, error: humanize(e) } : cur); }
  };
  const onHistory = async (d: Deployment) => {
    const ticket = request.current;
    try {
      const full = await localBridge.GetDeployment(localID(d.id));
      if (ticket === request.current && context.current.source === "local") setHistory({ ...full, source: "local" });
    } catch (e) { if (ticket === request.current && context.current.source === "local") flash(humanize(e)); }
  };
  const onDelete = async (d: Deployment) => {
    if (!currentDeployment(d)) return;
    if (!(await dialogs.confirm({ title: t("deployments.deleteConfirm.title", { name: d.name }), message: t("deployments.deleteConfirm.message"), confirmLabel: t("deployments.deleteLabel"), danger: true }))) return;
    const origin = d.source ?? "local";
    if (!currentDeployment(d)) return;
    try { await deploymentAPI(origin).remove(d.id); if (currentDeployment(d)) { flash(t("deployments.deletedFlash")); void refresh(origin, d.profileKey); } }
    catch (e) { if (currentDeployment(d)) flash(t("deployments.errors.generic", { message: humanize(e) })); }
  };

  return (
    <div className="h-full overflow-y-auto pr-2">
      <div className="flex flex-wrap items-center justify-between gap-3 mb-4">
        <h1 className="text-lg font-semibold flex items-center gap-2"><Server className="w-5 h-5" /> {t("deployments.title")}</h1>
        <div className="flex flex-wrap items-center gap-2">
          <button onClick={() => setSelectedSource("local")} aria-pressed={source === "local"} className={clsx("glass px-3 py-1.5 text-xs", source === "local" && "border-accent")}>{t("offlineDeployments.local")}</button>
          {cloudEnabled && <button onClick={() => setSelectedSource("cloud")} aria-pressed={source === "cloud"} className={clsx("glass px-3 py-1.5 text-xs", source === "cloud" && "border-accent")}>{t("offlineDeployments.cloud")}</button>}
          <button onClick={() => void refresh()} className="glass glass-hover px-3 py-1.5 text-xs flex items-center gap-1.5"><RefreshCw className="w-3.5 h-3.5" /> {t("deployments.reloadButton")}</button>
          {source === "local" && <button onClick={() => setImporting("json")} className="glass glass-hover px-3 py-1.5 text-xs">{t("offlineDeployments.importJson")}</button>}
          <button onClick={() => setShowPresets(true)} className="glass glass-hover px-3 py-1.5 text-xs flex items-center gap-1.5" title={t("deployments.presetsButton.tooltip")}><FileCode2 className="w-3.5 h-3.5" /> {t("deployments.presetsButton.label")}</button>
          {canUseSource && <button onClick={() => setShowNew({ source, profileKey: source === "cloud" ? profileKey : undefined })} disabled={hosts.length === 0} className="px-3 py-1.5 rounded-md text-xs border border-accent bg-accent/30 disabled:opacity-40 flex items-center gap-1.5" title={hosts.length === 0 ? t("deployments.newButton.needHostTooltip") : t("deployments.newButton.tooltip")}><Plus className="w-3.5 h-3.5" /> {t("deployments.newButton.label")}</button>}
        </div>
      </div>
      <p className="text-xs opacity-60 mb-4">{source === "local" ? t("offlineDeployments.localDescription") : t("offlineDeployments.cloudDescription")}</p>
      {loadError && <div role="alert" className="text-xs text-red-300 mb-4 whitespace-pre-wrap">{loadError}</div>}
      {loading ? <div className="text-xs opacity-50 p-4">{t("deployments.loading")}</div> : !canUseSource ? (
        <GlassCard title={t("offlineDeployments.cloud")} icon={<Sparkles className="w-4 h-4" />}>
          <p className="text-sm opacity-70">{status?.loggedIn ? t("deployments.proFeature.description") : t("offlineDeployments.cloudSignIn")}</p>
        </GlassCard>
      ) : deployments.length === 0 ? (
        <GlassCard><div className="text-center text-sm opacity-50 py-8">{hosts.length === 0 ? t("deployments.empty.needHost") : t("deployments.empty.noDeployments")}</div></GlassCard>
      ) : (
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
          {deployments.filter(d => d.source === source && currentDeployment(d)).map(d => <DeploymentCard key={`${source}:${d.profileKey ?? ""}:${d.id}`} d={d}
            onDelete={() => void onDelete(d)} onDetect={() => void onDetect(d)} onApply={() => void onApplyClick(d)}
            onDeploy={() => void startRun("deploy", d)} onDestroy={() => setDestroyConfirm(d)} onActivity={() => void onActivity(d)}
            onImport={source === "cloud" ? () => setImporting(d) : undefined} onHistory={source === "local" ? () => void onHistory(d) : undefined} />)}
        </div>
      )}
      {showNew && currentProfile(showNew.source, showNew.profileKey) && <NewDeploymentModal hosts={hosts} create={deploymentAPI(showNew.source).create} isCurrent={() => currentProfile(showNew.source, showNew.profileKey)} onClose={() => setShowNew(null)} onCreated={() => { setShowNew(null); void refresh(showNew.source, showNew.profileKey); }} flash={flash} />}
      {detecting && currentProfile(detecting.source, detecting.profileKey) && <DetectModal state={detecting} onClose={() => setDetecting(null)} />}
      {applyForm && currentDeployment(applyForm.deployment) && <ApplyConfigModal deployment={applyForm.deployment} detection={applyForm.detection} onClose={() => setApplyForm(null)} onSubmit={cfg => void startRun("apply", applyForm.deployment, cfg)} />}
      {applying && currentProfile(applying.source, applying.profileKey) && <RunLogModal state={applying} onClose={() => setApplying(null)} />}
      {destroyConfirm && currentDeployment(destroyConfirm) && <DestroyConfirmModal deployment={destroyConfirm} onClose={() => setDestroyConfirm(null)} onConfirm={() => void startRun("destroy", destroyConfirm)} />}
      {activity && currentDeployment(activity.deployment) && <ActivityModal deployment={activity.deployment} entries={activity.entries} loading={activity.loading} error={activity.error} onClose={() => setActivity(null)} />}
      {history && <RunHistoryModal deployment={history} onClose={() => setHistory(null)} />}
      {importing && (importing === "json" || currentDeployment(importing)) && <ImportDeploymentModal hosts={hosts} cloudDeployment={importing === "json" ? undefined : importing} isCurrent={() => importing === "json" || currentDeployment(importing)} onClose={() => setImporting(null)} onImported={() => void refresh("local")} />}
      {showPresets && <DeployPresetsModal onClose={() => setShowPresets(false)} flash={flash} />}
    </div>
  );
}
type ActivityEntry = { at: string; before: string; after: string };

function ImportDeploymentModal({ hosts, cloudDeployment, isCurrent, onClose, onImported }: {
  hosts: SSHHost[]; cloudDeployment?: Deployment; isCurrent: () => boolean; onClose: () => void; onImported: () => void;
}) {
  const t = useT();
  const [raw, setRaw] = useState("");
  const [bundle, setBundle] = useState<{ version: number; source: unknown; deployments: Deployment[] } | null>(null);
  const [mappings, setMappings] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [result, setResult] = useState<ImportResult | null>(null);
  const rows = cloudDeployment ? [cloudDeployment] : bundle?.deployments ?? [];
  const sourceHosts = [...new Set(rows.map(d => d.ssh_host_external_id))];
  const ready = rows.length > 0 && sourceHosts.every(host => !!mappings[host]);
  const preview = () => {
    setError(""); setResult(null); setMappings({}); setBundle(null);
    try {
      if (new Blob([raw]).size > 16 * 1024 * 1024) throw new Error(t("offlineDeployments.importTooLarge"));
      const value = JSON.parse(raw);
      if (value?.version !== 1 || !value.source || !Array.isArray(value.deployments) || value.deployments.length === 0 || value.deployments.some((d: Deployment) => !d || typeof d.id !== "string" || typeof d.name !== "string" || typeof d.ssh_host_external_id !== "string" || !d.ssh_host_external_id)) {
        throw new Error(t("offlineDeployments.invalidImport"));
      }
      setBundle(value);
    } catch (e) { setError(humanize(e)); }
  };
  const readFile = async (file?: File) => {
    if (!file) return;
    setError(""); setBundle(null); setResult(null); setMappings({});
    if (file.size > 16 * 1024 * 1024) { setError(t("offlineDeployments.importTooLarge")); return; }
    try { setRaw(await file.text()); } catch (e) { setError(humanize(e)); }
  };
  const submit = async () => {
    if (!ready || !isCurrent()) return;
    setBusy(true); setError("");
    try {
      const imported = cloudDeployment
        ? await localBridge.ImportCloudDeployments([cloudID(cloudDeployment.id)], mappings)
        : await localBridge.ImportDeploymentsJSON(JSON.stringify({ ...bundle, host_mappings: mappings }));
      if (isCurrent()) { setResult(imported); onImported(); }
    } catch (e) { if (isCurrent()) setError(humanize(e)); }
    finally { setBusy(false); }
  };
  return (
    <div className="fixed inset-0 bg-black/60 backdrop-blur-sm flex items-center justify-center z-50 p-4" onClick={busy ? undefined : onClose}>
      <div role="dialog" aria-label={t("offlineDeployments.importTitle")} className="glass-strong w-full max-w-2xl p-5 max-h-[90vh] overflow-y-auto" onClick={e => e.stopPropagation()}>
        <div className="flex justify-between items-center mb-3"><h2 className="text-base font-semibold">{t("offlineDeployments.importTitle")}</h2><button onClick={onClose} disabled={busy} aria-label={t("common.closeTooltip")}><X className="w-4 h-4" /></button></div>
        <p className="text-xs opacity-70 mb-3">{t("offlineDeployments.importHint")}</p>
        {!cloudDeployment && <div className="space-y-2 mb-3">
          <label className="block text-xs">{t("offlineDeployments.importFile")}<input type="file" accept=".json,application/json" disabled={busy} className="block mt-1 text-xs" onChange={e => void readFile(e.target.files?.[0])} /></label>
          <label className="block text-xs">{t("offlineDeployments.importContent")}<textarea className="w-full mt-1 bg-black/20 border border-white/10 rounded p-2 font-mono text-xs" rows={5} value={raw} disabled={busy} onChange={e => { setRaw(e.target.value); setBundle(null); setResult(null); }} /></label>
          <button onClick={preview} disabled={!raw.trim() || busy} className="glass px-3 py-1.5 text-xs disabled:opacity-40">{t("offlineDeployments.previewImport")}</button>
        </div>}
        {rows.length > 0 && <div className="space-y-3 mb-3">
          <ul className="text-xs space-y-1">{rows.map(d => <li key={String(d.id)}><span className="font-medium">{d.name}</span><span className="opacity-60"> · {d.config?.apply?.name || d.name} · {d.ssh_host_external_id}</span></li>)}</ul>
          <p className="text-xs opacity-70">{t("offlineDeployments.hostMappingHint")}</p>
          {sourceHosts.map(host => <label key={host} className="block text-xs">{host}<select className="dpl-input mt-1" disabled={busy} value={mappings[host] ?? ""} onChange={e => setMappings(cur => ({ ...cur, [host]: e.target.value }))}>
            <option value="">{t("offlineDeployments.chooseHost")}</option>{hosts.map(h => <option value={h.name} key={h.name}>{h.name} ({h.address})</option>)}
          </select></label>)}
        </div>}
        {error && <p role="alert" className="text-xs text-red-300 whitespace-pre-wrap mb-3">{error}</p>}
        {result && <div role="status" className="text-xs space-y-2 mb-3">
          <p>{t("offlineDeployments.importSummary", { inserted: result.inserted?.length ?? 0, skipped: result.skipped?.length ?? 0, conflicts: result.conflicts?.length ?? 0 })}</p>
          {result.conflicts?.map((conflict, i) => <p key={`${conflict.id}:${i}`} className="text-amber-300">{conflict.id}: {conflict.reason}</p>)}
        </div>}
        <div className="flex justify-end gap-2 mt-4"><button onClick={onClose} disabled={busy} className="glass px-3 py-1.5 text-xs">{t("common.close")}</button><button onClick={() => void submit()} disabled={!ready || busy} className="glass px-3 py-1.5 text-xs border-accent disabled:opacity-40">{busy ? t("offlineDeployments.importing") : t("offlineDeployments.confirmImport")}</button></div>
      </div>
    </div>
  );
}

function RunHistoryModal({ deployment, onClose }: { deployment: Deployment; onClose: () => void }) {
  const t = useT();
  const { formatDate } = useI18n();
  const [log, setLog] = useState<string | null>(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  const ticket = useRef(0);
  useEffect(() => () => { ticket.current++; }, []);
  const readLog = async (run: RunSummary) => {
    const current = ++ticket.current;
    setLoading(true); setError(""); setLog(null);
    try {
      const text = await localBridge.ReadDeploymentRunLog(localID(deployment.id), localID(run.id));
      if (current === ticket.current) setLog(text);
    } catch (e) { if (current === ticket.current) setError(humanize(e)); }
    finally { if (current === ticket.current) setLoading(false); }
  };
  return <div className="fixed inset-0 bg-black/60 backdrop-blur-sm flex items-center justify-center z-50 p-4" onClick={onClose}>
    <div role="dialog" aria-label={t("offlineDeployments.history")} className="glass-strong w-full max-w-3xl p-5 max-h-[90vh] overflow-y-auto" onClick={e => e.stopPropagation()}>
      <div className="flex justify-between items-center mb-3"><h2>{t("offlineDeployments.history")} · {deployment.name}</h2><button onClick={onClose} aria-label={t("common.closeTooltip")}><X className="w-4 h-4" /></button></div>
      <p className="text-xs opacity-60 mb-3">{t("offlineDeployments.historyLimits")}</p>
      {deployment.secretsUnavailable && <p className="text-xs text-amber-300">{t("offlineDeployments.secretsUnavailable")}</p>}
      {!(deployment.runs?.length) && <p className="text-xs opacity-60">{t("offlineDeployments.noRuns")}</p>}
      <ul className="text-xs space-y-2">{deployment.runs?.map(run => <li key={run.id} className="glass p-2">
        <div className="flex justify-between gap-2"><span>{run.kind} · {run.status} · {formatDate(new Date(run.started_at))}</span>{run.has_log && <button className="underline" disabled={deployment.secretsUnavailable} onClick={() => void readLog(run)}>{t("offlineDeployments.readLog")}</button>}</div>
        {run.truncated && <p className="text-amber-300">{t("offlineDeployments.logTruncated")}</p>}{run.interrupted && <p className="text-amber-300">{t("offlineDeployments.runInterrupted")}</p>}{run.error_message && <p className="text-red-300 whitespace-pre-wrap">{run.error_message}</p>}
      </li>)}</ul>
      {loading && <p className="text-xs mt-3">{t("deployments.loading")}</p>}{error && <p role="alert" className="text-xs text-red-300 mt-3">{error}</p>}{log !== null && <pre className="text-xs font-mono whitespace-pre-wrap break-all glass p-3 mt-3 max-h-[45vh] overflow-auto">{log}</pre>}
    </div>
  </div>;
}

function DeploymentCard({ d, onDelete, onDetect, onApply, onDeploy, onDestroy, onActivity, onImport, onHistory }: {
  d: Deployment;
  onDelete: () => void;
  onDetect?: () => void;
  onApply?: () => void;
  onDeploy?: () => void;
  onDestroy?: () => void;
  onActivity?: () => void;
  onImport?: () => void;
  onHistory?: () => void;
}) {
  const t = useT();
  const detected = d.os_family != null;
  const applied = d.last_status === "ok" || d.last_status === "error";
  const autoDeploy = (d as any)?.config?.apply?.auto_deploy === true;
  const canDeploy = applied && !!(d as any)?.config?.apply?.git_url;
  return (
    <GlassCard title={d.name} icon={<Server className="w-4 h-4" />}>
      <div className="space-y-2 text-xs">
        {d.secretsUnavailable && <p role="alert" className="text-amber-300">{t("offlineDeployments.secretsUnavailable")}</p>}
        {d.origin && <p className="text-[11px] opacity-60">{t("offlineDeployments.importedFrom", { source: d.origin.source.namespace || d.origin.source.service_url || "", id: d.origin.id })}</p>}
        <div className="flex justify-between">
          <span className="opacity-50">{t("deployments.sshHostLabel")}</span>
          <span className="font-mono">{d.ssh_host_external_id}</span>
        </div>
        <div className="flex justify-between">
          <span className="opacity-50">{t("deployments.card.osLabel")}</span>
          <span>{d.os_family ? `${d.os_family} ${d.os_version || ""}` : <span className="opacity-40 italic">{t("deployments.card.osNotDetected")}</span>}</span>
        </div>
        <div className="flex justify-between">
          <span className="opacity-50">{t("deployments.card.projectLabel")}</span>
          <span>{d.project_kind || <span className="opacity-40 italic">{t("deployments.card.projectNotSet")}</span>}</span>
        </div>
        <div className="flex justify-between">
          <span className="opacity-50">{t("deployments.card.lastRunLabel")}</span>
          <span><StatusPill status={d.last_status} at={d.last_run_at} /></span>
        </div>
      </div>
      <div className="flex flex-wrap justify-end gap-2 mt-4 pt-3 border-t border-white/5">
        {onImport && <button onClick={onImport} className="px-2.5 py-1 rounded-md text-[11px] border border-accent bg-accent/20">{t("offlineDeployments.importCloud")}</button>}
        {onHistory && <button onClick={onHistory} className="px-2.5 py-1 rounded-md text-[11px] border border-white/10">{t("offlineDeployments.history")}</button>}
        <button
          onClick={onDetect}
          disabled={d.secretsUnavailable}
          className="px-2.5 py-1 rounded-md text-[11px] border border-white/10 hover:bg-white/10 flex items-center gap-1.5"
          title={t("deployments.card.detectTooltip")}
        >
          <Search className="w-3 h-3" /> {t("deployments.card.detectButton")}
        </button>
        <button
          onClick={onApply}
          disabled={!detected || d.secretsUnavailable}
          className={clsx(
            "px-2.5 py-1 rounded-md text-[11px] border flex items-center gap-1.5",
            !detected
              ? "border-white/10 opacity-40 cursor-not-allowed"
              : "bg-accent/30 border-accent hover:bg-accent/50",
          )}
          title={!detected ? t("deployments.card.applyNeedDetectTooltip") : t("deployments.card.applyTooltip")}
        >
          <Rocket className="w-3 h-3" /> {t("deployments.card.applyButton")}
        </button>
        {canDeploy && (
          <button
            onClick={onDeploy}
            disabled={d.secretsUnavailable}
            className="px-2.5 py-1 rounded-md text-[11px] border border-emerald-500/40 bg-emerald-500/10 hover:bg-emerald-500/20 text-emerald-300 flex items-center gap-1.5"
            title={t("deployments.card.deployTooltip")}
          >
            <Play className="w-3 h-3" /> {t("deployments.card.deployButton")}
          </button>
        )}
        {applied && autoDeploy && (
          <button
            onClick={onActivity}
            disabled={d.secretsUnavailable}
            className="px-2.5 py-1 rounded-md text-[11px] border border-white/10 hover:bg-white/10 flex items-center gap-1.5"
            title={t("deployments.card.activityTooltip")}
          >
            <ActivityIcon className="w-3 h-3" /> {t("deployments.card.activityButton")}
          </button>
        )}
        {applied && (
          <button
            onClick={onDestroy}
            disabled={d.secretsUnavailable}
            className="px-2.5 py-1 rounded-md text-[11px] border border-red-500/30 bg-red-500/10 hover:bg-red-500/20 text-red-300 flex items-center gap-1.5"
            title={t("deployments.card.destroyTooltip")}
          >
            <Trash2 className="w-3 h-3" /> {t("deployments.destroyButton")}
          </button>
        )}
        <button
          onClick={onDelete}
          className="p-1.5 rounded-md border border-white/10 hover:bg-red-500/10 hover:border-red-500/30 hover:text-red-300"
          title={applied ? t("deployments.card.deleteAppliedTooltip") : t("deployments.card.deleteTooltip")}
        >
          <Trash2 className="w-3 h-3" />
        </button>
      </div>
    </GlassCard>
  );
}

function StatusPill({ status, at }: { status?: string; at?: string }) {
  const t = useT();
  const { formatDate } = useI18n();
  if (!status) return <span className="opacity-40 italic">{t("deployments.status.never")}</span>;
  const time = at ? formatDate(new Date(at)) : "";
  if (status === "ok")     return <span className="inline-flex items-center gap-1 text-emerald-300"><Check className="w-3 h-3" />{t("deployments.status.ok")} · {time}</span>;
  if (status === "error")  return <span className="inline-flex items-center gap-1 text-red-300"><AlertTriangle className="w-3 h-3" />{t("deployments.status.error")} · {time}</span>;
  if (status === "running")return <span className="inline-flex items-center gap-1 text-amber-300"><Loader2 className="w-3 h-3 animate-spin" />{t("deployments.status.running")}</span>;
  return <span className="opacity-60">{status} · {time}</span>;
}

function NewDeploymentModal({
  hosts, create, isCurrent, onClose, onCreated, flash,
}: {
  hosts: SSHHost[];
  create: DeploymentAPI["create"];
  isCurrent: () => boolean;
  onClose: () => void;
  onCreated: () => void;
  flash: (m: string) => void;
}) {
  const t = useT();
  const [name, setName] = useState("");
  const [host, setHost] = useState(hosts[0]?.name ?? "");
  const [busy, setBusy] = useState(false);

  const submit = async () => {
    if (!isCurrent()) return;
    if (!name.trim()) { flash(t("deployments.newModal.nameRequired")); return; }
    if (!host) { flash(t("deployments.newModal.pickHostRequired")); return; }
    setBusy(true);
    try {
      await create(name.trim(), host);
      if (!isCurrent()) return;
      flash(t("deployments.newModal.createdFlash"));
      onCreated();
    } catch (e) { if (isCurrent()) flash(t("deployments.errors.generic", { message: humanize(e) })); }
    finally { setBusy(false); }
  };

  return (
    <div className="fixed inset-0 bg-black/60 backdrop-blur-sm flex items-center justify-center z-50 p-4" onClick={onClose}>
      <div className="glass-strong w-full max-w-md p-5" onClick={(e) => e.stopPropagation()}>
        <div className="flex items-center justify-between mb-4">
          <h2 className="text-base font-semibold">{t("deployments.newButton.label")}</h2>
          <button onClick={onClose} className="p-1 rounded hover:bg-white/10"><X className="w-4 h-4" /></button>
        </div>

        <div className="space-y-3">
          <div>
            <div className="text-[11px] uppercase tracking-wider opacity-60 mb-1">{t("deployments.newModal.nameLabel")}</div>
            <input
              autoFocus
              value={name}
              onChange={(e) => setName(e.target.value)}
              className="dpl-input"
              placeholder="prod-web"
            />
          </div>

          <div>
            <div className="text-[11px] uppercase tracking-wider opacity-60 mb-1">{t("deployments.sshHostLabel")}</div>
            <select value={host} onChange={(e) => setHost(e.target.value)} className="dpl-input">
              {hosts.map((h) => (
                <option key={h.name} value={h.name}>{h.name} ({h.address})</option>
              ))}
            </select>
            <div className="text-[10px] opacity-50 mt-1">{t("deployments.newModal.hostHint")}</div>
          </div>
        </div>

        <div className="flex justify-end gap-2 mt-5">
          <button onClick={onClose} className="px-3 py-1.5 rounded-md text-xs border border-white/10 hover:bg-white/10">{t("common.cancel")}</button>
          <button onClick={submit} disabled={busy} className="px-3 py-1.5 rounded-md text-xs border bg-accent/30 border-accent hover:bg-accent/50 flex items-center gap-1.5">
            {busy && <Loader2 className="w-3.5 h-3.5 animate-spin" />}
            {t("deployments.newModal.createButton")}
          </button>
        </div>
      </div>
      <style>{`.dpl-input{width:100%;background:rgba(255,255,255,0.04);border:1px solid rgba(255,255,255,0.1);border-radius:6px;padding:6px 10px;font-size:12px;color:var(--c-text);outline:none;color-scheme:dark}.dpl-input:focus{border-color:var(--c-accent)}.dpl-input option{background:#0b1026;color:var(--c-text)}`}</style>
    </div>
  );
}

function DetectModal({
  state, onClose,
}: {
  state: { id: DeploymentID; lines: string[]; done: boolean; error?: string };
  onClose: () => void;
}) {
  const t = useT();
  return (
    <div className="fixed inset-0 bg-black/60 backdrop-blur-sm flex items-center justify-center z-50 p-4" onClick={state.done ? onClose : undefined}>
      <div className="glass-strong w-full max-w-3xl p-5" onClick={(e) => e.stopPropagation()}>
        <div className="flex items-center justify-between mb-3">
          <div className="flex items-center gap-2">
            {!state.done && <Loader2 className="w-4 h-4 animate-spin" />}
            {state.done && !state.error && <Check className="w-4 h-4 text-emerald-400" />}
            {state.done && state.error && <AlertTriangle className="w-4 h-4 text-red-400" />}
            <h2 className="text-base font-semibold">
              {!state.done ? t("deployments.detectModal.inProgress") : state.error ? t("deployments.detectModal.failed") : t("deployments.detectModal.complete")}
            </h2>
          </div>
          <button onClick={onClose} disabled={!state.done} className={clsx("p-1 rounded", state.done ? "hover:bg-white/10" : "opacity-40 cursor-not-allowed")}>
            <X className="w-4 h-4" />
          </button>
        </div>
        {state.error && (
          <div className="mb-3 text-[12px] text-red-300 bg-red-500/10 border border-red-500/30 rounded p-2">{state.error}</div>
        )}
        <pre className="glass p-3 text-[11px] whitespace-pre-wrap font-mono leading-relaxed max-h-[60vh] overflow-y-auto">
          {state.lines.length === 0 ? t("deployments.waitingForOutput") : state.lines.join("\n")}
        </pre>
      </div>
    </div>
  );
}

export function ApplyConfigModal({
  deployment, detection, onClose, onSubmit,
}: {
  deployment: Deployment;
  detection?: any;
  onClose: () => void;
  onSubmit: (cfg: any) => void;
}) {
  const t = useT();
  const installed = (detection?.installed ?? {}) as Record<string, string>;
  // Try to extract a default PHP version from "PHP 8.3.x ..." string.
  const phpDefault = (() => {
    const m = String(installed.php || "").match(/(\d+\.\d+)/);
    return m?.[1] ?? "8.3";
  })();
  const prevApply = (deployment as any)?.config?.apply ?? {};

  const [name] = useState(prevApply.name ?? deployment.name);
  const dialogs = useDialogs();
  const [projectKind, setProjectKind] = useState<string>(prevApply.project_kind ?? "laravel");
  const [domain, setDomain] = useState<string>(prevApply.domain ?? "");
  const [environment, setEnvironment] = useState<string>(prevApply.environment ?? "production");
  const [gitURL, setGitURL] = useState<string>(prevApply.git_url ?? "");
  const [gitPAT, setGitPAT] = useState<string>(deployment.source === "cloud" ? "" : prevApply.git_pat ?? "");
  const [gitBranch, setGitBranch] = useState<string>(prevApply.git_branch ?? "main");
  const [webServer, setWebServer] = useState<string>(prevApply.web_server ?? "nginx");
  const [phpVersion, setPhpVersion] = useState<string>(prevApply.php_version ?? phpDefault);
  const [database, setDatabase] = useState<string>(prevApply.database ?? "");
  const [databaseRemoteAccess, setDatabaseRemoteAccess] = useState(prevApply.database_remote_access === true);
  const [databaseAllowedIPs, setDatabaseAllowedIPs] = useState<string>((prevApply.database_allowed_ips ?? []).join("\n"));
  const [buildScript, setBuildScript] = useState<string>(
    prevApply.build_script ??
    "composer install --no-dev --optimize-autoloader && php artisan key:generate --force && php artisan migrate --force && php artisan config:cache",
  );
  const [enableSSL, setEnableSSL] = useState<boolean>(prevApply.enable_ssl ?? false);
  const [letsEncryptEmail, setLetsEncryptEmail] = useState<string>(prevApply.lets_encrypt_email ?? "");
  const [autoDeploy, setAutoDeploy] = useState<boolean>(prevApply.auto_deploy ?? false);
  const [autoDeployCron, setAutoDeployCron] = useState<string>(prevApply.auto_deploy_cron ?? "*/5 * * * *");

  // Deploy pipeline: preset selection + per-deployment variables + optional
  // edited copy of the preset body. "__legacy__" = the old free-text
  // build_script mode — preselected for deployments saved before presets.
  const LEGACY = "__legacy__";
  const [presets, setPresets] = useState<DeployPreset[]>([]);
  const [presetId, setPresetId] = useState<string>(() => {
    if (prevApply.preset_id || prevApply.script_override) return prevApply.preset_id || "";
    if (prevApply.build_script) return LEGACY;
    return defaultPresetId(prevApply.project_kind ?? "laravel", prevApply.environment ?? "production");
  });
  const [presetVars, setPresetVars] = useState<Record<string, string>>(prevApply.preset_vars ?? {});
  const [override, setOverride] = useState<string>(prevApply.script_override ?? "");
  const [editingOverride, setEditingOverride] = useState<boolean>(!!prevApply.script_override);
  const [showPreview, setShowPreview] = useState(false);

  useEffect(() => {
    ListDeployPresets().then((p: DeployPreset[]) => setPresets(p ?? [])).catch(() => {});
  }, []);

  const selectedPreset = presets.find((p) => p.id === presetId);
  const isLegacyMode = presetId === LEGACY;

  const effectiveDomain = environment === "production" ? domain : `${environment === "development" ? "dev" : "staging"}.${domain}`;

  const buildCfg = (includePAT: boolean) => {
    // Only keep values for variables the selected preset actually declares.
    const cleanVars: Record<string, string> = {};
    for (const v of selectedPreset?.variables ?? []) {
      const val = (presetVars[v.key] ?? "").trim();
      if (val) cleanVars[v.key] = val;
    }
    return {
      name,
      project_kind: projectKind,
      domain: domain.trim(),
      environment,
      git_url: gitURL.trim(),
      git_pat: includePAT ? gitPAT.trim() : "",
      git_branch: gitBranch.trim() || "main",
      web_server: webServer,
      php_version: phpVersion,
      database,
      database_remote_access: !!database && databaseRemoteAccess,
      database_allowed_ips: database && databaseRemoteAccess
        ? [...new Set(databaseAllowedIPs.split(/[\s,;]+/).map((ip) => ip.trim()).filter(Boolean))]
        : [],
      build_script: isLegacyMode ? buildScript.trim() : "",
      preset_id: isLegacyMode ? "" : presetId,
      preset_vars: isLegacyMode ? {} : cleanVars,
      script_override: isLegacyMode || !editingOverride ? "" : override,
      enable_ssl: enableSSL,
      lets_encrypt_email: letsEncryptEmail.trim(),
      auto_deploy: autoDeploy,
      auto_deploy_cron: autoDeployCron.trim() || "*/5 * * * *",
    };
  };

  const submit = async () => {
    if (!domain.trim()) { await dialogs.alert(t("deployments.applyModal.domainRequired")); return; }
    if (database && databaseRemoteAccess && !buildCfg(false).database_allowed_ips.length) {
      await dialogs.alert(t("deployments.applyModal.databaseAllowedIpsRequired")); return;
    }
    if (enableSSL && !letsEncryptEmail.trim()) { await dialogs.alert(t("deployments.applyModal.letsEncryptEmailRequired")); return; }
    if (autoDeploy && !gitURL.trim()) { await dialogs.alert(t("deployments.applyModal.autoDeployGitUrlRequired")); return; }
    if (!isLegacyMode && !selectedPreset && !(editingOverride && override.trim())) {
      await dialogs.alert(t("deployments.applyModal.pickPresetRequired")); return;
    }
    onSubmit(buildCfg(true));
  };

  return (
    <div className="fixed inset-0 bg-black/60 backdrop-blur-sm flex items-center justify-center z-50 p-4" onClick={onClose}>
      <div className="glass-strong w-full max-w-2xl p-5 max-h-[90vh] overflow-y-auto" onClick={(e) => e.stopPropagation()}>
        <div className="flex items-center justify-between mb-4">
          <h2 className="text-base font-semibold flex items-center gap-2"><Rocket className="w-4 h-4" /> {t("deployments.applyModal.heading", { name: deployment.name })}</h2>
          <button onClick={onClose} className="p-1 rounded hover:bg-white/10"><X className="w-4 h-4" /></button>
        </div>

        <div className="space-y-3 text-xs">
          <Field label={t("deployments.applyModal.projectTypeLabel")}>
            <select className="dpl-input" value={projectKind} onChange={(e) => setProjectKind(e.target.value)}>
              <option value="laravel">{t("deployments.applyModal.projectKind.laravel")}</option>
              <option value="static">{t("deployments.applyModal.projectKind.static")}</option>
              <option value="custom">{t("deployments.applyModal.projectKind.custom")}</option>
            </select>
          </Field>

          <div className="grid grid-cols-2 gap-3">
            <Field label={t("deployments.applyModal.domainLabel")} hint={domain ? t("deployments.applyModal.domainHintEffective", { domain: effectiveDomain }) : t("deployments.applyModal.domainHintExample")}>
              <input className="dpl-input" value={domain} onChange={(e) => setDomain(e.target.value)} placeholder="example.com" />
            </Field>
            <Field label={t("deployments.applyModal.environmentLabel")}>
              <select className="dpl-input" value={environment} onChange={(e) => setEnvironment(e.target.value)}>
                <option value="production">{t("deployments.applyModal.env.production")} → {domain || "domain.com"}</option>
                <option value="development">{t("deployments.applyModal.env.development")} → dev.{domain || "domain.com"}</option>
                <option value="staging">{t("deployments.applyModal.env.staging")} → staging.{domain || "domain.com"}</option>
              </select>
            </Field>
          </div>

          <div className="glass p-3 space-y-3 border border-white/5">
            <div className="text-[11px] uppercase tracking-wider opacity-60">{t("deployments.applyModal.gitSourceHeader")}</div>
            <Field label={t("deployments.applyModal.gitUrlLabel")} hint={t("deployments.applyModal.gitUrlHint")}>
              <input className="dpl-input font-mono" value={gitURL} onChange={(e) => setGitURL(e.target.value)} placeholder="https://github.com/you/repo.git" />
            </Field>
            <Field label={t("deployments.applyModal.gitPatLabel")} hint={deployment.source === "cloud" ? t("deployments.applyModal.gitPatHint") : t("offlineDeployments.gitPatHint")}>
              <input className="dpl-input font-mono" type="password" value={gitPAT} onChange={(e) => setGitPAT(e.target.value)} placeholder="ghp_••••••••••••" autoComplete="off" />
            </Field>
            <Field label={t("deployments.applyModal.branchLabel")}>
              <input className="dpl-input font-mono" value={gitBranch} onChange={(e) => setGitBranch(e.target.value)} placeholder="main" />
            </Field>
          </div>

          <div className="grid grid-cols-3 gap-3">
            <Field label={t("deployments.applyModal.webServerLabel")}>
              <select className="dpl-input" value={webServer} onChange={(e) => setWebServer(e.target.value)}>
                <option value="nginx">nginx{installed.nginx ? " ✓" : ""}</option>
                <option value="apache">Apache{installed.apache ? " ✓" : ""}</option>
              </select>
            </Field>
            <Field label="PHP">
              <select className="dpl-input" value={phpVersion} onChange={(e) => setPhpVersion(e.target.value)}>
                <option value="8.4">8.4</option>
                <option value="8.3">8.3</option>
                <option value="8.2">8.2</option>
                <option value="">{t("deployments.applyModal.phpNoneOption")}</option>
              </select>
            </Field>
            <Field label={t("deployments.applyModal.databaseLabel")}>
              <select className="dpl-input" value={database} onChange={(e) => setDatabase(e.target.value)}>
                <option value="">{t("deployments.applyModal.databaseNoneOption")}</option>
                <option value="mysql">MySQL{installed.mysql ? " ✓" : ""}</option>
                <option value="postgres">PostgreSQL{installed.postgres ? " ✓" : ""}</option>
              </select>
            </Field>
          </div>

          {database && (
            <div className="glass p-3 space-y-2 border border-white/5">
              <label className="flex items-start gap-2 cursor-pointer">
                <input type="checkbox" checked={databaseRemoteAccess} onChange={(e) => setDatabaseRemoteAccess(e.target.checked)} className="mt-0.5" />
                <span>
                  <span className="block font-medium">{t("deployments.applyModal.databaseRemoteAccessLabel")}</span>
                  <span className="block text-[10px] opacity-60">{t("deployments.applyModal.databaseRemoteAccessHint")}</span>
                </span>
              </label>
              {databaseRemoteAccess && (
                <div>
                  <label htmlFor="database-allowed-ips" className="block mb-1">{t("deployments.applyModal.databaseAllowedIpsLabel")}</label>
                  <textarea id="database-allowed-ips" className="dpl-input font-mono" rows={2} value={databaseAllowedIPs}
                    onChange={(e) => setDatabaseAllowedIPs(e.target.value)} placeholder={t("deployments.applyModal.databaseAllowedIpsPlaceholder")} />
                  <div className="text-[10px] opacity-60 mt-1">{t("deployments.applyModal.databaseAllowedIpsHint")}</div>
                </div>
              )}
            </div>
          )}

          <div className="glass p-3 space-y-3 border border-white/5">
            <div className="flex items-center justify-between">
              <div className="text-[11px] uppercase tracking-wider opacity-60">{t("deployments.applyModal.pipelineHeader")}</div>
              <button
                onClick={() => setShowPreview(true)}
                className="px-2.5 py-1 rounded-md text-[11px] border border-white/10 hover:bg-white/10 flex items-center gap-1.5"
                title={t("deployments.applyModal.previewScriptTooltip")}
              >
                <Eye className="w-3 h-3" /> {t("deployments.applyModal.previewScriptButton")}
              </button>
            </div>

            <Field label={t("deployments.applyModal.presetLabel")} hint={t("deployments.applyModal.presetHint")}>
              <select className="dpl-input" value={presetId} onChange={(e) => setPresetId(e.target.value)}>
                {presets.map((p) => (
                  <option key={p.id} value={p.id}>{p.name}{p.builtIn ? ` ${t("deployments.builtInSuffix")}` : ""}</option>
                ))}
                <option value={LEGACY}>{t("deployments.applyModal.legacyOption")}</option>
              </select>
            </Field>

            {isLegacyMode ? (
              <Field label={t("deployments.applyModal.buildScriptLabel")} hint={t("deployments.applyModal.buildScriptHint")}>
                <textarea
                  className="dpl-input font-mono"
                  rows={4}
                  value={buildScript}
                  onChange={(e) => setBuildScript(e.target.value)}
                />
              </Field>
            ) : (
              <>
                {selectedPreset?.description && (
                  <div className="text-[10px] opacity-50">{selectedPreset.description}</div>
                )}

                {(selectedPreset?.variables ?? []).length > 0 && (
                  <div className="grid grid-cols-2 gap-3">
                    {(selectedPreset?.variables ?? []).map((v) => (
                      <Field
                        key={v.key}
                        label={v.label || v.key}
                        hint={v.default
                          ? t("deployments.applyModal.presetVarHintWithDefault", { token: `{${v.key}}`, default: v.default })
                          : t("deployments.applyModal.presetVarHint", { token: `{${v.key}}` })}
                      >
                        <input
                          className="dpl-input font-mono"
                          value={presetVars[v.key] ?? ""}
                          placeholder={v.default ?? ""}
                          onChange={(e) => setPresetVars((cur) => ({ ...cur, [v.key]: e.target.value }))}
                        />
                      </Field>
                    ))}
                  </div>
                )}

                {editingOverride ? (
                  <div className="space-y-1.5">
                    <div className="flex items-center justify-between">
                      <div className="text-[11px] uppercase tracking-wider opacity-60">{t("deployments.applyModal.customScriptHeader")}</div>
                      <button
                        onClick={() => { setEditingOverride(false); setOverride(""); }}
                        className="px-2 py-0.5 rounded text-[10px] border border-white/10 hover:bg-white/10 flex items-center gap-1"
                        title={t("deployments.applyModal.resetToPresetTooltip")}
                      >
                        <RotateCcw className="w-2.5 h-2.5" /> {t("deployments.applyModal.resetToPresetButton")}
                      </button>
                    </div>
                    <div className="rounded-lg overflow-hidden border border-white/10" style={{ height: 240 }}>
                      <Editor
                        height="100%"
                        theme="vs-dark"
                        language="shell"
                        value={override}
                        onChange={(v) => setOverride(v ?? "")}
                        options={{ fontSize: 12, minimap: { enabled: false }, automaticLayout: true, tabSize: 2, wordWrap: "on", scrollBeyondLastLine: false }}
                      />
                    </div>
                    <div className="text-[10px] opacity-50">{t("deployments.applyModal.overrideHint", { token: "{variables}" })}</div>
                  </div>
                ) : (
                  <button
                    onClick={() => { setOverride(selectedPreset?.script ?? ""); setEditingOverride(true); }}
                    disabled={!selectedPreset}
                    className="px-2.5 py-1 rounded-md text-[11px] border border-white/10 hover:bg-white/10 flex items-center gap-1.5 disabled:opacity-40"
                    title={t("deployments.applyModal.customizeScriptTooltip")}
                  >
                    <Pencil className="w-3 h-3" /> {t("deployments.applyModal.customizeScriptButton")}
                  </button>
                )}
              </>
            )}
          </div>

          <div className="glass p-3 space-y-3 border border-white/5">
            <div className="text-[11px] uppercase tracking-wider opacity-60">{t("deployments.applyModal.httpsHeader")}</div>

            <label className="flex items-start gap-2 cursor-pointer">
              <input type="checkbox" checked={enableSSL} onChange={(e) => setEnableSSL(e.target.checked)} className="mt-0.5" />
              <div className="flex-1">
                <div className="font-medium">{t("deployments.applyModal.enableSslLabel")}</div>
                <div className="text-[10px] opacity-60">{t("deployments.applyModal.enableSslHint", { domain: effectiveDomain || t("deployments.applyModal.domainFallback") })}</div>
              </div>
            </label>
            {enableSSL && (
              <Field label={t("deployments.applyModal.letsEncryptEmailLabel")}>
                <input className="dpl-input" type="email" value={letsEncryptEmail} onChange={(e) => setLetsEncryptEmail(e.target.value)} placeholder="you@example.com" />
              </Field>
            )}

            <label className="flex items-start gap-2 cursor-pointer">
              <input type="checkbox" checked={autoDeploy} onChange={(e) => setAutoDeploy(e.target.checked)} className="mt-0.5" />
              <div className="flex-1">
                <div className="font-medium">{t("deployments.applyModal.autoDeployLabel")}</div>
                <div className="text-[10px] opacity-60">{t("deployments.applyModal.autoDeployHintBefore")} <span className="font-mono">{`/tmp/laraenv-${name}.log`}</span>{t("deployments.applyModal.autoDeployHintAfter")}</div>
              </div>
            </label>
            {autoDeploy && (
              <Field label={t("deployments.applyModal.cronLabel")} hint={t("deployments.applyModal.cronHint")}>
                <input className="dpl-input font-mono" value={autoDeployCron} onChange={(e) => setAutoDeployCron(e.target.value)} placeholder="*/5 * * * *" />
              </Field>
            )}
          </div>

          <div className="text-[10px] opacity-60 leading-relaxed border-t border-white/10 pt-3">
            {t("deployments.applyModal.footerA")} <span className="font-mono">{deployment.ssh_host_external_id}</span>{t("deployments.applyModal.footerB")} <span className="text-amber-300">sudo -n</span> {t("deployments.applyModal.footerC")} <span className="font-mono">{`/var/www/${deployment.name}`}</span>{t("deployments.applyModal.footerD")}
          </div>
        </div>

        <div className="flex justify-end gap-2 mt-5">
          <button onClick={onClose} className="px-3 py-1.5 rounded-md text-xs border border-white/10 hover:bg-white/10">{t("common.cancel")}</button>
          <button onClick={submit} className="px-3 py-1.5 rounded-md text-xs border bg-accent/30 border-accent hover:bg-accent/50 flex items-center gap-1.5">
            <Rocket className="w-3.5 h-3.5" /> {t("deployments.applyModal.runApplyButton")}
          </button>
        </div>
        <style>{`.dpl-input{width:100%;background:rgba(255,255,255,0.04);border:1px solid rgba(255,255,255,0.1);border-radius:6px;padding:6px 10px;font-size:12px;color:var(--c-text);outline:none;color-scheme:dark}.dpl-input:focus{border-color:var(--c-accent)}.dpl-input option{background:#0b1026;color:var(--c-text)}`}</style>
      </div>

      {showPreview && (
        <DeployScriptPreviewModal cfg={buildCfg(false)} onClose={() => setShowPreview(false)} />
      )}
    </div>
  );
}

// defaultPresetId picks the built-in preset that matches a fresh config's
// project kind + environment. "custom" projects start on the free-text mode.
function defaultPresetId(kind: string, environment: string): string {
  if (kind === "laravel") {
    return environment === "production" ? "builtin:laravel-production" : "builtin:laravel-development";
  }
  if (kind === "static") return "builtin:static-node";
  return "__legacy__";
}

type ApplyLogState = {
  id: DeploymentID;
  kind?: "apply" | "destroy" | "deploy";
  name: string;
  host: string;
  steps: DeployStepItem[];
  total: number;
  currentIdx: number;
  lines: string[];
  done: boolean;
  error?: string;
  domain?: string;
};

function RunLogModal({
  state, onClose,
}: {
  state: ApplyLogState & { kind: "apply" | "destroy" | "deploy" };
  onClose: () => void;
}) {
  const t = useT();
  const isDestroy = state.kind === "destroy";
  const completed = state.steps.filter((s) => s.status === "ok" || s.status === "error" || s.status === "ignored").length;
  const total = Math.max(state.total, state.steps.length, 1);
  const pct = Math.round((completed / total) * 100);
  const currentTitle = state.done
    ? (state.error ? t("deployments.runLog.failedSeeLog") : t("deployments.runLog.done"))
    : (state.steps[state.currentIdx] ? stepLabel(state.steps[state.currentIdx], t) : t("deployments.runLog.preparing"));
  const title = !state.done
    ? t(
        state.kind === "destroy" ? "deployments.runLog.destroying" : state.kind === "deploy" ? "deployments.runLog.deploying" : "deployments.runLog.applying",
        { name: state.name, host: state.host },
      )
    : state.error
    ? t(
        state.kind === "destroy" ? "deployments.runLog.destroyFailed" : state.kind === "deploy" ? "deployments.runLog.deployFailed" : "deployments.runLog.applyFailed",
        { name: state.name },
      )
    : isDestroy
    ? t("deployments.runLog.destroyed", { name: state.name })
    : state.domain
    ? t("deployments.runLog.deployedTo", { domain: state.domain })
    : t("deployments.runLog.deployed");

  const logRef = useRef<HTMLPreElement>(null);
  // Auto-scroll only when the user is already near the bottom, so they can
  // scroll up to read an error without being yanked back by trailing lines.
  useEffect(() => {
    const el = logRef.current;
    if (!el) return;
    const nearBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 40;
    if (nearBottom) el.scrollTop = el.scrollHeight;
  }, [state.lines.length]);

  return (
    <div className="fixed inset-0 bg-black/60 backdrop-blur-sm flex items-center justify-center z-50 p-4" onClick={state.done ? onClose : undefined}>
      <div className="glass-strong w-full max-w-5xl max-h-[92vh] flex flex-col p-5" onClick={(e) => e.stopPropagation()}>
        <div className="flex items-center justify-between mb-3 flex-shrink-0">
          <div className="flex items-center gap-2">
            {!state.done && <Loader2 className="w-4 h-4 animate-spin text-amber-300" />}
            {state.done && !state.error && <Check className="w-4 h-4 text-emerald-400" />}
            {state.done && state.error && <AlertTriangle className="w-4 h-4 text-red-400" />}
            <h2 className="text-base font-semibold">
              {title}
            </h2>
          </div>
          <button onClick={onClose} disabled={!state.done} className={clsx("p-1 rounded", state.done ? "hover:bg-white/10" : "opacity-40 cursor-not-allowed")} title={state.done ? t("common.closeTooltip") : t("deployments.runLog.busyTooltip")}>
            <X className="w-4 h-4" />
          </button>
        </div>

        {/* Progress bar */}
        <div className="flex-shrink-0 mb-3">
          <div className="h-2 rounded-full overflow-hidden bg-white/[0.04] border border-white/10">
            <div
              className={clsx(
                "h-full transition-all duration-300",
                state.error
                  ? "bg-red-500"
                  : "bg-gradient-to-r from-accent to-accent2",
              )}
              style={{ width: `${pct}%` }}
            />
          </div>
          <div className="flex items-center justify-between text-[11px] mt-1.5">
            <span className="opacity-70">{currentTitle}</span>
            <span className="font-mono opacity-60">{pct}% &nbsp; ({completed}/{total})</span>
          </div>
        </div>

        {state.error && (
          <div className="flex-shrink-0 mb-3 text-[12px] text-red-300 bg-red-500/10 border border-red-500/30 rounded p-2 whitespace-pre-wrap break-all max-h-32 overflow-y-auto">{state.error}</div>
        )}

        {/* Step list + log, side by side. Step list scrolls on its own; log
            scrolls on its own with auto-bottom-pin. */}
        <div className="flex-1 min-h-0 grid grid-cols-1 lg:grid-cols-[280px_1fr] gap-3">
          <div className="glass rounded-md p-2 overflow-y-auto min-h-0 min-w-0">
            {state.steps.length === 0 ? (
              <div className="text-[11px] opacity-50 italic p-2">{t("deployments.runLog.buildingPlan")}</div>
            ) : (
              <ul className="space-y-1">
                {state.steps.map((s, i) => (
                  <StepRow key={i} title={stepLabel(s, t)} status={s.status} active={!state.done && i === state.currentIdx} />
                ))}
              </ul>
            )}
          </div>
          <pre
            ref={logRef}
            className="glass p-3 text-[11px] whitespace-pre-wrap break-all font-mono leading-relaxed overflow-y-auto min-h-0 min-w-0"
          >
            {state.lines.length === 0 ? t("deployments.waitingForOutput") : state.lines.join("\n")}
          </pre>
        </div>
      </div>
    </div>
  );
}

function StepRow({ title, status, active }: {
  title: string;
  status: "pending" | "running" | "ok" | "error" | "ignored";
  active?: boolean;
}) {
  let icon: React.ReactNode;
  let cls = "text-[11px] flex items-start gap-1.5 px-1.5 py-1 rounded ";
  switch (status) {
    case "ok":
      icon = <Check className="w-3 h-3 text-emerald-400 mt-0.5" />;
      cls += "text-emerald-200/80";
      break;
    case "error":
      icon = <AlertTriangle className="w-3 h-3 text-red-400 mt-0.5" />;
      cls += "text-red-200";
      break;
    case "running":
      icon = <Loader2 className="w-3 h-3 text-amber-300 mt-0.5 animate-spin" />;
      cls += "text-amber-200";
      break;
    case "ignored":
      icon = <Check className="w-3 h-3 text-white/30 mt-0.5" />;
      cls += "text-white/40 line-through";
      break;
    default:
      icon = <span className="w-3 h-3 mt-0.5 rounded-full border border-white/20 inline-block" />;
      cls += "text-white/40";
  }
  if (active) cls += " bg-amber-500/10 border border-amber-500/30";
  return (
    <li className={cls}>
      {icon}
      <span className="leading-snug break-words">{title}</span>
    </li>
  );
}

function Field({ label, hint, children }: { label: string; hint?: string; children: React.ReactNode }) {
  return (
    <div>
      <div className="text-[11px] uppercase tracking-wider opacity-60 mb-1">{label}</div>
      {children}
      {hint && <div className="text-[10px] opacity-50 mt-1">{hint}</div>}
    </div>
  );
}

function DestroyConfirmModal({
  deployment, onClose, onConfirm,
}: {
  deployment: Deployment;
  onClose: () => void;
  onConfirm: () => void;
}) {
  const t = useT();
  const [typed, setTyped] = useState("");
  const matches = typed.trim() === deployment.name;
  const cfg = (deployment as any)?.config?.apply ?? {};
  const sslDomainDisplay = cfg.domain
    ? `${cfg.environment === "development" ? "dev." : cfg.environment === "staging" ? "staging." : ""}${cfg.domain}`
    : t("deployments.destroyModal.domainFallback");
  const remoteName = cfg.name || deployment.name;
  const willRemove = [
    `/var/www/${remoteName}`,
    t("deployments.destroyModal.itemVhost", { name: remoteName }),
    cfg.database === "mysql" ? t("deployments.destroyModal.itemMysql", { name: remoteName.replace(/-/g, "_") }) : null,
    t("deployments.destroyModal.itemManagedPostgres"),
    cfg.auto_deploy ? t("deployments.destroyModal.itemCron", { name: remoteName }) : null,
    cfg.enable_ssl ? t("deployments.destroyModal.itemSsl", { domain: sslDomainDisplay }) : null,
  ].filter(Boolean) as string[];

  return (
    <div className="fixed inset-0 bg-black/60 backdrop-blur-sm flex items-center justify-center z-50 p-4" onClick={onClose}>
      <div className="glass-strong w-full max-w-md p-5" onClick={(e) => e.stopPropagation()}>
        <div className="flex items-center justify-between mb-4">
          <h2 className="text-base font-semibold flex items-center gap-2 text-red-300">
            <AlertTriangle className="w-4 h-4" /> {t("deployments.destroyModal.title", { name: deployment.name })}
          </h2>
          <button onClick={onClose} className="p-1 rounded hover:bg-white/10"><X className="w-4 h-4" /></button>
        </div>

        <p className="text-sm text-white/80 leading-relaxed mb-3">
          {t("deployments.destroyModal.introBefore")} <span className="font-mono text-white">{deployment.ssh_host_external_id}</span> {t("deployments.destroyModal.introAfter")}
        </p>
        <ul className="text-[11px] space-y-1 mb-4 font-mono text-white/70">
          {willRemove.map((s, i) => <li key={i}>• {s}</li>)}
        </ul>
        <p className="text-[11px] text-amber-300 leading-relaxed mb-3">
          {deployment.source === "cloud" ? t("deployments.destroyModal.recordDeletedNote") : t("offlineDeployments.destroyRecordNote")}
        </p>

        <div className="space-y-2">
          <div className="text-[11px] uppercase tracking-wider opacity-60">
            {t("deployments.destroyModal.typeToConfirmBefore")} <span className="font-mono text-white">{deployment.name}</span> {t("deployments.destroyModal.typeToConfirmAfter")}
          </div>
          <input
            autoFocus
            value={typed}
            onChange={(e) => setTyped(e.target.value)}
            className="dpl-input"
            placeholder={deployment.name}
            onKeyDown={(e) => { if (e.key === "Enter" && matches) onConfirm(); }}
          />
        </div>

        <div className="flex justify-end gap-2 mt-5">
          <button onClick={onClose} className="px-3 py-1.5 rounded-md text-xs border border-white/10 hover:bg-white/10">{t("common.cancel")}</button>
          <button
            onClick={onConfirm}
            disabled={!matches}
            className={clsx(
              "px-3 py-1.5 rounded-md text-xs border flex items-center gap-1.5",
              matches
                ? "bg-red-500/30 border-red-500 hover:bg-red-500/50 text-white"
                : "border-white/10 opacity-40 cursor-not-allowed",
            )}
          >
            <Trash2 className="w-3.5 h-3.5" /> {t("deployments.destroyButton")}
          </button>
        </div>
        <style>{`.dpl-input{width:100%;background:rgba(255,255,255,0.04);border:1px solid rgba(255,255,255,0.1);border-radius:6px;padding:6px 10px;font-size:12px;color:var(--c-text);outline:none}.dpl-input:focus{border-color:var(--c-accent)}`}</style>
      </div>
    </div>
  );
}

function ActivityModal({
  deployment, entries, loading, error, onClose,
}: {
  deployment: Deployment;
  entries: ActivityEntry[];
  loading: boolean;
  error?: string;
  onClose: () => void;
}) {
  const t = useT();
  const { formatDate, formatRelative } = useI18n();
  const cfg = (deployment as any)?.config?.apply ?? {};
  const repoBase = githubCompareBase(cfg.git_url ?? "");

  const openCompare = (before: string, after: string) => {
    if (!repoBase) return;
    OpenURL(`${repoBase}/compare/${before}...${after}`);
  };

  return (
    <div className="fixed inset-0 bg-black/60 backdrop-blur-sm flex items-center justify-center z-50 p-4" onClick={onClose}>
      <div className="glass-strong w-full max-w-2xl max-h-[85vh] flex flex-col p-5" onClick={(e) => e.stopPropagation()}>
        <div className="flex items-center justify-between mb-4 flex-shrink-0">
          <h2 className="text-base font-semibold flex items-center gap-2">
            <ActivityIcon className="w-4 h-4" /> {t("deployments.activityModal.title", { name: deployment.name })}
          </h2>
          <button onClick={onClose} className="p-1 rounded hover:bg-white/10"><X className="w-4 h-4" /></button>
        </div>

        {loading && (
          <div className="flex items-center gap-2 text-sm opacity-60">
            <Loader2 className="w-4 h-4 animate-spin" /> {t("deployments.activityModal.loadingLabel")}
          </div>
        )}

        {error && (
          <div className="text-[12px] text-red-300 bg-red-500/10 border border-red-500/30 rounded p-2">{error}</div>
        )}

        {!loading && !error && entries.length === 0 && (
          <div className="text-sm opacity-60 py-8 text-center">
            {t("deployments.activityModal.emptyTitle")}<br />
            <span className="text-[11px]">{t("deployments.activityModal.emptyDetailBefore")} <span className="font-mono">git pull</span> {t("deployments.activityModal.emptyDetailAfter")}</span>
          </div>
        )}

        {!loading && !error && entries.length > 0 && (
          <div className="flex-1 min-h-0 overflow-y-auto">
            <ul className="space-y-1.5">
              {entries.map((e, i) => (
                <li
                  key={i}
                  onClick={() => openCompare(e.before, e.after)}
                  className={clsx(
                    "glass rounded-md px-3 py-2 text-[12px] flex items-center gap-3",
                    repoBase ? "cursor-pointer hover:bg-white/[0.05]" : "",
                  )}
                  title={repoBase ? t("deployments.activityModal.openOnGithubTooltip", { before: e.before.slice(0,7), after: e.after.slice(0,7) }) : ""}
                >
                  <GitCommit className="w-3.5 h-3.5 text-accent2 flex-shrink-0" />
                  <div className="flex-1 min-w-0">
                    <div className="text-white">{formatRelative(new Date(e.at))}</div>
                    <div className="text-[10px] opacity-50 font-mono truncate">
                      {e.before.slice(0, 7)} → {e.after.slice(0, 7)}
                    </div>
                  </div>
                  <div className="text-[10px] opacity-40 font-mono flex-shrink-0">
                    {formatDate(new Date(e.at))}
                  </div>
                </li>
              ))}
            </ul>
          </div>
        )}

        <div className="text-[10px] opacity-40 mt-3 flex-shrink-0">
          {t("deployments.activityModal.footer", { count: entries.length })}
        </div>
      </div>
    </div>
  );
}

// githubCompareBase strips a `https://[pat@]github.com/owner/repo[.git]` URL
// down to `https://github.com/owner/repo` for use in /compare/<a>...<b>
// links. Returns "" if the URL isn't a parseable GitHub HTTPS URL.
function githubCompareBase(url: string): string {
  try {
    const u = new URL(url);
    if (!u.hostname.includes("github.com")) return "";
    let path = u.pathname.replace(/\.git$/, "");
    return `https://github.com${path}`;
  } catch { return ""; }
}

function humanize(e: any): string {
  if (e?.Message) return e.Message;
  if (typeof e === "string") return e;
  return String(e);
}
