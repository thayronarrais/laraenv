// @vitest-environment jsdom
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { LanguageSelector } from "../../components/LanguageSelector";
import { I18nProvider } from "../../lib/i18n";

const getLanguageOverrides = vi.fn(() =>
  Promise.resolve({ dir: "", catalogues: {}, results: [] }),
);

vi.mock("../../../wailsjs/go/main/App", () => ({
  GetLanguage: () => Promise.resolve("en"),
  SetLanguage: () => Promise.resolve(),
  ListLocales: () => Promise.resolve([]),
  GetLanguageOverrides: () => getLanguageOverrides(),
}));
vi.mock("../../../wailsjs/runtime", () => ({ EventsOn: () => () => {} }));

// This suite's own vitest run has no global afterEach registered for
// Testing Library's automatic unmount, so each render() leaks into the next
// test's DOM unless cleaned up explicitly.
afterEach(cleanup);

describe("language selector", () => {
  it("lists every shipped locale by its native name", async () => {
    render(
      <I18nProvider>
        <LanguageSelector />
      </I18nProvider>,
    );
    // Someone looking for their language looks for "Português", not
    // "Portuguese" — the selector shows nativeName.
    expect(await screen.findByText("Português (Brasil)")).toBeTruthy();
    expect(screen.getByText("English")).toBeTruthy();
  });

  it("says nothing about local overrides when there are none (the common case)", async () => {
    getLanguageOverrides.mockResolvedValueOnce({ dir: "", catalogues: {}, results: [] });
    render(
      <I18nProvider>
        <LanguageSelector />
      </I18nProvider>,
    );
    await screen.findByText("English");
    expect(screen.queryByText(/Using local translations/)).toBeNull();
  });

  it("shows the override path and names any rejected key, for the active locale", async () => {
    getLanguageOverrides.mockResolvedValueOnce({
      dir: "C:\\laraenv\\langs",
      catalogues: { en: { projects: { title: "Projects (local)" } } },
      results: [
        {
          locale: "en",
          applied: 1,
          rejected: [{ key: "errors.ssh.fileTooLarge", reason: 'must contain "file too large"' }],
        },
      ],
    });
    render(
      <I18nProvider>
        <LanguageSelector />
      </I18nProvider>,
    );
    expect(await screen.findByText(/Using local translations from C:\\laraenv\\langs/)).toBeTruthy();
    expect(screen.getByText(/errors\.ssh\.fileTooLarge/)).toBeTruthy();
  });

  it("names the file and reason when an override could not even be read", async () => {
    getLanguageOverrides.mockResolvedValueOnce({
      dir: "C:\\laraenv\\langs",
      catalogues: {},
      results: [
        {
          locale: "en",
          applied: 0,
          rejected: [],
          unreadable: "invalid character ',' looking for beginning of object key string",
        },
      ],
    });
    render(
      <I18nProvider>
        <LanguageSelector />
      </I18nProvider>,
    );
    expect(await screen.findByText(/en\.json could not be read and was skipped/)).toBeTruthy();
    expect(screen.getByText(/invalid character/)).toBeTruthy();
  });
});
