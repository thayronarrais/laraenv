import { useEffect, useMemo, useState } from "react";
import { createPortal } from "react-dom";
import { X, Tag as TagIcon } from "lucide-react";
// @ts-ignore
import { SetProjectTags } from "../../wailsjs/go/main/App";
import { addTag, canonicalTag, commitDraft, suggestTags } from "../lib/tags";
import { useT } from "../lib/i18n";

type Project = { name: string; tags: string[] };

export default function ProjectTagsModal({
  project, open, onClose, flash, suggestions = [],
}: {
  project: Project | null;
  open: boolean;
  onClose: () => void;
  flash: (m: string) => void;
  /** Tags already used by any project, offered as autocomplete. */
  suggestions?: string[];
}) {
  const t = useT();
  const [tags, setTags] = useState<string[]>([]);
  const [draft, setDraft] = useState("");
  const [busy, setBusy] = useState(false);
  const [highlight, setHighlight] = useState(-1);

  const matches = useMemo(() => suggestTags(suggestions, tags, draft), [suggestions, tags, draft]);

  const pick = (raw: string) => {
    setTags((current) => addTag(current, canonicalTag(suggestions, raw)));
    setDraft("");
    setHighlight(-1);
  };

  useEffect(() => {
    if (!open) return;
    setTags((project?.tags ?? []).map((tag) => tag.trim()).filter(Boolean));
    setDraft("");
    setHighlight(-1);
    setBusy(false);
  }, [open, project?.name]);

  if (!open || !project) return null;

  const save = async () => {
    setBusy(true);
    try {
      await SetProjectTags(project.name, commitDraft(tags, canonicalTag(suggestions, draft)));
      flash(t("projects.tagsModal.savedFlash", { name: project.name }));
      onClose();
    } catch (e) {
      flash(t("projects.errors.generic", { message: String(e) }));
      setBusy(false);
    }
  };

  return createPortal(
    <div className="modal-backdrop">
      <div
        onClick={(e) => e.stopPropagation()}
        className="modal-card w-[440px] max-w-[92vw] p-5 flex flex-col gap-4"
      >
        <header className="flex items-center justify-between">
          <h2 className="text-base font-semibold">{t("projects.tagsModal.title", { name: project.name })}</h2>
          <button
            onClick={onClose}
            disabled={busy}
            className="p-1 rounded-md hover:bg-white/10 disabled:opacity-30"
          >
            <X className="w-4 h-4" />
          </button>
        </header>

        <div
          className="bg-white/5 border border-white/10 rounded-lg px-3 py-2 flex flex-wrap items-center gap-1 cursor-text min-h-[44px]"
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
            value={draft}
            autoFocus
            role="combobox"
            aria-expanded={matches.length > 0}
            aria-controls="project-tag-suggestions"
            aria-autocomplete="list"
            aria-activedescendant={highlight >= 0 ? `project-tag-suggestion-${highlight}` : undefined}
            onChange={(e) => {
              const v = e.target.value;
              setHighlight(-1);
              if (v.endsWith(",")) {
                pick(v.slice(0, -1));
              } else {
                setDraft(v);
              }
            }}
            onKeyDown={(e) => {
              if (e.key === "ArrowDown" && matches.length > 0) {
                e.preventDefault();
                setHighlight((h) => (h + 1) % matches.length);
              } else if (e.key === "ArrowUp" && matches.length > 0) {
                e.preventDefault();
                setHighlight((h) => (h <= 0 ? matches.length - 1 : h - 1));
              } else if (e.key === "Tab" && draft.trim() && matches.length > 0) {
                // Tab completes with the highlighted (or first) suggestion.
                e.preventDefault();
                pick(matches[highlight >= 0 ? highlight : 0]);
              } else if (e.key === "Enter") {
                e.preventDefault();
                if (highlight >= 0 && matches[highlight]) pick(matches[highlight]);
                else if (draft.trim()) pick(draft);
              } else if (e.key === "Escape" && highlight >= 0) {
                e.stopPropagation();
                setHighlight(-1);
              } else if (e.key === "Backspace" && !draft && tags.length > 0) {
                setTags(tags.slice(0, -1));
              }
            }}
            onBlur={() => {
              if (draft.trim()) pick(draft);
            }}
            placeholder={tags.length === 0 ? t("projects.common.tagsPlaceholder") : ""}
            className="bg-transparent outline-none flex-1 min-w-[80px] text-sm"
          />
        </div>

        {matches.length > 0 && (
          <div className="flex flex-col gap-1.5">
            <div className="text-[10px] uppercase tracking-[0.14em] text-white/45">
              {draft.trim() ? t("projects.tagsModal.matchingTags") : t("projects.tagsModal.existingTags")}
              <span className="ml-2 normal-case tracking-normal text-white/30">{t("projects.tagsModal.keyboardHint")}</span>
            </div>
            <div id="project-tag-suggestions" role="listbox" aria-label={t("projects.tagsModal.suggestionsAriaLabel")} className="flex flex-wrap gap-1">
              {matches.map((tag, i) => (
                <button
                  key={tag}
                  id={`project-tag-suggestion-${i}`}
                  type="button"
                  role="option"
                  aria-selected={i === highlight}
                  // onMouseDown so the input blur does not commit the draft first.
                  onMouseDown={(e) => { e.preventDefault(); pick(tag); }}
                  className={`px-1.5 py-0.5 rounded-md text-[10px] flex items-center gap-1 border uppercase tracking-wider transition ${
                    i === highlight
                      ? "bg-accent/35 border-accent text-white"
                      : "bg-white/5 border-white/15 text-white/75 hover:bg-white/10 hover:text-white"
                  }`}
                >
                  <TagIcon className="w-2.5 h-2.5" />
                  {tag}
                </button>
              ))}
            </div>
          </div>
        )}

        <div className="flex justify-end gap-2">
          <button
            onClick={onClose}
            disabled={busy}
            className="px-4 py-1.5 rounded-lg border border-white/10 bg-white/5 hover:bg-white/10 text-sm disabled:opacity-50"
          >
            {t("common.cancel")}
          </button>
          <button
            onClick={save}
            disabled={busy}
            className="px-4 py-1.5 rounded-lg bg-accent/30 border border-accent text-sm hover:bg-accent/50 disabled:opacity-50"
          >
            {t("common.save")}
          </button>
        </div>
      </div>
    </div>,
    document.body,
  );
}
