import { useEffect, useMemo, useState } from "react";
import { useDialogs } from "../components/dialogs/DialogProvider";
import clsx from "clsx";
import { Clock, Plus, Play, Trash2, Pencil, RefreshCw, Filter, X, Check, AlertTriangle, Loader2 } from "lucide-react";
// @ts-ignore
import { ListCrons, SaveCron, DeleteCron, ToggleCron, RunCronNow, ListProjects } from "../../wailsjs/go/main/App";
// @ts-ignore
import { EventsOn } from "../../wailsjs/runtime";
import GlassCard from "../components/GlassCard";
import { useT, useI18n } from "../lib/i18n";

type Job = {
  id: string;
  name: string;
  projectName?: string;
  schedule: string;
  command: string;
  workingDir?: string;
  phpVersion?: string;
  enabled: boolean;
  backgroundMode?: boolean;
  lastRun?: string;
  lastStatus?: string;
  lastOutput?: string;
  lastError?: string;
  nextRun?: string;
};

type Project = { name: string };

type Preset = { labelKey: string; schedule: string; command?: string; needsProject?: boolean };

const PRESETS: Preset[] = [
  { labelKey: "cron.presets.everyMinute",  schedule: "* * * * *" },
  { labelKey: "cron.presets.every5Min",   schedule: "*/5 * * * *" },
  { labelKey: "cron.presets.every15Min",  schedule: "*/15 * * * *" },
  { labelKey: "cron.presets.everyHour",    schedule: "0 * * * *" },
  { labelKey: "cron.presets.daily3am",   schedule: "0 3 * * *" },
  { labelKey: "cron.presets.laravelScheduler", schedule: "* * * * *", command: "php artisan schedule:run", needsProject: true },
];

function fmtTime(iso: string | undefined, formatDate: (d: Date | number, opts?: Intl.DateTimeFormatOptions) => string) {
  if (!iso) return "—";
  const d = new Date(iso);
  if (isNaN(d.getTime()) || d.getFullYear() < 2000) return "—";
  return formatDate(d, { dateStyle: "short", timeStyle: "short" });
}

function cronStatusLabel(t: (key: string) => string, status?: string): string {
  if (status === "running") return t("cron.status.running");
  if (status === "ok") return t("cron.status.ok");
  if (status === "error") return t("cron.status.error");
  return t("cron.status.never");
}

function StatusPill({ status }: { status?: string }) {
  const t = useT();
  if (status === "running") {
    return <span className="inline-flex items-center gap-1 text-[10px] text-amber-300 bg-amber-500/15 border border-amber-500/40 px-1.5 py-0.5 rounded"><Loader2 className="w-3 h-3 animate-spin" />{t("cron.status.running")}</span>;
  }
  if (status === "ok") {
    return <span className="inline-flex items-center gap-1 text-[10px] text-emerald-300 bg-emerald-500/15 border border-emerald-500/40 px-1.5 py-0.5 rounded"><Check className="w-3 h-3" />{t("cron.status.ok")}</span>;
  }
  if (status === "error") {
    return <span className="inline-flex items-center gap-1 text-[10px] text-red-300 bg-red-500/15 border border-red-500/40 px-1.5 py-0.5 rounded"><AlertTriangle className="w-3 h-3" />{t("cron.status.error")}</span>;
  }
  return <span className="text-[10px] opacity-40">{t("cron.status.never")}</span>;
}

