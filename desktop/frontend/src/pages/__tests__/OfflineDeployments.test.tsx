// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import Deployments from "../Deployments";
import { DialogProvider } from "../../components/dialogs/DialogProvider";

const bridge = vi.hoisted(() => ({
  ListDeployments: vi.fn(), GetDeployment: vi.fn(), CreateDeployment: vi.fn(),
  DeleteDeployment: vi.fn(), DetectDeployment: vi.fn(), ApplyDeployment: vi.fn(),
  DeployDeployment: vi.fn(), DestroyDeployment: vi.fn(), DeploymentActivity: vi.fn(),
  ImportDeploymentsJSON: vi.fn(), ImportCloudDeployments: vi.fn(), ReadDeploymentRunLog: vi.fn(),
  CloudStatus: vi.fn(), CloudListDeployments: vi.fn(), CloudGetDeployment: vi.fn(),
  CloudCreateDeployment: vi.fn(), CloudDeleteDeployment: vi.fn(), CloudDeploymentDetect: vi.fn(), CloudDeploymentApply: vi.fn(), CloudDeploymentDeploy: vi.fn(),
  ListSSHHosts: vi.fn(), ListDeployPresets: vi.fn(),
}));
const eventHandlers = vi.hoisted(() => new Map<string, (...args: any[]) => void>());
vi.mock("../../../wailsjs/go/main/App", () => new Proxy(bridge, {
  get(target, key) { return key === "then" ? undefined : key in target ? target[key as keyof typeof target] : async () => []; },
}));
vi.mock("../../../wailsjs/runtime", () => ({
  EventsOn: (name: string, callback: (...args: any[]) => void) => { eventHandlers.set(name, callback); return () => eventHandlers.delete(name); },
  EventsOff: (name: string) => eventHandlers.delete(name),
}));
vi.mock("../../lib/i18n", () => ({
  useT: () => (key: string) => key,
  useI18n: () => ({ formatDate: () => "fixture date", formatRelative: () => "fixture date" }),
}));

const localID = "562b6490-c2f4-4a52-af9d-042d0b441051";
const local = {
  id: localID, name: "Local storefront", ssh_host_external_id: "local-host",
  os_family: "ubuntu", last_status: "ok",
  config: { apply: { name: "original-remote-name", domain: "example.test", git_url: "https://example.test/repo.git", build_script: "echo ready" } },
};
const savedPro = { enabled: true, loggedIn: true, plan: "pro", entitlements: { deployments: true }, baseUrl: "https://example.invalid" };
function mount(status: any = { enabled: false, loggedIn: false }) {
  return render(<DialogProvider><Deployments flash={vi.fn()} status={status} /></DialogProvider>);
}

beforeEach(() => {
  vi.clearAllMocks();
  eventHandlers.clear();
  bridge.ListDeployments.mockResolvedValue([local]);
  bridge.GetDeployment.mockResolvedValue(local);
  bridge.ListSSHHosts.mockResolvedValue([{ name: "local-host", address: "192.0.2.2" }]);
  bridge.ListDeployPresets.mockResolvedValue([]);
  bridge.CloudStatus.mockResolvedValue(savedPro);
  bridge.CloudDeploymentDetect.mockResolvedValue(undefined);
  bridge.CloudDeploymentDeploy.mockResolvedValue(undefined);
  bridge.CloudListDeployments.mockResolvedValue([{ id: 17, name: "Cloud storefront", ssh_host_external_id: "old-host" }]);
  bridge.ImportDeploymentsJSON.mockResolvedValue({ inserted: [localID], skipped: [], conflicts: [] });
  bridge.ImportCloudDeployments.mockResolvedValue({ inserted: [localID], skipped: [], conflicts: [] });
});
afterEach(cleanup);

