import React from "react";
import ReactDOM from "react-dom/client";
import App from "./App";
import ErrorBoundary from "./components/ErrorBoundary";
import { DialogProvider } from "./components/dialogs/DialogProvider";
import { I18nProvider } from "./lib/i18n";
import "@fontsource/jetbrains-mono/400.css";
import "@fontsource/jetbrains-mono/700.css";
import "./style.css";
import { loadTheme } from "./theme";
// Side-effect import: must run before any editor mounts so Monaco resolves
// from the bundled copy rather than the CDN default.
import "./lib/monaco";

loadTheme();

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <ErrorBoundary>
      <I18nProvider>
        <DialogProvider>
          <App />
        </DialogProvider>
      </I18nProvider>
    </ErrorBoundary>
  </React.StrictMode>
);
