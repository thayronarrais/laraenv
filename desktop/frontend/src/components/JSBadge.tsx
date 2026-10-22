import clsx from "clsx";

const styles: Record<string, string> = {
  next:   "bg-zinc-500/15 border-zinc-300/40 text-zinc-100",
  nuxt:   "bg-emerald-500/15 border-emerald-400/40 text-emerald-200",
  astro:  "bg-orange-500/15 border-orange-400/40 text-orange-200",
  vite:   "bg-purple-500/15 border-purple-400/40 text-purple-200",
  vue:    "bg-emerald-500/15 border-emerald-400/40 text-emerald-200",
  react:  "bg-cyan-500/15 border-cyan-400/40 text-cyan-200",
};

const labels: Record<string, string> = {
  next:  "Next.js",
  nuxt:  "Nuxt",
  astro: "Astro",
  vite:  "Vite",
  vue:   "Vue",
  react: "React",
};

export default function JSBadge({ framework }: { framework: string }) {
  if (!framework) return null;
  return (
    <span
      className={clsx(
        "px-2 py-0.5 rounded-full text-[10px] uppercase tracking-wider border",
        styles[framework] ?? "bg-white/5 border-white/10"
      )}
    >
      {labels[framework] ?? framework}
    </span>
  );
}
