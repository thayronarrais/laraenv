import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import clsx from "clsx";
import { X, Upload, FileKey, Loader2 } from "lucide-react";
// @ts-ignore
import { SaveSSHKeyText, PickAndReadSSHKeyFile } from "../../wailsjs/go/main/App";
import { GeneratedKey } from "./SSHKeyGenModal";
import { useT } from "../lib/i18n";

// Modal for importing an existing SSH key into a host. Three input paths:
//   1. Paste private (and optionally public) into textareas
//   2. Click "Import from key file" → OS file picker → fills textareas
//   3. Drag-and-drop a key file onto the dropzone → fills textareas
//
// Saving validates the private key parses; the public key is derived from
// the private one if left blank.
export default function SSHImportKeyModal({
  open, host, onClose, onImported, flash,
}: {
  open: boolean;
  host: string;
  onClose: () => void;
  onImported: (k: GeneratedKey) => void;
  flash: (m: string) => void;
}) {
  const t = useT();
  const [name, setName] = useState("id_imported");
  const [privateKey, setPrivateKey] = useState("");
  const [publicKey, setPublicKey] = useState("");
  const [busy, setBusy] = useState(false);
  const [dragOver, setDragOver] = useState(false);
  const dropRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    setName("id_imported");
    setPrivateKey("");
    setPublicKey("");
    setBusy(false);
    setDragOver(false);
  }, [open]);

  if (!open) return null;

  const sniffName = (filename: string) => {
    if (!filename) return;
    const base = filename.replace(/\.pub$/, "");
    if (base) setName(base);
  };

  // Backend file picker — gives us the absolute path's contents in one shot,
  // bypassing browser file-input quirks inside Wails' WebView.
  const importFromFile = async () => {
    setBusy(true);
    try {
      const picked: { filename: string; content: string } | null = await PickAndReadSSHKeyFile();
      if (!picked) return; // cancelled
      if (picked.filename.endsWith(".pub")) {
        setPublicKey(picked.content);
      } else {
        setPrivateKey(picked.content);
        sniffName(picked.filename);
      }
    } catch (e) {
      flash(t("ssh.errors.generic", { message: String(e) }));
    } finally {
      setBusy(false);
    }
  };

  // For drag-drop we read the file contents via the browser FileReader API.
  // We never see the source path — that's fine, we only need the bytes.
  const handleDrop = async (e: React.DragEvent) => {
    e.preventDefault();
    setDragOver(false);
    const file = e.dataTransfer.files?.[0];
    if (!file) return;
    try {
      const text = await file.text();
      if (file.name.endsWith(".pub")) {
        setPublicKey(text);
      } else {
        setPrivateKey(text);
        sniffName(file.name);
      }
    } catch (err) {
      flash(t("ssh.importKeyModal.errors.readFile", { message: String(err) }));
    }
  };

  const save = async () => {
    if (!host) { flash(t("ssh.errors.hostNameRequired")); return; }
    if (!name.trim()) { flash(t("ssh.importKeyModal.errors.filenameRequired")); return; }
    if (!privateKey.trim()) { flash(t("ssh.importKeyModal.errors.privateKeyRequired")); return; }
    setBusy(true);
    try {
      const k: GeneratedKey = await SaveSSHKeyText(host, name.trim(), privateKey, publicKey);
      onImported(k);
    } catch (e) {
      flash(t("ssh.errors.generic", { message: String(e) }));
    } finally {
      setBusy(false);
    }
  };

  return createPortal(
    <div className="modal-backdrop">
      <div
        onClick={(e) => e.stopPropagation()}
        className="modal-card w-[640px] max-w-[95vw] max-h-[92vh] flex flex-col p-5"
      >
        <header className="flex items-center justify-between mb-4">
          <div>
            <h2 className="text-base font-semibold flex items-center gap-2">
              <FileKey className="w-4 h-4" /> {t("ssh.importKeyModal.title")}
            </h2>
            <div className="text-[11px] opacity-50 mt-0.5">
              {t("ssh.importKeyModal.hostPrefix")}<code>{host || t("ssh.importKeyModal.noneFallback")}</code> {t("ssh.importKeyModal.storedAt", { host })}
            </div>
          </div>
          <button onClick={onClose} disabled={busy} className="p-1 rounded-md hover:bg-white/10 disabled:opacity-30">
            <X className="w-4 h-4" />
          </button>
        </header>

        <div className="flex flex-col gap-3 overflow-y-auto pr-1">
          <label className="flex flex-col gap-1">
            <span className="text-[11px] uppercase tracking-wider opacity-60">{t("ssh.importKeyModal.filenameLabel")}</span>
            <input
              value={name}
              onChange={(e) => setName(e.target.value)}
              className="input"
              placeholder="id_ed25519, id_rsa…"
              disabled={busy}
            />
          </label>

          <label className="flex flex-col gap-1">
            <span className="text-[11px] uppercase tracking-wider text-emerald-300">
              {t("ssh.importKeyModal.privateKeyLabel")} <span className="opacity-50">*</span>
            </span>
            <textarea
              value={privateKey}
              onChange={(e) => setPrivateKey(e.target.value)}
              className="input font-mono text-[10px] h-32 resize-none"
              placeholder={"-----BEGIN OPENSSH PRIVATE KEY-----\n...\n-----END OPENSSH PRIVATE KEY-----"}
              disabled={busy}
              spellCheck={false}
            />
          </label>

          <label className="flex flex-col gap-1">
            <span className="text-[11px] uppercase tracking-wider opacity-60">{t("ssh.importKeyModal.publicKeyLabel")}</span>
            <textarea
              value={publicKey}
              onChange={(e) => setPublicKey(e.target.value)}
              className="input font-mono text-[10px] h-20 resize-none"
              placeholder={t("ssh.importKeyModal.publicKeyPlaceholder")}
              disabled={busy}
              spellCheck={false}
            />
          </label>

          {/* Drag-drop zone + file picker */}
          <div
            ref={dropRef}
            onDragOver={(e) => { e.preventDefault(); setDragOver(true); }}
            onDragLeave={() => setDragOver(false)}
            onDrop={handleDrop}
            className={clsx(
              "mt-2 rounded-xl border-2 border-dashed p-5 flex flex-col items-center gap-3 transition-colors",
              dragOver
                ? "border-accent bg-accent/10"
                : "border-white/15 bg-white/[0.02]",
            )}
          >
            <Upload className={clsx("w-6 h-6", dragOver ? "text-accent2" : "opacity-40")} />
            <div className="text-[11px] opacity-60 text-center">
              {t("ssh.importKeyModal.dragDropText")}
            </div>
            <button
              onClick={importFromFile}
              disabled={busy}
              className="px-4 py-1.5 rounded-lg bg-emerald-500/30 hover:bg-emerald-500/50 border border-emerald-400/50 text-sm text-emerald-50 flex items-center gap-2 disabled:opacity-50"
            >
              {busy ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Upload className="w-3.5 h-3.5" />}
              {t("ssh.importKeyModal.importFromFileButton")}
            </button>
          </div>
        </div>

        <div className="flex justify-end gap-2 mt-4">
          <button
            onClick={onClose}
            disabled={busy}
            className="px-4 py-1.5 rounded-lg border border-white/10 bg-white/5 hover:bg-white/10 text-sm disabled:opacity-50"
          >
            {t("common.cancel")}
          </button>
          <button
            onClick={save}
            disabled={busy || !privateKey.trim()}
            className="px-4 py-1.5 rounded-lg bg-accent/30 border border-accent text-sm hover:bg-accent/50 disabled:opacity-50 flex items-center gap-2"
          >
            {busy && <Loader2 className="w-3.5 h-3.5 animate-spin" />}
            {t("ssh.importKeyModal.saveKeyButton")}
          </button>
        </div>
      </div>
    </div>,
    document.body,
  );
}
