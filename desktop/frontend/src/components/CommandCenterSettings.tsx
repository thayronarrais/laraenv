// Settings → Command Center tab. Lets the user rebind the palette hotkey and
// manage custom commands (add / edit / delete) with optional per-command
// hotkeys. Changes persist immediately via SetCommandCenter and notify the host
// (App) so the live palette/keybindings refresh.

import { useEffect, useRef, useState } from "react";
import clsx from "clsx";
import { Command, Plus, Pencil, Trash2, Keyboard } from "lucide-react";
import GlassCard from "./GlassCard";
// @ts-ignore
import { GetCommandCenter, SetCommandCenter, ListServices, ListProjects, ListTerminals } from "../../wailsjs/go/main/App";
import { PAGES } from "../commandCenter/builtins";
import { formatHotkey } from "../commandCenter/hotkeys";
import type { CommandAction, CommandCenterConfig, CommandShortcut, ServiceOp } from "../commandCenter/types";
import { useT } from "../lib/i18n";

const inputCls =
  "mt-1 w-full px-2 py-1.5 rounded-md text-sm bg-white/[0.04] border border-white/15 text-white focus:outline-none focus:border-accent/60";

// t() is called at render time (see the two components below) — these hold
// the catalogue key, not the label itself, so the option lists stay correct
// when the language changes.
const ACTIONS: { id: CommandAction; labelKey: string }[] = [
  { id: "terminal", labelKey: "settings.commandcenter.actions.terminal" },
  { id: "openTerminal", labelKey: "settings.commandcenter.actions.openTerminal" },
  { id: "service", labelKey: "settings.commandcenter.actions.service" },
  { id: "navigate", labelKey: "settings.commandcenter.actions.navigate" },
];

const SERVICE_OPS: { id: ServiceOp; labelKey: string }[] = [
  { id: "start", labelKey: "settings.commandcenter.serviceOps.start" },
  { id: "stop", labelKey: "settings.commandcenter.serviceOps.stop" },
  { id: "reload", labelKey: "settings.commandcenter.serviceOps.reload" },
  { id: "startAll", labelKey: "settings.commandcenter.serviceOps.startAll" },
  { id: "stopAll", labelKey: "settings.commandcenter.serviceOps.stopAll" },
  { id: "reloadStack", labelKey: "settings.commandcenter.serviceOps.reloadStack" },
];

const newId = () =>
  (globalThis.crypto?.randomUUID?.() ?? `cmd-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`);

type TFn = (key: string, vars?: Record<string, string | number>) => string;

function actionSummary(c: CommandShortcut, t: TFn): string {
  switch (c.action) {
    case "terminal":
      return c.project
        ? t("settings.commandcenter.summary.terminalWithProject", { command: c.command ?? "", project: c.project })
        : t("settings.commandcenter.summary.terminal", { command: c.command ?? "" });
    case "openTerminal":
      return c.project
        ? t("settings.commandcenter.summary.openTerminalWithProject", { project: c.project })
        : t("settings.commandcenter.summary.openTerminal");
    case "service": {
      const opEntry = SERVICE_OPS.find((o) => o.id === c.serviceOp);
      const op = opEntry ? t(opEntry.labelKey) : c.serviceOp ?? "";
      const needsTarget = c.serviceOp === "start" || c.serviceOp === "stop" || c.serviceOp === "reload";
      return needsTarget ? t("settings.commandcenter.summary.serviceWithTarget", { op, service: c.service ?? "" }) : op;
    }
    case "navigate": {
      const page = PAGES.find((p) => p.id === c.page);
      return t("settings.commandcenter.summary.navigate", { page: page ? t(page.labelKey) : c.page ?? "" });
    }
    default: return "";
  }
}

