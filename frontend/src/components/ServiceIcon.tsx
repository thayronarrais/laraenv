import { Database, Globe, Mail, Server } from "lucide-react";

type Props = { name: string; className?: string };

const BRAND: Record<string, { color: string; bg: string; ring: string }> = {
  nginx:    { color: "#00B140", bg: "rgba(0,177,64,0.12)",   ring: "rgba(0,177,64,0.35)" },
  apache:   { color: "#D22128", bg: "rgba(210,33,40,0.12)",  ring: "rgba(210,33,40,0.35)" },
  caddy:    { color: "#22B638", bg: "rgba(34,182,56,0.12)",  ring: "rgba(34,182,56,0.35)"  },
  mysql:    { color: "#F29111", bg: "rgba(242,145,17,0.12)", ring: "rgba(242,145,17,0.35)" },
  postgres: { color: "#4A90E2", bg: "rgba(74,144,226,0.14)", ring: "rgba(74,144,226,0.4)"  },
  pgvector: { color: "#4A90E2", bg: "rgba(74,144,226,0.14)", ring: "rgba(74,144,226,0.4)"  },
  redis:    { color: "#FF4438", bg: "rgba(255,68,56,0.12)",  ring: "rgba(255,68,56,0.35)"  },
  mailpit:  { color: "#2DD4BF", bg: "rgba(45,212,191,0.12)", ring: "rgba(45,212,191,0.35)" },
};

const DEFAULT_BRAND = { color: "#A78BFA", bg: "rgba(167,139,250,0.12)", ring: "rgba(167,139,250,0.35)" };

export default function ServiceIcon({ name, className }: Props) {
  const brand = BRAND[name] ?? DEFAULT_BRAND;

  const Glyph = name === "mailpit" ? Mail
    : ["nginx", "apache", "caddy"].includes(name) ? Globe
    : /^(my|pg|maria|mongo|postgres|redis)/i.test(name) ? Database
    : Server;

  return (
    <div
      className={
        "w-9 h-9 rounded-lg flex items-center justify-center flex-shrink-0 border " +
        (className ?? "")
      }
      style={{ background: brand.bg, borderColor: brand.ring }}
    >
      <Glyph className="w-5 h-5" style={{ color: brand.color }} aria-hidden="true" />
    </div>
  );
}

export function brandColor(name: string): string {
  return (BRAND[name] ?? DEFAULT_BRAND).color;
}
