import { Bot, Code2, Sparkles, TerminalSquare, type LucideIcon } from "lucide-react";

type ProviderID = "claude" | "codex" | "kimi" | "opencode" | string;

const providerGlyphs: Record<string, { Glyph: LucideIcon; tile: string }> = {
  claude: { Glyph: Bot, tile: "bg-transparent text-orange-300 p-[2px]" },
  codex: { Glyph: TerminalSquare, tile: "bg-white text-slate-900 p-[2px]" },
  kimi: { Glyph: Sparkles, tile: "bg-black text-white p-[2px]" },
  opencode: { Glyph: Code2, tile: "bg-white text-slate-900 p-[3px]" },
};

export default function AgentProviderIcon({ provider, compact = false }: { provider: ProviderID; compact?: boolean }) {
  const { Glyph, tile } = providerGlyphs[provider] || providerGlyphs.opencode;
  return (
    <span
      aria-hidden="true"
      className={`inline-flex shrink-0 items-center justify-center overflow-hidden rounded-md ${tile} ${compact ? "h-5 w-5" : "h-8 w-8"}`}
    >
      <Glyph className="h-full w-full" strokeWidth={1.8} />
    </span>
  );
}
