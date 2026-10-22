// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import AgentLauncherMenu, { type AgentProfile } from "../../AgentLauncherMenu";
import { SelectAgentContextDirectory } from "../../../../wailsjs/go/main/App";
import { I18nProvider } from "../../../lib/i18n";

vi.mock("../../../../wailsjs/go/main/App", () => ({
  GetAgentGitWorkspace: vi.fn(async () => ({ repository: true, currentBranch: "main", branches: [], worktrees: [] })),
  ListAgentSessions: vi.fn(async () => []),
  ListProjectRoutes: vi.fn(async () => []),
  RestoreCanonicalRoute: vi.fn(async () => []),
  SuggestWorktreePreviewHostname: vi.fn(async () => "preview.local"),
  SelectAgentContextDirectory: vi.fn(),
  GetLanguage: vi.fn(async () => "en"),
  SetLanguage: vi.fn(async () => {}),
}));
vi.mock("../../../../wailsjs/runtime", () => ({ EventsOn: () => () => {} }));
vi.mock("../../PortalMenu", () => ({ default: ({ open, children }: { open: boolean; children: React.ReactNode }) => open ? <div>{children}</div> : null }));

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

const profiles: AgentProfile[] = [
  { id: "claude", label: "Claude Code", available: true, capabilities: { launch: true, resume: true, richStatus: true, history: true, usage: true, additionalDirectories: true } },
  { id: "opencode", label: "OpenCode", available: true, capabilities: { launch: true, resume: true, richStatus: true, history: true, usage: false, additionalDirectories: false } },
];

let host: HTMLDivElement;
let root: Root;
let onLaunch: ReturnType<typeof vi.fn>;

async function render(projectName = "laraenv", projectPath = "C:\\apps\\laraenv") {
  await act(async () => root.render(
    <I18nProvider>
      <AgentLauncherMenu projectName={projectName} projectPath={projectPath} canonicalHost="laraenv.local" mainProxyPort={0} profiles={profiles} open setOpen={vi.fn()} onLaunch={onLaunch} notify={vi.fn()} />
    </I18nProvider>,
  ));
}
function button(text: string) {
  const result = [...host.querySelectorAll("button")].find((item) => item.textContent?.trim() === text);
  if (!result) throw new Error(`Missing button: ${text}`);
  return result;
}
async function browse(path: string) {
  vi.mocked(SelectAgentContextDirectory).mockResolvedValueOnce(path);
  await act(async () => button("Browse").click());
}
function typePath(value: string) {
  const input = host.querySelector<HTMLInputElement>('input[aria-label="Additional folder path"]')!;
  act(() => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!.call(input, value);
    input.dispatchEvent(new Event("input", { bubbles: true }));
  });
  act(() => (host.querySelector('[aria-label="Add folder path"]') as HTMLButtonElement).click());
}

beforeEach(() => {
  vi.clearAllMocks();
  host = document.createElement("div");
  document.body.appendChild(host);
  root = createRoot(host);
  onLaunch = vi.fn();
});
afterEach(() => {
  act(() => root.unmount());
  host.remove();
});

describe("agent session folder context", () => {
  it("launches with three additional folders from different locations", async () => {
    await render();
    await browse("D:\\shared\\laraenv api");
    await browse("C:\\work\\server");
    typePath('"E:\\clients\\dashboard"');
    expect(host.textContent).toContain("4 folders");
    act(() => button("Start Claude Code").click());
    expect(onLaunch).toHaveBeenCalledWith(profiles[0], expect.objectContaining({
      mode: "current",
      additionalDirectories: ["D:\\shared\\laraenv api", "C:\\work\\server", "E:\\clients\\dashboard"],
    }));
  });

  it("rejects relative and duplicate paths, handles picker cancellation, and removes folders", async () => {
    await render();
    typePath("../server");
    expect(host.querySelector('[role="alert"]')?.textContent).toContain("full folder path");
    await browse("c:/APPS/laraenv/");
    expect(host.querySelector('[role="alert"]')?.textContent).toContain("already in the session");
    await browse("D:\\api");
    await browse("d:/API/");
    expect(host.textContent).toContain("2 folders");
    await browse("");
    expect(host.textContent).toContain("2 folders");
    act(() => (host.querySelector('button[aria-label="Remove folder D:\\\\api"]') as HTMLButtonElement).click());
    expect(host.textContent).toContain("1 folder");
    act(() => button("Start Claude Code").click());
    expect(onLaunch).toHaveBeenCalledWith(profiles[0], expect.objectContaining({ additionalDirectories: undefined }));
  });

  it("blocks providers without folder support until the user removes the folders", async () => {
    await render();
    await browse("D:\\api");
    act(() => [...host.querySelectorAll("button")].find((item) => item.textContent?.startsWith("OpenCode"))!.click());
    expect(button("Start OpenCode").disabled).toBe(true);
    expect(host.textContent).toContain("Remove them or choose");
    act(() => (host.querySelector('button[aria-label^="Remove folder"]') as HTMLButtonElement).click());
    expect(button("Start OpenCode").disabled).toBe(false);
  });

  it("clears additional context when changing the primary project", async () => {
    await render();
    await browse("D:\\api");
    await render("another", "C:\\apps\\another");
    expect(host.textContent).not.toContain("D:\\api");
    expect(host.textContent).toContain("1 folder");
  });
});
