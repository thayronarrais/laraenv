import { useEffect, useState } from "react";
import clsx from "clsx";
import { Play, Square, Settings } from "lucide-react";
// @ts-ignore
import { ListPhpFpmStatus, StartService, StopService } from "../../wailsjs/go/main/App";
// @ts-ignore
import { EventsOn } from "../../wailsjs/runtime";
import PhpExtensionsModal from "./PhpExtensionsModal";
import { useT } from "../lib/i18n";

type Info = { version: string; port: number; status: string; binary: string };

export default function PhpFpmStatus({ flash }: { flash: (m: string) => void }) {
  const t = useT();
  const [items, setItems] = useState<Info[]>([]);
  const [editing, setEditing] = useState<string | null>(null);

  const refresh = () => ListPhpFpmStatus().then((list: Info[]) => setItems(list ?? []));

  useEffect(() => {
    refresh();
    const id = setInterval(refresh, 2500);
    // Use the canceler EventsOn returns — EventsOff("service:status") would
    // remove every listener for the event app-wide, deafening the kept-alive
    // pages (Dashboard cards, Projects, Downloads) until app restart.
    const off = EventsOn("service:status", () => refresh());
    return () => {
      clearInterval(id);
      off();
    };
  }, []);

  const toggle = async (v: Info) => {
    const name = "php-fpm:" + v.version;
    try {
      if (v.status === "running") await StopService(name);
      else await StartService(name);
      refresh();
    } catch (e) { flash(t("dashboard.errors.generic", { message: String(e) })); }
  };

  if (items.length === 0) {
    return <div className="text-xs opacity-50">{t("dashboard.phpFpm.empty")}</div>;
  }

  return (
    <div className="flex flex-col gap-1.5">
      {items.map((v) => (
        <div key={v.version} className="flex items-center gap-2 text-xs">
          <span
            className={clsx(
              "w-2 h-2 rounded-full",
              v.status === "running"
                ? "bg-emerald-400 shadow-[0_0_6px_rgba(52,211,153,0.7)]"
                : "bg-zinc-600"
            )}
          />
          <span className="font-mono w-12">PHP {v.version}</span>
          <span className="opacity-50 font-mono">:{v.port}</span>
          <span
            className={clsx(
              "ml-auto text-[10px] uppercase tracking-wider",
              v.status === "running" ? "text-emerald-400" : "text-zinc-500"
            )}
          >{t(v.status === "running" ? "dashboard.status.running" : "dashboard.status.stopped")}</span>
          <button
            onClick={() => setEditing(v.version)}
            className="px-1.5 py-0.5 rounded-md bg-white/5 hover:bg-white/10 border border-white/10"
            title={t("dashboard.phpFpm.extensionsTooltip")}
          >
            <Settings className="w-3 h-3" />
          </button>
          <button
            onClick={() => toggle(v)}
            className="px-1.5 py-0.5 rounded-md bg-white/5 hover:bg-white/10 border border-white/10"
            title={v.status === "running" ? t("dashboard.phpFpm.stopTooltip") : t("dashboard.phpFpm.startTooltip")}
          >
            {v.status === "running" ? <Square className="w-3 h-3" /> : <Play className="w-3 h-3" />}
          </button>
        </div>
      ))}

      <PhpExtensionsModal
        version={editing}
        open={editing !== null}
        onClose={() => setEditing(null)}
        flash={flash}
      />
    </div>
  );
}