// A button that captures the next key combo pressed and reports it as a
// canonical hotkey string. Clears on Backspace/Delete.
function HotkeyCapture({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  const t = useT();
  const [listening, setListening] = useState(false);
  useEffect(() => {
    if (!listening) return;
    const handler = (e: KeyboardEvent) => {
      e.preventDefault();
      e.stopPropagation();
      if (e.key === "Escape") { setListening(false); return; }
      if (e.key === "Backspace" || e.key === "Delete") { onChange(""); setListening(false); return; }
      const hk = formatHotkey(e);
      if (hk) { onChange(hk); setListening(false); }
    };
    window.addEventListener("keydown", handler, true);
    return () => window.removeEventListener("keydown", handler, true);
  }, [listening, onChange]);
  return (
    <button
      type="button"
      data-command-hotkey-capture={listening ? "true" : undefined}
      onClick={() => setListening((l) => !l)}
      className={clsx(
        "inline-flex items-center gap-1.5 px-2.5 py-1.5 rounded-md text-xs border transition-colors font-mono",
        listening ? "bg-accent/30 border-accent/60 text-white animate-pulse" : "bg-white/[0.04] border-white/15 text-white/80 hover:bg-white/10",
      )}
    >
      <Keyboard className="w-3.5 h-3.5" />
      {listening ? t("settings.commandcenter.hotkeyCapture.listening") : value || t("settings.commandcenter.hotkeyCapture.notSet")}
    </button>
  );
}

type Lists = {
  services: { name: string; label: string }[];
  projects: { name: string }[];
  shells: { kind: string; label: string }[];
};

export default function CommandCenterSettings({ flash, onChange }: { flash: (m: string) => void; onChange?: () => void }) {
  const t = useT();
  const [cc, setCc] = useState<CommandCenterConfig>({ paletteHotkey: "Ctrl+K", globalHotkey: "", commands: [] });
  const [loaded, setLoaded] = useState(false);
  const [lists, setLists] = useState<Lists>({ services: [], projects: [], shells: [] });
  const [editing, setEditing] = useState<CommandShortcut | null>(null);
  const ccRef = useRef(cc);
  ccRef.current = cc;

  useEffect(() => {
    GetCommandCenter()
      .then((v: any) => setCc({ paletteHotkey: v?.paletteHotkey || "Ctrl+K", globalHotkey: v?.globalHotkey || "", commands: v?.commands ?? [] }))
      .finally(() => setLoaded(true));
    Promise.all([ListServices(), ListProjects(), ListTerminals()])
      .then(([svcs, projs, term]: any[]) => {
        setLists({
          services: (svcs ?? []).map((s: any) => ({ name: s.name, label: s.label })),
          projects: (projs ?? []).map((p: any) => ({ name: p.name })),
          shells: (term?.profiles ?? []).filter((p: any) => p.available).map((p: any) => ({ kind: p.kind, label: p.label })),
        });
      })
      .catch(() => {});
  }, []);

  // Persist the whole config and refresh the live palette/keybindings.
  const persist = async (next: CommandCenterConfig) => {
    setCc(next);
    try {
      await SetCommandCenter(next as any);
      onChange?.();
    } catch (e: any) {
      flash(t("settings.common.saveFailed", { message: e?.message ?? e }));
    }
  };

  const setPaletteHotkey = (hk: string) => persist({ ...ccRef.current, paletteHotkey: hk || "Ctrl+K" });
  const setGlobalHotkey = (hk: string) => persist({ ...ccRef.current, globalHotkey: hk });

  const startAdd = () =>
    setEditing({ id: newId(), label: "", action: "terminal", shell: "", project: "", command: "", serviceOp: "start", service: "", page: "dashboard", hotkey: "" });
  const startEdit = (c: CommandShortcut) => setEditing({ ...c });

  const saveEditing = async () => {
    const c = editing;
    if (!c) return;
    if (!c.label.trim()) { flash(t("settings.commandcenter.errors.labelRequired")); return; }
    const list = ccRef.current.commands;
    const idx = list.findIndex((x) => x.id === c.id);
    const nextList = idx >= 0 ? list.map((x) => (x.id === c.id ? c : x)) : [...list, c];
    await persist({ ...ccRef.current, commands: nextList });
    setEditing(null);
    flash(t("settings.commandcenter.saveSuccessFlash"));
  };

  const remove = async (id: string) => {
    await persist({ ...ccRef.current, commands: ccRef.current.commands.filter((x) => x.id !== id) });
    flash(t("settings.commandcenter.removeSuccessFlash"));
  };

  return (
    <div className="space-y-3">
      <GlassCard title={t("settings.commandcenter.palette.cardTitle")} icon={<Command className="w-4 h-4" />}>
        <div className="space-y-2">
          <p className="text-xs text-white/60">
            {t("settings.commandcenter.palette.description")}
          </p>
          <div className="flex items-center gap-3 pt-1">
            <span className="text-[11px] text-white/70 font-mono w-28">{t("settings.commandcenter.palette.paletteHotkeyLabel")}</span>
            <HotkeyCapture value={cc.paletteHotkey} onChange={setPaletteHotkey} />
          </div>
          <div className="flex items-center gap-3 pt-1">
            <span className="text-[11px] text-white/70 font-mono w-28">{t("settings.commandcenter.palette.globalHotkeyLabel")}</span>
            <HotkeyCapture value={cc.globalHotkey} onChange={setGlobalHotkey} />
            {cc.globalHotkey && (
              <button type="button" onClick={() => setGlobalHotkey("")} className="text-[11px] text-white/40 hover:text-white/70 underline">
                {t("settings.commandcenter.palette.disableButton")}
              </button>
            )}
          </div>
          <p className="text-[10px] text-white/40">
            {t("settings.commandcenter.palette.globalHotkeyHintPart1")} <span className="font-mono">Win+…</span>{" "}
            {t("settings.commandcenter.palette.globalHotkeyHintPart2")} <span className="font-mono">Ctrl+Alt+…</span>{" "}
            {t("settings.commandcenter.palette.globalHotkeyHintPart3")}
          </p>
          <p className="text-[10px] text-white/40">
            {t("settings.commandcenter.palette.terminalHotkeysPart1")} <span className="font-mono">Ctrl+T</span>{" "}
            {t("settings.commandcenter.palette.terminalHotkeysPart2")}{" "}
            <span className="font-mono">Ctrl+W</span> {t("settings.commandcenter.palette.terminalHotkeysPart3")}
          </p>
        </div>
      </GlassCard>

      <GlassCard title={t("settings.commandcenter.custom.cardTitle")} icon={<Command className="w-4 h-4" />}>
        <div className="space-y-2">
          <div className="flex items-center justify-between">
            <p className="text-xs text-white/60">{t("settings.commandcenter.custom.description")}</p>
            <button
              onClick={startAdd}
              disabled={!loaded}
              className="px-2.5 py-1.5 rounded-md text-xs border bg-accent/25 border-accent/60 text-white hover:bg-accent/35 disabled:opacity-50 inline-flex items-center gap-1.5"
            >
              <Plus className="w-3.5 h-3.5" /> {t("settings.commandcenter.custom.addButton")}
            </button>
          </div>

          {cc.commands.length === 0 && (
            <div className="text-xs text-white/40 py-4 text-center">{t("settings.commandcenter.custom.empty")}</div>
          )}

          <div className="space-y-1.5">
            {cc.commands.map((c) => (
              <div key={c.id} className="flex items-center gap-3 px-3 py-2 rounded-md bg-white/[0.03] border border-white/10">
                <div className="flex-1 min-w-0">
                  <div className="text-sm text-white/90 truncate">{c.label || t("settings.commandcenter.custom.untitled")}</div>
                  <div className="text-[11px] text-white/45 truncate">{actionSummary(c, t)}</div>
                </div>
                {c.hotkey && (
                  <span className="text-[10px] px-1.5 py-0.5 rounded border border-white/15 text-white/45 font-mono">{c.hotkey}</span>
                )}
                <button onClick={() => startEdit(c)} className="p-1.5 rounded-md hover:bg-white/10 text-white/60 hover:text-white" title={t("settings.commandcenter.custom.editTooltip")}>
                  <Pencil className="w-3.5 h-3.5" />
                </button>
                <button onClick={() => remove(c.id)} className="p-1.5 rounded-md hover:bg-red-500/15 text-white/60 hover:text-red-300" title={t("settings.commandcenter.custom.deleteTooltip")}>
                  <Trash2 className="w-3.5 h-3.5" />
                </button>
              </div>
            ))}
          </div>
        </div>
      </GlassCard>

      {editing && (
        <CommandEditor
          value={editing}
          lists={lists}
          onChange={setEditing}
          onCancel={() => setEditing(null)}
          onSave={saveEditing}
        />
      )}
    </div>
  );
}

function CommandEditor({
  value,
  lists,
  onChange,
  onCancel,
  onSave,
}: {
  value: CommandShortcut;
  lists: Lists;
  onChange: (c: CommandShortcut) => void;
  onCancel: () => void;
  onSave: () => void;
}) {
  const t = useT();
  const set = <K extends keyof CommandShortcut>(k: K, v: CommandShortcut[K]) => onChange({ ...value, [k]: v });
  const needsServiceTarget = value.serviceOp === "start" || value.serviceOp === "stop" || value.serviceOp === "reload";

  const ShellSelect = (
    <label className="block">
      <div className="text-[11px] text-white/70 font-mono">{t("settings.commandcenter.editor.shellLabel")}</div>
      <select className={inputCls} value={value.shell ?? ""} onChange={(e) => set("shell", e.target.value)}>
        <option value="">{t("settings.commandcenter.editor.shellDefaultOption")}</option>
        {lists.shells.map((s) => <option key={s.kind} value={s.kind}>{s.label}</option>)}
      </select>
    </label>
  );
  const ProjectSelect = (
    <label className="block">
      <div className="text-[11px] text-white/70 font-mono">{t("settings.commandcenter.editor.projectLabel")}</div>
      <select className={inputCls} value={value.project ?? ""} onChange={(e) => set("project", e.target.value)}>
        <option value="">{t("settings.commandcenter.editor.projectNoneOption")}</option>
        {lists.projects.map((p) => <option key={p.name} value={p.name}>{p.name}</option>)}
      </select>
    </label>
  );

  return (
    <GlassCard
      title={value.label ? t("settings.commandcenter.editor.editTitle", { label: value.label }) : t("settings.commandcenter.editor.newTitle")}
      icon={<Command className="w-4 h-4" />}
    >
      <div className="space-y-3">
        <div className="grid grid-cols-2 gap-3">
          <label className="block">
            <div className="text-[11px] text-white/70 font-mono">{t("settings.commandcenter.editor.labelLabel")}</div>
            <input className={inputCls} value={value.label} onChange={(e) => set("label", e.target.value)} placeholder={t("settings.commandcenter.editor.labelPlaceholder")} />
          </label>
          <label className="block">
            <div className="text-[11px] text-white/70 font-mono">{t("settings.commandcenter.editor.actionLabel")}</div>
            <select className={inputCls} value={value.action} onChange={(e) => set("action", e.target.value as CommandAction)}>
              {ACTIONS.map((a) => <option key={a.id} value={a.id}>{t(a.labelKey)}</option>)}
            </select>
          </label>
        </div>

        {value.action === "terminal" && (
          <>
            <label className="block">
              <div className="text-[11px] text-white/70 font-mono">{t("settings.commandcenter.editor.commandLabel")}</div>
              <input className={inputCls} value={value.command ?? ""} onChange={(e) => set("command", e.target.value)} placeholder="php artisan migrate" />
            </label>
            <div className="grid grid-cols-2 gap-3">{ShellSelect}{ProjectSelect}</div>
          </>
        )}

        {value.action === "openTerminal" && (
          <div className="grid grid-cols-2 gap-3">{ShellSelect}{ProjectSelect}</div>
        )}

        {value.action === "service" && (
          <div className="grid grid-cols-2 gap-3">
            <label className="block">
              <div className="text-[11px] text-white/70 font-mono">{t("settings.commandcenter.editor.operationLabel")}</div>
              <select className={inputCls} value={value.serviceOp ?? "start"} onChange={(e) => set("serviceOp", e.target.value as ServiceOp)}>
                {SERVICE_OPS.map((o) => <option key={o.id} value={o.id}>{t(o.labelKey)}</option>)}
              </select>
            </label>
            {needsServiceTarget && (
              <label className="block">
                <div className="text-[11px] text-white/70 font-mono">{t("settings.commandcenter.editor.serviceLabel")}</div>
                <select className={inputCls} value={value.service ?? ""} onChange={(e) => set("service", e.target.value)}>
                  <option value="">{t("settings.commandcenter.editor.serviceSelectOption")}</option>
                  {lists.services.map((s) => <option key={s.name} value={s.name}>{s.label}</option>)}
                </select>
              </label>
            )}
          </div>
        )}

        {value.action === "navigate" && (
          <label className="block">
            <div className="text-[11px] text-white/70 font-mono">{t("settings.commandcenter.editor.pageLabel")}</div>
            <select className={inputCls} value={value.page ?? "dashboard"} onChange={(e) => set("page", e.target.value)}>
              {PAGES.map((p) => <option key={p.id} value={p.id}>{t(p.labelKey)}</option>)}
            </select>
          </label>
        )}

        <div className="flex items-center gap-3">
          <span className="text-[11px] text-white/70 font-mono">{t("settings.commandcenter.editor.hotkeyLabel")}</span>
          <HotkeyCapture value={value.hotkey ?? ""} onChange={(v) => set("hotkey", v)} />
        </div>

        <div className="flex items-center gap-2 pt-1">
          <button onClick={onSave} className="px-3 py-1.5 rounded-md text-xs border bg-accent/25 border-accent/60 text-white hover:bg-accent/35">{t("settings.commandcenter.editor.saveButton")}</button>
          <button onClick={onCancel} className="px-3 py-1.5 rounded-md text-xs border border-white/15 text-white/70 hover:bg-white/5">{t("settings.commandcenter.editor.cancelButton")}</button>
        </div>
      </div>
    </GlassCard>
  );
}
