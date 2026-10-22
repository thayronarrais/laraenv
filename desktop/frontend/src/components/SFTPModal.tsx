import { useEffect, useMemo, useRef, useState } from "react";
import { useDialogs } from "./dialogs/DialogProvider";
import { createPortal } from "react-dom";
import clsx from "clsx";
import {
  X, RefreshCw, ArrowUp, ArrowRight, ArrowLeft, FolderPlus, Trash2, Pencil,
  Folder, FileText, Loader2, AlertTriangle, Server, Monitor, FileCode, ExternalLink,
} from "lucide-react";
// @ts-ignore
import {
  SFTPOpen, SFTPListLocal, SFTPListRemote, SFTPHomeLocal,
  SFTPMkdirRemote, SFTPRenameRemote, SFTPRemoveRemote,
  SFTPUpload, SFTPDownload, SFTPCancel,
  SFTPOpenExternal, SFTPCleanupTemp,
} from "../../wailsjs/go/main/App";
import SFTPFileEditorModal from "./SFTPFileEditorModal";
import SFTPExternalEditSession, { ExternalSession } from "./SFTPExternalEditSession";
// @ts-ignore
import { EventsOn, EventsOff } from "../../wailsjs/runtime";
import { useT, useI18n } from "../lib/i18n";

type Entry = {
  name: string;
  path: string;
  size: number;
  modTime: number;
  isDir: boolean;
  mode: string;
};

type Side = "local" | "remote";

// Persist last-visited paths per SSH session (keyed by termId) across open/close cycles.
const sftpSessionPaths = new Map<string, { local: string; remote: string }>();

type Transfer = {
  opId: string;
  direction: "upload" | "download";
  label: string;
  bytes: number;
  total: number;
  current: string;
  done: boolean;
  error?: string;
};

function formatSize(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  if (n < 1024 * 1024 * 1024) return `${(n / 1024 / 1024).toFixed(1)} MB`;
  return `${(n / 1024 / 1024 / 1024).toFixed(2)} GB`;
}

// POSIX-style parent of a remote path. Local paths use the JS-side '..' helper.
function remoteParent(p: string): string {
  if (!p || p === "/") return "/";
  const trimmed = p.endsWith("/") ? p.slice(0, -1) : p;
  const idx = trimmed.lastIndexOf("/");
  if (idx <= 0) return "/";
  return trimmed.slice(0, idx);
}

// Local parent — fall back to walking via separator detection so it works on
// both Windows (`C:\foo\bar`) and POSIX paths.
function localParent(p: string): string {
  const winSep = p.includes("\\") && !p.includes("/");
  const sep = winSep ? "\\" : "/";
  const trimmed = p.endsWith(sep) ? p.slice(0, -1) : p;
  const idx = trimmed.lastIndexOf(sep);
  if (idx < 0) return p;
  // Preserve drive root on Windows (e.g. C:\)
  if (winSep && idx === 2 && trimmed[1] === ":") return trimmed.slice(0, 3);
  if (idx === 0) return "/";
  return trimmed.slice(0, idx);
}

function joinPath(parent: string, name: string, side: Side): string {
  if (side === "remote") {
    if (parent.endsWith("/")) return parent + name;
    return parent + "/" + name;
  }
  const winSep = parent.includes("\\") && !parent.includes("/");
  const sep = winSep ? "\\" : "/";
  if (parent.endsWith(sep)) return parent + name;
  return parent + sep + name;
}

function basename(p: string, side: Side): string {
  if (side === "remote") {
    const t = p.endsWith("/") ? p.slice(0, -1) : p;
    const i = t.lastIndexOf("/");
    return i < 0 ? t : t.slice(i + 1);
  }
  const winSep = p.includes("\\") && !p.includes("/");
  const sep = winSep ? "\\" : "/";
  const t = p.endsWith(sep) ? p.slice(0, -1) : p;
  const i = t.lastIndexOf(sep);
  return i < 0 ? t : t.slice(i + 1);
}