describe("offline deployments", () => {
  it.each([{ enabled: false, loggedIn: false }, { enabled: false, loggedIn: true, plan: "free" }, savedPro])("opens the local list without account requests for %j", async (status) => {
    mount(status);
    expect(await screen.findByText("Local storefront")).toBeTruthy();
    expect(screen.queryByText("deployments.proFeature.title")).toBeNull();
    expect(bridge.ListDeployments).toHaveBeenCalled();
    expect(bridge.CloudStatus).not.toHaveBeenCalled();
    expect(bridge.CloudListDeployments).not.toHaveBeenCalled();
  });

  it("creates a local destination without an account", async () => {
    mount();
    fireEvent.click(await screen.findByRole("button", { name: "deployments.newButton.label" }));
    fireEvent.change(screen.getByPlaceholderText("prod-web"), { target: { value: "New local site" } });
    fireEvent.click(screen.getByRole("button", { name: "deployments.newModal.createButton" }));
    await waitFor(() => expect(bridge.CreateDeployment).toHaveBeenCalledWith("New local site", "local-host"));
    expect(bridge.CloudCreateDeployment).not.toHaveBeenCalled();
  });

  it("applies saved local configuration using the original remote name", async () => {
    mount({ enabled: false, loggedIn: true, plan: "free" });
    fireEvent.click(await screen.findByRole("button", { name: "deployments.card.applyButton" }));
    fireEvent.click(await screen.findByRole("button", { name: "deployments.applyModal.runApplyButton" }));
    await waitFor(() => expect(bridge.ApplyDeployment).toHaveBeenCalledWith(localID, expect.objectContaining({ name: "original-remote-name", domain: "example.test" })));
    expect(bridge.CloudGetDeployment).not.toHaveBeenCalled();
    expect(bridge.CloudDeploymentApply).not.toHaveBeenCalled();
  });

  it("preserves the protected local PAT when reapplying a saved destination", async () => {
    bridge.GetDeployment.mockResolvedValue({ ...local, config: { apply: { ...local.config.apply, git_pat: "fixture-protected-pat" } } });
    mount();
    fireEvent.click(await screen.findByRole("button", { name: "deployments.card.applyButton" }));
    fireEvent.click(await screen.findByRole("button", { name: "deployments.applyModal.runApplyButton" }));
    await waitFor(() => expect(bridge.ApplyDeployment).toHaveBeenCalledWith(localID, expect.objectContaining({ git_pat: "fixture-protected-pat" })));
  });

  it("discards cloud configuration fetched before leaving and returning to cloud mode", async () => {
    let finishConfig!: (value: unknown) => void;
    bridge.CloudGetDeployment.mockImplementation(() => new Promise(resolve => { finishConfig = resolve; }));
    bridge.CloudListDeployments.mockResolvedValue([{ ...local, id: 17, name: "Cloud storefront" }]);
    mount(savedPro);
    fireEvent.click(await screen.findByRole("button", { name: "offlineDeployments.cloud" }));
    await screen.findByText("Cloud storefront");
    fireEvent.click(screen.getByRole("button", { name: "deployments.card.applyButton" }));
    await waitFor(() => expect(bridge.CloudGetDeployment).toHaveBeenCalledWith(17));
    fireEvent.click(screen.getByRole("button", { name: "offlineDeployments.local" }));
    await screen.findByText("Local storefront");
    fireEvent.click(screen.getByRole("button", { name: "offlineDeployments.cloud" }));
    await screen.findByText("Cloud storefront");
    await act(async () => finishConfig({ ...local, id: 17, name: "Obsolete cloud configuration" }));
    expect(screen.queryByRole("button", { name: "deployments.applyModal.runApplyButton" })).toBeNull();
  });

  it("rejects stale cloud list results after opt-out", async () => {
    let finishCloud!: (value: unknown) => void;
    bridge.CloudListDeployments.mockImplementation(() => new Promise(resolve => { finishCloud = resolve; }));
    const view = mount(savedPro);
    await screen.findByText("Local storefront");
    fireEvent.click(screen.getByRole("button", { name: "offlineDeployments.cloud" }));
    await waitFor(() => expect(bridge.CloudListDeployments).toHaveBeenCalledOnce());
    view.rerender(<DialogProvider><Deployments flash={vi.fn()} status={{ ...savedPro, enabled: false }} /></DialogProvider>);
    expect(await screen.findByText("Local storefront")).toBeTruthy();
    await act(async () => finishCloud([{ id: 17, name: "Stale cloud row", ssh_host_external_id: "old-host" }]));
    expect(screen.queryByText("Stale cloud row")).toBeNull();
    expect(screen.queryByRole("button", { name: "offlineDeployments.cloud" })).toBeNull();
  });

  it.each([
    { email: "second@example.invalid" },
    { baseUrl: "https://second-service.invalid" },
    { githubId: "second-github-account" },
    { accountId: "second-account-id" },
  ])("reloads hosted data for a changed profile and rejects the prior pending list: %j", async (changed) => {
    let finishOldList!: (value: unknown) => void;
    const initial = { ...savedPro, email: "first@example.invalid", githubId: "first-github-account", accountId: "first-account-id" };
    bridge.CloudListDeployments.mockImplementationOnce(() => new Promise(resolve => { finishOldList = resolve; }));
    bridge.CloudListDeployments.mockResolvedValue([{ id: 17, name: "Second profile destination", ssh_host_external_id: "second-host" }]);
    const view = mount(initial);
    fireEvent.click(await screen.findByRole("button", { name: "offlineDeployments.cloud" }));
    await waitFor(() => expect(bridge.CloudListDeployments).toHaveBeenCalledOnce());
    view.rerender(<DialogProvider><Deployments flash={vi.fn()} status={{ ...initial, ...changed } as any} /></DialogProvider>);
    expect(await screen.findByText("Second profile destination")).toBeTruthy();
    expect(bridge.CloudListDeployments).toHaveBeenCalledTimes(2);
    await act(async () => finishOldList([{ id: 17, name: "First profile stale destination", ssh_host_external_id: "first-host" }]));
    expect(screen.queryByText("First profile stale destination")).toBeNull();
  });

  it("removes old hosted cards and apply forms before their IDs can be used with a new account", async () => {
    let finishNewList!: (value: unknown) => void;
    bridge.CloudListDeployments.mockResolvedValueOnce([{ ...local, id: 17, name: "First account destination" }]);
    bridge.CloudListDeployments.mockImplementation(() => new Promise(resolve => { finishNewList = resolve; }));
    bridge.CloudGetDeployment.mockResolvedValue({ ...local, id: 17, name: "First account destination" });
    const initial = { ...savedPro, email: "first@example.invalid" };
    const view = mount(initial);
    fireEvent.click(await screen.findByRole("button", { name: "offlineDeployments.cloud" }));
    await screen.findByText("First account destination");
    const oldDeployButton = screen.getByRole("button", { name: "deployments.card.deployButton" });
    fireEvent.click(screen.getByRole("button", { name: "deployments.card.applyButton" }));
    const oldApplyButton = await screen.findByRole("button", { name: "deployments.applyModal.runApplyButton" });
    view.rerender(<DialogProvider><Deployments flash={vi.fn()} status={{ ...initial, email: "second@example.invalid" }} /></DialogProvider>);
    expect(screen.queryByText("First account destination")).toBeNull();
    expect(screen.queryByRole("button", { name: "deployments.applyModal.runApplyButton" })).toBeNull();
    fireEvent.click(oldApplyButton);
    fireEvent.click(oldDeployButton);
    expect(bridge.CloudDeploymentApply).not.toHaveBeenCalled();
    expect(bridge.CloudDeploymentDeploy).not.toHaveBeenCalled();
    await waitFor(() => expect(bridge.CloudListDeployments).toHaveBeenCalledTimes(2));
    await act(async () => finishNewList([{ id: 17, name: "Second account destination", ssh_host_external_id: "second-host" }]));
    expect(await screen.findByText("Second account destination")).toBeTruthy();
  });

  it("preserves local data and a local run across hosted account changes", async () => {
    const initial = { ...savedPro, email: "first@example.invalid" };
    const view = mount(initial);
    fireEvent.click(await screen.findByRole("button", { name: "deployments.card.deployButton" }));
    view.rerender(<DialogProvider><Deployments flash={vi.fn()} status={{ ...initial, email: "second@example.invalid", baseUrl: "https://second-service.invalid" }} /></DialogProvider>);
    act(() => eventHandlers.get(`deployment:deploy:log:${localID}`)?.("local run survived the cloud profile change"));
    expect(await screen.findByText("local run survived the cloud profile change")).toBeTruthy();
    expect(screen.getByText("Local storefront")).toBeTruthy();
    expect(bridge.ListDeployments).toHaveBeenCalledOnce();
    expect(bridge.CloudListDeployments).not.toHaveBeenCalled();
    expect(bridge.DeployDeployment).toHaveBeenCalledOnce();
  });

  it("keeps a new profile run subscribed when an old profile completion arrives for the same numeric ID", async () => {
    bridge.CloudListDeployments.mockResolvedValue([{ ...local, id: 17, name: "Hosted destination" }]);
    const initial = { ...savedPro, email: "first@example.invalid" };
    const view = mount(initial);
    fireEvent.click(await screen.findByRole("button", { name: "offlineDeployments.cloud" }));
    fireEvent.click(await screen.findByRole("button", { name: "deployments.card.deployButton" }));
    const oldLog = eventHandlers.get("deployment:deploy:log:17")!;
    const oldDone = eventHandlers.get("deployment:deploy:done:17")!;
    view.rerender(<DialogProvider><Deployments flash={vi.fn()} status={{ ...initial, email: "second@example.invalid" }} /></DialogProvider>);
    await waitFor(() => expect(bridge.CloudListDeployments).toHaveBeenCalledTimes(2));
    fireEvent.click(await screen.findByRole("button", { name: "deployments.card.deployButton" }));
    act(() => { oldLog("first profile stale log"); oldDone("first-profile.example.invalid"); });
    act(() => eventHandlers.get("deployment:deploy:log:17")?.("second profile is still streaming"));
    expect(await screen.findByText("second profile is still streaming")).toBeTruthy();
    expect(screen.queryByText("first profile stale log")).toBeNull();
    expect(bridge.CloudListDeployments).toHaveBeenCalledTimes(2);
  });

  it("rejects a hosted delete confirmation opened before the account changed", async () => {
    bridge.CloudListDeployments.mockResolvedValue([{ ...local, id: 17, name: "Hosted destination" }]);
    const initial = { ...savedPro, email: "first@example.invalid" };
    const view = mount(initial);
    fireEvent.click(await screen.findByRole("button", { name: "offlineDeployments.cloud" }));
    fireEvent.click(await screen.findByTitle("deployments.card.deleteAppliedTooltip"));
    const confirm = await screen.findByRole("button", { name: "deployments.deleteLabel" });
    view.rerender(<DialogProvider><Deployments flash={vi.fn()} status={{ ...initial, email: "second@example.invalid" }} /></DialogProvider>);
    fireEvent.click(confirm);
    await act(async () => {});
    expect(bridge.CloudDeleteDeployment).not.toHaveBeenCalled();
  });

  it.each(["detect", "deploy"] as const)("keeps the new profile's %s listeners when the old profile's pending RPC rejects", async (kind) => {
    let rejectOld!: (reason: Error) => void;
    const operation = kind === "detect" ? bridge.CloudDeploymentDetect : bridge.CloudDeploymentDeploy;
    operation.mockImplementationOnce(() => new Promise((_, reject) => { rejectOld = reject; }));
    bridge.CloudListDeployments.mockResolvedValue([{ ...local, id: 17, name: "Hosted destination" }]);
    const initial = { ...savedPro, email: "first@example.invalid" };
    const view = mount(initial);
    fireEvent.click(await screen.findByRole("button", { name: "offlineDeployments.cloud" }));
    const button = kind === "detect" ? "deployments.card.detectButton" : "deployments.card.deployButton";
    fireEvent.click(await screen.findByRole("button", { name: button }));
    await waitFor(() => expect(operation).toHaveBeenCalledOnce());
    view.rerender(<DialogProvider><Deployments flash={vi.fn()} status={{ ...initial, email: "second@example.invalid" }} /></DialogProvider>);
    await waitFor(() => expect(bridge.CloudListDeployments).toHaveBeenCalledTimes(2));
    fireEvent.click(await screen.findByRole("button", { name: button }));
    await waitFor(() => expect(operation).toHaveBeenCalledTimes(2));
    await act(async () => rejectOld(new Error("first profile request was canceled")));
    act(() => eventHandlers.get(`deployment:${kind}:log:17`)?.("second profile callback remains active"));
    expect(await screen.findByText("second profile callback remains active")).toBeTruthy();
    expect(screen.queryByText("first profile request was canceled")).toBeNull();
  });

  it("keeps local run events attached to their original source while switching lists", async () => {
    mount(savedPro);
    fireEvent.click(await screen.findByRole("button", { name: "deployments.card.deployButton" }));
    await waitFor(() => expect(bridge.DeployDeployment).toHaveBeenCalledWith(localID));
    fireEvent.click(screen.getByRole("button", { name: "offlineDeployments.cloud" }));
    await screen.findByText("Cloud storefront");
    act(() => eventHandlers.get(`deployment:deploy:log:${localID}`)?.("local pipeline still running"));
    expect(await screen.findByText("local pipeline still running")).toBeTruthy();
    expect(bridge.CloudDeploymentDeploy).not.toHaveBeenCalled();
  });

  it("previews file imports and requires explicit host mapping before saving", async () => {
    mount();
    fireEvent.click(await screen.findByRole("button", { name: "offlineDeployments.importJson" }));
    const bundle = { version: 1, source: "fixture-source", deployments: [{ id: "17", name: "Imported site", ssh_host_external_id: "old-host", config: { apply: { name: "remote-site" } } }] };
    fireEvent.change(screen.getByRole("textbox", { name: "offlineDeployments.importContent" }), { target: { value: JSON.stringify(bundle) } });
    fireEvent.click(screen.getByRole("button", { name: "offlineDeployments.previewImport" }));
    expect(await screen.findByText("Imported site")).toBeTruthy();
    expect(bridge.ImportDeploymentsJSON).not.toHaveBeenCalled();
    const confirm = screen.getByRole("button", { name: "offlineDeployments.confirmImport" }) as HTMLButtonElement;
    expect(confirm.disabled).toBe(true);
    fireEvent.change(screen.getByRole("combobox", { name: "old-host" }), { target: { value: "local-host" } });
    fireEvent.click(confirm);
    await waitFor(() => expect(bridge.ImportDeploymentsJSON).toHaveBeenCalledOnce());
    expect(JSON.parse(bridge.ImportDeploymentsJSON.mock.calls[0][0])).toMatchObject({ host_mappings: { "old-host": "local-host" } });
    expect(bridge.ApplyDeployment).not.toHaveBeenCalled();
    expect(bridge.DeployDeployment).not.toHaveBeenCalled();
  });

  it("imports a selected cloud destination with an explicit local host", async () => {
    mount(savedPro);
    fireEvent.click(await screen.findByRole("button", { name: "offlineDeployments.cloud" }));
    fireEvent.click(await screen.findByRole("button", { name: "offlineDeployments.importCloud" }));
    fireEvent.change(screen.getByRole("combobox", { name: "old-host" }), { target: { value: "local-host" } });
    fireEvent.click(screen.getByRole("button", { name: "offlineDeployments.confirmImport" }));
    await waitFor(() => expect(bridge.ImportCloudDeployments).toHaveBeenCalledWith([17], { "old-host": "local-host" }));
    expect(bridge.CloudDeploymentApply).not.toHaveBeenCalled();
  });

  it("reports import conflicts without running or replacing a destination", async () => {
    bridge.ImportCloudDeployments.mockResolvedValue({ inserted: [], skipped: [], conflicts: [{ id: "17", existing_id: localID, reason: "Existing local edits were preserved" }] });
    mount(savedPro);
    fireEvent.click(await screen.findByRole("button", { name: "offlineDeployments.cloud" }));
    fireEvent.click(await screen.findByRole("button", { name: "offlineDeployments.importCloud" }));
    fireEvent.change(screen.getByRole("combobox", { name: "old-host" }), { target: { value: "local-host" } });
    fireEvent.click(screen.getByRole("button", { name: "offlineDeployments.confirmImport" }));
    expect(await screen.findByText("17: Existing local edits were preserved")).toBeTruthy();
    expect(bridge.ApplyDeployment).not.toHaveBeenCalled();
    expect(bridge.DestroyDeployment).not.toHaveBeenCalled();
  });

  it("disables remote actions when protected local configuration is unavailable", async () => {
    bridge.ListDeployments.mockResolvedValue([{ ...local, secretsUnavailable: true }]);
    mount();
    expect(await screen.findByText("offlineDeployments.secretsUnavailable")).toBeTruthy();
    expect((screen.getByRole("button", { name: "deployments.card.applyButton" }) as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByRole("button", { name: "deployments.card.detectButton" }) as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByRole("button", { name: "deployments.card.deployButton" }) as HTMLButtonElement).disabled).toBe(true);
  });

  it("loads a protected run log and displays truncation and interruption state", async () => {
    bridge.GetDeployment.mockResolvedValue({ ...local, runs: [{ id: "fixture-run", kind: "apply", status: "interrupted", started_at: "2026-10-19T12:00:00Z", has_log: true, truncated: true, interrupted: true }] });
    bridge.ReadDeploymentRunLog.mockResolvedValue("fixture protected log body");
    mount();
    fireEvent.click(await screen.findByRole("button", { name: "offlineDeployments.history" }));
    expect(await screen.findByText("offlineDeployments.logTruncated")).toBeTruthy();
    expect(screen.getByText("offlineDeployments.runInterrupted")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "offlineDeployments.readLog" }));
    expect(await screen.findByText("fixture protected log body")).toBeTruthy();
    expect(bridge.ReadDeploymentRunLog).toHaveBeenCalledWith(localID, "fixture-run");
  });

  it("deletes only the local registration after confirmation", async () => {
    mount();
    fireEvent.click(await screen.findByTitle("deployments.card.deleteAppliedTooltip"));
    fireEvent.click(await screen.findByRole("button", { name: "deployments.deleteLabel" }));
    await waitFor(() => expect(bridge.DeleteDeployment).toHaveBeenCalledWith(localID));
    expect(bridge.DestroyDeployment).not.toHaveBeenCalled();
  });
});