export default function Cron({
  flash,
  filterRequest,
}: {
  flash: (m: string) => void;
  filterRequest?: { project: string; seq: number } | null;
}) {
  const [jobs, setJobs] = useState<Job[]>([]);
  const dialogs = useDialogs();
  const t = useT();
  const { formatDate } = useI18n();
  const [projects, setProjects] = useState<Project[]>([]);
  const [filter, setFilter] = useState<string>("");
  const [editing, setEditing] = useState<Job | null>(null);
  const [logFor, setLogFor] = useState<Job | null>(null);

  const refresh = async () => {
    const list: Job[] = await ListCrons();
    setJobs(list ?? []);
  };
  const refreshProjects = async () => {
    const list: Project[] = await ListProjects();
    setProjects(list ?? []);
  };

  useEffect(() => {
    refresh();
    refreshProjects();
    // Per-listener canceler from EventsOn — a global EventsOff("cron:status")
    // would remove every listener for the event app-wide.
    const off = EventsOn("cron:status", (j: Job) => {
      setJobs((prev) => {
        const idx = prev.findIndex((p) => p.id === j.id);
        if (idx < 0) return [...prev, j];
        const out = [...prev];
        out[idx] = j;
        return out;
      });
    });
    return () => off();
  }, []);

  // External nav (e.g. from Projects page) requesting a project filter.
  // The seq token forces this to fire even if the same project is picked twice.
  useEffect(() => {
    if (filterRequest?.project) setFilter(filterRequest.project);
  }, [filterRequest?.seq]);

  const visible = useMemo(() => {
    if (!filter) return jobs;
    if (filter === "__global__") return jobs.filter((j) => !j.projectName);
    return jobs.filter((j) => j.projectName === filter);
  }, [jobs, filter]);

  const onSave = async (j: Job) => {
    try {
      await SaveCron(j as any);
      flash(j.id ? t("cron.flash.updated") : t("cron.flash.created"));
      setEditing(null);
      refresh();
    } catch (e) { flash(t("cron.errors.generic", { message: String(e) })); }
  };
  const onDelete = async (id: string) => {
    if (!(await dialogs.confirm({ title: t("cron.deleteConfirm.title"), confirmLabel: t("cron.deleteLabel"), danger: true }))) return;
    try { await DeleteCron(id); flash(t("cron.flash.deleted")); refresh(); }
    catch (e) { flash(t("cron.errors.generic", { message: String(e) })); }
  };
  const onToggle = async (j: Job) => {
    try { await ToggleCron(j.id, !j.enabled); refresh(); }
    catch (e) { flash(t("cron.errors.generic", { message: String(e) })); }
  };
  const onRunNow = async (j: Job) => {
    try { await RunCronNow(j.id); flash(t("cron.flash.runningNow", { name: j.name })); }
    catch (e) { flash(t("cron.errors.generic", { message: String(e) })); }
  };

  return (
    <div className="h-full overflow-y-auto pr-2">
      <div className="flex items-center justify-between mb-4 gap-3">
        <h1 className="text-lg font-semibold flex items-center gap-2"><Clock className="w-5 h-5" /> {t("cron.page.title")}</h1>
        <div className="flex items-center gap-2">
          <div className="flex items-center gap-1.5 glass px-2 py-1">
            <Filter className="w-3.5 h-3.5 opacity-60" />
            <select
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
              className="bg-transparent text-xs outline-none"
            >
              <option value="">{t("cron.filter.all")}</option>
              <option value="__global__">{t("cron.filter.globalOnly")}</option>
              {projects.map((p) => <option key={p.name} value={p.name}>{p.name}</option>)}
            </select>
          </div>
          <button onClick={refresh} className="glass glass-hover px-3 py-1.5 text-xs flex items-center gap-1.5">
            <RefreshCw className="w-3.5 h-3.5" /> {t("cron.reloadButton")}
          </button>
          <button
            onClick={() => setEditing({ id: "", name: "", schedule: "* * * * *", command: "", enabled: true, backgroundMode: false })}
            className="px-3 py-1.5 rounded-md text-xs border bg-accent/30 border-accent hover:bg-accent/50 flex items-center gap-1.5"
          >
            <Plus className="w-3.5 h-3.5" /> {t("cron.newCronButton")}
          </button>
        </div>
      </div>

      <GlassCard>
        {visible.length === 0 ? (
          <div className="text-center text-sm opacity-50 py-8">
            {jobs.length === 0 ? t("cron.empty.noCronsYet") : t("cron.empty.noMatch")}
          </div>
        ) : (
          <table className="w-full text-sm">
            <thead className="text-[11px] uppercase tracking-wider opacity-50 text-left">
              <tr className="border-b border-white/5">
                <th className="py-2 pr-2">{t("cron.table.on")}</th>
                <th className="py-2 pr-2">{t("cron.table.name")}</th>
                <th className="py-2 pr-2">{t("cron.table.schedule")}</th>
                <th className="py-2 pr-2">{t("cron.table.project")}</th>
                <th className="py-2 pr-2">{t("cron.table.lastRun")}</th>
                <th className="py-2 pr-2">{t("cron.table.status")}</th>
                <th className="py-2 pr-2 text-right">{t("cron.table.actions")}</th>
              </tr>
            </thead>
            <tbody>
              {visible.map((j) => (
                <tr key={j.id} className="border-b border-white/5 hover:bg-white/[0.02]">
                  <td className="py-2 pr-2">
                    <button
                      onClick={() => onToggle(j)}
                      className={clsx(
                        "relative inline-flex h-4 w-7 items-center rounded-full transition-colors",
                        j.enabled ? "bg-accent" : "bg-white/10"
                      )}
                    >
                      <span className={clsx("inline-block h-3 w-3 transform rounded-full bg-white transition-transform", j.enabled ? "translate-x-3.5" : "translate-x-0.5")} />
                    </button>
                  </td>
                  <td className="py-2 pr-2">
                    <div className="font-medium">{j.name}</div>
                    <div className="font-mono text-[10px] opacity-50 truncate max-w-[280px]" title={j.command}>{j.command}</div>
                  </td>
                  <td className="py-2 pr-2 font-mono text-xs">{j.schedule}</td>
                  <td className="py-2 pr-2 text-xs">{j.projectName || <span className="opacity-40">—</span>}</td>
                  <td className="py-2 pr-2 text-xs opacity-70">{fmtTime(j.lastRun, formatDate)}</td>
                  <td className="py-2 pr-2"><StatusPill status={j.lastStatus} /></td>
                  <td className="py-2 pr-2">
                    <div className="flex justify-end gap-1">
                      <IconButton title={t("cron.actions.runNow")} onClick={() => onRunNow(j)}><Play className="w-3.5 h-3.5" /></IconButton>
                      <IconButton title={t("cron.actions.viewOutput")} onClick={() => setLogFor(j)} disabled={!j.lastOutput && !j.lastError}><Clock className="w-3.5 h-3.5" /></IconButton>
                      <IconButton title={t("cron.actions.edit")} onClick={() => setEditing(j)}><Pencil className="w-3.5 h-3.5" /></IconButton>
                      <IconButton title={t("cron.actions.delete")} onClick={() => onDelete(j.id)}><Trash2 className="w-3.5 h-3.5" /></IconButton>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </GlassCard>

      {editing && (
        <CronModal
          job={editing}
          projects={projects}
          onClose={() => setEditing(null)}
          onSave={onSave}
        />
      )}

      {logFor && (
        <LogModal job={logFor} onClose={() => setLogFor(null)} />
      )}
    </div>
  );
}

function IconButton({ children, title, onClick, disabled }: { children: React.ReactNode; title: string; onClick: () => void; disabled?: boolean }) {
  return (
    <button
      title={title}
      onClick={onClick}
      disabled={disabled}
      className={clsx(
        "p-1.5 rounded-md border transition-colors",
        disabled ? "opacity-30 cursor-not-allowed border-transparent"
                 : "border-white/10 hover:bg-white/10 hover:border-white/25"
      )}
    >
      {children}
    </button>
  );
}

function CronModal({
  job, projects, onClose, onSave,
}: {
  job: Job;
  projects: Project[];
  onClose: () => void;
  onSave: (j: Job) => void;
}) {
  const t = useT();
  const [draft, setDraft] = useState<Job>({ ...job });
  const set = (patch: Partial<Job>) => setDraft((prev) => ({ ...prev, ...patch }));
  const applyPreset = (p: Preset) => {
    const patch: Partial<Job> = { schedule: p.schedule };
    if (p.command) patch.command = p.command;
    if (p.needsProject && !draft.projectName && projects[0]) patch.projectName = projects[0].name;
    set(patch);
  };

  return (
    <div className="fixed inset-0 bg-black/60 backdrop-blur-sm flex items-center justify-center z-50 p-4" onClick={onClose}>
      <div className="glass-strong w-full max-w-lg p-5" onClick={(e) => e.stopPropagation()}>
        <div className="flex items-center justify-between mb-4">
          <h2 className="text-base font-semibold">{job.id ? t("cron.modal.editTitle") : t("cron.modal.newTitle")}</h2>
          <button onClick={onClose} className="p-1 rounded hover:bg-white/10"><X className="w-4 h-4" /></button>
        </div>

        <div className="space-y-3">
          <Field label={t("cron.modal.nameLabel")}>
            <input className="input" value={draft.name} onChange={(e) => set({ name: e.target.value })} placeholder={t("cron.modal.namePlaceholder")} />
          </Field>

          <Field label={t("cron.modal.scheduleLabel")} hint={t("cron.modal.scheduleHint")}>
            <input className="input font-mono" value={draft.schedule} onChange={(e) => set({ schedule: e.target.value })} placeholder="* * * * *" />
            <div className="flex flex-wrap gap-1 mt-1.5">
              {PRESETS.map((p) => (
                <button key={p.labelKey} onClick={() => applyPreset(p)}
                  className="text-[10px] px-2 py-0.5 rounded border border-white/10 hover:bg-white/10 hover:border-white/25">
                  {t(p.labelKey)}
                </button>
              ))}
            </div>
          </Field>

          <Field label={t("cron.modal.commandLabel")} hint={t("cron.modal.commandHint")}>
            <input className="input font-mono" value={draft.command} onChange={(e) => set({ command: e.target.value })} placeholder="php artisan schedule:run" />
          </Field>

          <Field label={t("cron.modal.projectLabel")} hint={t("cron.modal.projectHint")}>
            <select className="input" value={draft.projectName || ""} onChange={(e) => set({ projectName: e.target.value || undefined })}>
              <option value="">{t("cron.modal.globalOption")}</option>
              {projects.map((p) => <option key={p.name} value={p.name}>{p.name}</option>)}
            </select>
          </Field>

          <Field label={t("cron.modal.workingDirLabel")} hint={t("cron.modal.workingDirHint")}>
            <input className="input font-mono" value={draft.workingDir || ""} onChange={(e) => set({ workingDir: e.target.value || undefined })} placeholder="" />
          </Field>

          <label className="flex items-center gap-2 text-sm pt-1">
            <input type="checkbox" checked={draft.enabled} onChange={(e) => set({ enabled: e.target.checked })} />
            {t("cron.modal.enabledLabel")}
          </label>

          <label className="flex items-start gap-2 text-sm pt-1">
            <input
              type="checkbox"
              className="mt-0.5"
              checked={!!draft.backgroundMode}
              onChange={(e) => set({ backgroundMode: e.target.checked })}
            />
            <span>
              {t("cron.modal.backgroundModeLabel")}
              <span className="block text-[10px] opacity-60">
                {t("cron.modal.backgroundModeHint")}
              </span>
            </span>
          </label>
        </div>

        <div className="flex justify-end gap-2 mt-5">
          <button onClick={onClose} className="px-3 py-1.5 rounded-md text-xs border border-white/10 hover:bg-white/10">{t("common.cancel")}</button>
          <button onClick={() => onSave(draft)} className="px-3 py-1.5 rounded-md text-xs border bg-accent/30 border-accent hover:bg-accent/50">{t("common.save")}</button>
        </div>
      </div>
      <style>{`.input{width:100%;background:rgba(255,255,255,0.04);border:1px solid rgba(255,255,255,0.1);border-radius:6px;padding:6px 10px;font-size:12px;color:inherit;outline:none}.input:focus{border-color:var(--c-accent)}`}</style>
    </div>
  );
}

function Field({ label, hint, children }: { label: string; hint?: string; children: React.ReactNode }) {
  return (
    <div>
      <div className="text-[11px] uppercase tracking-wider opacity-60 mb-1">{label}</div>
      {children}
      {hint && <div className="text-[10px] opacity-40 mt-1">{hint}</div>}
    </div>
  );
}

function LogModal({ job, onClose }: { job: Job; onClose: () => void }) {
  const t = useT();
  const { formatDate } = useI18n();
  const statusText = job.lastStatus ? cronStatusLabel(t, job.lastStatus) : "—";
  return (
    <div className="fixed inset-0 bg-black/60 backdrop-blur-sm flex items-center justify-center z-50 p-4" onClick={onClose}>
      <div className="glass-strong w-full max-w-2xl p-5" onClick={(e) => e.stopPropagation()}>
        <div className="flex items-center justify-between mb-3">
          <div>
            <div className="text-base font-semibold">{job.name}</div>
            <div className="text-[11px] opacity-50">{t("cron.logModal.summary", { lastRun: fmtTime(job.lastRun, formatDate), status: statusText })}</div>
          </div>
          <button onClick={onClose} className="p-1 rounded hover:bg-white/10"><X className="w-4 h-4" /></button>
        </div>
        {job.lastError && (
          <div className="mb-2 text-[12px] text-red-300 bg-red-500/10 border border-red-500/30 rounded p-2">{job.lastError}</div>
        )}
        <pre className="glass p-3 text-[11px] whitespace-pre-wrap font-mono leading-relaxed max-h-[60vh] overflow-y-auto">
          {job.lastOutput || t("cron.logModal.noOutput")}
        </pre>
      </div>
    </div>
  );
}
