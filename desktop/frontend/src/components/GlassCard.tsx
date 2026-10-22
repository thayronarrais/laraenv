import { PropsWithChildren } from "react";
import clsx from "clsx";

type Props = PropsWithChildren<{
  className?: string;
  title?: string;
  icon?: React.ReactNode;
  strong?: boolean;
}>;

export default function GlassCard({ className, title, icon, strong, children }: Props) {
  return (
    <section className={clsx(strong ? "glass-strong" : "glass", "p-5 flex flex-col", className)}>
      {(title || icon) && (
        <header className="flex items-center gap-2 text-xs uppercase tracking-wider opacity-70 mb-4">
          {icon}
          <span>{title}</span>
        </header>
      )}
      <div className="flex-1 min-h-0">{children}</div>
    </section>
  );
}
