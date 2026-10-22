// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import Settings from "../../pages/Settings";
import { I18nProvider } from "../../lib/i18n";

const bridge = vi.hoisted(() => ({
  status: vi.fn(),
  enable: vi.fn(),
  register: vi.fn(),
  listeners: new Map<string, (...args: any[]) => void>(),
  unsubscribe: vi.fn(),
}));

vi.mock("../../../wailsjs/go/main/App", () => ({
  GetMCPStatus: bridge.status,
  SetMCPEnabled: bridge.enable,
  RegisterMCPClient: bridge.register,
  GetLanguage: () => Promise.resolve("en"),
  SetLanguage: () => Promise.resolve(),
  GetLanguageOverrides: () => Promise.resolve({ dir: "", catalogues: {}, results: [] }),
  GetAgentHooksEnabled: () => Promise.resolve(false),
  GetAgentHooksStatus: () => Promise.resolve([]),
  SetAgentHooksEnabled: vi.fn(),
  SetAgentSessionsEnabled: vi.fn(),
  ResetAgentMetadata: vi.fn(),
  GetServicesConfig: vi.fn(),
  SetNginxTuning: vi.fn(),
  SetApacheTuning: vi.fn(),
  SetPhpTuning: vi.fn(),
  LocalCAStatus: vi.fn(),
  TrustLocalCA: vi.fn(),
}));
vi.mock("../../../wailsjs/runtime", () => ({
  EventsOn: (name: string, listener: (...args: any[]) => void) => {
    bridge.listeners.set(name, listener);
    return () => {
      bridge.listeners.delete(name);
      bridge.unsubscribe(name);
    };
  },
}));
vi.mock("../../pages/Account", () => ({ default: () => null }));
vi.mock("../../pages/Updates", () => ({ default: () => null }));
vi.mock("../CommandCenterSettings", () => ({ default: () => null }));
vi.mock("../LanguageSelector", () => ({ LanguageSelector: () => null }));

const clients = [
  { id: "claude", label: "Claude Code", path: "C:\\Users\\test\\.claude.json", registered: false, restartRequired: false },
  { id: "codex", label: "Codex", path: "C:\\Users\\test\\.codex\\config.toml", registered: false, restartRequired: false },
  { id: "cursor", label: "Cursor", path: "C:\\Users\\test\\.cursor\\mcp.json", registered: true, restartRequired: false },
];

function status(overrides: Record<string, unknown> = {}) {
  return { enabled: false, running: false, clients, activity: [], ...overrides };
}

function mount() {
  const flash = vi.fn();
  const view = render(<I18nProvider><Settings initialTab="experimental" flash={flash} /></I18nProvider>);
  return { ...view, flash };
}

beforeEach(() => {
  bridge.listeners.clear();
  bridge.unsubscribe.mockClear();
  bridge.status.mockReset().mockResolvedValue(status());
  bridge.enable.mockReset();
  bridge.register.mockReset();
});
afterEach(cleanup);