export default function SFTPModal({
  open, termId, hostLabel, onClose,
}: {
  open: boolean;
  termId: string;
  hostLabel: string;
  onClose: () => void;
}) {
  const [localPath, setLocalPath] = useState("");
  const dialogs = useDialogs();
  const t = useT();
  const [remotePath, setRemotePath] = useState("");
  const [localEntries, setLocalEntries] = useState<Entry[]>([]);
  const [remoteEntries, setRemoteEntries] = useState<Entry[]>([]);
  const [localSel, setLocalSel] = useState<Set<string>>(new Set());
  const [remoteSel, setRemoteSel] = useState<Set<string>>(new Set());
  const [focused, setFocused] = useState<Side>("local");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [transfers, setTransfers] = useState<Transfer[]>([]);
  const [editor, setEditor] = useState<{ path: string } | null>(null);
  const [editChooser, setEditChooser] = useState<{ path: string } | null>(null);
  const [externalSessions, setExternalSessions] = useState<ExternalSession[]>([]);
  const [ctxMenu, setCtxMenu] = useState<{ x: number; y: number; entry: Entry } | null>(null);

  const opCounter = useRef(0);
  const externalCounter = useRef(0);

  // Open SFTP + load both sides on first open. Restore last-visited paths when
  // reopening the same session so the user lands back where they left off.
  useEffect(() => {
    if (!open) return;
    let cancelled = false;
    (async () => {
      setLoading(true);
      setError(null);
      try {
        const saved = sftpSessionPaths.get(termId);
        if (saved) {
          await SFTPOpen(termId);
          if (cancelled) return;
          setLocalPath(saved.local);
          setRemotePath(saved.remote);
        } else {
          const [home, remoteHome] = await Promise.all([
            SFTPHomeLocal() as Promise<string>,
            SFTPOpen(termId) as Promise<string>,
          ]);
          if (cancelled) return;
          setLocalPath(home);
          setRemotePath(remoteHome || "/");
        }
      } catch (e: any) {
        if (!cancelled) setError(String(e?.message ?? e));
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => { cancelled = true; };
  }, [open, termId]);

  const refreshLocal = async (p?: string) => {
    const target = p ?? localPath;
    if (!target) return;
    try {
      const list: Entry[] = await SFTPListLocal(target);
      setLocalEntries(list);
      if (p) setLocalPath(p);
      setLocalSel(new Set());
    } catch (e: any) {
      setError(t("ssh.sftp.errors.local", { message: String(e?.message ?? e) }));
    }
  };
  const refreshRemote = async (p?: string) => {
    const target = p ?? remotePath;
    if (!target) return;
    try {
      const list: Entry[] = await SFTPListRemote(termId, target);
      setRemoteEntries(list);
      if (p) setRemotePath(p);
      setRemoteSel(new Set());
    } catch (e: any) {
      setError(t("ssh.sftp.errors.remote", { message: String(e?.message ?? e) }));
    }
  };

  useEffect(() => { if (localPath) refreshLocal(); /* eslint-disable-next-line */ }, [localPath]);
  useEffect(() => { if (remotePath) refreshRemote(); /* eslint-disable-next-line */ }, [remotePath]);

  // Keep the session path map in sync so reopening lands back here.
  useEffect(() => {
    if (localPath && remotePath) sftpSessionPaths.set(termId, { local: localPath, remote: remotePath });
  }, [termId, localPath, remotePath]);

  // ---- transfers ----

  const startTransfer = async (direction: "upload" | "download") => {
    const sel = direction === "upload" ? localSel : remoteSel;
    if (sel.size === 0) return;
    const entries = Array.from(sel);
    for (const srcPath of entries) {
      opCounter.current += 1;
      const opId = `sftp-op-${termId}-${Date.now()}-${opCounter.current}`;
      const name = basename(srcPath, direction === "upload" ? "local" : "remote");
      const dst =
        direction === "upload"
          ? joinPath(remotePath, name, "remote")
          : joinPath(localPath, name, "local");
      const t: Transfer = {
        opId, direction, label: name, bytes: 0, total: -1, current: "", done: false,
      };
      setTransfers((prev) => [...prev, t]);

      const evt = `sftp:progress:${opId}`;
      const handler = (p: any) => {
        setTransfers((prev) =>
          prev.map((x) =>
            x.opId !== opId
              ? x
              : { ...x, bytes: p.bytes ?? x.bytes, total: p.total ?? x.total, current: p.current ?? x.current, done: !!p.done, error: p.error },
          ),
        );
        if (p.done) {
          EventsOff(evt);
          // After completion, refresh the destination side so the new file appears.
          if (direction === "upload") refreshRemote(); else refreshLocal();
        }
      };
      EventsOn(evt, handler);

      try {
        if (direction === "upload") {
          await SFTPUpload(termId, opId, srcPath, dst);
        } else {
          await SFTPDownload(termId, opId, srcPath, dst);
        }
      } catch (e: any) {
        EventsOff(evt);
        setTransfers((prev) =>
          prev.map((x) => (x.opId === opId ? { ...x, done: true, error: String(e?.message ?? e) } : x)),
        );
      }
    }
  };

  const cancelTransfer = (opId: string) => { SFTPCancel(opId).catch(() => {}); };
  const dismissTransfer = (opId: string) => {
    setTransfers((prev) => prev.filter((t) => t.opId !== opId));
  };

  // ---- remote management ----

  const mkdir = async (side: Side) => {
    const name = await dialogs.prompt({ title: side === "remote" ? t("ssh.sftp.mkdir.remoteTitle") : t("ssh.sftp.mkdir.localTitle"), label: t("ssh.sftp.mkdir.folderNameLabel") });
    if (!name) return;
    if (side === "remote") {
      try {
        await SFTPMkdirRemote(termId, joinPath(remotePath, name, "remote"));
        await refreshRemote();
      } catch (e: any) { setError(String(e?.message ?? e)); }
    } else {
      // Local mkdir is rare in this UI; skip for v1.
    }
  };

  const removeSelected = async (side: Side) => {
    if (side !== "remote") return; // local removal not supported in v1
    const sel = Array.from(remoteSel);
    if (sel.length === 0) return;
    if (!(await dialogs.confirm({ title: t("ssh.sftp.removeConfirm.title", { count: sel.length }), message: t("ssh.sftp.removeConfirm.message"), confirmLabel: t("ssh.sftp.deleteTooltip"), danger: true }))) return;
    for (const p of sel) {
      try {
        await SFTPRemoveRemote(termId, p);
      } catch (e: any) {
        setError(String(e?.message ?? e));
      }
    }
    refreshRemote();
  };

  const renameSelected = async (side: Side) => {
    if (side !== "remote") return;
    if (remoteSel.size !== 1) return;
    const oldPath = Array.from(remoteSel)[0];
    const oldName = basename(oldPath, "remote");
    const newName = await dialogs.prompt({ title: t("ssh.sftp.renamePrompt.title"), label: t("ssh.sftp.renamePrompt.label"), defaultValue: oldName });
    if (!newName || newName === oldName) return;
    try {
      const newPath = joinPath(remoteParent(oldPath), newName, "remote");
      await SFTPRenameRemote(termId, oldPath, newPath);
      refreshRemote();
    } catch (e: any) {
      setError(String(e?.message ?? e));
    }
  };

  // ---- pane row click handling ----

  const onRowClick = (side: Side, entry: Entry, ev: React.MouseEvent) => {
    setFocused(side);
    const sel = side === "local" ? localSel : remoteSel;
    const setSel = side === "local" ? setLocalSel : setRemoteSel;
    const next = new Set(sel);
    if (ev.ctrlKey || ev.metaKey) {
      if (next.has(entry.path)) next.delete(entry.path);
      else next.add(entry.path);
    } else {
      next.clear();
      next.add(entry.path);
    }
    setSel(next);
  };

  const onRowDouble = (side: Side, entry: Entry) => {
    if (entry.isDir) {
      if (side === "local") setLocalPath(entry.path);
      else setRemotePath(entry.path);
      return;
    }
    // File double-click on the remote pane opens the built-in editor.
    if (side === "remote") setEditor({ path: entry.path });
  };

  // Edit-button handler for the remote pane: opens a small chooser so the
  // user picks between built-in (Monaco) and the OS default editor.
  const openEditChooser = (forcedPath?: string) => {
    const p = forcedPath ?? (remoteSel.size === 1 ? Array.from(remoteSel)[0] : "");
    if (!p) return;
    const entry = remoteEntries.find((e) => e.path === p);
    if (!entry || entry.isDir) return;
    setEditChooser({ path: p });
  };

  // Right-click on a remote row → context menu (Edit / Rename / Delete).
  const onRowContextMenu = (side: Side, entry: Entry, ev: React.MouseEvent) => {
    if (side !== "remote") return;
    ev.preventDefault();
    setFocused("remote");
    setRemoteSel(new Set([entry.path]));
    setCtxMenu({ x: ev.clientX, y: ev.clientY, entry });
  };

  const launchExternalEditor = async (remoteFilePath: string) => {
    try {
      const tempPath: string = await SFTPOpenExternal(termId, remoteFilePath);
      externalCounter.current += 1;
      const id = `sftp-ext-${termId}-${Date.now()}-${externalCounter.current}`;
      setExternalSessions((prev) => [
        ...prev,
        { id, termId, remotePath: remoteFilePath, tempPath },
      ]);
    } catch (e: any) {
      setError(String(e?.message ?? e));
    }
  };

  const closeExternalSession = (id: string) => {
    setExternalSessions((prev) => prev.filter((s) => s.id !== id));
  };

  // Cleanup any lingering external temp files when the modal closes.
  useEffect(() => {
    if (open) return;
    if (externalSessions.length === 0) return;
    externalSessions.forEach((s) => { SFTPCleanupTemp(s.tempPath).catch(() => {}); });
    setExternalSessions([]);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  if (!open) return null;
  return createPortal(
    <div className="modal-backdrop">
      <div
        onClick={(e) => e.stopPropagation()}
        className="modal-card flex flex-col p-4 relative"
        style={{ width: "min(1100px, 95vw)", height: "min(720px, 88vh)" }}
      >
        <header className="flex items-center justify-between mb-3">
          <h2 className="text-sm font-semibold flex items-center gap-2">
            <Server className="w-4 h-4 opacity-70" />
            SFTP · <span className="font-mono opacity-80">{hostLabel}</span>
          </h2>
          <button onClick={onClose} className="p-1 rounded-md hover:bg-white/10">
            <X className="w-4 h-4" />
          </button>
        </header>

        {error && (
          <div className="mb-2 px-3 py-1.5 text-[11px] rounded-md border border-red-400/40 bg-red-500/10 text-red-100 flex items-center gap-2">
            <AlertTriangle className="w-3 h-3" />
            <span className="flex-1 break-all">{error}</span>
            <button onClick={() => setError(null)} className="opacity-60 hover:opacity-100"><X className="w-3 h-3" /></button>
          </div>
        )}

        {loading ? (
          <div className="flex-1 flex items-center justify-center text-xs opacity-70">
            <Loader2 className="w-4 h-4 animate-spin mr-2" /> {t("ssh.sftp.opening")}
          </div>
        ) : (
          <div className="flex-1 grid grid-cols-[1fr_auto_1fr] gap-3 min-h-0">
            <Pane
              side="local"
              icon={<Monitor className="w-3.5 h-3.5" />}
              title={t("ssh.sftp.localTitle")}
              path={localPath}
              entries={localEntries}
              selected={localSel}
              focused={focused === "local"}
              onPathChange={setLocalPath}
              onUp={() => setLocalPath(localParent(localPath))}
              onRefresh={() => refreshLocal()}
              onRowClick={(e, ev) => onRowClick("local", e, ev)}
              onRowDouble={(e) => onRowDouble("local", e)}
              onMkdir={() => mkdir("local")}
              onRemove={undefined}
              onRename={undefined}
            />

            <div className="flex flex-col items-center justify-center gap-2 px-1">
              <button
                title={t("ssh.sftp.uploadTooltip")}
                disabled={localSel.size === 0}
                onClick={() => startTransfer("upload")}
                className={clsx(
                  "w-9 h-9 rounded-md border flex items-center justify-center transition",
                  localSel.size === 0
                    ? "border-white/10 text-white/30"
                    : "border-accent/60 bg-accent/20 text-white hover:bg-accent/30",
                )}
              >
                <ArrowRight className="w-4 h-4" />
              </button>
              <button
                title={t("ssh.sftp.downloadTooltip")}
                disabled={remoteSel.size === 0}
                onClick={() => startTransfer("download")}
                className={clsx(
                  "w-9 h-9 rounded-md border flex items-center justify-center transition",
                  remoteSel.size === 0
                    ? "border-white/10 text-white/30"
                    : "border-accent/60 bg-accent/20 text-white hover:bg-accent/30",
                )}
              >
                <ArrowLeft className="w-4 h-4" />
              </button>
            </div>

            <Pane
              side="remote"
              icon={<Server className="w-3.5 h-3.5" />}
              title={t("ssh.sftp.remoteTitle")}
              path={remotePath}
              entries={remoteEntries}
              selected={remoteSel}
              focused={focused === "remote"}
              onPathChange={setRemotePath}
              onUp={() => setRemotePath(remoteParent(remotePath))}
              onRefresh={() => refreshRemote()}
              onRowClick={(e, ev) => onRowClick("remote", e, ev)}
              onRowDouble={(e) => onRowDouble("remote", e)}
              onRowContextMenu={(e, ev) => onRowContextMenu("remote", e, ev)}
              onMkdir={() => mkdir("remote")}
              onRemove={() => removeSelected("remote")}
              onRename={() => renameSelected("remote")}
              onEdit={() => openEditChooser()}
              editEnabled={remoteSel.size === 1 && !(remoteEntries.find((e) => e.path === Array.from(remoteSel)[0])?.isDir)}
            />
          </div>
        )}

        {externalSessions.length > 0 && (
          <div className="absolute bottom-4 right-4 flex flex-col gap-2 z-10">
            {externalSessions.map((s) => (
              <SFTPExternalEditSession key={s.id} session={s} onDone={closeExternalSession} />
            ))}
          </div>
        )}

        {ctxMenu && (
          <div
            className="fixed inset-0 z-30"
            onClick={() => setCtxMenu(null)}
            onContextMenu={(e) => { e.preventDefault(); setCtxMenu(null); }}
          >
            <div
              className="absolute rounded-md border border-white/15 bg-zinc-900 shadow-xl py-1 text-[12px] min-w-[160px]"
              style={{ left: ctxMenu.x, top: ctxMenu.y }}
              onClick={(e) => e.stopPropagation()}
            >
              {!ctxMenu.entry.isDir && (
                <button
                  onClick={() => { openEditChooser(ctxMenu.entry.path); setCtxMenu(null); }}
                  className="w-full text-left px-3 py-1.5 hover:bg-white/10 flex items-center gap-2"
                >
                  <FileCode className="w-3.5 h-3.5 opacity-70" /> {t("ssh.sftp.editFileLabel")}
                </button>
              )}
              {!ctxMenu.entry.isDir && (
                <button
                  onClick={() => { setEditor({ path: ctxMenu.entry.path }); setCtxMenu(null); }}
                  className="w-full text-left px-3 py-1.5 hover:bg-white/10 flex items-center gap-2"
                >
                  <FileCode className="w-3.5 h-3.5 opacity-70" /> {t("ssh.sftp.contextMenu.openBuiltIn")}
                </button>
              )}
              {!ctxMenu.entry.isDir && (
                <button
                  onClick={() => { launchExternalEditor(ctxMenu.entry.path); setCtxMenu(null); }}
                  className="w-full text-left px-3 py-1.5 hover:bg-white/10 flex items-center gap-2"
                >
                  <ExternalLink className="w-3.5 h-3.5 opacity-70" /> {t("ssh.sftp.openSystemEditorButton")}
                </button>
              )}
              <div className="border-t border-white/10 my-1" />
              <button
                onClick={() => { renameSelected("remote"); setCtxMenu(null); }}
                className="w-full text-left px-3 py-1.5 hover:bg-white/10 flex items-center gap-2"
              >
                <Pencil className="w-3.5 h-3.5 opacity-70" /> {t("ssh.sftp.renameTooltip")}
              </button>
              <button
                onClick={() => { removeSelected("remote"); setCtxMenu(null); }}
                className="w-full text-left px-3 py-1.5 hover:bg-white/10 hover:text-red-300 flex items-center gap-2"
              >
                <Trash2 className="w-3.5 h-3.5 opacity-70" /> {t("ssh.sftp.deleteTooltip")}
              </button>
            </div>
          </div>
        )}

        {editChooser && (
          <div
            className="absolute inset-0 bg-black/40 z-20 flex items-center justify-center"
            onClick={() => setEditChooser(null)}
          >
            <div
              onClick={(e) => e.stopPropagation()}
              className="rounded-lg border border-white/15 bg-zinc-900 p-4 w-[360px] flex flex-col gap-3"
            >
              <h3 className="text-sm font-semibold">{t("ssh.sftp.editChooser.title")}</h3>
              <p className="text-[11px] opacity-60 font-mono break-all">{editChooser.path}</p>
              <button
                onClick={() => { setEditor({ path: editChooser.path }); setEditChooser(null); }}
                className="px-3 py-2 rounded-md border border-accent/60 bg-accent/20 hover:bg-accent/30 text-xs text-left flex items-start gap-2"
              >
                <FileCode className="w-4 h-4 mt-0.5" />
                <span className="flex-1">
                  <span className="font-medium">{t("ssh.sftp.editChooser.builtInLabel")}</span>
                  <span className="block text-[10px] opacity-70">{t("ssh.sftp.editChooser.builtInDesc")}</span>
                </span>
              </button>
              <button
                onClick={() => { launchExternalEditor(editChooser.path); setEditChooser(null); }}
                className="px-3 py-2 rounded-md border border-white/15 hover:bg-white/10 text-xs text-left flex items-start gap-2"
              >
                <ExternalLink className="w-4 h-4 mt-0.5" />
                <span className="flex-1">
                  <span className="font-medium">{t("ssh.sftp.editChooser.systemLabel")}</span>
                  <span className="block text-[10px] opacity-70">{t("ssh.sftp.editChooser.systemDesc")}</span>
                </span>
              </button>
              <button
                onClick={() => setEditChooser(null)}
                className="text-[11px] opacity-60 hover:opacity-100 self-end"
              >
                {t("ssh.sftp.cancelAction")}
              </button>
            </div>
          </div>
        )}

        {editor && (
          <SFTPFileEditorModal
            open={true}
            termId={termId}
            remotePath={editor.path}
            onClose={() => setEditor(null)}
            onFallbackExternal={(p) => launchExternalEditor(p)}
          />
        )}

        {transfers.length > 0 && (
          <div className="mt-3 border-t border-white/10 pt-2 max-h-40 overflow-y-auto">
            <div className="text-[10px] uppercase tracking-wider opacity-60 mb-1">{t("ssh.sftp.transfers.heading")}</div>
            <ul className="flex flex-col gap-1">
              {transfers.map((tr) => {
                const pct = tr.total > 0 ? Math.min(100, Math.round((tr.bytes / tr.total) * 100)) : 0;
                return (
                  <li key={tr.opId} className="flex items-center gap-2 text-[11px]">
                    <span className="opacity-60 w-16">{tr.direction === "upload" ? t("ssh.sftp.transfers.uploadLabel") : t("ssh.sftp.transfers.downloadLabel")}</span>
                    <span className="font-mono truncate flex-1" title={tr.current || tr.label}>
                      {tr.label}
                      {tr.current && <span className="opacity-50"> · {basename(tr.current, tr.direction === "upload" ? "remote" : "local")}</span>}
                    </span>
                    <span className="opacity-70 w-24 text-right">
                      {formatSize(tr.bytes)}{tr.total > 0 ? ` / ${formatSize(tr.total)}` : ""}
                    </span>
                    <div className="w-24 h-1.5 rounded bg-white/10 overflow-hidden">
                      <div
                        className={clsx(
                          "h-full transition-all",
                          tr.error ? "bg-red-400" : tr.done ? "bg-emerald-400" : "bg-accent",
                        )}
                        style={{ width: `${tr.error ? 100 : pct}%` }}
                      />
                    </div>
                    {!tr.done && !tr.error && (
                      <button
                        onClick={() => cancelTransfer(tr.opId)}
                        className="px-1.5 py-0.5 text-[10px] rounded bg-white/5 hover:bg-white/10"
                      >
                        {t("ssh.sftp.cancelAction")}
                      </button>
                    )}
                    {(tr.done || tr.error) && (
                      <button
                        onClick={() => dismissTransfer(tr.opId)}
                        className="opacity-60 hover:opacity-100"
                        title={tr.error ? t("ssh.errors.generic", { message: tr.error }) : t("ssh.sftp.transfers.dismissTooltip")}
                      >
                        <X className="w-3 h-3" />
                      </button>
                    )}
                  </li>
                );
              })}
            </ul>
          </div>
        )}
      </div>
    </div>,
    document.body,
  );
}

// ---- pane subcomponent ----

function Pane({
  side, icon, title, path, entries, selected, focused,
  onPathChange, onUp, onRefresh,
  onRowClick, onRowDouble, onRowContextMenu,
  onMkdir, onRemove, onRename, onEdit, editEnabled,
}: {
  side: Side;
  icon: React.ReactNode;
  title: string;
  path: string;
  entries: Entry[];
  selected: Set<string>;
  focused: boolean;
  onPathChange: (p: string) => void;
  onUp: () => void;
  onRefresh: () => void;
  onRowClick: (e: Entry, ev: React.MouseEvent) => void;
  onRowDouble: (e: Entry) => void;
  onRowContextMenu?: (e: Entry, ev: React.MouseEvent) => void;
  onMkdir?: () => void;
  onRemove?: () => void;
  onRename?: () => void;
  onEdit?: () => void;
  editEnabled?: boolean;
}) {
  const [draftPath, setDraftPath] = useState(path);
  useEffect(() => { setDraftPath(path); }, [path]);
  const t = useT();
  const { formatDate } = useI18n();

  const sortedEntries = useMemo(() => entries, [entries]);

  return (
    <div className={clsx(
      "flex flex-col min-h-0 rounded-lg border overflow-hidden",
      focused ? "border-accent/60" : "border-white/10",
    )}>
      <div className="flex items-center gap-1 px-2 py-1.5 border-b border-white/10 bg-white/5">
        <span className="opacity-70">{icon}</span>
        <span className="text-[10px] uppercase tracking-wider opacity-60">{title}</span>
        <button
          onClick={onUp}
          className="ml-1 p-1 rounded hover:bg-white/10"
          title={t("ssh.sftp.parentDirTooltip")}
        >
          <ArrowUp className="w-3 h-3" />
        </button>
        <input
          value={draftPath}
          onChange={(e) => setDraftPath(e.target.value)}
          onKeyDown={(e) => { if (e.key === "Enter") onPathChange(draftPath); }}
          className="flex-1 bg-black/20 border border-white/10 rounded px-2 py-0.5 text-[11px] font-mono focus:outline-none focus:border-accent"
        />
        <button onClick={onRefresh} className="p-1 rounded hover:bg-white/10" title={t("ssh.sftp.refreshTooltip")}>
          <RefreshCw className="w-3 h-3" />
        </button>
        {onMkdir && (
          <button onClick={onMkdir} className="p-1 rounded hover:bg-white/10" title={t("ssh.sftp.newFolderTooltip")}>
            <FolderPlus className="w-3 h-3" />
          </button>
        )}
        {onRename && (
          <button onClick={onRename} className="p-1 rounded hover:bg-white/10" title={t("ssh.sftp.renameTooltip")} disabled={selected.size !== 1}>
            <Pencil className={clsx("w-3 h-3", selected.size !== 1 && "opacity-30")} />
          </button>
        )}
        {onEdit && (
          <button
            onClick={onEdit}
            disabled={!editEnabled}
            title={t("ssh.sftp.editFileLabel")}
            className={clsx(
              "ml-1 px-2 py-0.5 rounded-md border text-[10px] uppercase tracking-wider flex items-center gap-1 transition",
              editEnabled
                ? "border-accent/60 bg-accent/20 hover:bg-accent/30 text-white"
                : "border-white/10 text-white/40",
            )}
          >
            <FileCode className="w-3 h-3" /> {t("ssh.sftp.editButton")}
          </button>
        )}
        {onRemove && (
          <button onClick={onRemove} className="p-1 rounded hover:bg-white/10 hover:text-red-300" title={t("ssh.sftp.deleteTooltip")} disabled={selected.size === 0}>
            <Trash2 className={clsx("w-3 h-3", selected.size === 0 && "opacity-30")} />
          </button>
        )}
      </div>

      <div className="flex-1 overflow-y-auto text-[11px] font-mono">
        {sortedEntries.length === 0 && (
          <div className="px-3 py-4 opacity-50">{t("ssh.sftp.emptyPane")}</div>
        )}
        {sortedEntries.map((e) => {
          const isSel = selected.has(e.path);
          return (
            <div
              key={e.path}
              onClick={(ev) => onRowClick(e, ev)}
              onDoubleClick={() => onRowDouble(e)}
              onContextMenu={onRowContextMenu ? (ev) => onRowContextMenu(e, ev) : undefined}
              className={clsx(
                "grid grid-cols-[20px_1fr_80px_120px] items-center gap-2 px-2 py-0.5 cursor-pointer border-l-2 transition-colors",
                isSel
                  ? "bg-accent/40 text-white border-accent shadow-[inset_0_0_0_1px_rgba(124,92,255,0.45)]"
                  : "border-transparent hover:bg-white/10",
              )}
            >
              {e.isDir ? <Folder className="w-3 h-3 text-amber-300" /> : <FileText className="w-3 h-3 opacity-60" />}
              <span className="truncate" title={e.name}>{e.name}</span>
              <span className="text-right opacity-60">{e.isDir ? "" : formatSize(e.size)}</span>
              <span className="text-right opacity-50 text-[10px]">
                {e.modTime ? formatDate(e.modTime, { dateStyle: "short", timeStyle: "short" }) : ""}
              </span>
            </div>
          );
        })}
      </div>
    </div>
  );
}
