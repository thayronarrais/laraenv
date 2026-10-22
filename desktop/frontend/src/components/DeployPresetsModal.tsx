import { useEffect, useState } from "react";
import { useDialogs } from "./dialogs/DialogProvider";
import { createPortal } from "react-dom";
import { X, Plus, Trash2, Copy, Save, Loader2, Lock, FileCode2, ArrowLeft } from "lucide-react";
import Editor from "@monaco-editor/react";
// @ts-ignore
import { ListDeployPresets, SaveDeployPreset, DeleteDeployPreset } from "../../wailsjs/go/main/App";
import { useT } from "../lib/i18n";

export type PresetVar = { key: string; label?: string; default?: string };
export type DeployPreset = {
  id: string;
  name: string;
  description?: string;
  script: string;
  variables?: PresetVar[] | null;
  builtIn?: boolean;
  updatedAt?: string;
};

// Variables every preset can use — derived from the deployment's config at
// render time. Custom vars declared on the preset are added on top. Most
// hints are translated (hintKey); a few describe raw enum values or a path
// pattern rather than prose, and stay as literal data (hint).
const BUILTIN_TOKENS: { token: string; hint?: string; hintKey?: string }[] = [
  { token: "name", hintKey: "deployments.presetsModal.builtinTokens.name" },
  { token: "domain", hintKey: "deployments.presetsModal.builtinTokens.domain" },
  { token: "raw_domain", hintKey: "deployments.presetsModal.builtinTokens.rawDomain" },
  { token: "branch", hintKey: "deployments.presetsModal.builtinTokens.branch" },
  { token: "php_version", hintKey: "deployments.presetsModal.builtinTokens.phpVersion" },
  { token: "deploy_root", hint: "/var/www/<name>" },
  { token: "environment", hint: "production | staging | development" },
  { token: "git_url", hintKey: "deployments.presetsModal.builtinTokens.gitUrl" },
  { token: "web_server", hint: "nginx | apache" },
  { token: "database", hint: "mysql | postgres | empty" },
];