describe("MCP settings", () => {
  it("queries status in Experimental without enabling MCP or registering clients", async () => {
    mount();
    const toggle = await screen.findByRole("switch", { name: "Enable local MCP" });
    await waitFor(() => expect((toggle as HTMLButtonElement).disabled).toBe(false));
    expect(toggle.getAttribute("aria-checked")).toBe("false");
    expect(screen.getByText("Keep LaraEnv open to use MCP.")).toBeTruthy();
    expect((screen.getByRole("button", { name: "Register Codex" }) as HTMLButtonElement).disabled).toBe(true);
    expect(bridge.status).toHaveBeenCalledTimes(1);
    expect(bridge.enable).not.toHaveBeenCalled();
    expect(bridge.register).not.toHaveBeenCalled();
  });

  it("waits for the real enable response before showing Ready", async () => {
    let resolve!: (value: ReturnType<typeof status>) => void;
    bridge.enable.mockReturnValue(new Promise((done) => { resolve = done; }));
    const { flash } = mount();
    const toggle = await screen.findByRole("switch", { name: "Enable local MCP" });
    await waitFor(() => expect((toggle as HTMLButtonElement).disabled).toBe(false));
    fireEvent.click(toggle);
    expect(bridge.enable).toHaveBeenCalledWith(true);
    expect(toggle.getAttribute("aria-checked")).toBe("false");
    expect((toggle as HTMLButtonElement).disabled).toBe(true);
    await act(async () => { resolve(status({ enabled: true, running: true })); });
    expect(toggle.getAttribute("aria-checked")).toBe("true");
    expect(screen.getByText("Ready")).toBeTruthy();
    expect(flash).toHaveBeenCalledWith("Local MCP enabled");
  });

  it("shows unavailable rather than Ready when enabled but the bridge is not running", async () => {
    bridge.status.mockResolvedValue(status({ enabled: true, running: false, message: "Could not start local access" }));
    mount();
    expect(await screen.findByText("Unavailable")).toBeTruthy();
    expect(screen.getByText("Could not start local access")).toBeTruthy();
    expect(screen.queryByText("Ready")).toBeNull();
  });

  it("keeps the previous state when enabling fails and does not expose transport errors", async () => {
    bridge.enable.mockRejectedValue(new Error("Bearer private-token http://127.0.0.1:9876"));
    const { flash } = mount();
    const toggle = await screen.findByRole("switch", { name: "Enable local MCP" });
    await waitFor(() => expect((toggle as HTMLButtonElement).disabled).toBe(false));
    fireEvent.click(toggle);
    expect(await screen.findByRole("alert")).toHaveProperty("textContent", "Could not change local MCP access. Try again.");
    expect(toggle.getAttribute("aria-checked")).toBe("false");
    expect(document.body.textContent).not.toContain("private-token");
    expect(flash).not.toHaveBeenCalledWith("Local MCP enabled");
  });

  it("registers only the selected client and shows its returned path and restart instruction", async () => {
    bridge.status.mockResolvedValue(status({ enabled: true, running: true }));
    bridge.register.mockResolvedValue({ ...clients[1], path: "D:\\Codex Home\\config.toml", registered: true, restartRequired: true });
    const { flash } = mount();
    fireEvent.click(await screen.findByRole("button", { name: "Register Codex" }));
    expect(await screen.findByRole("button", { name: "Update registration for Codex" })).toBeTruthy();
    expect(bridge.register).toHaveBeenCalledTimes(1);
    expect(bridge.register).toHaveBeenCalledWith("codex");
    expect(screen.getByText("D:\\Codex Home\\config.toml")).toBeTruthy();
    expect(screen.getByText("Reopen Codex to apply the registration.")).toBeTruthy();
    expect(flash).toHaveBeenCalledWith("Codex registered");
  });

  it("does not report registration success for a failed client and preserves other results", async () => {
    bridge.status.mockResolvedValue(status({ enabled: true, running: true }));
    bridge.register.mockResolvedValue({ ...clients[1], registered: false, error: "Configuration is invalid" });
    const { flash } = mount();
    fireEvent.click(await screen.findByRole("button", { name: "Register Codex" }));
    expect(await screen.findByText("Configuration is invalid")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Update registration for Cursor" })).toBeTruthy();
    expect(screen.getByText(clients[2].path)).toBeTruthy();
    expect(flash).not.toHaveBeenCalledWith("Codex registered");
    expect(screen.getByRole("button", { name: "Register Codex" })).toBeTruthy();
  });

  it("blocks duplicate registration clicks until the selected client's result arrives", async () => {
    let resolve!: (value: typeof clients[number]) => void;
    bridge.status.mockResolvedValue(status({ enabled: true, running: true }));
    bridge.register.mockReturnValue(new Promise((done) => { resolve = done; }));
    mount();
    const button = await screen.findByRole("button", { name: "Register Codex" });
    fireEvent.click(button);
    fireEvent.click(button);
    expect(bridge.register).toHaveBeenCalledTimes(1);
    expect((button as HTMLButtonElement).disabled).toBe(true);
    await act(async () => { resolve({ ...clients[1], registered: true }); });
    expect((screen.getByRole("button", { name: "Update registration for Codex" }) as HTMLButtonElement).disabled).toBe(false);
  });

  it("keeps an existing registration on a rejected update without displaying private exception content", async () => {
    bridge.status.mockResolvedValue(status({ enabled: true, running: true }));
    bridge.register.mockRejectedValue(new Error("SECRET-CONFIG-CONTENT"));
    mount();
    fireEvent.click(await screen.findByRole("button", { name: "Update registration for Cursor" }));
    expect(await screen.findByRole("alert")).toHaveProperty("textContent", "Could not register Cursor. Check its configuration and try again.");
    expect(screen.getByText(clients[2].path)).toBeTruthy();
    expect(screen.getByRole("button", { name: "Update registration for Cursor" })).toBeTruthy();
    expect(document.body.textContent).not.toContain("SECRET-CONFIG-CONTENT");
  });

  it("distinguishes an unreadable status from disabled MCP and offers a read-only retry", async () => {
    bridge.status.mockRejectedValueOnce(new Error("private bridge error"));
    mount();
    expect(await screen.findByRole("alert")).toHaveProperty("textContent", "Could not check MCP status. Try again.");
    expect(screen.queryByText("Disabled")).toBeNull();
    bridge.status.mockResolvedValueOnce(status());
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(await screen.findByText("Disabled")).toBeTruthy();
    expect(bridge.enable).not.toHaveBeenCalled();
  });

  it("shows five recent metadata entries, updates activity once per ID, and cleans up listeners", async () => {
    const activity = Array.from({ length: 7 }, (_, i) => ({
      id: `entry-${i}`, time: `2026-10-06T12:00:0${i}Z`, group: "projects", action: "list", target: `project-${i}`,
      success: true, durationMs: 25, payload: "PRIVATE-CONTENT", token: "PRIVATE-TOKEN",
    }));
    bridge.status.mockResolvedValue(status({ enabled: true, running: true, activity }));
    const { unmount } = mount();
    const list = await screen.findByRole("list", { name: "Recent MCP activity" });
    expect(within(list).getAllByRole("listitem")).toHaveLength(5);
    expect(within(list).queryByText("project-0")).toBeNull();
    expect(within(list).getByText("project-6")).toBeTruthy();
    const latest = { id: "new", time: "2026-10-06T13:00:00Z", group: "services", action: "start", target: "nginx", success: false, error: "PRIVATE-CONTENT", durationMs: 50 };
    act(() => {
      bridge.listeners.get("mcp:activity")?.(latest);
      bridge.listeners.get("mcp:activity")?.(latest);
    });
    expect(within(list).getAllByRole("listitem")).toHaveLength(5);
    expect(within(list).getAllByText("nginx")).toHaveLength(1);
    expect(within(list).getByText("Failed")).toBeTruthy();
    expect(document.body.textContent).not.toMatch(/PRIVATE-(CONTENT|TOKEN)/);
    act(() => bridge.listeners.get("mcp:status-changed")?.(status({ enabled: false, running: false, activity: [latest] })));
    expect(screen.getByRole("switch", { name: "Enable local MCP" }).getAttribute("aria-checked")).toBe("false");
    expect(screen.getByText("Disabled")).toBeTruthy();
    unmount();
    expect(bridge.unsubscribe).toHaveBeenCalledWith("mcp:activity");
    expect(bridge.unsubscribe).toHaveBeenCalledWith("mcp:status-changed");
  });

  it("keeps newer status and activity events when the initial snapshot arrives late", async () => {
    let resolve!: (value: ReturnType<typeof status>) => void;
    bridge.status.mockReturnValue(new Promise((done) => { resolve = done; }));
    mount();
    const latest = { id: "latest", time: "2026-10-06T14:00:00Z", group: "logs", action: "tail", target: "mysql", success: true, durationMs: 20 };
    act(() => {
      bridge.listeners.get("mcp:status-changed")?.(status({ enabled: true, running: true }));
      bridge.listeners.get("mcp:activity")?.(latest);
    });
    await act(async () => { resolve(status()); });
    expect(screen.getByText("Ready")).toBeTruthy();
    expect(screen.getByRole("switch", { name: "Enable local MCP" }).getAttribute("aria-checked")).toBe("true");
    expect(within(screen.getByRole("list", { name: "Recent MCP activity" })).getByText("mysql")).toBeTruthy();
  });
});
