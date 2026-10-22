export default function FilterChip({
  active, onClick, label,
}: { active: boolean; onClick: () => void; label: string }) {
  return (
    <button
      onClick={onClick}
      className={
        "px-2.5 py-1 rounded-full text-[11px] border transition-colors " +
        (active
          ? "bg-accent/30 border-accent text-white shadow-[0_0_12px_rgba(124,92,255,0.35)]"
          : "bg-white/5 border-white/10 hover:bg-white/10 text-white/70")
      }
    >
      {label}
    </button>
  );
}
