import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { AlertTriangle, Info } from "lucide-react";
import { useT } from "../../lib/i18n";

// In-app replacement for window.confirm / window.alert / window.prompt.
//
// Native dialogs render as a browser-owned "wails.localhost says" box inside
// the Wails window, which looks broken and cannot be styled. Every call site
// goes through useDialogs() instead; the API is Promise-based so a handler
// stays a one-liner:
//
//   if (!(await dialogs.confirm("Delete this cron?"))) return;
//   const name = await dialogs.prompt({ title: "Rename session", defaultValue: title });
//
// Only one dialog is shown at a time; further requests queue behind it.

export type ConfirmOptions = {
  title: string;
  message?: ReactNode;
  confirmLabel?: string;
  cancelLabel?: string;
  /** Style the confirm button as destructive (red). */
  danger?: boolean;
};

export type AlertOptions = {
  title: string;
  message?: ReactNode;
  confirmLabel?: string;
};

export type PromptOptions = {
  title: string;
  message?: ReactNode;
  /** Label rendered above the input. */
  label?: string;
  defaultValue?: string;
  placeholder?: string;
  confirmLabel?: string;
  cancelLabel?: string;
};

export type Dialogs = {
  /** Resolves true when confirmed, false when cancelled (button, Escape or backdrop). */
  confirm(options: ConfirmOptions | string): Promise<boolean>;
  /** Resolves once the user acknowledges the message. */
  alert(options: AlertOptions | string): Promise<void>;
  /** Resolves the trimmed value, or null when cancelled. Empty values cannot be submitted. */
  prompt(options: PromptOptions | string): Promise<string | null>;
};

type Request = { id: number } & (
  | { kind: "confirm"; options: ConfirmOptions; resolve: (value: boolean) => void }
  | { kind: "alert"; options: AlertOptions; resolve: () => void }
  | { kind: "prompt"; options: PromptOptions; resolve: (value: string | null) => void }
);

let nextId = 0;

// Outside a provider (isolated component tests) fall back to the browser so
// nothing silently no-ops.
const fallback: Dialogs = {
  confirm: (o) => Promise.resolve(window.confirm(typeof o === "string" ? o : o.title)),
  alert: (o) => { window.alert(typeof o === "string" ? o : o.title); return Promise.resolve(); },
  prompt: (o) => Promise.resolve(typeof o === "string" ? window.prompt(o) : window.prompt(o.title, o.defaultValue)),
};

const DialogContext = createContext<Dialogs>(fallback);

export function useDialogs(): Dialogs {
  return useContext(DialogContext);
}

export function DialogProvider({ children }: { children: ReactNode }) {
  const [queue, setQueue] = useState<Request[]>([]);
  const enqueue = useCallback((request: Omit<Request, "id">) => setQueue((current) => [...current, { ...request, id: ++nextId } as Request]), []);

  const dialogs = useMemo<Dialogs>(() => ({
    confirm: (o) => new Promise((resolve) => enqueue({ kind: "confirm", options: typeof o === "string" ? { title: o } : o, resolve })),
    alert: (o) => new Promise((resolve) => enqueue({ kind: "alert", options: typeof o === "string" ? { title: o } : o, resolve })),
    prompt: (o) => new Promise((resolve) => enqueue({ kind: "prompt", options: typeof o === "string" ? { title: o } : o, resolve })),
  }), [enqueue]);

  const current = queue[0] ?? null;
  const settle = useCallback((request: Request, value: unknown) => {
    setQueue((rest) => rest.filter((r) => r !== request));
    (request.resolve as (v: unknown) => void)(value);
  }, []);

  return (
    <DialogContext.Provider value={dialogs}>
      {children}
      {current && createPortal(<AppDialog key={current.id} request={current} settle={settle} />, document.body)}
    </DialogContext.Provider>
  );
}

