import clsx from "clsx";
import { AlertTriangle, Bot, CheckCheck, CircleAlert, Gauge, MessageCircleQuestion } from "lucide-react";
import type { notifications } from "../../wailsjs/go/models";
import { useI18n } from "../lib/i18n";

const iconFor = (entry: notifications.Entry) => {
  if (entry.category === "usage") return <Gauge className="h-3.5 w-3.5" />;
  if (entry.category === "needs-input") return <MessageCircleQuestion className="h-3.5 w-3.5" />;
  if (entry.category === "completion") return <CheckCheck className="h-3.5 w-3.5" />;
  return entry.severity === "error" ? <CircleAlert className="h-3.5 w-3.5" /> : <AlertTriangle className="h-3.5 w-3.5" />;
};

export default function NotificationInbox({
  entries,
  preferences,
  onEntry,
  onMarkAll,
  onPreference,
}: {
  entries: notifications.Entry[];
  preferences: notifications.Preferences | null;
  onEntry: (entry: notifications.Entry) => void;
  onMarkAll: () => void;
  onPreference: (key: keyof notifications.Preferences, enabled: boolean) => void;
}) {
  const { t, formatRelative } = useI18n();
  const unread = entries.filter((entry) => !entry.read).length;
  return (
    <div className="fixed right-20 top-10 z-[110] flex max-h-[min(620px,calc(100vh-56px))] w-[390px] flex-col overflow-hidden rounded-xl border border-white/10 bg-[#16191f]/[0.98] shadow-[0_24px_80px_rgba(0,0,0,0.65)] backdrop-blur-xl">
      <div className="flex items-center justify-between border-b border-white/[0.08] px-4 py-3">
        <div>
          <div className="text-xs font-semibold text-white">{t("notifications.title")}</div>
          <div className="mt-0.5 text-[10px] text-white/45">{unread ? t("notifications.unreadCount", { count: unread }) : t("notifications.allCaughtUp")}</div>
        </div>
        <button type="button" onClick={onMarkAll} disabled={!unread} className="text-[10px] text-accent2 disabled:opacity-30">{t("notifications.markAllRead")}</button>
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto">
        {!entries.length ? (
          <div className="flex flex-col items-center gap-2 px-6 py-12 text-center text-white/35">
            <Bot className="h-6 w-6" />
            <p className="text-[11px]">{t("notifications.emptyMessage")}</p>
          </div>
        ) : entries.map((entry) => (
          <button
            key={entry.id}
            type="button"
            onClick={() => onEntry(entry)}
            className={clsx("flex w-full gap-3 border-b border-white/[0.06] px-4 py-3 text-left transition-colors hover:bg-white/[0.05]", !entry.read && "bg-accent/[0.07]")}
          >
            <span className={clsx(
              "mt-0.5 flex h-7 w-7 flex-none items-center justify-center rounded-lg border",
              entry.severity === "error" ? "border-red-400/30 bg-red-400/10 text-red-300" : entry.severity === "success" ? "border-emerald-400/30 bg-emerald-400/10 text-emerald-300" : "border-amber-400/30 bg-amber-400/10 text-amber-300",
            )}>{iconFor(entry)}</span>
            <span className="min-w-0 flex-1">
              <span className="flex items-start gap-2">
                <span className={clsx("min-w-0 flex-1 truncate text-[11px]", entry.read ? "text-white/65" : "font-medium text-white")}>{entry.title}</span>
                <span className="flex-none text-[9px] text-white/30">{formatRelative(new Date(String(entry.createdAt)))}</span>
              </span>
              <span className="mt-1 line-clamp-2 block text-[10px] leading-4 text-white/45">{entry.message}</span>
            </span>
            {!entry.read && <span className="mt-2 h-1.5 w-1.5 flex-none rounded-full bg-accent2" />}
          </button>
        ))}
      </div>

      {preferences && (
        <div className="border-t border-white/[0.08] px-4 py-3">
          <div className="mb-2 text-[9px] font-semibold uppercase tracking-[0.16em] text-white/35">{t("notifications.preferencesHeader")}</div>
          <div className="grid grid-cols-2 gap-x-3 gap-y-2">
            {([
              ["errors", "notifications.preferences.errors"], ["usageWarnings", "notifications.preferences.usage"],
              ["completion", "notifications.preferences.completion"], ["needsInput", "notifications.preferences.needsInput"],
            ] as [keyof notifications.Preferences, string][]).map(([key, labelKey]) => (
              <label key={key} className="flex cursor-pointer items-center gap-1.5 text-[9px] text-white/55">
                <input type="checkbox" checked={preferences[key]} onChange={(event) => onPreference(key, event.target.checked)} className="h-3 w-3 accent-[var(--c-accent)]" />
                {t(labelKey)}
              </label>
            ))}
          </div>
          <p className="mt-2 text-[9px] leading-4 text-white/30">{t("notifications.windowsUnavailableNote")}</p>
        </div>
      )}
    </div>
  );
}
