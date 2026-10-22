import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import clsx from "clsx";
import { FileText, Code2, FileCode, GitBranch, X, Loader2, Boxes, Zap, Rocket, Sparkles, Tag as TagIcon } from "lucide-react";
// @ts-ignore
import { CreateProject, SetProjectTags, ListPhpVersions } from "../../wailsjs/go/main/App";
// @ts-ignore
import { EventsOn, EventsOff } from "../../wailsjs/runtime";
import { addTag, commitDraft } from "../lib/tags";
import VcRedistModal, { REDIST_PROBE } from "./VcRedistModal";
import { useT } from "../lib/i18n";

type Kind = "blank" | "laravel" | "wordpress" | "git" | "vite" | "next" | "nuxt" | "astro";
type PM = "npm" | "pnpm" | "yarn" | "bun";

// Catalogue keys for the TYPES descriptions that are genuine prose rather
// than a literal shell command ("composer create-project laravel", "git
// clone <url>" stay data). "blank" has its own dedicated
// blankLabel/blankDesc pair below, resolved separately at the render site.
const TYPE_DESC_KEYS: Partial<Record<Kind, string>> = {
  wordpress: "projects.newModal.types.wordpressDesc",
  vite: "projects.newModal.types.viteDesc",
  next: "projects.newModal.types.nextDesc",
  nuxt: "projects.newModal.types.nuxtDesc",
  astro: "projects.newModal.types.astroDesc",
};

const TYPES: { id: Kind; label: string; desc: string; icon: React.ReactNode }[] = [
  { id: "blank",     label: "Blank",          desc: "Empty folder + welcome page",        icon: <FileText className="w-5 h-5" /> },
  { id: "laravel",   label: "Laravel",        desc: "composer create-project laravel",    icon: <Code2 className="w-5 h-5" /> },
  { id: "wordpress", label: "WordPress",      desc: "Latest WP from wordpress.org",       icon: <FileCode className="w-5 h-5" /> },
  { id: "git",       label: "Clone from Git", desc: "git clone <url>",                    icon: <GitBranch className="w-5 h-5" /> },
  { id: "vite",      label: "Vite",           desc: "create vite (react/vue/svelte/…)",   icon: <Zap className="w-5 h-5" /> },
  { id: "next",      label: "Next.js",        desc: "create-next-app (React + SSR)",      icon: <Rocket className="w-5 h-5" /> },
  { id: "nuxt",      label: "Nuxt",           desc: "create nuxt (Vue + SSR)",            icon: <Boxes className="w-5 h-5" /> },
  { id: "astro",     label: "Astro",          desc: "create astro (content-driven)",      icon: <Sparkles className="w-5 h-5" /> },
];

const VITE_TEMPLATES = [
  "vanilla", "vanilla-ts",
  "react", "react-ts", "react-swc", "react-swc-ts",
  "vue", "vue-ts",
  "svelte", "svelte-ts",
  "preact", "preact-ts",
  "lit", "lit-ts",
  "solid", "solid-ts",
];

// Laravel major → minimum PHP (major, minor) it runs on. Source: official
// Laravel docs (server requirements per release). Restrict so the user can't
// pick a combo that composer would refuse to install.
const LARAVEL_VERSIONS = ["13", "12", "11", "10", "9", "8"] as const;
type LaravelVersion = (typeof LARAVEL_VERSIONS)[number];

const LARAVEL_MIN_PHP: Record<LaravelVersion, [number, number]> = {
  "13": [8, 3],
  "12": [8, 2],
  "11": [8, 2],
  "10": [8, 1],
  "9":  [8, 0],
  "8":  [7, 3],
};

type PhpRow = { version: string; path: string; active: boolean };

function parseMajorMinor(v: string): [number, number] {
  const [maj = "0", min = "0"] = v.split(".");
  return [parseInt(maj, 10) || 0, parseInt(min, 10) || 0];
}

