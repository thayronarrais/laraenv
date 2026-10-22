// Visual progress timeline shown while an SSH pane is connecting. Lifted
// unchanged from the old TerminalTabs.

import { Check, Loader2, Circle } from "lucide-react";
import clsx from "clsx";
import { useT } from "../../lib/i18n";

export const SSH_STAGES: { key: string; labelKey: string }[] = [
  { key: "dial",      labelKey: "terminal.sshTimeline.stages.dial" },
  { key: "handshake", labelKey: "terminal.sshTimeline.stages.handshake" },
  { key: "session",   labelKey: "terminal.sshTimeline.stages.session" },
  { key: "pty",       labelKey: "terminal.sshTimeline.stages.pty" },
  { key: "shell",     labelKey: "terminal.sshTimeline.stages.shell" },
];

export default function SSHConnectingTimeline({ label, stage }: { label: string; stage?: string }) {
  const t = useT();
  const currentIdx = stage ? Math.max(0, SSH_STAGES.findIndex((s) => s.key === stage)) : 0;
  return (
    <div className="h-full w-full flex items-center justify-center p-6">
      <div className="w-full max-w-md glass-strong rounded-2xl p-6 flex flex-col gap-5">
        <div className="text-center">
          <div className="text-xs uppercase tracking-[0.2em] opacity-50">{t("terminal.sshTimeline.connecting")}</div>
          <div className="text-sm font-mono mt-1 break-all">{label.replace(/ \(.+\)$/, "")}</div>
        </div>
        <ol className="flex flex-col gap-1">
          {SSH_STAGES.map((s, idx) => {
            const done = idx < currentIdx;
            const active = idx === currentIdx;
            const last = idx === SSH_STAGES.length - 1;
            return (
              <li key={s.key} className="flex items-stretch gap-3">
                <div className="flex flex-col items-center">
                  <div className={clsx(
                    "w-6 h-6 rounded-full flex items-center justify-center border transition-colors",
                    done && "bg-emerald-500/25 border-emerald-400/60 text-emerald-200",
                    active && "bg-accent/25 border-accent text-accent2 shadow-[0_0_18px_rgba(124,92,255,0.55)]",
                    !done && !active && "bg-white/5 border-white/15 text-white/40",
                  )}>
                    {done ? <Check className="w-3 h-3" /> : active ? <Loader2 className="w-3 h-3 animate-spin" /> : <Circle className="w-2 h-2" />}
                  </div>
                  {!last && <div className={clsx("w-px flex-1 my-1 transition-colors", done ? "bg-emerald-400/50" : "bg-white/10")} />}
                </div>
                <div className={clsx("py-1 text-sm leading-tight", last && "pb-0")}>
                  <div className={clsx("transition-colors", done && "text-emerald-200", active && "text-white font-medium", !done && !active && "opacity-40")}>
                    {t(s.labelKey)}
                  </div>
                  {active && stage === s.key && <div className="text-[10px] opacity-60 mt-0.5">{t("terminal.sshTimeline.inProgress")}</div>}
                </div>
              </li>
            );
          })}
        </ol>
      </div>
    </div>
  );
}
