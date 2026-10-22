// @vitest-environment jsdom
// The in-app replacement for window.confirm / alert / prompt. Native dialogs
// render as an ugly "wails.localhost says" browser box inside the Wails
// window, so every call site goes through useDialogs() instead.
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import React, { act, useEffect } from "react";
import { createRoot, Root } from "react-dom/client";
import { DialogProvider, useDialogs, type Dialogs } from "../dialogs/DialogProvider";
import { I18nProvider } from "../../lib/i18n";

// AppDialog now resolves its default button labels through useT(), which
// needs I18nProvider's bindings mocked the same way every other i18n-aware
// component test does — otherwise GetLanguage() throws synchronously outside
// a real Wails window.
vi.mock("../../../wailsjs/go/main/App", () => ({
  GetLanguage: async () => "en",
  SetLanguage: async () => {},
}));
vi.mock("../../../wailsjs/runtime", () => ({ EventsOn: () => () => {} }));

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let dialogs: Dialogs;

function Capture() {
  const d = useDialogs();
  useEffect(() => { dialogs = d; }, [d]);
  return null;
}

const flush = () => act(async () => { await Promise.resolve(); });
const $ = <T extends Element>(sel: string) => document.body.querySelector<T>(sel);
const byText = (text: string) =>
  Array.from(document.body.querySelectorAll("button")).find((b) => b.textContent?.trim() === text) as HTMLButtonElement | undefined;
const press = (key: string, target: Element | Window = window) =>
  act(() => { target.dispatchEvent(new KeyboardEvent("keydown", { key, bubbles: true, cancelable: true })); });

beforeEach(async () => {
  host = document.createElement("div");
  document.body.appendChild(host);
  root = createRoot(host);
  await act(async () => { root.render(<I18nProvider><DialogProvider><Capture /></DialogProvider></I18nProvider>); });
});

afterEach(async () => {
  await act(async () => { root.unmount(); });
  host.remove();
});

describe("DialogProvider", () => {
  it("renders nothing until a dialog is requested", () => {
    expect($("[role=dialog]")).toBeNull();
  });

  it("confirm resolves true on the confirm button and false on cancel", async () => {
    let p!: Promise<boolean>;
    act(() => { p = dialogs.confirm({ title: "Delete host", message: "Keys are kept." }); });
    await flush();
    expect($("[role=dialog]")?.textContent).toContain("Delete host");
    expect($("[role=dialog]")?.textContent).toContain("Keys are kept.");
    act(() => { byText("Confirm")!.click(); });
    await expect(p).resolves.toBe(true);
    expect($("[role=dialog]")).toBeNull();

    act(() => { p = dialogs.confirm("Delete this cron?"); });
    await flush();
    act(() => { byText("Cancel")!.click(); });
    await expect(p).resolves.toBe(false);
  });

  it("confirm honours custom labels and marks danger actions", async () => {
    act(() => { void dialogs.confirm({ title: "Remove worktree", confirmLabel: "Remove", danger: true }); });
    await flush();
    const button = byText("Remove")!;
    expect(button).toBeTruthy();
    expect(button.dataset.danger).toBe("true");
  });

  it("Escape cancels a confirm and does not leak to underlying listeners", async () => {
    let leaked = 0;
    const onWindow = () => { leaked += 1; };
    window.addEventListener("keydown", onWindow);
    let p!: Promise<boolean>;
    act(() => { p = dialogs.confirm("Sure?"); });
    await flush();
    press("Escape");
    await expect(p).resolves.toBe(false);
    expect(leaked).toBe(0);
    window.removeEventListener("keydown", onWindow);
  });

  it("clicking the backdrop cancels", async () => {
    let p!: Promise<boolean>;
    act(() => { p = dialogs.confirm("Sure?"); });
    await flush();
    act(() => { $<HTMLElement>("[data-dialog-backdrop]")!.click(); });
    await expect(p).resolves.toBe(false);
  });

  it("alert resolves once acknowledged", async () => {
    let p!: Promise<void>;
    act(() => { p = dialogs.alert("Domain is required"); });
    await flush();
    expect($("[role=dialog]")?.textContent).toContain("Domain is required");
    expect(byText("Cancel")).toBeUndefined();
    act(() => { byText("OK")!.click(); });
    await expect(p).resolves.toBeUndefined();
  });

  it("prompt pre-fills, focuses and selects the input, and resolves the trimmed value on Enter", async () => {
    let p!: Promise<string | null>;
    act(() => { p = dialogs.prompt({ title: "Rename session", label: "Session name", defaultValue: "Claude Code" }); });
    await flush();
    const input = $<HTMLInputElement>("[role=dialog] input")!;
    expect(input.value).toBe("Claude Code");
    expect(document.activeElement).toBe(input);
    expect(input.selectionStart).toBe(0);
    expect(input.selectionEnd).toBe("Claude Code".length);

    act(() => {
      const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!;
      setter.call(input, "  Rewards suite  ");
      input.dispatchEvent(new Event("input", { bubbles: true }));
    });
    press("Enter", input);
    await expect(p).resolves.toBe("Rewards suite");
  });

  it("prompt refuses an empty value and resolves null on cancel", async () => {
    let p!: Promise<string | null>;
    act(() => { p = dialogs.prompt({ title: "New folder", label: "Folder name" }); });
    await flush();
    const input = $<HTMLInputElement>("[role=dialog] input")!;
    expect(byText("Save")!.disabled).toBe(true);
    press("Enter", input);
    expect($("[role=dialog]")).not.toBeNull();
    act(() => { byText("Cancel")!.click(); });
    await expect(p).resolves.toBeNull();
  });

  it("queues a second request until the first one settles", async () => {
    let first!: Promise<boolean>;
    let second!: Promise<boolean>;
    act(() => { first = dialogs.confirm("First"); second = dialogs.confirm("Second"); });
    await flush();
    expect($("[role=dialog]")?.textContent).toContain("First");
    expect($("[role=dialog]")?.textContent).not.toContain("Second");
    act(() => { byText("Confirm")!.click(); });
    await expect(first).resolves.toBe(true);
    await flush();
    expect($("[role=dialog]")?.textContent).toContain("Second");
    act(() => { byText("Cancel")!.click(); });
    await expect(second).resolves.toBe(false);
  });
});