function AppDialog({ request, settle }: { request: Request; settle: (request: Request, value: unknown) => void }) {
  const t = useT();
  const { kind, options } = request;
  const [value, setValue] = useState(kind === "prompt" ? options.defaultValue ?? "" : "");
  const inputRef = useRef<HTMLInputElement>(null);
  const confirmRef = useRef<HTMLButtonElement>(null);
  const trimmed = value.trim();
  const canSubmit = kind !== "prompt" || trimmed.length > 0;

  const cancel = useCallback(() => {
    if (kind === "confirm") settle(request, false);
    else if (kind === "prompt") settle(request, null);
    else settle(request, undefined);
  }, [kind, request, settle]);

  const submit = useCallback(() => {
    if (!canSubmit) return;
    if (kind === "confirm") settle(request, true);
    else if (kind === "prompt") settle(request, trimmed);
    else settle(request, undefined);
  }, [canSubmit, kind, request, settle, trimmed]);

  useEffect(() => {
    if (kind === "prompt") { inputRef.current?.focus(); inputRef.current?.select(); }
    else confirmRef.current?.focus();
  }, [kind]);

  // Capture-phase so modals underneath (which listen for Escape on window)
  // never see the key while this dialog is open.
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") { event.preventDefault(); event.stopImmediatePropagation(); cancel(); }
      // Enter on a focused button must trigger that button (Cancel included), so only intercept it elsewhere.
      else if (event.key === "Enter" && !(event.target instanceof HTMLButtonElement)) { event.preventDefault(); event.stopImmediatePropagation(); submit(); }
    };
    window.addEventListener("keydown", onKey, true);
    return () => window.removeEventListener("keydown", onKey, true);
  }, [cancel, submit]);

  const confirmLabel = options.confirmLabel ?? (kind === "alert" ? t("common.ok") : kind === "prompt" ? t("common.save") : t("common.confirm"));
  const cancelLabel = kind === "alert" ? null : (options as ConfirmOptions | PromptOptions).cancelLabel ?? t("common.cancel");
  const danger = kind === "confirm" && !!options.danger;
  const Icon = danger ? AlertTriangle : Info;

  return (
    <div
      data-dialog-backdrop
      onClick={cancel}
      className="fixed inset-0 z-[90] flex items-center justify-center bg-[#05030f]/70 backdrop-blur-sm"
    >
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby="app-dialog-title"
        onClick={(event) => event.stopPropagation()}
        className="modal-card w-[440px] max-w-[92vw] p-5 flex flex-col gap-4"
      >
        <header className="flex items-start gap-3">
          <span className={`mt-0.5 flex h-8 w-8 shrink-0 items-center justify-center rounded-lg ${danger ? "bg-red-500/15 text-red-300" : "bg-accent/15 text-accent2"}`}>
            <Icon className="h-4 w-4" />
          </span>
          <div className="min-w-0 flex-1">
            <h2 id="app-dialog-title" className="text-sm font-semibold text-white/95">{options.title}</h2>
            {options.message && <p className="mt-1 whitespace-pre-line break-words text-xs leading-relaxed text-white/65">{options.message}</p>}
          </div>
        </header>

        {kind === "prompt" && (
          <label className="flex flex-col gap-1.5 text-xs">
            {options.label && <span className="text-white/70">{options.label}</span>}
            <input
              ref={inputRef}
              className="input w-full"
              value={value}
              placeholder={options.placeholder}
              onChange={(event) => setValue(event.target.value)}
              spellCheck={false}
              autoComplete="off"
            />
          </label>
        )}

        <footer className="flex justify-end gap-2">
          {cancelLabel && (
            <button
              type="button"
              onClick={cancel}
              className="rounded-md border border-white/10 px-3 py-1.5 text-xs text-white/75 hover:bg-white/10 hover:text-white"
            >
              {cancelLabel}
            </button>
          )}
          <button
            ref={confirmRef}
            type="button"
            onClick={submit}
            disabled={!canSubmit}
            data-danger={danger ? "true" : undefined}
            className={`rounded-md px-3 py-1.5 text-xs font-medium disabled:cursor-not-allowed disabled:opacity-40 ${danger ? "bg-red-500/85 text-white hover:bg-red-500" : "bg-accent text-white hover:bg-accent/85"}`}
          >
            {confirmLabel}
          </button>
        </footer>
      </div>
    </div>
  );
}