// Manager + editor for deploy script presets. Built-ins are read-only and
// duplicable; custom presets are saved locally and synced to the cloud when
// sync is enabled.
export default function DeployPresetsModal({
  onClose, flash,
}: {
  onClose: () => void;
  flash: (m: string) => void;
}) {
  const t = useT();
  const [presets, setPresets] = useState<DeployPreset[]>([]);
  const dialogs = useDialogs();
  const [loading, setLoading] = useState(true);
  const [editing, setEditing] = useState<DeployPreset | null>(null);

  const refresh = async () => {
    try {
      const list: DeployPreset[] = await ListDeployPresets();
      setPresets(list ?? []);
    } catch (e: any) {
      flash(t("deployments.errors.generic", { message: String(e?.message ?? e) }));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { refresh(); }, []);

  const duplicate = (p: DeployPreset): DeployPreset => ({
    id: "",
    name: `${p.name} (copy)`,
    description: p.description,
    script: p.script,
    variables: (p.variables ?? []).map((v) => ({ ...v })),
  });

  const onDelete = async (p: DeployPreset) => {
    if (!(await dialogs.confirm({ title: t("deployments.presetsModal.deleteConfirm.title", { name: p.name }), message: t("deployments.presetsModal.deleteConfirm.message"), confirmLabel: t("deployments.deleteLabel"), danger: true }))) return;
    try {
      await DeleteDeployPreset(p.id);
      flash(t("deployments.presetsModal.deletedFlash"));
      refresh();
    } catch (e: any) { flash(t("deployments.errors.generic", { message: String(e?.message ?? e) })); }
  };

  return createPortal(
    <div className="fixed inset-0 bg-black/60 backdrop-blur-sm flex items-center justify-center z-50 p-4" onClick={onClose}>
      <div
        className="glass-strong w-full max-w-5xl flex flex-col p-4"
        style={{ height: "min(760px, 92vh)" }}
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between mb-3 flex-shrink-0">
          <h2 className="text-base font-semibold flex items-center gap-2">
            {editing && (
              <button onClick={() => setEditing(null)} className="p-1 rounded hover:bg-white/10" title={t("deployments.presetsModal.backButtonTooltip")}>
                <ArrowLeft className="w-4 h-4" />
              </button>
            )}
            <FileCode2 className="w-4 h-4" />
            {editing
              ? editing.builtIn
                ? `${editing.name} ${t("deployments.builtInSuffix")}`
                : editing.id
                ? t("deployments.presetsModal.editTitle", { name: editing.name })
                : t("deployments.presetsModal.newPresetLabel")
              : t("deployments.presetsModal.title")}
          </h2>
          <button onClick={onClose} className="p-1 rounded hover:bg-white/10"><X className="w-4 h-4" /></button>
        </div>

        {editing ? (
          <PresetEditor
            preset={editing}
            onSaved={() => { setEditing(null); refresh(); }}
            onDuplicate={() => setEditing(duplicate(editing))}
            flash={flash}
          />
        ) : (
          <div className="flex-1 min-h-0 overflow-y-auto">
            <div className="text-[11px] opacity-60 mb-3">
              {t("deployments.presetsModal.introBefore")} <span className="font-mono">{"{variables}"}</span> {t("deployments.presetsModal.introAfter")}
            </div>
            {loading ? (
              <div className="flex items-center gap-2 text-xs opacity-60 py-6 justify-center">
                <Loader2 className="w-4 h-4 animate-spin" /> {t("deployments.presetsModal.loadingLabel")}
              </div>
            ) : (
              <ul className="space-y-2">
                {presets.map((p) => (
                  <li key={p.id} className="glass rounded-md px-3 py-2.5 flex items-center gap-3">
                    <div className="flex-1 min-w-0">
                      <div className="text-sm flex items-center gap-2">
                        {p.name}
                        {p.builtIn && (
                          <span className="inline-flex items-center gap-1 text-[10px] uppercase tracking-wider text-sky-300/80 border border-sky-500/30 rounded px-1.5 py-0.5">
                            <Lock className="w-2.5 h-2.5" /> {t("deployments.presetsModal.builtInBadge")}
                          </span>
                        )}
                      </div>
                      {p.description && <div className="text-[11px] opacity-50 truncate">{p.description}</div>}
                    </div>
                    <button
                      onClick={() => setEditing(p)}
                      className="px-2.5 py-1 rounded-md text-[11px] border border-white/10 hover:bg-white/10"
                    >
                      {p.builtIn ? t("deployments.presetsModal.viewButton") : t("deployments.presetsModal.editButton")}
                    </button>
                    <button
                      onClick={() => setEditing(duplicate(p))}
                      className="px-2.5 py-1 rounded-md text-[11px] border border-white/10 hover:bg-white/10 flex items-center gap-1"
                      title={t("deployments.presetsModal.duplicateTooltip")}
                    >
                      <Copy className="w-3 h-3" /> {t("deployments.presetsModal.duplicateButton")}
                    </button>
                    {!p.builtIn && (
                      <button
                        onClick={() => onDelete(p)}
                        className="p-1.5 rounded-md border border-white/10 hover:bg-red-500/10 hover:border-red-500/30 hover:text-red-300"
                        title={t("deployments.presetsModal.deleteTooltip")}
                      >
                        <Trash2 className="w-3 h-3" />
                      </button>
                    )}
                  </li>
                ))}
              </ul>
            )}
            <div className="mt-3">
              <button
                onClick={() => setEditing({ id: "", name: "", description: "", script: "# New deploy pipeline\n", variables: [] })}
                className="px-3 py-1.5 rounded-md text-xs border bg-accent/30 border-accent hover:bg-accent/50 flex items-center gap-1.5"
              >
                <Plus className="w-3.5 h-3.5" /> {t("deployments.presetsModal.newPresetLabel")}
              </button>
            </div>
          </div>
        )}
      </div>
    </div>,
    document.body,
  );
}

function PresetEditor({
  preset, onSaved, onDuplicate, flash,
}: {
  preset: DeployPreset;
  onSaved: () => void;
  onDuplicate: () => void;
  flash: (m: string) => void;
}) {
  const t = useT();
  const readOnly = !!preset.builtIn;
  const [name, setName] = useState(preset.name);
  const [description, setDescription] = useState(preset.description ?? "");
  const [script, setScript] = useState(preset.script);
  const [vars, setVars] = useState<PresetVar[]>((preset.variables ?? []).map((v) => ({ ...v })));
  const [saving, setSaving] = useState(false);

  const setVar = (i: number, patch: Partial<PresetVar>) => {
    setVars((cur) => cur.map((v, j) => (j === i ? { ...v, ...patch } : v)));
  };

  const save = async () => {
    if (!name.trim()) { flash(t("deployments.presetsModal.nameRequired")); return; }
    const cleanVars = vars
      .map((v) => ({ ...v, key: v.key.trim() }))
      .filter((v) => v.key !== "");
    const bad = cleanVars.find((v) => !/^[a-z_][a-z0-9_]*$/.test(v.key));
    if (bad) { flash(t("deployments.presetsModal.invalidVarKey", { key: bad.key })); return; }
    const reserved = cleanVars.find((v) => BUILTIN_TOKENS.some((bt) => bt.token === v.key));
    if (reserved) { flash(t("deployments.presetsModal.reservedVarKey", { key: reserved.key })); return; }
    setSaving(true);
    try {
      await SaveDeployPreset({
        id: preset.id,
        name: name.trim(),
        description: description.trim(),
        script,
        variables: cleanVars,
      } as any);
      flash(t("deployments.presetsModal.savedFlash"));
      onSaved();
    } catch (e: any) {
      flash(t("deployments.errors.generic", { message: String(e?.message ?? e) }));
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="flex-1 min-h-0 flex flex-col gap-3">
      <div className="grid grid-cols-2 gap-3 flex-shrink-0">
        <div>
          <div className="text-[11px] uppercase tracking-wider opacity-60 mb-1">{t("deployments.presetsModal.editor.nameLabel")}</div>
          <input className="dpl-input" value={name} onChange={(e) => setName(e.target.value)} disabled={readOnly} placeholder="Laravel — client X" />
        </div>
        <div>
          <div className="text-[11px] uppercase tracking-wider opacity-60 mb-1">{t("deployments.presetsModal.editor.descriptionLabel")}</div>
          <input className="dpl-input" value={description} onChange={(e) => setDescription(e.target.value)} disabled={readOnly} placeholder={t("deployments.presetsModal.editor.descriptionPlaceholder")} />
        </div>
      </div>

      <div className="flex-1 min-h-0 grid grid-cols-[1fr_240px] gap-3">
        <div className="min-h-0 rounded-lg overflow-hidden border border-white/10">
          <Editor
            height="100%"
            theme="vs-dark"
            language="shell"
            value={script}
            onChange={(v) => setScript(v ?? "")}
            options={{
              readOnly,
              fontSize: 12,
              minimap: { enabled: false },
              automaticLayout: true,
              tabSize: 2,
              wordWrap: "on",
              scrollBeyondLastLine: false,
            }}
          />
        </div>

        <div className="min-h-0 overflow-y-auto space-y-3">
          <div className="glass rounded-md p-2.5">
            <div className="text-[11px] uppercase tracking-wider opacity-60 mb-1.5">{t("deployments.presetsModal.editor.builtinVarsHeader")}</div>
            <ul className="space-y-1">
              {BUILTIN_TOKENS.map((tok) => (
                <li key={tok.token} className="text-[11px]">
                  <span className="font-mono text-accent2">{`{${tok.token}}`}</span>
                  <span className="opacity-50 ml-1.5">{tok.hintKey ? t(tok.hintKey) : tok.hint}</span>
                </li>
              ))}
            </ul>
          </div>

          <div className="glass rounded-md p-2.5">
            <div className="text-[11px] uppercase tracking-wider opacity-60 mb-1.5">{t("deployments.presetsModal.editor.customVarsHeader")}</div>
            <div className="text-[10px] opacity-50 mb-2">
              {t("deployments.presetsModal.editor.customVarsDescBefore")} <span className="font-mono">{"{key}"}</span> {t("deployments.presetsModal.editor.customVarsDescAfter")}
            </div>
            {vars.length === 0 && <div className="text-[11px] opacity-40 italic mb-2">{t("deployments.presetsModal.editor.noneLabel")}</div>}
            <div className="space-y-2">
              {vars.map((v, i) => (
                <div key={i} className="space-y-1 border border-white/5 rounded p-1.5">
                  <div className="flex items-center gap-1">
                    <input className="dpl-input !text-[11px] font-mono" value={v.key} disabled={readOnly} placeholder={t("deployments.presetsModal.editor.keyPlaceholder")} onChange={(e) => setVar(i, { key: e.target.value })} />
                    {!readOnly && (
                      <button onClick={() => setVars((cur) => cur.filter((_, j) => j !== i))} className="p-1 rounded hover:bg-red-500/10 hover:text-red-300 flex-shrink-0">
                        <Trash2 className="w-3 h-3" />
                      </button>
                    )}
                  </div>
                  <input className="dpl-input !text-[11px]" value={v.label ?? ""} disabled={readOnly} placeholder={t("deployments.presetsModal.editor.labelPlaceholder")} onChange={(e) => setVar(i, { label: e.target.value })} />
                  <input className="dpl-input !text-[11px] font-mono" value={v.default ?? ""} disabled={readOnly} placeholder={t("deployments.presetsModal.editor.defaultValuePlaceholder")} onChange={(e) => setVar(i, { default: e.target.value })} />
                </div>
              ))}
            </div>
            {!readOnly && (
              <button
                onClick={() => setVars((cur) => [...cur, { key: "", label: "", default: "" }])}
                className="mt-2 px-2 py-1 rounded-md text-[11px] border border-white/10 hover:bg-white/10 flex items-center gap-1"
              >
                <Plus className="w-3 h-3" /> {t("deployments.presetsModal.editor.addVariableButton")}
              </button>
            )}
          </div>
        </div>
      </div>

      <div className="flex justify-end gap-2 flex-shrink-0">
        {readOnly ? (
          <button onClick={onDuplicate} className="px-3 py-1.5 rounded-md text-xs border bg-accent/30 border-accent hover:bg-accent/50 flex items-center gap-1.5">
            <Copy className="w-3.5 h-3.5" /> {t("deployments.presetsModal.editor.duplicateToCustomizeButton")}
          </button>
        ) : (
          <button onClick={save} disabled={saving} className="px-3 py-1.5 rounded-md text-xs border bg-accent/30 border-accent hover:bg-accent/50 flex items-center gap-1.5">
            {saving ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Save className="w-3.5 h-3.5" />}
            {t("deployments.presetsModal.editor.savePresetButton")}
          </button>
        )}
      </div>
      <style>{`.dpl-input{width:100%;background:rgba(255,255,255,0.04);border:1px solid rgba(255,255,255,0.1);border-radius:6px;padding:6px 10px;font-size:12px;color:var(--c-text);outline:none;color-scheme:dark}.dpl-input:focus{border-color:var(--c-accent)}.dpl-input:disabled{opacity:.5}`}</style>
    </div>
  );
}
