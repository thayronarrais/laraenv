import { Search } from "lucide-react";
import { useT } from "../../lib/i18n";
import { agentStatusLabel } from "./AgentStatusGlyph";

export type AgentFilterState = { text: string; provider: string; state: string; archived: boolean };

export default function AgentFilters({ value, onChange, providers }: { value: AgentFilterState; onChange: (value: AgentFilterState) => void; providers: string[] }) {
  const t = useT();
  return (
    <div className="flex flex-wrap items-center gap-2">
      <label className="flex h-8 min-w-52 flex-1 items-center gap-2 rounded-lg border border-white/10 bg-white/[0.035] px-2.5">
        <Search className="h-3.5 w-3.5 opacity-45" />
        <input value={value.text} onChange={(event) => onChange({ ...value, text: event.target.value })} className="w-full bg-transparent text-[11px] outline-none" placeholder={t("agents.filters.searchPlaceholder")} />
      </label>
      <select value={value.provider} onChange={(event) => onChange({ ...value, provider: event.target.value })} className="input h-8 min-w-32 text-[10px]">
        <option value="">{t("agents.filters.allProviders")}</option>
        {providers.map((provider) => <option key={provider} value={provider}>{provider}</option>)}
      </select>
      <select value={value.state} onChange={(event) => onChange({ ...value, state: event.target.value })} className="input h-8 min-w-28 text-[10px]">
        <option value="">{t("agents.filters.allStates")}</option>
        {["starting", "working", "needs-input", "done", "idle", "stopped", "error"].map((state) => <option key={state} value={state}>{agentStatusLabel(t, state)}</option>)}
      </select>
      <label className="flex h-8 items-center gap-2 rounded-lg border border-white/10 bg-white/[0.035] px-2.5 text-[10px] opacity-70">
        <input type="checkbox" checked={value.archived} onChange={(event) => onChange({ ...value, archived: event.target.checked })} /> {t("agents.filters.archivedLabel")}
      </label>
    </div>
  );
}
