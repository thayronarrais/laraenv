// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import LegalNotices from "../LegalNotices";
import Settings from "../../pages/Settings";

vi.mock("../../lib/i18n", () => ({
  useT: () => (key: string) => key,
  useI18n: () => ({ t: (key: string) => key, locales: [], overrideResults: [] }),
}));
vi.mock("../../../wailsjs/go/main/App", async importOriginal => {
  const actual = await importOriginal<Record<string, unknown>>();
  return Object.fromEntries(Object.keys(actual).map(key => [key, async () => []]));
});
vi.mock("../../../wailsjs/runtime", () => ({ EventsOn: () => () => {} }));

const files = {
  "/legal/LICENSE.txt": "Apache License, Version 2.0\nCopyright 2026 Thayron Arrais",
  "/legal/THIRD_PARTY_NOTICES.txt": "Third-party notices\n<img src='https://external.invalid/pixel' onerror='alert(1)'>",
};
let fetchText: ReturnType<typeof vi.fn>;

beforeEach(() => {
  fetchText = vi.fn(async (path: string) => {
    if (!(path in files)) throw new Error(`Unexpected request: ${path}`);
    return { ok: true, text: async () => files[path as keyof typeof files] };
  });
  vi.stubGlobal("fetch", fetchText);
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

describe("legal notices", () => {
  it("makes no requests before the user opens the notices", async () => {
    render(<LegalNotices />);
    await act(async () => {});
    expect(screen.getByRole("button", { name: "licenses.open" })).toBeTruthy();
    expect(fetchText).not.toHaveBeenCalled();
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("loads only bundled same-origin files and renders their contents as plain text", async () => {
    const { container } = render(<LegalNotices />);
    fireEvent.click(screen.getByRole("button", { name: "licenses.open" }));
    const dialog = await screen.findByRole("dialog", { name: "licenses.title" });
    await within(dialog).findByText(/Copyright 2026 Thayron Arrais/);
    expect(fetchText.mock.calls.map(([path]) => path).sort()).toEqual([
      "/legal/LICENSE.txt", "/legal/THIRD_PARTY_NOTICES.txt",
    ]);
    for (const [, options] of fetchText.mock.calls) {
      expect(options).toMatchObject({ mode: "same-origin", redirect: "error", credentials: "omit" });
    }
    expect(dialog.textContent).toContain(files["/legal/THIRD_PARTY_NOTICES.txt"]);
    expect(dialog.querySelector("img")).toBeNull();
    expect(dialog.querySelector("script")).toBeNull();
    expect(container.querySelector("iframe")).toBeNull();
  });

  it("shows an offline read error and allows retry without leaving the dialog", async () => {
    fetchText.mockRejectedValueOnce(new TypeError("offline"));
    render(<LegalNotices />);
    fireEvent.click(screen.getByRole("button", { name: "licenses.open" }));
    const dialog = await screen.findByRole("dialog");
    expect(await within(dialog).findByRole("alert")).toHaveProperty("textContent", "licenses.error");
    const retry = within(dialog).getByRole("button", { name: "licenses.retry" });
    retry.focus();
    fireEvent.click(retry);
    expect(document.activeElement).toBe(within(dialog).getByRole("button", { name: "licenses.close" }));
    expect(await within(dialog).findByText(/Copyright 2026 Thayron Arrais/)).toBeTruthy();
    expect(within(dialog).queryByRole("alert")).toBeNull();
  });

  it("preserves every character of long legal documents across line boundaries", async () => {
    const longText = ("Copyright Example — café 🐘\r\nPermission and disclaimer.\n".repeat(1200)) + "<script>plain text only</script>\nFinal notice";
    fetchText.mockImplementation(async (path: string) => ({ ok: true, text: async () => path.endsWith("THIRD_PARTY_NOTICES.txt") ? longText : files["/legal/LICENSE.txt"] }));
    render(<LegalNotices />);
    fireEvent.click(screen.getByRole("button", { name: "licenses.open" }));
    const dialog = await screen.findByRole("dialog");
    await waitFor(() => expect(dialog.querySelectorAll("pre")).toHaveLength(2));
    const texts = dialog.querySelectorAll("pre");
    expect(texts[0].textContent).toBe(files["/legal/LICENSE.txt"]);
    expect(texts[1].textContent).toBe(longText);
    expect(texts[1].querySelector("script")).toBeNull();
    expect(texts[1].tabIndex).toBe(0);
    expect(fetchText).toHaveBeenCalledTimes(2);
  });

  it("treats a missing bundled text file as an error", async () => {
    fetchText.mockResolvedValueOnce({ ok: false, status: 404, text: async () => "" });
    render(<LegalNotices />);
    fireEvent.click(screen.getByRole("button", { name: "licenses.open" }));
    expect(await screen.findByRole("alert")).toHaveProperty("textContent", "licenses.error");
  });

  it("supports keyboard close and restores focus to the opening button", async () => {
    render(<LegalNotices />);
    const trigger = screen.getByRole("button", { name: "licenses.open" });
    trigger.focus();
    fireEvent.click(trigger);
    const dialog = await screen.findByRole("dialog");
    const close = within(dialog).getByRole("button", { name: "licenses.close" });
    expect(document.activeElement).toBe(close);
    fireEvent.keyDown(close, { key: "Escape" });
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(document.activeElement).toBe(trigger);
    fireEvent.click(trigger);
    fireEvent.click(within(await screen.findByRole("dialog")).getByRole("button", { name: "licenses.close" }));
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("aborts an unfinished local read when closed", async () => {
    fetchText.mockImplementation(() => new Promise(() => {}));
    render(<LegalNotices />);
    fireEvent.click(screen.getByRole("button", { name: "licenses.open" }));
    fireEvent.click(within(await screen.findByRole("dialog")).getByRole("button", { name: "licenses.close" }));
    expect(fetchText.mock.calls).toHaveLength(2);
    expect(fetchText.mock.calls[0][1].signal.aborted).toBe(true);
  });

  it("shows the card only on the General settings tab without fetching it", async () => {
    render(<Settings flash={vi.fn()} />);
    expect(screen.getByRole("button", { name: "licenses.open" })).toBeTruthy();
    expect(fetchText).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "settings.tabs.appearance" }));
    expect(screen.queryByRole("button", { name: "licenses.open" })).toBeNull();
  });
});
