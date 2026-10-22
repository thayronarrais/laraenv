import { useEffect, useRef, useState } from "react";
import { Folder, FolderOpen, Loader2, Plus, X } from "lucide-react";
// @ts-ignore
import { SelectAgentContextDirectory } from "../../../wailsjs/go/main/App";
import { useT } from "../../lib/i18n";

function directoryKey(value: string) {
  const path = value.replace(/\\/g, "/").replace(/\/+$/, "");
  return /^[a-z]:/i.test(path) || path.startsWith("//") ? path.toLowerCase() : path;
}

export default function AgentContextFolders({
  projectPath, directories, onChange, supported, worktree, providerLabel,
}: {
  projectPath: string;
  directories: string[];
  onChange: (directories: string[]) => void;
  supported: boolean;
  worktree: boolean;
  providerLabel: string;
}) {
  const t = useT();
  const [path, setPath] = useState("");
  const [error, setError] = useState("");
  const [browsing, setBrowsing] = useState(false);
  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; };
  }, []);

  const addDirectory = (value: string) => {
    const trimmed = value.trim();
    const directory = /^(".*"|'.*')$/.test(trimmed) ? trimmed.slice(1, -1) : trimmed;
    if (!directory) return;
    if (!/^(?:[a-z]:[\\/]|[\\/]{2}[^\\/]+[\\/][^\\/]+|\/)/i.test(directory)) {
      setError(t("agents.contextFolders.errorInvalidPath"));
      return;
    }
    const key = directoryKey(directory);
    if ((!worktree && key === directoryKey(projectPath)) || directories.some((existing) => directoryKey(existing) === key)) {
      setError(t("agents.contextFolders.errorDuplicate"));
      return;
    }
    onChange([...directories, directory]);
    setPath("");
    setError("");
  };

  const browse = async () => {
    setBrowsing(true);
    setError("");
    try {
      const directory = await SelectAgentContextDirectory();
      if (mounted.current && directory) addDirectory(directory);
    } catch (err) {
      if (mounted.current) setError(t("agents.contextFolders.errorBrowseFailed", { message: String(err) }));
    } finally {
      if (mounted.current) setBrowsing(false);
    }
  };

  return (
    <section className="mb-3 rounded-lg border border-white/10 bg-black/15 p-2.5" aria-label={t("agents.contextFolders.heading")}>
      <div className="mb-2 flex items-center justify-between gap-2">
        <span className="text-[10px] font-semibold text-white/80">{t("agents.contextFolders.heading")}</span>
        <span className="font-mono text-[9px] text-white/45">{t("agents.contextFolders.folderCount", { count: directories.length + 1 })}</span>
      </div>
      <div className="flex items-center gap-2 text-white/65" title={projectPath}>
        <Folder className="h-3.5 w-3.5 shrink-0 text-accent2" />
        <span className="min-w-0 flex-1 truncate font-mono text-[10px]">{projectPath}</span>
        <span className="shrink-0 text-[9px] text-white/40">{worktree ? t("agents.contextFolders.newWorktreeLabel") : t("agents.contextFolders.mainLabel")}</span>
      </div>
      {directories.length > 0 && (
        <ul className="mt-1 space-y-1">
          {directories.map((directory) => (
            <li key={directory} className="flex items-center gap-2" title={directory}>
              <Folder className="h-3.5 w-3.5 shrink-0 text-white/40" />
              <span className="min-w-0 flex-1 truncate font-mono text-[10px] text-white/70">{directory}</span>
              <button type="button" disabled={browsing} onClick={() => { onChange(directories.filter((item) => item !== directory)); setError(""); }} aria-label={t("agents.contextFolders.removeFolderAria", { directory })} className="flex h-6 w-6 shrink-0 items-center justify-center rounded text-white/45 hover:bg-white/10 hover:text-white focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-accent2/60">
                <X className="h-3 w-3" />
              </button>
            </li>
          ))}
        </ul>
      )}
      {supported ? (
        <>
          <div className="mt-2 flex items-center gap-1.5">
            <input
              aria-label={t("agents.contextFolders.pathInputAria")}
              value={path}
              onChange={(event) => { setPath(event.target.value); setError(""); }}
              onKeyDown={(event) => { if (event.key === "Enter") { event.preventDefault(); addDirectory(path); } }}
              disabled={browsing}
              className="input h-8 min-w-0 flex-1 font-mono text-[10px]"
              placeholder={t("agents.contextFolders.pathPlaceholder")}
            />
            <button type="button" disabled={!path.trim() || browsing} onClick={() => addDirectory(path)} aria-label={t("agents.contextFolders.addFolderAria")} title={t("agents.contextFolders.addFolderTooltip")} className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md border border-white/10 text-white/70 hover:bg-white/10 disabled:opacity-30 focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-accent2/60">
              <Plus className="h-3.5 w-3.5" />
            </button>
            <button type="button" disabled={browsing} onClick={() => void browse()} className="flex h-8 shrink-0 items-center gap-1.5 rounded-md border border-white/10 px-2 text-[10px] text-white/75 hover:bg-white/10 disabled:opacity-40 focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-accent2/60">
              {browsing ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <FolderOpen className="h-3.5 w-3.5" />} {t("agents.contextFolders.browseButton")}
            </button>
          </div>
          <p className="mt-1.5 text-[10px] leading-4 text-white/45">{t("agents.contextFolders.hint")}</p>
          {worktree && directories.length > 0 && <p className="mt-1 text-[10px] leading-4 text-white/55">{t("agents.contextFolders.worktreeHint")}</p>}
        </>
      ) : <p className={`mt-2 text-[10px] leading-4 ${directories.length ? "text-amber-200" : "text-white/45"}`}>{directories.length ? t("agents.contextFolders.unsupportedHasFolders", { provider: providerLabel }) : t("agents.contextFolders.unsupportedNoFolders", { provider: providerLabel })}</p>}
      {error && <p role="alert" className="mt-1.5 break-words text-[10px] leading-4 text-amber-200">{error}</p>}
    </section>
  );
}
