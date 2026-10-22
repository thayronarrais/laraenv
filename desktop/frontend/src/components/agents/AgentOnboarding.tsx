import { Bot, FolderKanban, GitCommitHorizontal, Play, TerminalSquare } from "lucide-react";
import type { AgentProfile } from "../AgentLauncherMenu";
import { useT } from "../../lib/i18n";

export default function AgentOnboarding({
  projectName,
  profiles,
  hasProjects,
  onStart,
}: {
  projectName?: string;
  profiles: AgentProfile[];
  hasProjects: boolean;
  onStart: () => void;
}) {
  const t = useT();
  return (
    <div className="flex h-full min-h-[360px] items-center justify-center rounded-xl border border-dashed border-white/10 bg-white/[0.015] px-6 py-8">
      <div className="w-full max-w-3xl">
        <div className="mx-auto max-w-xl text-center">
          <div className="mb-3 inline-flex items-center gap-2 rounded-full border border-accent2/20 bg-accent2/[0.07] px-2.5 py-1 font-mono text-[8px] uppercase tracking-[0.18em] text-accent2/80">
            <TerminalSquare className="h-3 w-3" /> {t("agents.onboarding.badge")}
          </div>
          <h2 className="text-[17px] font-semibold tracking-tight">{t("agents.onboarding.title")}</h2>
          <p className="mx-auto mt-2 max-w-lg text-[10px] leading-5 opacity-45">
            {t("agents.onboarding.description")}
          </p>
        </div>

        <div className="relative mx-auto mt-7 grid max-w-2xl grid-cols-3 gap-2.5">
          <div className="pointer-events-none absolute left-[16.6%] right-[16.6%] top-5 h-px bg-white/10" />
          {[
            { icon: <FolderKanban className="h-3.5 w-3.5" />, label: t("agents.onboarding.steps.project"), value: projectName || t("agents.onboarding.projectPlaceholder") },
            { icon: <Bot className="h-3.5 w-3.5" />, label: t("agents.onboarding.steps.agent"), value: "Claude · Codex · Kimi · OpenCode" },
            { icon: <GitCommitHorizontal className="h-3.5 w-3.5" />, label: t("agents.onboarding.steps.workingCopy"), value: t("agents.onboarding.workingCopyValue") },
          ].map((step) => (
            <div key={step.label} className="relative rounded-lg border border-white/10 bg-[#111]/95 px-3 py-3 text-center">
              <span className="mx-auto mb-2 flex h-4 w-4 items-center justify-center bg-[#111] text-accent2">{step.icon}</span>
              <span className="block font-mono text-[8px] uppercase tracking-widest opacity-35">{step.label}</span>
              <span className="mt-1.5 block min-h-7 text-[9px] leading-3.5 opacity-70">{step.value}</span>
            </div>
          ))}
        </div>

        <div className="mt-5 flex flex-wrap items-center justify-center gap-1.5">
          {profiles.map((profile) => (
            <span key={profile.id} className={`rounded border px-2 py-1 font-mono text-[8px] ${profile.available ? "border-emerald-400/20 bg-emerald-400/[0.06] text-emerald-200/70" : "border-white/10 bg-white/[0.025] opacity-35"}`} title={profile.version || profile.reason}>
              {profile.label} · {profile.available ? t("agents.onboarding.readyLabel") : t("agents.onboarding.notFoundLabel")}
            </span>
          ))}
        </div>

        <div className="mt-5 text-center">
          <button type="button" disabled={!hasProjects} onClick={onStart} className="inline-flex h-9 items-center gap-2 rounded-lg bg-accent/80 px-4 text-[11px] font-medium text-white hover:bg-accent disabled:cursor-not-allowed disabled:opacity-35">
            <Play className="h-3 w-3 fill-current" /> {hasProjects ? t("agents.onboarding.startButton") : t("agents.onboarding.addProjectButton")}
          </button>
        </div>
      </div>
    </div>
  );
}
