import { useEffect, useState } from "react";
import { createPortal } from "react-dom";
import clsx from "clsx";
import { X, KeyRound, Lock, Plus, PlugZap, Loader2, Check, AlertTriangle, Eye, EyeOff, Layers, Upload, Tag as TagIcon, Network, ChevronDown, ChevronRight } from "lucide-react";
// @ts-ignore
import { SaveSSHHost, ListSSHKeys, ReadSSHPublicKey, TestSSHConnection, ListSSHHosts } from "../../wailsjs/go/main/App";
import SSHKeyGenModal from "./SSHKeyGenModal";
import SSHImportKeyModal from "./SSHImportKeyModal";
import { SSHHost } from "../pages/SSH";
import { addTag, commitDraft } from "../lib/tags";
import { useT } from "../lib/i18n";

export default function SSHHostModal({
  open, editing, onClose, onSaved, flash,
}: {
  open: boolean;
  editing: SSHHost | null;
  onClose: () => void;
  onSaved?: (name: string, isNew: boolean) => void;
  flash: (m: string) => void;
}) {
  const t = useT();
  const [name, setName] = useState("");
  const [tags, setTags] = useState<string[]>([]);
  const [tagDraft, setTagDraft] = useState("");
  const [address, setAddress] = useState("");
  const [port, setPort] = useState(22);
  const [user, setUser] = useState("");
  const [authMode, setAuthMode] = useState<"password" | "key" | "both">("key");
  const [password, setPassword] = useState("");
  const [keyName, setKeyName] = useState("");
  const [availableKeys, setAvailableKeys] = useState<string[]>([]);
  const [proxyJump, setProxyJump] = useState("");
  const [proxyCommand, setProxyCommand] = useState("");
  // Fresh snapshot from the backend, fetched on open — the source of truth for
  // the duplicate-name check (page state can be stale) and the proxy dropdown.
  const [savedHosts, setSavedHosts] = useState<SSHHost[]>([]);
  const [advancedOpen, setAdvancedOpen] = useState(false);
  const [genOpen, setGenOpen] = useState(false);
  const [importOpen, setImportOpen] = useState(false);
  const [pubKey, setPubKey] = useState<string>("");
  const [testing, setTesting] = useState(false);
  const [testResult, setTestResult] = useState<{ ok: boolean; message: string; elapsedMs: number } | null>(null);
  // Validation/save errors surface inline — a toast alone is easy to miss
  // while the modal backdrop dims the rest of the app.
  const [saveError, setSaveError] = useState<string | null>(null);
  const [showPassword, setShowPassword] = useState(false);

  // Hydrate from `editing` whenever the modal opens.
  useEffect(() => {
    if (!open) return;
    if (editing) {
      setName(editing.name);
      setTags((editing.tags ?? []).map((t) => t.trim()).filter(Boolean));
      setAddress(editing.address);
      setPort(editing.port || 22);
      setUser(editing.user);
      setAuthMode(editing.authMode);
      setPassword(editing.password ?? "");
      setKeyName(editing.keyName ?? "");
      setProxyJump(editing.proxyJump ?? "");
      setProxyCommand(editing.proxyCommand ?? "");
      setAdvancedOpen(Boolean(editing.proxyJump || editing.proxyCommand));
    } else {
      setName(""); setTags([]); setAddress(""); setPort(22);
      setUser(""); setAuthMode("key"); setPassword(""); setKeyName("");
      setProxyJump(""); setProxyCommand(""); setAdvancedOpen(false);
    }
    ListSSHHosts()
      .then((hs: any[]) => setSavedHosts((hs ?? []) as SSHHost[]))
      .catch(() => setSavedHosts([]));
    setTagDraft("");
    setPubKey("");
    setTestResult(null);
    setSaveError(null);
    setTesting(false);
    setShowPassword(false);
  }, [open, editing]);

  // Reset stale test result / validation error when any relevant field changes.
  useEffect(() => {
    setTestResult(null);
    setSaveError(null);
  }, [name, address, port, user, authMode, password, keyName]);

  // Keep the "available keys" list in sync with the host name (keys live in
  // C:\laraenv\ssh\<name>\). We only fetch once a name is committed; also
  // refetch whenever the generate / import modal closes (so a freshly added
  // key shows up in the dropdown without manual reload).
  useEffect(() => {
    if (!open || !name) { setAvailableKeys([]); return; }
    ListSSHKeys(name).then((k: string[]) => setAvailableKeys(k ?? []));
  }, [open, name, genOpen, importOpen]);

  // When a key is selected, fetch its .pub for the read-only preview.
  useEffect(() => {
    if (!name || !keyName) { setPubKey(""); return; }
    ReadSSHPublicKey(name, keyName)
      .then((s: string) => setPubKey(s))
      .catch(() => setPubKey(""));
  }, [name, keyName]);

  if (!open) return null;

  const isNew = !editing;
  const cleanName = (s: string) => s.trim().toLowerCase().replace(/\s+/g, "-");
  // Saved hosts excluding self — options for the ProxyJump dropdown.
  const otherHosts = savedHosts
    .map((h) => h.name)
    .filter((n) => n && (!editing || n !== editing.name));

  const credPayload = () => {
    const wantKey = authMode === "key" || authMode === "both";
    const wantPwd = authMode === "password" || authMode === "both";
    return {
      authMode,
      password: wantPwd ? password : "",
      keyName: wantKey ? keyName : "",
      proxyJump: proxyJump.trim(),
      proxyCommand: proxyCommand.trim(),
    };
  };

  const test = async () => {
    if (!address.trim()) { setSaveError(t("ssh.hostModal.errors.addressRequired")); return; }
    if (!user.trim()) { setSaveError(t("ssh.hostModal.errors.userRequired")); return; }
    if (authMode === "password" && !password) { setSaveError(t("ssh.hostModal.errors.passwordRequired")); return; }
    if (authMode === "key" && !keyName) { setSaveError(t("ssh.hostModal.errors.pickKeyFile")); return; }
    if (authMode === "both" && !password && !keyName) { setSaveError(t("ssh.hostModal.errors.provideCredential")); return; }
    setSaveError(null);
    setTesting(true);
    setTestResult(null);
    try {
      const r = await TestSSHConnection({
        name: cleanName(name) || "(unsaved)",
        tags: commitDraft(tags, tagDraft),
        address: address.trim(),
        port: port || 22,
        user: user.trim(),
        ...credPayload(),
      });
      setTestResult(r);
    } catch (e) {
      setTestResult({ ok: false, message: String(e), elapsedMs: 0 });
    } finally {
      setTesting(false);
    }
  };

  const save = async () => {
    const n = cleanName(name);
    if (!n) { setSaveError(t("ssh.hostModal.errors.nameRequired")); return; }
    const dup = isNew ? savedHosts.find((h) => h.name === n) : undefined;
    if (dup) {
      // Point at the existing entry — a bare "already exists" is confusing
      // when the card is truncated or hidden behind a tag filter.
      setSaveError(t("ssh.hostModal.errors.duplicateHost", { name: n, user: dup.user, address: dup.address, port: dup.port || 22 }));
      return;
    }
    if (!address.trim()) { setSaveError(t("ssh.hostModal.errors.addressRequired")); return; }
    if (!user.trim()) { setSaveError(t("ssh.hostModal.errors.userRequired")); return; }
    if (editing?.passwordUnavailable && authMode === "password" && !password) {
      setSaveError(t("ssh.hostModal.passwordUnavailable"));
      return;
    }
    setSaveError(null);
    try {
      await SaveSSHHost({
        name: n,
        tags: commitDraft(tags, tagDraft),
        address: address.trim(),
        port: port || 22,
        user: user.trim(),
        ...credPayload(),
      });
      flash(isNew ? t("ssh.flash.hostAdded", { name: n }) : t("ssh.flash.hostSaved", { name: n }));
      onSaved?.(n, isNew);
      onClose();
    } catch (e) {
      setSaveError(t("ssh.errors.generic", { message: String(e) }));
    }
  };

  return createPortal(
    <div className="modal-backdrop">
      <div
        onClick={(e) => e.stopPropagation()}
        className="modal-card w-[640px] max-w-[95vw] max-h-[92vh] flex flex-col p-5"
      >
        <header className="flex items-center justify-between mb-4">
          <h2 className="text-base font-semibold">
            {isNew ? t("ssh.hostModal.newTitle") : t("ssh.hostModal.editTitle", { name: editing!.name })}
          </h2>
          <button onClick={onClose} className="p-1 rounded-md hover:bg-white/10">
            <X className="w-4 h-4" />
          </button>
        </header>

        <div className="grid grid-cols-2 gap-3 overflow-y-auto pr-1">
          <Field label={t("ssh.hostModal.nameLabel")}>
            <input
              value={name}
              onChange={(e) => setName(e.target.value)}
              disabled={!isNew}
              placeholder="staging"
              className="input"
              autoFocus={isNew}
            />
          </Field>
          <Field label={t("ssh.hostModal.tagsLabel")}>
            <div
              className="input flex flex-wrap items-center gap-1 cursor-text"
              onClick={(e) => {
                const t = e.target as HTMLElement;
                if (t.tagName !== "INPUT") {
                  (e.currentTarget.querySelector("input") as HTMLInputElement | null)?.focus();
                }
              }}
            >
              {tags.map((tag) => (
                <span
                  key={tag}
                  className="px-1.5 py-0.5 rounded-md text-[10px] flex items-center gap-1 bg-accent/20 border border-accent/40 text-white uppercase tracking-wider"
                >
                  <TagIcon className="w-2.5 h-2.5" />
                  {tag}
                  <button
                    type="button"
                    onClick={() => setTags(tags.filter((x) => x !== tag))}
                    className="opacity-70 hover:opacity-100"
                    title={t("ssh.hostModal.removeTagTooltip")}
                  >
                    <X className="w-2.5 h-2.5" />
                  </button>
                </span>
              ))}
              <input
                value={tagDraft}
                onChange={(e) => {
                  const v = e.target.value;
                  if (v.endsWith(",")) {
                    setTags(addTag(tags, v.slice(0, -1)));
                    setTagDraft("");
                  } else {
                    setTagDraft(v);
                  }
                }}
                onKeyDown={(e) => {
                  if (e.key === "Enter") {
                    e.preventDefault();
                    setTags(addTag(tags, tagDraft));
                    setTagDraft("");
                  } else if (e.key === "Backspace" && !tagDraft && tags.length > 0) {
                    setTags(tags.slice(0, -1));
                  }
                }}
                onBlur={() => {
                  if (tagDraft.trim()) {
                    setTags(addTag(tags, tagDraft));
                    setTagDraft("");
                  }
                }}
                placeholder={tags.length === 0 ? "staging, prod, client-x…" : ""}
                className="bg-transparent outline-none flex-1 min-w-[80px] text-sm"
              />
            </div>
          </Field>

          <Field label={t("ssh.hostModal.addressLabel")} wide>
            <input
              value={address}
              onChange={(e) => setAddress(e.target.value)}
              placeholder="203.0.113.5 or staging.example.com"
              className="input"
            />
          </Field>

          <Field label={t("ssh.hostModal.portLabel")}>
            <input
              type="number"
              value={port}
              onChange={(e) => setPort(parseInt(e.target.value, 10) || 22)}
              className="input"
              min={1}
              max={65535}
            />
          </Field>
          <Field label={t("ssh.hostModal.userLabel")}>
            <input
              value={user}
              onChange={(e) => setUser(e.target.value)}
              placeholder="deploy"
              className="input"
            />
          </Field>

          <Field label={t("ssh.hostModal.authMethodLabel")} wide>
            <div className="flex items-stretch rounded-md border border-white/10 bg-white/5 overflow-hidden w-fit">
              <AuthTab active={authMode === "key"} onClick={() => setAuthMode("key")} icon={<KeyRound className="w-3 h-3" />} label={t("ssh.hostModal.authKey")} />
              <AuthTab active={authMode === "password"} onClick={() => setAuthMode("password")} icon={<Lock className="w-3 h-3" />} label={t("ssh.hostModal.authPassword")} />
              <AuthTab active={authMode === "both"} onClick={() => setAuthMode("both")} icon={<Layers className="w-3 h-3" />} label={t("ssh.hostModal.authBoth")} />
            </div>
            {authMode === "both" && (
              <div className="text-[10px] opacity-50 mt-1">
                {t("ssh.hostModal.bothHint")}
              </div>
            )}
          </Field>

          {(authMode === "password" || authMode === "both") && (
            <Field label={t("ssh.hostModal.passwordLabel")} wide>
              <div className="relative">
                <input
                  type={showPassword ? "text" : "password"}
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  placeholder={t("ssh.hostModal.passwordPlaceholder")}
                  className="input w-full pr-10"
                />
                <button
                  type="button"
                  onClick={() => setShowPassword((v) => !v)}
                  className="absolute right-2 top-1/2 -translate-y-1/2 p-1 rounded-md hover:bg-white/10 opacity-70 hover:opacity-100"
                  title={showPassword ? t("ssh.hostModal.hidePasswordTooltip") : t("ssh.hostModal.showPasswordTooltip")}
                >
                  {showPassword ? <EyeOff className="w-3.5 h-3.5" /> : <Eye className="w-3.5 h-3.5" />}
                </button>
              </div>
              {editing?.passwordUnavailable && !password && (
                <p role="alert" className="text-xs text-amber-300 mt-1">{t("ssh.hostModal.passwordUnavailable")}</p>
              )}
            </Field>
          )}

          {(authMode === "key" || authMode === "both") && (
            <>
              <Field label={t("ssh.hostModal.keyFileLabel")} wide>
                <div className="flex gap-2">
                  <select
                    value={keyName}
                    onChange={(e) => setKeyName(e.target.value)}
                    className="input flex-1"
                    disabled={!name}
                  >
                    <option value="">{t("ssh.hostModal.noneOption")}</option>
                    {availableKeys.map((k) => (
                      <option key={k} value={k}>{k}</option>
                    ))}
                  </select>
                  <button
                    onClick={() => setGenOpen(true)}
                    disabled={!name}
                    className="px-3 py-1.5 rounded-md bg-accent/20 border border-accent/50 hover:bg-accent/30 text-xs flex items-center gap-1 disabled:opacity-40"
                    title={name ? t("ssh.hostModal.generateTooltip") : t("ssh.hostModal.setNameFirstTooltip")}
                  >
                    <Plus className="w-3 h-3" /> {t("ssh.hostModal.generateButton")}
                  </button>
                  <button
                    onClick={() => setImportOpen(true)}
                    disabled={!name}
                    className="px-3 py-1.5 rounded-md bg-white/5 hover:bg-white/10 border border-white/10 text-xs flex items-center gap-1 disabled:opacity-40"
                    title={name ? t("ssh.hostModal.importTooltip") : t("ssh.hostModal.setNameFirstTooltip")}
                  >
                    <Upload className="w-3 h-3" /> {t("ssh.hostModal.importButton")}
                  </button>
                </div>
                {!name && (
                  <div className="text-[10px] opacity-50 mt-1">
                    {t("ssh.hostModal.setNameHint")}
                  </div>
                )}
              </Field>

              {pubKey && (
                <Field label={t("ssh.hostModal.publicKeyLabel")} wide>
                  <textarea
                    readOnly
                    value={pubKey}
                    className="input font-mono text-[10px] h-20 resize-none"
                    onClick={(e) => (e.target as HTMLTextAreaElement).select()}
                  />
                </Field>
              )}
            </>
          )}

          <div className="col-span-2 mt-1">
            <button
              type="button"
              onClick={() => setAdvancedOpen((v) => !v)}
              className="flex items-center gap-1.5 text-[11px] uppercase tracking-wider opacity-70 hover:opacity-100"
            >
              {advancedOpen ? <ChevronDown className="w-3 h-3" /> : <ChevronRight className="w-3 h-3" />}
              <Network className="w-3 h-3" />
              {t("ssh.hostModal.advancedToggle")}
              {(proxyJump || proxyCommand) && !advancedOpen && (
                <span className="opacity-60 normal-case tracking-normal">
                  {t("ssh.hostModal.viaSummary", { target: proxyCommand ? "ProxyCommand" : proxyJump! })}
                </span>
              )}
            </button>
          </div>

          {advancedOpen && (
            <>
              <Field label={t("ssh.hostModal.proxyJumpLabel")} wide>
                <select
                  value={proxyJump}
                  onChange={(e) => setProxyJump(e.target.value)}
                  className="input"
                  disabled={Boolean(proxyCommand.trim())}
                  title={t("ssh.hostModal.proxyJumpAria")}
                  aria-label={t("ssh.hostModal.proxyJumpAria")}
                >
                  <option value="">{t("ssh.hostModal.noneDirectOption")}</option>
                  {otherHosts.map((n) => (
                    <option key={n} value={n}>{n}</option>
                  ))}
                </select>
                <div className="text-[10px] opacity-50 mt-1">
                  {t("ssh.hostModal.proxyJumpHintPrefix")}<code>ssh -J</code>{t("ssh.hostModal.proxyJumpHintSuffix")}
                </div>
              </Field>

              <Field label={t("ssh.hostModal.proxyCommandLabel")} wide>
                <input
                  value={proxyCommand}
                  onChange={(e) => setProxyCommand(e.target.value)}
                  placeholder="e.g. cloudflared access ssh --hostname %h"
                  className="input font-mono text-xs"
                />
                <div className="text-[10px] opacity-50 mt-1">
                  {t("ssh.hostModal.proxyCommandHintPrefix")}<code>%h</code>{t("ssh.hostModal.proxyCommandHintMiddle")}<code>%p</code>{t("ssh.hostModal.proxyCommandHintSuffix")}
                </div>
              </Field>
            </>
          )}
        </div>

        {saveError && (
          <div className="mt-4 px-3 py-2 rounded-lg border text-xs flex items-start gap-2 bg-red-500/15 border-red-400/40 text-red-100">
            <AlertTriangle className="w-4 h-4 shrink-0 mt-0.5" />
            <div className="flex-1 break-all">{saveError}</div>
          </div>
        )}

        {testResult && (
          <div
            className={
              "mt-4 px-3 py-2 rounded-lg border text-xs flex items-start gap-2 " +
              (testResult.ok
                ? "bg-emerald-500/15 border-emerald-400/40 text-emerald-100"
                : "bg-red-500/15 border-red-400/40 text-red-100")
            }
          >
            {testResult.ok ? (
              <Check className="w-4 h-4 shrink-0 mt-0.5" />
            ) : (
              <AlertTriangle className="w-4 h-4 shrink-0 mt-0.5" />
            )}
            <div className="flex-1 break-all">
              {testResult.message}
              {testResult.elapsedMs > 0 && (
                <span className="opacity-60"> · {testResult.elapsedMs} ms</span>
              )}
            </div>
          </div>
        )}

        <div className="flex justify-between items-center gap-2 mt-4">
          <button
            onClick={test}
            disabled={testing}
            className="px-3 py-1.5 rounded-lg border border-white/10 bg-white/5 hover:bg-white/10 text-sm flex items-center gap-2 disabled:opacity-50"
            title={t("ssh.hostModal.testTooltip")}
          >
            {testing ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <PlugZap className="w-3.5 h-3.5" />}
            {t("ssh.hostModal.testButton")}
          </button>
          <div className="flex gap-2">
            <button
              onClick={onClose}
              disabled={testing}
              className="px-4 py-1.5 rounded-lg border border-white/10 bg-white/5 hover:bg-white/10 text-sm disabled:opacity-50"
            >
              {t("common.cancel")}
            </button>
            <button
              onClick={save}
              disabled={testing}
              className="px-4 py-1.5 rounded-lg bg-accent/30 border border-accent text-sm hover:bg-accent/50 disabled:opacity-50"
            >
              {isNew ? t("ssh.hostModal.addHostButton") : t("common.save")}
            </button>
          </div>
        </div>

        <SSHKeyGenModal
          open={genOpen}
          host={cleanName(name)}
          onClose={() => setGenOpen(false)}
          onGenerated={(k) => {
            setKeyName(k.privatePath.split(/[\\/]/).pop() ?? "");
            setPubKey(k.publicKey);
            setGenOpen(false);
            flash(t("ssh.hostModal.flash.keyGenerated", { name: cleanName(name) }));
          }}
          flash={flash}
        />
        <SSHImportKeyModal
          open={importOpen}
          host={cleanName(name)}
          onClose={() => setImportOpen(false)}
          onImported={(k) => {
            const fname = k.privatePath.split(/[\\/]/).pop() ?? "";
            setKeyName(fname);
            setPubKey(k.publicKey);
            setImportOpen(false);
            flash(t("ssh.hostModal.flash.keyImported", { name: fname }));
          }}
          flash={flash}
        />
      </div>
    </div>,
    document.body,
  );
}

function Field({ label, wide, children }: { label: string; wide?: boolean; children: React.ReactNode }) {
  return (
    <label className={clsx("flex flex-col gap-1", wide && "col-span-2")}>
      <span className="text-[11px] uppercase tracking-wider opacity-60">{label}</span>
      {children}
    </label>
  );
}

function AuthTab({ active, onClick, icon, label }: { active: boolean; onClick: () => void; icon: React.ReactNode; label: string }) {
  return (
    <button
      onClick={onClick}
      className={clsx(
        "px-3 py-1 text-xs flex items-center gap-1.5",
        active ? "bg-accent/30 text-white" : "hover:bg-white/10 text-white/70",
      )}
    >
      {icon} {label}
    </button>
  );
}
