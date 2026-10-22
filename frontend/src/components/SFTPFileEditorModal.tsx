import { useEffect, useMemo, useRef, useState } from "react";
import { useDialogs } from "./dialogs/DialogProvider";
import { createPortal } from "react-dom";
import { X, Save, RotateCcw, AlertTriangle, Loader2, FileWarning } from "lucide-react";
import Editor, { OnMount } from "@monaco-editor/react";
// @ts-ignore
import { SFTPReadFile, SFTPWriteFile } from "../../wailsjs/go/main/App";
import { useT, useI18n } from "../lib/i18n";
import { languageForPath } from "../lib/fileLanguage";

const AUTOSAVE_KEY = "lara4ever:sftp-editor:autosave";
const AUTOSAVE_DEBOUNCE_MS = 800;

function basename(p: string): string {
  if (!p) return "";
  const t = p.endsWith("/") ? p.slice(0, -1) : p;
  const i = t.lastIndexOf("/");
  return i < 0 ? t : t.slice(i + 1);
}

export default function SFTPFileEditorModal({
  open, termId, remotePath, onClose, onFallbackExternal,
}: {
  open: boolean;
  termId: string;
  remotePath: string;
  onClose: () => void;
  // Invoked when the file is too large or binary — caller should switch to
  // the external-editor flow for the same path.
  onFallbackExternal: (path: string) => void;
}) {
  const [content, setContent] = useState("");
  const dialogs = useDialogs();
  const t = useT();
  const { formatDate } = useI18n();
  const [original, setOriginal] = useState("");
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [fallback, setFallback] = useState<{ reason: "too-large" | "binary"; message: string } | null>(null);
  const [autoSave, setAutoSave] = useState<boolean>(() => {
    try { return localStorage.getItem(AUTOSAVE_KEY) === "1"; } catch { return false; }
  });
  const [lastSavedAt, setLastSavedAt] = useState<number | null>(null);
  const editorRef = useRef<any>(null);
  const autoSaveTimer = useRef<number | null>(null);
  const latestContent = useRef("");
  const latestOriginal = useRef("");

  const language = useMemo(() => languageForPath(remotePath), [remotePath]);

  useEffect(() => {
    try { localStorage.setItem(AUTOSAVE_KEY, autoSave ? "1" : "0"); } catch { /* ignore */ }
  }, [autoSave]);

  // Initial load.
  useEffect(() => {
    if (!open || !remotePath) return;
    let cancelled = false;
    setLoading(true);
    setError(null);
    setFallback(null);
    SFTPReadFile(termId, remotePath)
      .then((c: string) => {
        if (cancelled) return;
        setContent(c);
        setOriginal(c);
        latestContent.current = c;
        latestOriginal.current = c;
      })
      .catch((e: any) => {
        if (cancelled) return;
        const msg = String(e?.message ?? e);
        if (msg.includes("file too large")) {
          setFallback({ reason: "too-large", message: msg });
        } else if (msg.includes("binary file")) {
          setFallback({ reason: "binary", message: msg });
        } else {
          setError(msg);
        }
      })
      .finally(() => { if (!cancelled) setLoading(false); });
    return () => { cancelled = true; };
  }, [open, termId, remotePath]);

  const doSave = async () => {
    const value = latestContent.current;
    setSaving(true);
    setError(null);
    try {
      await SFTPWriteFile(termId, remotePath, value);
      setOriginal(value);
      latestOriginal.current = value;
      setLastSavedAt(Date.now());
    } catch (e: any) {
      setError(String(e?.message ?? e));
    } finally {
      setSaving(false);
    }
  };

  const onEditorMount: OnMount = (editor, monaco) => {
    editorRef.current = editor;
    editor.addCommand(monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyS, () => { doSave(); });
  };

  const onChange = (v: string | undefined) => {
    const val = v ?? "";
    setContent(val);
    latestContent.current = val;
    if (autoSave) {
      if (autoSaveTimer.current) window.clearTimeout(autoSaveTimer.current);
      autoSaveTimer.current = window.setTimeout(() => {
        if (latestContent.current !== latestOriginal.current) doSave();
      }, AUTOSAVE_DEBOUNCE_MS);
    }
  };

  // If autosave is toggled on while there are unsaved changes, flush now.
  useEffect(() => {
    if (autoSave && latestContent.current !== latestOriginal.current) {
      doSave();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [autoSave]);

  useEffect(() => () => {
    if (autoSaveTimer.current) window.clearTimeout(autoSaveTimer.current);
  }, []);

  if (!open) return null;

  const dirty = content !== original;

  const reset = async () => {
    if (!(await dialogs.confirm({ title: t("ssh.sftp.editor.resetConfirm.title"), confirmLabel: t("ssh.sftp.editor.discardLabel"), danger: true }))) return;
    setContent(original);
    latestContent.current = original;
  };

  const close = async () => {
    if (dirty && !autoSave && !(await dialogs.confirm({ title: t("ssh.sftp.editor.closeConfirm.title"), confirmLabel: t("ssh.sftp.editor.discardLabel"), danger: true }))) return;
    if (autoSaveTimer.current) window.clearTimeout(autoSaveTimer.current);
    onClose();
  };

  return createPortal(
    <div className="modal-backdrop">
      <div
        onClick={(e) => e.stopPropagation()}
        className="modal-card flex flex-col p-3"
        style={{ width: "min(1200px, 95vw)", height: "min(820px, 92vh)" }}
      >
        <header className="flex items-center gap-2 mb-2 pl-1">
          <FileWarning className="w-4 h-4 opacity-0" /> {/* spacer for alignment */}
          <h2 className="text-sm font-semibold flex-1 min-w-0">
            <span className="opacity-60">{t("ssh.sftp.editor.editingPrefix")}</span>
            <span className="font-mono opacity-90 truncate inline-block max-w-[60ch] align-bottom" title={remotePath}>{basename(remotePath)}</span>
            <span className="opacity-50 font-mono text-[11px] ml-2 truncate" title={remotePath}>· {remotePath}</span>
            {dirty && <span className="ml-2 text-[10px] uppercase tracking-wider text-amber-300/80">{t("ssh.sftp.editor.unsavedBadge")}</span>}
          </h2>
          <button onClick={close} className="p-1 rounded-md hover:bg-white/10"><X className="w-4 h-4" /></button>
        </header>

        {error && (
          <div className="mb-2 px-3 py-1.5 text-[11px] rounded-md border border-red-400/40 bg-red-500/10 text-red-100 flex items-center gap-2">
            <AlertTriangle className="w-3 h-3" />
            <span className="flex-1 break-all">{error}</span>
            <button onClick={() => setError(null)} className="opacity-60 hover:opacity-100"><X className="w-3 h-3" /></button>
          </div>
        )}

        {fallback ? (
          <div className="flex-1 flex flex-col items-center justify-center gap-3 text-xs">
            <FileWarning className="w-8 h-8 text-amber-300/70" />
            <div className="opacity-80 max-w-md text-center">
              {fallback.reason === "too-large"
                ? t("ssh.sftp.editor.tooLarge")
                : t("ssh.sftp.editor.binaryFile")}
              <div className="opacity-50 mt-1 font-mono break-all">{fallback.message}</div>
            </div>
            <button
              onClick={() => { onFallbackExternal(remotePath); onClose(); }}
              className="px-3 py-1.5 rounded-md border border-accent/60 bg-accent/20 hover:bg-accent/30 text-xs"
            >
              {t("ssh.sftp.openSystemEditorButton")}
            </button>
          </div>
        ) : loading ? (
          <div className="flex-1 flex items-center justify-center text-xs opacity-70">
            <Loader2 className="w-4 h-4 animate-spin mr-2" /> {t("ssh.sftp.editor.loading")}
          </div>
        ) : (
          <div className="flex-1 min-h-0 rounded-lg overflow-hidden border border-white/10">
            <Editor
              height="100%"
              theme="vs-dark"
              language={language}
              value={content}
              onChange={onChange}
              onMount={onEditorMount}
              options={{
                fontSize: 13,
                minimap: { enabled: true },
                automaticLayout: true,
                tabSize: 2,
                wordWrap: "on",
                scrollBeyondLastLine: false,
              }}
            />
          </div>
        )}

        <footer className="mt-2 flex items-center gap-3">
          <label className="flex items-center gap-2 text-[11px] cursor-pointer select-none">
            <span className="opacity-70">{t("ssh.sftp.autoSaveLabel")}</span>
            <span
              onClick={() => setAutoSave((v) => !v)}
              className={`w-9 h-5 rounded-full relative transition-colors ${autoSave ? "bg-accent/70" : "bg-white/20"}`}
              role="switch"
              aria-checked={autoSave}
            >
              <span
                className={`absolute top-0.5 ${autoSave ? "left-[18px]" : "left-0.5"} w-4 h-4 rounded-full bg-white transition-all`}
              />
            </span>
          </label>
          <span className="text-[10px] opacity-50 flex-1">
            {saving
              ? t("ssh.sftp.editor.savingStatus")
              : lastSavedAt
                ? t("ssh.sftp.editor.savedAt", { time: formatDate(lastSavedAt, { timeStyle: "medium" }) })
                : autoSave ? t("ssh.sftp.editor.autoSaveOnStatus") : t("ssh.sftp.editor.ctrlSHint")}
          </span>
          <button
            onClick={reset}
            disabled={!dirty}
            className="px-2.5 py-1 text-[11px] rounded-md border border-white/10 hover:bg-white/10 disabled:opacity-30 flex items-center gap-1"
          >
            <RotateCcw className="w-3 h-3" /> {t("ssh.sftp.editor.resetButton")}
          </button>
          <button
            onClick={doSave}
            disabled={!dirty || saving}
            className="px-3 py-1 text-[11px] rounded-md border border-accent/60 bg-accent/20 hover:bg-accent/30 disabled:opacity-30 flex items-center gap-1"
          >
            <Save className="w-3 h-3" /> {t("common.save")}
          </button>
        </footer>
      </div>
    </div>,
    document.body,
  );
}