function phpMeetsMin(phpVersion: string, min: [number, number]): boolean {
  const [pMaj, pMin] = parseMajorMinor(phpVersion);
  if (pMaj !== min[0]) return pMaj > min[0];
  return pMin >= min[1];
}

function compareVersionDesc(a: string, b: string): number {
  const [aMaj, aMin] = parseMajorMinor(a);
  const [bMaj, bMin] = parseMajorMinor(b);
  if (aMaj !== bMaj) return bMaj - aMaj;
  return bMin - aMin;
}

export default function NewProjectModal({
  open, onClose, flash, initialKind,
}: {
  open: boolean; onClose: () => void; flash: (m: string) => void; initialKind?: string;
}) {
  const t = useT();
  const [kind, setKind] = useState<Kind>("blank");
  const [name, setName] = useState("");
  const [gitUrl, setGitUrl] = useState("");
  const [viteTpl, setViteTpl] = useState("react-ts");
  const [pm, setPm] = useState<PM>("npm");
  const [busy, setBusy] = useState(false);
  const [log, setLog] = useState<string[]>([]);
  const [done, setDone] = useState<"ok" | "error" | null>(null);
  const [tags, setTags] = useState<string[]>([]);
  const [tagDraft, setTagDraft] = useState("");
  const [laravelVersion, setLaravelVersion] = useState<LaravelVersion>("12");
  const [phpVersions, setPhpVersions] = useState<PhpRow[]>([]);
  const [phpVersion, setPhpVersion] = useState<string>("");
  const [vcRedist, setVcRedist] = useState<{ open: boolean; redistUrl?: string }>({ open: false });
  const pendingTagsRef = useRef<{ name: string; tags: string[] } | null>(null);
  const logRef = useRef<HTMLPreElement>(null);

  useEffect(() => {
    if (!open) return;
    setLog([]);
    setDone(null);
    setBusy(false);
    // Pre-select the type when opened from a palette entry (e.g. "New Laravel
    // project"); otherwise keep the current selection.
    if (initialKind && TYPES.some((entry) => entry.id === initialKind)) {
      setKind(initialKind as Kind);
    }
    ListPhpVersions().then((v: PhpRow[]) => setPhpVersions(v ?? [])).catch(() => setPhpVersions([]));
  }, [open, initialKind]);

  // Compatible installed PHP versions for the selected Laravel major,
  // sorted newest-first. Re-derived whenever inputs change.
  const compatiblePhp = phpVersions
    .filter((v) => phpMeetsMin(v.version, LARAVEL_MIN_PHP[laravelVersion]))
    .sort((a, b) => compareVersionDesc(a.version, b.version));

  // Auto-pick the highest compatible PHP whenever the Laravel version changes
  // or the current selection becomes incompatible (e.g. user just installed
  // a newer PHP, or switched Laravel from 8 → 11).
  useEffect(() => {
    if (kind !== "laravel") return;
    if (!compatiblePhp.length) {
      setPhpVersion("");
      return;
    }
    if (!compatiblePhp.find((v) => v.version === phpVersion)) {
      setPhpVersion(compatiblePhp[0].version);
    }
  }, [kind, laravelVersion, phpVersions]);

  useEffect(() => {
    const onProgress = (line: string) => setLog((l) => [...l, line]);
    const onDone = () => {
      setBusy(false); setDone("ok");
      const pending = pendingTagsRef.current;
      pendingTagsRef.current = null;
      if (pending && pending.tags.length > 0) {
        SetProjectTags(pending.name, pending.tags).catch((e: any) =>
          setLog((l) => [...l, "WARN: tag save failed: " + String(e)]),
        );
      }
    };
    const onError = (e: { message: string; redistUrl?: string }) => {
      const msg = e?.message ?? String(e);
      setLog((l) => [...l, "ERROR: " + msg]);
      setBusy(false);
      setDone("error");
      // The composer pre-flight surfaces redist failures inline; promote
      // them to the dedicated modal so the URL is clickable instead of
      // buried in a non-selectable log line. redistUrl comes straight from
      // the backend (vcruntime.Check via ProjectCreateError.redistUrl).
      if (typeof msg === "string" && msg.includes(REDIST_PROBE)) {
        setVcRedist({ open: true, redistUrl: e?.redistUrl });
      }
    };
    EventsOn("project:create-progress", onProgress);
    EventsOn("project:create-done", onDone);
    EventsOn("project:create-error", onError);
    return () => {
      EventsOff("project:create-progress");
      EventsOff("project:create-done");
      EventsOff("project:create-error");
    };
  }, []);

  useEffect(() => {
    if (logRef.current) logRef.current.scrollTop = logRef.current.scrollHeight;
  }, [log]);

  if (!open) return null;

  const start = async () => {
    const cleanName = name.trim().toLowerCase().replace(/\s+/g, "-");
    if (!cleanName) { flash(t("projects.newModal.nameRequired")); return; }
    if (kind === "git" && !gitUrl.trim()) { flash(t("projects.newModal.gitUrlRequired")); return; }
    if (kind === "laravel" && !phpVersion) {
      flash(t("projects.newModal.installPhpRequired", { min: LARAVEL_MIN_PHP[laravelVersion].join("."), version: laravelVersion }));
      return;
    }
    setBusy(true);
    setLog([t("projects.newModal.creatingLog", { kind, name: cleanName })]);
    pendingTagsRef.current = { name: cleanName, tags: commitDraft(tags, tagDraft) };
    try {
      await CreateProject({
        name: cleanName,
        kind,
        gitUrl: gitUrl.trim(),
        jsTemplate: kind === "vite" ? viteTpl : "",
        packageManager: ["vite", "next", "nuxt", "astro"].includes(kind) ? pm : "",
        laravelVersion: kind === "laravel" ? laravelVersion : "",
        phpVersion: kind === "laravel" ? phpVersion : "",
      });
    } catch (e) {
      const msg = String(e);
      setLog((l) => [...l, "ERROR: " + msg]);
      setBusy(false);
      setDone("error");
      if (msg.includes(REDIST_PROBE)) {
        // This path is a raw promise rejection (CreateProject itself
        // failing to kick off), not the structured project:create-error
        // event — no typed redistUrl is available here, so the modal
        // falls back to its own default.
        setVcRedist({ open: true });
      }
    }
  };

  const close = () => {
    if (busy) return;
    setName(""); setGitUrl(""); setKind("blank"); setTags([]); setTagDraft("");
    onClose();
  };

  return createPortal(
    <div className="modal-backdrop">
      <div
        onClick={(e) => e.stopPropagation()}
        className="modal-card w-[600px] max-w-[92vw] max-h-[90vh] flex flex-col p-5"
      >
        <header className="flex items-center justify-between mb-4">
          <h2 className="text-base font-semibold">{t("projects.newModal.title")}</h2>
          <button
            onClick={close}
            disabled={busy}
            className="p-1 rounded-md hover:bg-white/10 disabled:opacity-30"
          >
            <X className="w-4 h-4" />
          </button>
        </header>

        {/* Type cards */}
        <div className="grid grid-cols-4 gap-2 mb-4 max-h-[280px] overflow-y-auto">
          {TYPES.map((entry) => (
            <button
              key={entry.id}
              disabled={busy}
              onClick={() => setKind(entry.id)}
              className={clsx(
                "p-3 rounded-lg border flex flex-col items-start gap-1.5 text-left transition-colors",
                kind === entry.id
                  ? "bg-accent/25 border-accent text-white"
                  : "bg-white/5 border-white/10 hover:bg-white/10 text-white/80",
                busy && "opacity-50 cursor-not-allowed"
              )}
            >
              {entry.icon}
              <div className="text-sm font-medium">
                {entry.id === "blank" ? t("projects.newModal.types.blankLabel")
                  : entry.id === "git" ? t("projects.newModal.types.gitLabel")
                  : entry.label}
              </div>
              <div className="text-[10px] opacity-60">
                {entry.id === "blank" ? t("projects.newModal.types.blankDesc")
                  : TYPE_DESC_KEYS[entry.id] ? t(TYPE_DESC_KEYS[entry.id]!)
                  : entry.desc}
              </div>
            </button>
          ))}
        </div>

        {/* Inputs */}
        <div className="flex flex-col gap-2 mb-4">
          <label className="text-[11px] uppercase tracking-wider opacity-60">{t("projects.newModal.nameLabel")}</label>
          <input
            value={name}
            onChange={(e) => setName(e.target.value)}
            disabled={busy}
            placeholder="mysite"
            className="bg-white/5 border border-white/10 rounded-lg px-3 py-1.5 text-sm focus:outline-none focus:border-accent disabled:opacity-50"
            autoFocus
          />
          <label className="text-[11px] uppercase tracking-wider opacity-60 mt-2">{t("projects.newModal.tagsLabel")} <span className="opacity-40 normal-case">{t("projects.newModal.tagsOptional")}</span></label>
          <div
            className="bg-white/5 border border-white/10 rounded-lg px-3 py-1.5 flex flex-wrap items-center gap-1 cursor-text min-h-[36px]"
            onClick={(e) => {
              const target = e.target as HTMLElement;
              if (target.tagName !== "INPUT") {
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
                  title={t("projects.common.removeTagTooltip")}
                >
                  <X className="w-2.5 h-2.5" />
                </button>
              </span>
            ))}
            <input
              value={tagDraft}
              disabled={busy}
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
              placeholder={tags.length === 0 ? t("projects.common.tagsPlaceholder") : ""}
              className="bg-transparent outline-none flex-1 min-w-[80px] text-sm disabled:opacity-50"
            />
          </div>
          {kind === "git" && (
            <>
              <label className="text-[11px] uppercase tracking-wider opacity-60 mt-2">{t("projects.newModal.gitUrlLabel")}</label>
              <input
                value={gitUrl}
                onChange={(e) => setGitUrl(e.target.value)}
                disabled={busy}
                placeholder="https://github.com/owner/repo.git"
                className="bg-white/5 border border-white/10 rounded-lg px-3 py-1.5 text-sm focus:outline-none focus:border-accent disabled:opacity-50"
              />
            </>
          )}
          {kind === "laravel" && (
            <div className="grid grid-cols-2 gap-2 mt-2">
              <div className="flex flex-col gap-1">
                <label className="text-[11px] uppercase tracking-wider opacity-60">{t("projects.newModal.laravelVersionLabel")}</label>
                <select
                  title={t("projects.newModal.laravelVersionLabel")}
                  value={laravelVersion}
                  onChange={(e) => setLaravelVersion(e.target.value as LaravelVersion)}
                  disabled={busy}
                  className="bg-white/5 border border-white/10 rounded-lg px-3 py-1.5 text-sm focus:outline-none focus:border-accent disabled:opacity-50"
                >
                  {LARAVEL_VERSIONS.map((v) => (
                    <option key={v} value={v}>Laravel {v}</option>
                  ))}
                </select>
                <span className="text-[10px] opacity-50">
                  {t("projects.newModal.phpMinHint", { min: LARAVEL_MIN_PHP[laravelVersion].join(".") })}
                </span>
              </div>
              <div className="flex flex-col gap-1">
                <label className="text-[11px] uppercase tracking-wider opacity-60">{t("projects.newModal.phpVersionLabel")}</label>
                {compatiblePhp.length > 0 ? (
                  <select
                    title={t("projects.newModal.phpVersionLabel")}
                    value={phpVersion}
                    onChange={(e) => setPhpVersion(e.target.value)}
                    disabled={busy}
                    className="bg-white/5 border border-white/10 rounded-lg px-3 py-1.5 text-sm focus:outline-none focus:border-accent disabled:opacity-50"
                  >
                    {compatiblePhp.map((v) => (
                      <option key={v.version} value={v.version}>
                        PHP {v.version}{v.active ? t("projects.newModal.activeSuffix") : ""}
                      </option>
                    ))}
                  </select>
                ) : (
                  <div className="bg-amber-500/10 border border-amber-400/30 rounded-lg px-3 py-1.5 text-[11px] text-amber-100">
                    {t("projects.newModal.noPhpAvailable", { min: LARAVEL_MIN_PHP[laravelVersion].join(".") })}
                  </div>
                )}
              </div>
            </div>
          )}
          {["vite", "next", "nuxt", "astro"].includes(kind) && (
            <>
              <label className="text-[11px] uppercase tracking-wider opacity-60 mt-2">{t("projects.newModal.packageManagerLabel")}</label>
              <div className="flex gap-2">
                {(["npm", "pnpm", "yarn", "bun"] as PM[]).map((p) => (
                  <button
                    key={p}
                    disabled={busy}
                    onClick={() => setPm(p)}
                    className={clsx(
                      "px-3 py-1.5 rounded-lg border text-xs",
                      pm === p
                        ? "bg-accent/30 border-accent text-white"
                        : "bg-white/5 border-white/10 hover:bg-white/10"
                    )}
                  >
                    {p}
                  </button>
                ))}
              </div>
              {kind === "vite" && (
                <>
                  <label className="text-[11px] uppercase tracking-wider opacity-60 mt-2">{t("projects.newModal.viteTemplateLabel")}</label>
                  <select
                    value={viteTpl}
                    onChange={(e) => setViteTpl(e.target.value)}
                    disabled={busy}
                    className="bg-white/5 border border-white/10 rounded-lg px-3 py-1.5 text-sm focus:outline-none focus:border-accent disabled:opacity-50"
                  >
                    {VITE_TEMPLATES.map((tpl) => (
                      <option key={tpl} value={tpl}>{tpl}</option>
                    ))}
                  </select>
                </>
              )}
            </>
          )}
        </div>

        {/* Log area (only when started) */}
        {log.length > 0 && (
          <pre
            ref={logRef}
            className="flex-1 min-h-[120px] max-h-[260px] overflow-auto bg-black/40 border border-white/10 rounded-lg p-2 text-[11px] font-mono text-white/80 mb-4"
          >
            {log.join("\n")}
          </pre>
        )}

        {/* Actions */}
        <div className="flex justify-end gap-2">
          {!done && !busy && (
            <>
              <button
                onClick={close}
                className="px-4 py-1.5 rounded-lg border border-white/10 bg-white/5 hover:bg-white/10 text-sm"
              >
                {t("common.cancel")}
              </button>
              <button
                onClick={start}
                disabled={kind === "laravel" && !phpVersion}
                className="px-4 py-1.5 rounded-lg bg-accent/30 border border-accent text-sm hover:bg-accent/50 disabled:opacity-40 disabled:cursor-not-allowed disabled:hover:bg-accent/30"
              >
                {t("projects.newModal.createButton")}
              </button>
            </>
          )}
          {busy && (
            <div className="flex items-center gap-2 text-sm opacity-70">
              <Loader2 className="w-4 h-4 animate-spin" /> {t("projects.common.workingLabel")}
            </div>
          )}
          {done && (
            <button
              onClick={close}
              className={clsx(
                "px-4 py-1.5 rounded-lg border text-sm",
                done === "ok"
                  ? "bg-emerald-500/20 border-emerald-400/40 hover:bg-emerald-500/30 text-emerald-100"
                  : "bg-red-500/20 border-red-400/40 hover:bg-red-500/30 text-red-100"
              )}
            >
              {t("common.close")}
            </button>
          )}
        </div>
      </div>
      <VcRedistModal
        open={vcRedist.open}
        binary="php.exe"
        redistUrl={vcRedist.redistUrl}
        onClose={() => setVcRedist({ open: false })}
      />
    </div>,
    document.body
  );
}
