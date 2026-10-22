// Root error boundary: without it, any uncaught render error makes React 18
// unmount the entire tree and the window becomes an empty gradient (see the
// Ctrl+K palette hook-order bug). Shows the error + a reload button instead.
//
// This sits OUTSIDE <I18nProvider> in main.tsx on purpose, so it can still
// catch a crash inside the provider itself — which means the fallback below
// cannot use useT()/useI18n() (there is no provider above it to read from).
// It calls the pure `translate` core directly against the bundled catalogues
// instead, resolving the language itself via GetLanguage(). That call is
// wrapped in try/catch: outside a real Wails window (tests, or a crash before
// the Wails runtime attaches) `window.go` does not exist and the binding
// throws synchronously — exactly the kind of failure this boundary exists to
// survive, so it must not let that failure escape here.
import { Component, type ReactNode } from "react";
import { catalogues, FALLBACK, translate, type Vars } from "../lib/i18n";
// @ts-ignore
import { GetLanguage } from "../../wailsjs/go/main/App";

export default class ErrorBoundary extends Component<
  { children: ReactNode },
  { error: Error | null; lang: string }
> {
  state = { error: null as Error | null, lang: FALLBACK };

  componentDidMount() {
    try {
      GetLanguage()
        .then((code: string) => code && this.setState({ lang: code }))
        .catch(() => { /* pre-startup or a non-main window: English until told otherwise */ });
    } catch {
      // Not running inside a Wails window (e.g. this component's own tests):
      // stay on FALLBACK rather than let the lookup crash the crash screen.
    }
  }

  static getDerivedStateFromError(error: Error) {
    return { error };
  }

  render() {
    const { error, lang } = this.state;
    if (!error) return this.props.children;
    const t = (key: string, vars?: Vars) => translate(catalogues, lang, key, vars);
    return (
      <div className="h-screen w-screen flex items-center justify-center p-8">
        <div className="max-w-[640px] w-full glass-strong p-6">
          <div className="text-lg font-semibold text-white mb-1">{t("common.errorBoundary.title")}</div>
          <div className="text-xs text-white/60 mb-4">
            {t("common.errorBoundary.description")}
          </div>
          <pre className="text-[11px] text-red-300/90 bg-black/30 rounded-lg p-3 max-h-60 overflow-auto whitespace-pre-wrap mb-4">
            {error.message}
            {error.stack ? "\n\n" + error.stack : ""}
          </pre>
          <button
            type="button"
            onClick={() => window.location.reload()}
            className="px-4 py-1.5 rounded-lg bg-accent/30 border border-accent text-sm text-white hover:bg-accent/40 transition-colors"
          >
            {t("common.errorBoundary.reload")}
          </button>
        </div>
      </div>
    );
  }
}
