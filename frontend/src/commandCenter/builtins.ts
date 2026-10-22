// Auto-generated palette commands built from live app state: navigation to each
// page, service controls (from ListServices), and per-project actions (from
// ListProjects). Merged with the user's custom commands by useCommandCenter.

// @ts-ignore
import {
  ListServices, ListProjects,
  StartService, StopService, ReloadService,
  StartManagedAll, StopManagedAll, ReloadStack,
  OpenProjectFolder, OpenProjectInEditor, OpenURL, OpenProjectTerminal,
} from "../../wailsjs/go/main/App";
import type { CommandContext, RuntimeCommand } from "./types";

// Keep in sync with Sidebar's Page type. `id` is compared/used to navigate
// (never translated); `labelKey` resolves the display word shown in "Go to
// {{page}}" and is resolved with ctx.t at build time — this module is not a
// component and cannot call useT() itself.
export const PAGES: { id: string; labelKey: string }[] = [
  { id: "dashboard", labelKey: "commandCenter.pages.dashboard" },
  { id: "projects", labelKey: "commandCenter.pages.projects" },
  { id: "terminal", labelKey: "commandCenter.pages.terminal" },
  { id: "ssh", labelKey: "commandCenter.pages.ssh" },
  { id: "cron", labelKey: "commandCenter.pages.cron" },
  { id: "deployments", labelKey: "commandCenter.pages.deployments" },
  { id: "downloads", labelKey: "commandCenter.pages.downloads" },
  { id: "settings", labelKey: "commandCenter.pages.settings" },
];

// New-project palette entries — one per type supported by NewProjectModal.
// Keep kinds in sync with the TYPES array in NewProjectModal.tsx. `hint` stays
// untranslated: it mirrors NewProjectModal's own `desc` for most of these
// kinds, which are either literal shell commands ("git clone <url>") or short
// technical taglines NewProjectModal itself leaves as English data.
const NEW_PROJECT_KINDS: { kind: string; labelKey: string; hint: string }[] = [
  { kind: "laravel",   labelKey: "commandCenter.builtins.newProject.laravel",   hint: "composer create-project laravel" },
  { kind: "wordpress", labelKey: "commandCenter.builtins.newProject.wordpress", hint: "latest WP from wordpress.org" },
  { kind: "git",       labelKey: "commandCenter.builtins.newProject.git",       hint: "git clone <url>" },
  { kind: "blank",     labelKey: "commandCenter.builtins.newProject.blank",     hint: "empty folder + welcome page" },
  { kind: "vite",      labelKey: "commandCenter.builtins.newProject.vite",      hint: "create vite (react/vue/svelte/…)" },
  { kind: "next",      labelKey: "commandCenter.builtins.newProject.next",      hint: "create-next-app (React + SSR)" },
  { kind: "nuxt",      labelKey: "commandCenter.builtins.newProject.nuxt",      hint: "create nuxt (Vue + SSR)" },
  { kind: "astro",     labelKey: "commandCenter.builtins.newProject.astro",     hint: "create astro (content-driven)" },
];

