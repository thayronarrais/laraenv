import { Crown, ExternalLink } from "lucide-react";
import { useT } from "../../lib/i18n";

export default function ProjectRouteBadge({ canonical, hostname }: { canonical: boolean; hostname: string }) {
  const t = useT();
  return (
    <span
      className={`inline-flex items-center gap-1 rounded border px-1.5 py-0.5 font-mono text-[8px] ${canonical ? "border-emerald-400/35 bg-emerald-500/10 text-emerald-200" : "border-sky-400/30 bg-sky-500/10 text-sky-200"}`}
      title={canonical ? t("agents.routeBadge.primaryTooltip") : t("agents.routeBadge.alternativeTooltip")}
    >
      {canonical ? <Crown className="h-2.5 w-2.5" /> : <ExternalLink className="h-2.5 w-2.5" />}
      {canonical ? t("agents.routeBadge.mainLabel") : t("agents.routeBadge.previewLabel")} · {hostname}
    </span>
  );
}
