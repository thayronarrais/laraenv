import { describe, it, expect } from "vitest";
import vm from "node:vm";
import { buildInitScript } from "../mock/base.mjs";
import { AGENT_MAP_STATE, AGENT_MAP_SNAPSHOT } from "../mock/agentActivity.mjs";

// Runs the generated script in a sandbox with just enough of a browser.
function runInSandbox(script) {
  const store = new Map();
  const sandbox = {
    window: {},
    document: { documentElement: { setAttribute() {} } },
    localStorage: {
      getItem: (k) => (store.has(k) ? store.get(k) : null),
      setItem: (k, v) => store.set(k, String(v)),
    },
  };
  sandbox.window.localStorage = sandbox.localStorage;
  sandbox.globalThis = sandbox;
  vm.createContext(sandbox);
  vm.runInContext(script, sandbox);
  return sandbox;
}

describe("buildInitScript", () => {
  it("serves local deployment fixtures independently of cloud account state", async () => {
    const deployments = [{ id: "local-1", name: "demo" }];
    const sb = runInSandbox(buildInitScript({ state: { deployments, cloud: { enabled: false, authenticated: false } } }));
    await expect(sb.window.go.main.App.ListDeployments()).resolves.toEqual(deployments);
    expect(sb.window.__harness.missing).toEqual([]);
  });
  it("mocks the four activity APIs explicitly even when agents are disabled", async () => {
    const sb = runInSandbox(buildInitScript({ state: {} }));
    const api = sb.window.go.main.App;
    await expect(api.GetAgentActivity("missing")).resolves.toMatchObject({ status: "unavailable", nodes: [], backgroundTasks: [] });
    await api.RefreshAgentActivity("missing");
    await api.SetAgentActivityInterest({ windowId: "test", purpose: "counter", sessionId: "missing" });
    await api.RemoveAgentActivityInterest("test", "counter");
    expect(sb.window.__harness.missing).toEqual([]);
  });

  it("keeps counter and dialog leases independent and serves synthetic activity metadata", async () => {
    const sb = runInSandbox(buildInitScript({ state: { ...AGENT_MAP_STATE, webgl: true } }));
    const api = sb.window.go.main.App;
    await expect(api.GetAgentActivity("map-claude")).resolves.toEqual(AGENT_MAP_SNAPSHOT);
    await api.SetAgentActivityInterest({ windowId: "main", purpose: "counter", sessionId: "map-claude" });
    await api.SetAgentActivityInterest({ windowId: "main", purpose: "dialog", sessionId: "map-codex" });
    await api.SetAgentActivityInterest({ windowId: "detached", purpose: "dialog", sessionId: "map-claude" });
    await api.RemoveAgentActivityInterest("main", "dialog");
    expect(Object.values(sb.window.__agentActivity.interests).map(x => [x.windowId, x.purpose, x.sessionId])).toEqual([["main", "counter", "map-claude"], ["detached", "dialog", "map-claude"]]);
    expect(AGENT_MAP_SNAPSHOT.nodes.every(node => !node.tokens)).toBe(true);
    expect(sb.window.__harness.missing).toEqual([]);
  });
  it("produces syntactically valid JavaScript", () => {
    const src = buildInitScript({ state: { version: "1.2.3" }, theme: "midnight" });
    expect(() => new vm.Script(src)).not.toThrow();
  });

  it("serves declared state through the bindings", async () => {
    const src = buildInitScript({ state: { version: "9.9.9" }, theme: "midnight" });
    const sb = runInSandbox(src);
    await expect(sb.window.go.main.App.AppVersion()).resolves.toBe("9.9.9");
  });

  it("keeps experimental agent sessions disabled in public captures", async () => {
    const src = buildInitScript({ state: {}, theme: "midnight" });
    const sb = runInSandbox(src);
    await expect(sb.window.go.main.App.GetAgentSessionsEnabled()).resolves.toBe(false);
    await expect(sb.window.go.main.App.ListAgentProfiles()).resolves.toEqual([]);
    await expect(sb.window.go.main.App.ListAgentSessions("", true)).resolves.toEqual([]);
  });

  it("seeds the theme into localStorage under the app's key", () => {
    const src = buildInitScript({ state: {}, theme: "aurora" });
    const sb = runInSandbox(src);
    expect(sb.localStorage.getItem("l4e:theme")).toBe("aurora");
  });

  it("records the name of any binding it does not mock", async () => {
    const src = buildInitScript({ state: {}, theme: "midnight" });
    const sb = runInSandbox(src);
    await sb.window.go.main.App.SomeUnknownBinding();
    expect(sb.window.__harness.missing).toContain("SomeUnknownBinding");
  });

  it("records each missing binding once, however often it is called", async () => {
    const src = buildInitScript({ state: {}, theme: "midnight" });
    const sb = runInSandbox(src);
    await sb.window.go.main.App.Repeated();
    await sb.window.go.main.App.Repeated();
    expect(sb.window.__harness.missing.filter((n) => n === "Repeated")).toHaveLength(1);
  });

  it("returns a canceler from EventsOn rather than undefined", () => {
    const src = buildInitScript({ state: {}, theme: "midnight" });
    const sb = runInSandbox(src);
    expect(typeof sb.window.runtime.EventsOn("anything", () => {})).toBe("function");
  });

  it("delivers emitted events to EventsOn subscribers", () => {
    const src = buildInitScript({ state: {}, theme: "midnight" });
    const sb = runInSandbox(src);
    const seen = [];
    sb.window.runtime.EventsOn("metrics", (s) => seen.push(s));
    sb.window.__harness.emit("metrics", { cpuPercent: 42 });
    expect(seen).toEqual([{ cpuPercent: 42 }]);
  });

  it("stops delivering after the canceler runs", () => {
    const src = buildInitScript({ state: {}, theme: "midnight" });
    const sb = runInSandbox(src);
    const seen = [];
    const off = sb.window.runtime.EventsOn("metrics", (s) => seen.push(s));
    off();
    sb.window.__harness.emit("metrics", { cpuPercent: 42 });
    expect(seen).toEqual([]);
  });

  it("serves Doctor reports through the project and overview bindings", async () => {
    const reports = [{ project: "acme-shop", healthy: true, findings: [] }, { project: "beacon-api", healthy: false, findings: [] }];
    const sb = runInSandbox(buildInitScript({ state: { doctor: reports } }));
    await expect(sb.window.go.main.App.DiagnoseProject("acme-shop")).resolves.toEqual(reports[0]);
    await expect(sb.window.go.main.App.DiagnoseAllProjects()).resolves.toEqual(reports);
    expect(sb.window.__harness.missing).toEqual([]);
  });

  it("restricts fictional workspace reads to their declared checkout and relative file", async () => {
    const reference = { context: { kind: "project", projectName: "acme-shop" }, rootKey: "root-acme" };
    const root = { rootKey: reference.rootKey, path: "C:/sites/acme-shop", canonicalRoot: "C:/sites/acme-shop", canRead: true, canWrite: true };
    const document = { text: "# Demo", revision: "demo-1" };
    const sb = runInSandbox(buildInitScript({ state: { workspaceFiles: { project: "acme-shop", root, files: { "README.md": document } } } }));
    await expect(sb.window.go.main.App.GetWorkspaceFileContext(reference.context)).resolves.toEqual({ code: "ok", context: root });
    await expect(sb.window.go.main.App.ReadWorkspaceDocument(reference.context, reference.rootKey, "README.md")).resolves.toEqual({ code: "ok", document });
    await expect(sb.window.go.main.App.ReadWorkspaceDocument(reference.context, "other-root", "README.md")).resolves.toMatchObject({ code: "unavailable" });
    await expect(sb.window.go.main.App.ReadWorkspaceDocument(reference.context, reference.rootKey, "../README.md")).resolves.toMatchObject({ code: "unavailable" });
    await expect(sb.window.go.main.App.GetWorkspaceFileContext({ kind: "project", projectName: "unknown" })).resolves.toMatchObject({ code: "unavailable" });
  });
});