export async function buildBuiltinCommands(ctx: CommandContext, _agentSessionsEnabled = true): Promise<RuntimeCommand[]> {
  const cmds: RuntimeCommand[] = [];
  const { t } = ctx;
  const groupNavigation = t("commandCenter.groups.navigation");
  const groupServices = t("commandCenter.groups.services");
  // Same word, same sense as the "Go to Projects" page label — reused rather
  // than minted twice (see commandCenter.pages.projects).
  const groupProjects = t("commandCenter.pages.projects");

  // --- Navigation ---
  for (const p of PAGES) {
    cmds.push({ id: `nav:${p.id}`, label: t("commandCenter.builtins.goTo", { page: t(p.labelKey) }), group: groupNavigation, intent: { action: "navigate", page: p.id }, run: () => ctx.navigate(p.id) });
  }

  // --- Services ---
  cmds.push({ id: "svc:startAll", label: t("commandCenter.builtins.startAllServices"), group: groupServices, intent: { action: "service", serviceOp: "startAll" }, run: async () => { await StartManagedAll(); ctx.flash(t("commandCenter.flash.startingAllServices")); } });
  cmds.push({ id: "svc:stopAll", label: t("commandCenter.builtins.stopAllServices"), group: groupServices, intent: { action: "service", serviceOp: "stopAll" }, run: async () => { await StopManagedAll(); ctx.flash(t("commandCenter.flash.stoppingAllServices")); } });
  cmds.push({ id: "svc:reloadStack", label: t("commandCenter.builtins.reloadWebStack"), group: groupServices, intent: { action: "service", serviceOp: "reloadStack" }, run: async () => { await ReloadStack(); ctx.flash(t("commandCenter.flash.reloadingStack")); } });
  try {
    const svcs: { name: string; label: string }[] = await ListServices();
    for (const s of svcs) {
      cmds.push({ id: `svc:start:${s.name}`, label: t("commandCenter.builtins.startService", { service: s.label }), group: groupServices, intent: { action: "service", serviceOp: "start", service: s.name }, run: async () => { await StartService(s.name); ctx.flash(t("commandCenter.flash.startingService", { service: s.label })); } });
      cmds.push({ id: `svc:stop:${s.name}`, label: t("commandCenter.builtins.stopService", { service: s.label }), group: groupServices, intent: { action: "service", serviceOp: "stop", service: s.name }, run: async () => { await StopService(s.name); ctx.flash(t("commandCenter.flash.stoppingService", { service: s.label })); } });
      cmds.push({ id: `svc:reload:${s.name}`, label: t("commandCenter.builtins.reloadService", { service: s.label }), group: groupServices, intent: { action: "service", serviceOp: "reload", service: s.name }, run: async () => { await ReloadService(s.name); ctx.flash(t("commandCenter.flash.reloadingService", { service: s.label })); } });
    }
  } catch (e) { console.error("[commandCenter] ListServices failed", e); }

  // --- Projects ---
  // One entry per supported project type; selecting it opens the New Project
  // form pre-selected to that kind (kinds/hints mirror NewProjectModal's TYPES).
  for (const kind of NEW_PROJECT_KINDS) {
    cmds.push({
      id: `proj:new:${kind.kind}`, label: t(kind.labelKey), group: groupProjects, hint: kind.hint,
      intent: { action: "newProject", projectKind: kind.kind },
      run: () => ctx.newProject(kind.kind),
    });
  }
  try {
    const projs: { name: string; url: string }[] = await ListProjects();
    for (const p of projs) {
      cmds.push({
        id: `proj:term:${p.name}`,
        label: t("commandCenter.builtins.openTerminal", { name: p.name }),
        group: groupProjects,
        // Tab fills "<project> " so the user can type a command (php artisan / npm)
        // and run it in that project; Enter just opens the terminal.
        complete: `${p.name} `,
        intent: { action: "openTerminal", project: p.name },
        // Enter → native external terminal; Shift+Enter (altRun) → in-app xterm.js tab.
        run: () => { void OpenProjectTerminal(p.name, ""); },
        altRun: () => ctx.openTerminal({ project: p.name }),
      });
      cmds.push({ id: `proj:folder:${p.name}`, label: t("commandCenter.builtins.openFolder", { name: p.name }), group: groupProjects, intent: { action: "openFolder", project: p.name }, run: async () => { await OpenProjectFolder(p.name); } });
      cmds.push({ id: `proj:editor:${p.name}`, label: t("commandCenter.builtins.openInEditor", { name: p.name }), group: groupProjects, intent: { action: "openEditor", project: p.name }, run: async () => { await OpenProjectInEditor(p.name, ""); } });
      if (p.url) cmds.push({ id: `proj:browser:${p.name}`, label: t("commandCenter.builtins.openInBrowser", { name: p.name }), group: groupProjects, hint: p.url, intent: { action: "openBrowser", url: p.url }, run: async () => { await OpenURL(p.url); } });
    }
  } catch (e) { console.error("[commandCenter] ListProjects failed", e); }

  return cmds;
}

export function buildOpenTerminalCommands(ctx: Pick<CommandContext, "t" | "openTerminals" | "focusTerminal">): RuntimeCommand[] {
  return (ctx.openTerminals ?? []).map((terminal) => ({
    id: `terminal:focus:${terminal.terminalId}`,
    label: terminal.label,
    group: ctx.t("commandCenter.groups.openTerminals"),
    hint: [terminal.active ? ctx.t("commandCenter.builtins.currentTerminal") : "", terminal.hint].filter(Boolean).join(" · "),
    intent: { action: "focusTerminal", terminalId: terminal.terminalId },
    run: () => ctx.focusTerminal?.(terminal.terminalId),
  }));
}
