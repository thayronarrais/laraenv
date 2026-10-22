import clsx from "clsx";
import ServiceIcon, { brandColor } from "./ServiceIcon";
import { useT } from "../lib/i18n";

export type ServiceStatus = "running" | "stopped" | "starting" | "stopping" | undefined;

type Props = {
  name: string;
  label?: string;
  status: ServiceStatus;
  onToggle: () => void;
  tooltip?: string;
};

const AMBER = "#f59e0b";

export default function ServiceCard({ name, label, status, onToggle, tooltip }: Props) {
  const t = useT();
  const running = status === "running";
  const transitioning = status === "starting" || status === "stopping";
  const accent = brandColor(name);
  // status is raw backend data ("running" | "stopped" | "starting" |
  // "stopping"); only the label shown to the user is translated — the
  // comparisons above stay against the untranslated value.
  const STATUS_LABELS: Record<string, string> = {
    running: t("dashboard.status.running"),
    stopped: t("dashboard.status.stopped"),
    starting: t("dashboard.status.starting"),
    stopping: t("dashboard.status.stopping"),
  };
  const statusText = status ? STATUS_LABELS[status] : t("dashboard.status.unknown");

  return (
    <button
      onClick={transitioning ? undefined : onToggle}
      disabled={transitioning}
      title={transitioning ? t("dashboard.serviceCard.pleaseWait") : tooltip}
      className={clsx(
        "glass glass-hover px-3 py-2.5 flex items-center gap-3 text-left w-full relative overflow-hidden transition-all",
        running && "ring-1",
        transitioning && "cursor-wait",
      )}
      style={
        running
          ? { boxShadow: `0 0 18px ${accent}33`, borderColor: `${accent}55` }
          : transitioning
            ? { borderColor: `${AMBER}55` }
            : undefined
      }
    >
      {/* Left brand-tinted strip when running, amber while transitioning */}
      {(running || transitioning) && (
        <span
          className="absolute left-0 top-0 bottom-0 w-[3px]"
          style={{ background: running ? accent : AMBER, boxShadow: `0 0 10px ${running ? accent : AMBER}` }}
        />
      )}

      <ServiceIcon name={name} />

      <div className="flex-1 min-w-0">
        <div className="font-medium text-sm leading-tight truncate">{label ?? name}</div>
        <div
          className={clsx(
            "text-[10px] leading-tight tracking-wide uppercase",
            running ? "opacity-90" : transitioning ? "" : "text-zinc-500",
          )}
          style={running ? { color: accent } : transitioning ? { color: AMBER } : undefined}
        >
          {statusText}
        </div>
      </div>

      <span
        className={clsx(
          "w-2 h-2 rounded-full flex-shrink-0",
          running || transitioning ? "animate-pulse" : "bg-zinc-600",
        )}
        style={
          running
            ? { background: accent, boxShadow: `0 0 8px ${accent}` }
            : transitioning
              ? { background: AMBER, boxShadow: `0 0 8px ${AMBER}` }
              : undefined
        }
      />
    </button>
  );
}
