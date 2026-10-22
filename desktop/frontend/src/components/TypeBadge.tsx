import clsx from "clsx";
import { useT } from "../lib/i18n";

const styles: Record<string, string> = {
  laravel:   "bg-red-500/15 border-red-400/40 text-red-200",
  wordpress: "bg-blue-500/15 border-blue-400/40 text-blue-200",
  public:    "bg-violet-500/15 border-violet-400/40 text-violet-200",
  plain:     "bg-zinc-500/15 border-zinc-400/30 text-zinc-300",
};

// Laravel, WordPress and public/ are brand/technology names and a directory
// path, so they stay untranslated. "plain" is the one genuine English word
// here and is translated below.
const labels: Record<string, string> = {
  laravel:   "Laravel",
  wordpress: "WordPress",
  public:    "public/",
};

export default function TypeBadge({ kind }: { kind: string }) {
  const t = useT();
  const k = kind || "plain";
  const label = k === "plain" ? t("common.typeBadge.plain") : (labels[k] ?? k);
  return (
    <span
      className={clsx(
        "px-2 py-0.5 rounded-full text-[10px] uppercase tracking-wider border",
        styles[k] ?? styles.plain
      )}
    >
      {label}
    </span>
  );
}
