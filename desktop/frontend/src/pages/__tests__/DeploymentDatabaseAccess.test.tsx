// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { ApplyConfigModal } from "../Deployments";
import { I18nProvider } from "../../lib/i18n";
import { DialogProvider } from "../../components/dialogs/DialogProvider";

vi.mock("../../../wailsjs/go/main/App", () => ({
  GetLanguage: () => Promise.resolve("en"), SetLanguage: () => Promise.resolve(),
  ListDeployPresets: () => Promise.resolve([]),
}));
vi.mock("../../../wailsjs/runtime", () => ({ EventsOn: () => () => {}, EventsOff: () => {} }));
afterEach(cleanup);

function mount(apply: Record<string, unknown> = {}) {
  const submit = vi.fn();
  render(<I18nProvider><DialogProvider><ApplyConfigModal
    deployment={{ name: "my-app", config: { apply: { domain: "example.com", database: "mysql", build_script: "echo ready", ...apply } } } as any}
    onClose={() => {}} onSubmit={submit}
  /></DialogProvider></I18nProvider>);
  return submit;
}

describe("deployment database access", () => {
  it("keeps existing deployments local unless explicitly enabled", async () => {
    const submit = mount();
    const toggle = await screen.findByRole("checkbox", { name: /Allow remote database access/ });
    expect((toggle as HTMLInputElement).checked).toBe(false);
    expect(screen.queryByRole("textbox", { name: "Allowed IP addresses" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: /Run apply/i }));
    await waitFor(() => expect(submit).toHaveBeenCalled());
    expect(submit.mock.calls[0][0]).toMatchObject({ database_remote_access: false, database_allowed_ips: [] });
  });

  it("requires IPs and submits them when remote access is enabled", async () => {
    const submit = mount();
    fireEvent.click(await screen.findByRole("checkbox", { name: /Allow remote database access/ }));
    fireEvent.click(screen.getByRole("button", { name: /Run apply/i }));
    expect(await screen.findByText("Enter at least one allowed IP address.")).toBeTruthy();
    expect(submit).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "OK" }));
    fireEvent.change(screen.getByRole("textbox", { name: "Allowed IP addresses" }), { target: { value: "203.0.113.42, 198.51.100.5\n203.0.113.42" } });
    fireEvent.click(screen.getByRole("button", { name: /Run apply/i }));
    await waitFor(() => expect(submit).toHaveBeenCalled());
    expect(submit.mock.calls[0][0]).toMatchObject({ database_remote_access: true, database_allowed_ips: ["203.0.113.42", "198.51.100.5"] });
  });

  it("restores saved access settings and clears access when no database is selected", async () => {
    const submit = mount({ database_remote_access: true, database_allowed_ips: ["203.0.113.42"] });
    expect((await screen.findByRole("textbox", { name: "Allowed IP addresses" }) as HTMLTextAreaElement).value).toBe("203.0.113.42");
    const database = screen.getAllByRole("combobox").find((el) => el.querySelector('option[value="mysql"]'))!;
    fireEvent.change(database, { target: { value: "" } });
    expect(screen.queryByRole("checkbox", { name: /Allow remote database access/ })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: /Run apply/i }));
    await waitFor(() => expect(submit).toHaveBeenCalled());
    expect(submit.mock.calls[0][0]).toMatchObject({ database: "", database_remote_access: false, database_allowed_ips: [] });
  });
});
