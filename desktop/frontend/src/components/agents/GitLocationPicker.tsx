import { GitBranch, GitCommitHorizontal, Boxes } from "lucide-react";
import { useT } from "../../lib/i18n";

export type GitMode = "current" | "branch" | "worktree";

export type RepositoryInfo = {
  repository: boolean;
  root?: string;
  currentBranch?: string;
  dirty: boolean;
  branches: { name: string; remote: boolean }[];
  worktrees: { path: string; branch?: string; head?: string }[];
  reason?: string;
};

export default function GitLocationPicker({
  info, mode, onChange,
}: {
  info: RepositoryInfo | null;
  mode: GitMode;
  onChange: (mode: GitMode) => void;
}) {
  const t = useT();
  const options: { id: GitMode; label: string; detail: string; icon: React.ReactNode; git: boolean }[] = [
    { id: "current", label: t("agents.gitLocation.current.label"), detail: t("agents.gitLocation.current.detail"), icon: <GitCommitHorizontal className="w-3.5 h-3.5" />, git: false },
    { id: "branch", label: t("agents.gitLocation.branch.label"), detail: t("agents.gitLocation.branch.detail"), icon: <GitBranch className="w-3.5 h-3.5" />, git: true },
    { id: "worktree", label: t("agents.gitLocation.worktree.label"), detail: t("agents.gitLocation.worktree.detail"), icon: <Boxes className="w-3.5 h-3.5" />, git: true },
  ];

  return (
    <div className="grid gap-1">
      {options.map((option) => {
        const disabled = option.git && info !== null && !info.repository;
        const selected = mode === option.id;
        return (
          <button
            key={option.id}
            type="button"
            disabled={disabled}
            onClick={() => onChange(option.id)}
            className={`group w-full grid grid-cols-[18px_1fr] gap-2 rounded-lg border px-2.5 py-2 text-left transition-colors
              ${selected ? "border-accent2/50 bg-accent2/10" : "border-white/10 bg-white/[0.025] hover:bg-white/[0.06]"}
              disabled:opacity-35 disabled:cursor-not-allowed`}
          >
            <span className={`mt-0.5 ${selected ? "text-accent2" : "opacity-55"}`}>{option.icon}</span>
            <span>
              <span className="flex items-center justify-between gap-3 text-[11px] font-medium">
                {option.label}
                {selected && <span className="font-mono text-[8px] uppercase tracking-widest text-accent2">{t("agents.gitLocation.selectedBadge")}</span>}
              </span>
              <span className="mt-0.5 block text-[9px] leading-4 opacity-50">{option.detail}</span>
            </span>
          </button>
        );
      })}
    </div>
  );
}
