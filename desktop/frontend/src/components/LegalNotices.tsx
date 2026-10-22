import { useEffect, useId, useMemo, useRef, useState, type KeyboardEvent } from "react";
import { createPortal } from "react-dom";
import { FileText, Loader2, X } from "lucide-react";
import GlassCard from "./GlassCard";
import { useT } from "../lib/i18n";

const LEGAL_FILES = ["/legal/LICENSE.txt", "/legal/THIRD_PARTY_NOTICES.txt"] as const;

function LegalText({ text }: { text: string }) {
  const chunks = useMemo(() => {
    const result: string[] = [];
    for (let start = 0; start < text.length;) {
      const newline = text.indexOf("\n", start + 8192);
      const end = newline < 0 ? text.length : newline + 1;
      result.push(text.slice(start, end));
      start = end;
    }
    return result;
  }, [text]);
  // Keep the full document in the DOM for find, copying and accessibility.
  // Small blocks let Chromium skip offscreen layout of multi-megabyte notices.
  return <pre tabIndex={0} className="whitespace-pre-wrap break-words text-[11px] leading-relaxed text-white/75 p-3 rounded-lg border border-white/10 bg-black/20">
    {chunks.map((chunk, index) => <span key={index} className="block" style={{ contentVisibility: "auto", containIntrinsicSize: "auto 400px" }}>{chunk}</span>)}
  </pre>;
}

export default function LegalNotices() {
  const t = useT();
  const titleID = useId();
  const openButton = useRef<HTMLButtonElement>(null);
  const closeButton = useRef<HTMLButtonElement>(null);
  const [open, setOpen] = useState(false);
  const [attempt, setAttempt] = useState(0);
  const [texts, setTexts] = useState<string[] | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState(false);

  useEffect(() => {
    if (!open) return;
    const trigger = openButton.current;
    closeButton.current?.focus();
    return () => trigger?.focus();
  }, [open]);

  useEffect(() => {
    if (!open) return;
    const controller = new AbortController();
    let cancelled = false;
    setLoading(true);
    setError(false);
    setTexts(null);
    // Relative paths are served from the app's bundled assets. Reject redirects
    // and omit credentials so reading notices never accesses an account service.
    Promise.all(LEGAL_FILES.map(async path => {
      const response = await fetch(path, {
        signal: controller.signal, mode: "same-origin", redirect: "error", credentials: "omit",
      });
      if (!response.ok) throw new Error("Bundled license file unavailable");
      return response.text();
    })).then(values => {
      if (!cancelled) setTexts(values);
    }).catch(() => {
      if (!cancelled) setError(true);
    }).finally(() => {
      if (!cancelled) setLoading(false);
    });
    return () => { cancelled = true; controller.abort(); };
  }, [open, attempt]);

  const handleKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key === "Escape") {
      event.preventDefault();
      event.stopPropagation();
      setOpen(false);
    }
    if (event.key !== "Tab") return;
    const items = Array.from(event.currentTarget.querySelectorAll<HTMLElement>(
      'button:not(:disabled), [tabindex="0"]',
    ));
    const first = items[0];
    const last = items[items.length - 1];
    if (event.shiftKey && document.activeElement === first) {
      event.preventDefault();
      last?.focus();
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault();
      first?.focus();
    }
  };

  return <>
    <GlassCard title={t("licenses.title")} icon={<FileText className="w-4 h-4" aria-hidden="true" />}>
      <p className="text-xs text-white/60 mb-3">{t("licenses.description")}</p>
      <button ref={openButton} onClick={() => setOpen(true)} className="px-3 py-2 rounded-lg bg-white/5 hover:bg-white/10 border border-white/10 text-xs">
        {t("licenses.open")}
      </button>
    </GlassCard>
    {open && createPortal(
      <div className="fixed inset-0 z-[70] bg-black/60 backdrop-blur-sm flex items-center justify-center p-4" onClick={() => setOpen(false)}>
        <div role="dialog" aria-modal="true" aria-labelledby={titleID} onKeyDown={handleKeyDown} onClick={event => event.stopPropagation()}
          className="glass-strong w-full max-w-4xl max-h-[90vh] flex flex-col p-5">
          <div className="flex items-center justify-between gap-3 mb-4 shrink-0">
            <h2 id={titleID} className="text-sm font-semibold">{t("licenses.title")}</h2>
            <button ref={closeButton} onClick={() => setOpen(false)} aria-label={t("licenses.close")} className="p-1 rounded hover:bg-white/10">
              <X className="w-4 h-4" aria-hidden="true" />
            </button>
          </div>
          {loading && <p role="status" className="flex items-center gap-2 text-xs text-white/60 py-4">
            <Loader2 className="w-4 h-4 animate-spin" aria-hidden="true" />{t("licenses.loading")}
          </p>}
          {error && <div className="space-y-3 text-xs py-4">
            <p role="alert" className="text-red-200">{t("licenses.error")}</p>
            <button onClick={() => { closeButton.current?.focus(); setAttempt(value => value + 1); }} className="px-3 py-2 rounded-lg bg-white/5 hover:bg-white/10 border border-white/10">
              {t("licenses.retry")}
            </button>
          </div>}
          {texts && <div className="min-h-0 overflow-y-auto space-y-6">
            {texts.map((text, index) => <section key={LEGAL_FILES[index]}>
              <h3 className="text-xs font-semibold mb-2">{t(index === 0 ? "licenses.desktopTitle" : "licenses.thirdPartyTitle")}</h3>
              <LegalText text={text} />
            </section>)}
          </div>}
        </div>
      </div>, document.body,
    )}
  </>;
}
