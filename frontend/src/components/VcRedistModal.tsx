import { useState } from "react";
import { createPortal } from "react-dom";
import { AlertTriangle, ExternalLink, Copy, Check, X } from "lucide-react";
// @ts-ignore
import { BrowserOpenURL } from "../../wailsjs/runtime";
import { useT } from "../lib/i18n";

const REDIST_URL = "https://aka.ms/vs/17/release/vc_redist.x64.exe";

// REDIST_PROBE is the discriminator the parent uses to decide whether an
// error message warrants opening this modal instead of a transient toast.
// Kept here so consumers don't drift apart on the wording.
export const REDIST_PROBE = "Visual C++ Redistributable";

export default function VcRedistModal({
  open, onClose, binary, redistUrl,
}: {
  open: boolean;
  onClose: () => void;
  binary?: string;
  // The specific Visual C++ redistributable this failure needs, reported
  // by the backend (vcruntime.Check, via ErrorEvent.redistUrl /
  // ProjectCreateError.redistUrl) — NOT re-derived here. Different PHP
  // builds need different ones (e.g. PHP 8.6's vs18 vs. everything else's
  // vs17); defaulting to the vs17 constant only when the caller truly has
  // no better answer keeps this component from ever showing a guessed URL.
  redistUrl?: string;
}) {
  const t = useT();
  const [copied, setCopied] = useState(false);
  const url = redistUrl || REDIST_URL;

  if (!open) return null;

  const copyURL = async () => {
    try {
      await navigator.clipboard.writeText(url);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      /* clipboard API may be unavailable in the WebView; user can still
         click the open-page button to fetch the redist directly. */
    }
  };

  const openPage = () => {
    BrowserOpenURL(url);
  };

  return createPortal(
    <div className="modal-backdrop">
      <div
        onClick={(e) => e.stopPropagation()}
        className="modal-card w-[520px] max-w-[92vw] flex flex-col p-5 gap-4"
      >
        <header className="flex items-start justify-between gap-3">
          <div className="flex items-center gap-2">
            <AlertTriangle className="w-5 h-5 text-amber-300" />
            <h2 className="text-base font-semibold">
              {t("settings.vcRedist.title")}
            </h2>
          </div>
          <button
            onClick={onClose}
            title={t("common.closeTooltip")}
            aria-label={t("common.closeTooltip")}
            className="p-1 rounded-md hover:bg-white/10"
          >
            <X className="w-4 h-4" />
          </button>
        </header>

        <p className="text-sm leading-relaxed opacity-80">
          {binary ? (
            <>
              <span className="font-mono text-xs bg-white/5 border border-white/10 rounded px-1.5 py-0.5">
                {binary}
              </span>{" "}
              {t("settings.vcRedist.binaryReason")}
            </>
          ) : (
            <>
              {t("settings.vcRedist.genericReason")}
            </>
          )}{" "}
          {t("settings.vcRedist.installNote")}
        </p>

        <div className="flex items-stretch gap-2">
          <code className="flex-1 min-w-0 truncate text-[11px] bg-white/5 border border-white/10 rounded-lg px-3 py-2 font-mono">
            {url}
          </code>
          <button
            onClick={copyURL}
            title={t("settings.vcRedist.copyTooltip")}
            aria-label={t("settings.vcRedist.copyTooltip")}
            className="px-3 rounded-lg border border-white/10 bg-white/5 hover:bg-white/10 text-xs flex items-center gap-1.5"
          >
            {copied ? (
              <>
                <Check className="w-3.5 h-3.5 text-emerald-300" /> {t("settings.vcRedist.copiedLabel")}
              </>
            ) : (
              <>
                <Copy className="w-3.5 h-3.5" /> {t("settings.vcRedist.copyLabel")}
              </>
            )}
          </button>
        </div>

        <div className="flex justify-end gap-2">
          <button
            onClick={onClose}
            className="px-4 py-1.5 rounded-lg border border-white/10 bg-white/5 hover:bg-white/10 text-sm"
          >
            {t("settings.vcRedist.dismissButton")}
          </button>
          <button
            onClick={openPage}
            className="px-4 py-1.5 rounded-lg bg-accent/30 border border-accent text-sm hover:bg-accent/50 flex items-center gap-2"
          >
            <ExternalLink className="w-3.5 h-3.5" /> {t("settings.vcRedist.openPageButton")}
          </button>
        </div>
      </div>
    </div>,
    document.body
  );
}
