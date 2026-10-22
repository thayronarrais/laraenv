import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import type { CSSProperties } from "react";
import { Bell } from "lucide-react";
import NotificationInbox from "./NotificationInbox";
import { notifications } from "../../wailsjs/go/models";
// @ts-ignore
import { ListAgentSessions, ListNotifications, MarkAllNotificationsRead, MarkNotificationRead, NotificationPreferences, SetNotificationPreferences, UnreadNotificationCount } from "../../wailsjs/go/main/App";
// @ts-ignore
import { EventsOn } from "../../wailsjs/runtime";
import { useT } from "../lib/i18n";

export default function NotificationBell({
  onOpenSession,
  onOpenAgents,
  flash,
}: {
  onOpenSession: (sessionId: string) => void;
  onOpenAgents: () => void;
  flash: (message: string) => void;
}) {
  const t = useT();
  const [open, setOpen] = useState(false);
  const [unread, setUnread] = useState(0);
  const [entries, setEntries] = useState<notifications.Entry[]>([]);
  const [preferences, setPreferences] = useState<notifications.Preferences | null>(null);
  const buttonRef = useRef<HTMLButtonElement>(null);

  const refresh = () => Promise.all([ListNotifications(), UnreadNotificationCount(), NotificationPreferences()])
    .then(([nextEntries, count, nextPreferences]: [notifications.Entry[], number, notifications.Preferences]) => {
      setEntries(nextEntries ?? []);
      setUnread(count ?? 0);
      setPreferences(nextPreferences);
    })
    .catch((error) => console.error("[notifications] refresh failed", error));

  useEffect(() => { void refresh(); }, []);
  useEffect(() => {
    const off = EventsOn("notification:changed", (count: number) => {
      setUnread(count ?? 0);
      void refresh();
    });
    return () => off();
  }, []);
  useEffect(() => {
    if (!open) return;
    const close = (event: MouseEvent) => {
      const target = event.target as Node;
      if (buttonRef.current?.contains(target) || (target instanceof Element && target.closest("[data-notification-inbox]"))) return;
      setOpen(false);
    };
    const escape = (event: KeyboardEvent) => { if (event.key === "Escape") setOpen(false); };
    document.addEventListener("mousedown", close);
    window.addEventListener("keydown", escape);
    return () => { document.removeEventListener("mousedown", close); window.removeEventListener("keydown", escape); };
  }, [open]);

  const handleEntry = async (entry: notifications.Entry) => {
    try {
      if (!entry.read) await MarkNotificationRead(entry.id);
      if (entry.target?.sessionId) {
        const sessions = await ListAgentSessions("", true);
        if ((sessions ?? []).some((session: any) => session.id === entry.target.sessionId)) onOpenSession(entry.target.sessionId);
        else { onOpenAgents(); flash(t("notifications.errors.sessionGone")); }
      } else {
        onOpenAgents();
      }
      setOpen(false);
    } catch (error) {
      flash(String(error));
    }
  };

  const markAll = async () => {
    try { await MarkAllNotificationsRead(); } catch (error) { flash(String(error)); }
  };

  const setPreference = async (key: keyof notifications.Preferences, enabled: boolean) => {
    if (!preferences) return;
    const next = new notifications.Preferences({ ...preferences, [key]: enabled });
    setPreferences(next);
    try { await SetNotificationPreferences(next); } catch (error) { setPreferences(preferences); flash(String(error)); }
  };

  return (
    <>
      <button
        ref={buttonRef}
        type="button"
        onClick={() => { setOpen((value) => !value); void refresh(); }}
        title={t("notifications.bellTooltip")}
        className="relative mr-1 flex h-7 w-8 items-center justify-center rounded-md text-white/70 transition-colors hover:bg-white/10 hover:text-white"
        // @ts-ignore
        style={{ "--wails-draggable": "no-drag" }}
      >
        <Bell className="h-4 w-4" />
        {unread > 0 && (
          <span className="absolute -right-0.5 -top-0.5 min-w-[15px] rounded-full border border-[#16191f] bg-accent2 px-1 text-center text-[8px] font-bold leading-[14px] text-[#10151d]">
            {unread > 99 ? "99+" : unread}
          </span>
        )}
      </button>
      {open && createPortal(<div data-notification-inbox style={{ "--wails-draggable": "no-drag" } as CSSProperties}><NotificationInbox entries={entries} preferences={preferences} onEntry={handleEntry} onMarkAll={markAll} onPreference={setPreference} /></div>, document.body)}
    </>
  );
}
