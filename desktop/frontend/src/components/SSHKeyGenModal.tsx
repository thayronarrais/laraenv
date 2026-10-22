import { useEffect, useState } from "react";
import { createPortal } from "react-dom";
import clsx from "clsx";
import { X, KeyRound, Loader2, Copy, Check } from "lucide-react";
// @ts-ignore
import { GenerateSSHKey } from "../../wailsjs/go/main/App";
import { useT } from "../lib/i18n";

type Kind = "ed25519" | "rsa4096" | "ecdsa256";

const KIND_IDS: Kind[] = ["ed25519", "rsa4096", "ecdsa256"];
const KIND_META: Record<Kind, { label: string; defaultName: string }> = {
  ed25519: { label: "ED25519", defaultName: "id_ed25519" },
  rsa4096: { label: "RSA 4096", defaultName: "id_rsa" },
  ecdsa256: { label: "ECDSA P-256", defaultName: "id_ecdsa" },
};
const KIND_DESC_KEYS: Record<Kind, string> = {
  ed25519: "ssh.keyGenModal.kinds.ed25519.desc",
  rsa4096: "ssh.keyGenModal.kinds.rsa4096.desc",
  ecdsa256: "ssh.keyGenModal.kinds.ecdsa256.desc",
};

export type GeneratedKey = {
  privatePath: string;
  publicPath: string;
  publicKey: string;
};

export default function SSHKeyGenModal({
  open, host, onClose, onGenerated, flash,
}: {
  open: boolean;
  host: string;
  onClose: () => void;
  onGenerated: (k: GeneratedKey) => void;
  flash: (m: string) => void;
}) {
  const t = useT();
  const [kind, setKind] = useState<Kind>("ed25519");
  const [keyName, setKeyName] = useState("id_ed25519");
  const [comment, setComment] = useState("");
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<GeneratedKey | null>(null);
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    if (!open) return;
    setKind("ed25519");
    setKeyName("id_ed25519");
    setComment(`lara4ever@${host}`);
    setResult(null);
    setBusy(false);
    setCopied(false);
  }, [open, host]);

  // Sync the suggested filename with the selected algorithm.
  useEffect(() => {
    const def = KIND_META[kind]?.defaultName ?? "id_key";
    setKeyName(def);
  }, [kind]);

  if (!open) return null;

  const generate = async () => {
    if (!host) { flash(t("ssh.errors.hostNameRequired")); return; }
    if (!keyName.trim()) { flash(t("ssh.keyGenModal.errors.filenameRequired")); return; }
    setBusy(true);
    try {
      const gk: GeneratedKey = await GenerateSSHKey(host, keyName.trim(), kind, comment.trim());
      setResult(gk);
    } catch (e) {
      flash(t("ssh.errors.generic", { message: String(e) }));
    } finally {
      setBusy(false);
    }
  };

  const copy = async () => {
    if (!result) return;
    try {
      await navigator.clipboard.writeText(result.publicKey);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch { flash(t("ssh.keyGenModal.errors.copyFailed")); }
  };

  return createPortal(
    <div className="modal-backdrop">
      <div
        onClick={(e) => e.stopPropagation()}
        className="modal-card w-[560px] max-w-[92vw] flex flex-col p-5"
      >
        <header className="flex items-center justify-between mb-4">
          <h2 className="text-base font-semibold flex items-center gap-2">
            <KeyRound className="w-4 h-4" /> {t("ssh.keyGenModal.title")}
          </h2>
          <button onClick={onClose} disabled={busy} className="p-1 rounded-md hover:bg-white/10 disabled:opacity-30">
            <X className="w-4 h-4" />
          </button>
        </header>

        {!result ? (
          <>
            <div className="grid grid-cols-3 gap-2 mb-4">
              {KIND_IDS.map((id) => (
                <button
                  key={id}
                  onClick={() => setKind(id)}
                  disabled={busy}
                  className={clsx(
                    "p-3 rounded-lg border text-left transition-colors",
                    kind === id
                      ? "bg-accent/25 border-accent text-white"
                      : "bg-white/5 border-white/10 hover:bg-white/10 text-white/80",
                  )}
                >
                  <div className="text-sm font-medium">{KIND_META[id].label}</div>
                  <div className="text-[10px] opacity-60 mt-0.5">{t(KIND_DESC_KEYS[id])}</div>
                </button>
              ))}
            </div>

            <div className="grid grid-cols-2 gap-3 mb-4">
              <label className="flex flex-col gap-1">
                <span className="text-[11px] uppercase tracking-wider opacity-60">{t("ssh.keyGenModal.filenameLabel")}</span>
                <input
                  value={keyName}
                  onChange={(e) => setKeyName(e.target.value)}
                  className="input"
                  disabled={busy}
                />
              </label>
              <label className="flex flex-col gap-1">
                <span className="text-[11px] uppercase tracking-wider opacity-60">{t("ssh.keyGenModal.commentLabel")}</span>
                <input
                  value={comment}
                  onChange={(e) => setComment(e.target.value)}
                  className="input"
                  disabled={busy}
                />
              </label>
            </div>

            <div className="text-[10px] opacity-50 mb-4">
              {t("ssh.keyGenModal.writeHintPrefix")}<code>C:\laraenv\ssh\{host || "<host>"}\</code>{t("ssh.keyGenModal.writeHintSuffix")}
            </div>

            <div className="flex justify-end gap-2">
              <button
                onClick={onClose}
                disabled={busy}
                className="px-4 py-1.5 rounded-lg border border-white/10 bg-white/5 hover:bg-white/10 text-sm disabled:opacity-50"
              >
                {t("common.cancel")}
              </button>
              <button
                onClick={generate}
                disabled={busy}
                className="px-4 py-1.5 rounded-lg bg-accent/30 border border-accent text-sm hover:bg-accent/50 disabled:opacity-50 flex items-center gap-2"
              >
                {busy && <Loader2 className="w-3.5 h-3.5 animate-spin" />}
                {t("ssh.keyGenModal.generateButton")}
              </button>
            </div>
          </>
        ) : (
          <>
            <div className="text-xs opacity-70 mb-2">
              {t("ssh.keyGenModal.resultIntroPrefix")}<code className="text-accent2">{result.privatePath}</code>{t("ssh.keyGenModal.resultIntroMiddle")}<code>~/.ssh/authorized_keys</code>{t("ssh.keyGenModal.resultIntroSuffix")}
            </div>
            <div className="relative">
              <textarea
                readOnly
                value={result.publicKey}
                className="input font-mono text-[10px] h-28 resize-none w-full pr-10"
                onClick={(e) => (e.target as HTMLTextAreaElement).select()}
              />
              <button
                onClick={copy}
                className="absolute top-2 right-2 p-1.5 rounded-md bg-white/10 hover:bg-white/20"
                title={t("ssh.keyGenModal.copyTooltip")}
              >
                {copied ? <Check className="w-3.5 h-3.5 text-emerald-300" /> : <Copy className="w-3.5 h-3.5" />}
              </button>
            </div>
            <div className="flex justify-end gap-2 mt-4">
              <button
                onClick={() => onGenerated(result)}
                className="px-4 py-1.5 rounded-lg bg-accent/30 border border-accent text-sm hover:bg-accent/50"
              >
                {t("ssh.keyGenModal.useThisKeyButton")}
              </button>
            </div>
          </>
        )}
      </div>
    </div>,
    document.body,
  );
}
