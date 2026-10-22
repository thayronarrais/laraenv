// @vitest-environment jsdom
// The php.ini editor's subtitle used to build the path by hand as
// `C:\laraenv\php\<version>\php.ini`. A portable copy resolves its root from
// os.Executable(), so that string named a file the app was not reading: it
// read and wrote the right one through PhpIniPath() and then told the user it
// was somewhere else entirely.
import { describe, expect, it, vi } from "vitest";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import PhpExtensionsModal from "../PhpExtensionsModal";
import { I18nProvider } from "../../lib/i18n";

const PORTABLE_INI = "D:\\LaraEnvPortable\\php\\8.4\\php.ini";

vi.mock("../../../wailsjs/go/main/App", () => ({
  GetLanguage: async () => "en",
  SetLanguage: async () => {},
  ListPhpExtensions: async () => [],
  SetPhpExtension: async () => {},
  GetPhpIni: async () => "[PHP]\nmemory_limit = 512M\n",
  SavePhpIni: async () => {},
  FetchPhpExtensionDLL: async () => {},
  ListPeclExtensions: async () => [],
  InstallPeclExtension: async () => {},
  OpenFileInDefaultApp: async () => {},
  PhpIniPath: async () => PORTABLE_INI,
}));
vi.mock("../../../wailsjs/runtime/runtime", () => ({ EventsOn: () => () => {} }));
// Monaco is loaded through an AMD loader that jsdom cannot run, and none of
// these assertions are about the editor's innards.
vi.mock("@monaco-editor/react", () => ({ default: () => <div data-testid="editor" /> }));

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

function clickByText(host: HTMLElement, text: string) {
  const el = [...host.ownerDocument.querySelectorAll("button")].find((b) =>
    (b.textContent ?? "").includes(text),
  );
  if (!el) throw new Error(`no button matching ${JSON.stringify(text)}`);
  act(() => {
    el.dispatchEvent(new MouseEvent("click", { bubbles: true }));
  });
}

describe("PhpExtensionsModal — php.ini editor", () => {
  it("shows the path the app actually reads, not a hardcoded install root", async () => {
    const host = document.createElement("div");
    document.body.appendChild(host);
    const root = createRoot(host);

    await act(async () => {
      root.render(
        <I18nProvider>
          <PhpExtensionsModal version="8.4" open onClose={() => {}} flash={() => {}} />
        </I18nProvider>,
      );
    });

    clickByText(host, "view php.ini");
    clickByText(host, "Built-in editor");
    await act(async () => {});

    const text = document.body.textContent ?? "";
    expect(text).toContain(PORTABLE_INI);
    expect(text).not.toContain("C:\\laraenv\\php\\8.4\\php.ini");

    act(() => root.unmount());
    host.remove();
  });
});
