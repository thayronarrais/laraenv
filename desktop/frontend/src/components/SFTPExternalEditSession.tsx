import { useEffect, useRef, useState } from "react";
import { ExternalLink, Upload, Trash2, Loader2, Check, AlertTriangle } from "lucide-react";
// @ts-ignore
import {
  SFTPReuploadFromTemp, SFTPCleanupTemp, SFTPWatchTemp, SFTPUnwatchTemp,
} from "../../wailsjs/go/main/App";
// @ts-ignore
import { EventsOn, EventsOff } from "../../wailsjs/runtime";
import { useT, useI18n } from "../lib/i18n";

const AUTOSAVE_KEY = "lara4ever:sftp-editor:autosave-external";

export type ExternalSession = {
  id: string;
  termId: string;
  remotePath: string;
  tempPath: string;
};

function basename(p: string): string {
  if (!p) return "";
  const t = p.replace(/[\\/]+$/, "");
  const i = Math.max(t.lastIndexOf("/"), t.lastIndexOf("\\"));
  return i < 0 ? t : t.slice(i + 1);
}

// Single floating card per open external-edit session. Watches the temp file
// when AutoSave is on; otherwise the user clicks "Reenviar" manually.
export default function SFTPExternalEditSession({
  session, onDone,
}: {
  session: ExternalSession;
  onDone: (id: string) => void;
}) {
  const t = useT();
  const { formatDate } = useI18n();
  const [uploading, setUploading] = useState(false);
  const [lastUpAt, setLastUpAt] = useState<number | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [autoSave, setAutoSave] = useState<boolean>(() => {
    try { return localStorage.getItem(AUTOSAVE_KEY) === "1"; } catch { return false; }
  });
  const lastModRef = useRef<number>(0);

  useEffect(() => {
    try { localStorage.setItem(AUTOSAVE_KEY, autoSave ? "1" : "0"); } catch { /* ignore */ }
  }, [autoSave]);

  const reupload = async () => {
    setUploading(true);
    setError(null);
    try {
      await SFTPReuploadFromTemp(session.termId, session.tempPath, session.remotePath);
      setLastUpAt(Date.now());
    } catch (e: any) {
      setError(String(e?.message ?? e));
    } finally {
      setUploading(false);
    }
  };

  // Wire the mtime watcher whenever AutoSave is on.
  useEffect(() => {
    if (!autoSave) return;
    const evt = `sftp:external-saved:${session.id}`;
    const handler = (data: any) => {
      const mt = Number(data?.modTime ?? 0);
      if (mt && mt === lastModRef.current) return;
      lastModRef.current = mt;
      reupload();
    };
    EventsOn(evt, handler);
    SFTPWatchTemp(session.id, session.tempPath).catch((e: any) => setError(String(e?.message ?? e)));
    return () => {
      EventsOff(evt);
      SFTPUnwatchTemp(session.id).catch(() => { /* ignore */ });
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [autoSave, session.id, session.tempPath]);

  const discard = async () => {
    try { await SFTPCleanupTemp(session.tempPath); } catch { /* ignore */ }
    onDone(session.id);
  };

  return (
    <div className="rounded-md border border-white/15 bg-black/40 backdrop-blur px-3 py-2 flex flex-col gap-1.5 text-[11px] shadow-lg w-[340px]">
      <div className="flex items-center gap-1.5">
        <ExternalLink className="w-3 h-3 opacity-60" />
        <span className="font-mono truncate flex-1" title={session.remotePath}>{basename(session.remotePath)}</span>
        <button onClick={discard} title={t("ssh.sftp.external.discardTooltip")} className="p-1 rounded hover:bg-white/10">
          <Trash2 className="w-3 h-3" />
        </button>
      </div>
      <div className="text-[10px] opacity-50 font-mono truncate" title={session.tempPath}>{session.tempPath}</div>

      {error && (
        <div className="flex items-center gap-1 text-red-300/90">
          <AlertTriangle className="w-3 h-3" />
          <span className="flex-1 break-all">{error}</span>
        </div>
      )}

      <div className="flex items-center gap-2 mt-1">
        <label className="flex items-center gap-1.5 cursor-pointer select-none">
          <span className="opacity-70">{t("ssh.sftp.autoSaveLabel")}</span>
          <span
            onClick={() => setAutoSave((v) => !v)}
            className={`w-8 h-4 rounded-full relative transition-colors ${autoSave ? "bg-accent/70" : "bg-white/20"}`}
            role="switch"
            aria-checked={autoSave}
          >
            <span className={`absolute top-0.5 ${autoSave ? "left-[16px]" : "left-0.5"} w-3 h-3 rounded-full bg-white transition-all`} />
          </span>
        </label>
        <span className="text-[10px] opacity-50 flex-1 truncate">
          {uploading
            ? <span className="inline-flex items-center gap-1"><Loader2 className="w-3 h-3 animate-spin" /> {t("ssh.sftp.external.uploadingStatus")}</span>
            : lastUpAt
              ? <span className="inline-flex items-center gap-1"><Check className="w-3 h-3 text-emerald-300" /> {formatDate(lastUpAt, { timeStyle: "medium" })}</span>
              : autoSave ? t("ssh.sftp.external.watchingStatus") : t("ssh.sftp.external.editAndClickHint")}
        </span>
        <button
          onClick={reupload}
          disabled={uploading}
          className="px-2 py-0.5 text-[10px] rounded border border-accent/60 bg-accent/20 hover:bg-accent/30 disabled:opacity-30 flex items-center gap-1"
        >
          <Upload className="w-3 h-3" /> {t("ssh.sftp.external.reuploadButton")}
        </button>
      </div>
    </div>
  );
}
