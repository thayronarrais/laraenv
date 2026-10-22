import { useEffect, useRef, useState } from "react";
import { X } from "lucide-react";
// @ts-ignore
import { GetAgentWorktreeDevServerLogs, GetDevServerLogs } from "../../wailsjs/go/main/App";
// @ts-ignore
import { EventsOn, EventsOff } from "../../wailsjs/runtime";
import { useT } from "../lib/i18n";

type LogLine = { project: string; instance?: string; line: string };

export default function DevServerLogsModal({
  projectName,
  worktreeSessionId,
  open,
  onClose,
}: {
  projectName: string | null;
  worktreeSessionId?: string | null;
  open: boolean;
  onClose: () => void;
}) {
  const t = useT();
  const [lines, setLines] = useState<string[]>([]);
  const boxRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open || !projectName) return;
    let mounted = true;

    const instance = worktreeSessionId ? `agent-worktree:${worktreeSessionId}` : projectName;
    const load = worktreeSessionId ? GetAgentWorktreeDevServerLogs(worktreeSessionId) : GetDevServerLogs(projectName);
    load.then((ls: string[]) => {
      if (mounted) setLines(ls ?? []);
    });

    const onLog = (data: LogLine) => {
      if ((data.instance || data.project) !== instance) return;
      setLines((prev) => {
        const next = [...prev, data.line];
        return next.length > 1000 ? next.slice(-1000) : next;
      });
    };
    EventsOn("devserver:log", onLog);
    return () => {
      mounted = false;
      EventsOff("devserver:log");
    };
  }, [open, projectName, worktreeSessionId]);

  useEffect(() => {
    if (boxRef.current) boxRef.current.scrollTop = boxRef.current.scrollHeight;
  }, [lines]);

  if (!open || !projectName) return null;

  return (
    <div className="fixed inset-0 z-50 bg-black/60 backdrop-blur-sm flex items-center justify-center p-4">
      <div className="glass w-[800px] max-w-full max-h-[80vh] flex flex-col">
        <div className="flex items-center justify-between px-4 py-3 border-b border-white/10">
          <div className="text-sm font-semibold">{t("terminal.devServerLogs.titlePrefix")}{projectName}{worktreeSessionId ? t("terminal.devServerLogs.worktreeSuffix") : ""}</div>
          <button onClick={onClose} className="opacity-60 hover:opacity-100">
            <X className="w-4 h-4" />
          </button>
        </div>
        <div
          ref={boxRef}
          className="flex-1 overflow-y-auto p-3 font-mono text-[11px] leading-relaxed bg-black/40"
        >
          {lines.length === 0 ? (
            <div className="opacity-50">{t("terminal.devServerLogs.noOutputYet")}</div>
          ) : (
            lines.map((l, i) => (
              <div key={i} className="whitespace-pre-wrap break-words">{l}</div>
            ))
          )}
        </div>
      </div>
    </div>
  );
}
