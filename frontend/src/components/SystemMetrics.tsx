import { useEffect, useRef, useState } from "react";
import { Cpu, MemoryStick, MonitorCog, HardDrive } from "lucide-react";
// @ts-ignore
import { GetSystemMetrics } from "../../wailsjs/go/main/App";
// @ts-ignore
import { EventsOn } from "../../wailsjs/runtime";
import { useI18n, useT } from "../lib/i18n";

type Sample = {
  cpuPercent: number;
  memPercent: number;
  memUsedMB: number;
  memTotalMB: number;
  gpuPercent: number;
  gpuVendor: string;
  gpuAvailable: boolean;
  diskPercent: number;
  diskUsedGB: number;
  diskTotalGB: number;
  diskPath: string;
};

const HISTORY = 32;

export default function SystemMetrics() {
  const t = useT();
  const { formatNumber } = useI18n();
  const [sample, setSample] = useState<Sample | null>(null);
  const cpuHist = useRef<number[]>([]);
  const memHist = useRef<number[]>([]);
  const gpuHist = useRef<number[]>([]);
  const diskHist = useRef<number[]>([]);
  const [, force] = useState(0);

  useEffect(() => {
    let alive = true;
    GetSystemMetrics().then((s: Sample) => { if (alive) push(s); });

    // Keep the canceler EventsOn returns — EventsOff("<name>") removes EVERY
    // listener for the event app-wide, which permanently deafens kept-alive
    // pages (see the service:status note in Dashboard.tsx).
    const off = EventsOn("sys:metrics", (s: Sample) => push(s));
    return () => { alive = false; off(); };

    function push(s: Sample) {
      cpuHist.current = trim([...cpuHist.current, s.cpuPercent]);
      memHist.current = trim([...memHist.current, s.memPercent]);
      gpuHist.current = trim([...gpuHist.current, s.gpuAvailable ? s.gpuPercent : 0]);
      diskHist.current = trim([...diskHist.current, s.diskPercent ?? 0]);
      setSample(s);
      force((n) => n + 1);
    }
    function trim(a: number[]) {
      return a.length > HISTORY ? a.slice(a.length - HISTORY) : a;
    }
  }, []);

  if (!sample) {
    return (
      <div className="grid grid-cols-3 gap-2 text-[11px] opacity-50">{t("dashboard.metrics.loading")}</div>
    );
  }

  const diskPct = sample.diskPercent ?? 0;
  const diskColor = "#f59e0b";
  const pct = (n: number) => `${formatNumber(n, { maximumFractionDigits: 0 })}%`;
  return (
    <div className="flex flex-col gap-2">
      <div className="grid grid-cols-3 gap-2">
        <Indicator
          icon={<Cpu className="w-3.5 h-3.5" />}
          label={t("dashboard.metrics.cpu")}
          value={pct(sample.cpuPercent)}
          sub=""
          percent={sample.cpuPercent}
          history={cpuHist.current}
          color="#7c5cff"
        />
        <Indicator
          icon={<MemoryStick className="w-3.5 h-3.5" />}
          label={t("dashboard.metrics.memory")}
          value={pct(sample.memPercent)}
          sub={`${formatMB(sample.memUsedMB, formatNumber)} / ${formatMB(sample.memTotalMB, formatNumber)}`}
          percent={sample.memPercent}
          history={memHist.current}
          color="#22d3ee"
        />
        <Indicator
          icon={<MonitorCog className="w-3.5 h-3.5" />}
          label={t("dashboard.metrics.gpu")}
          value={sample.gpuAvailable ? pct(sample.gpuPercent) : t("dashboard.metrics.gpuUnavailable")}
          sub={sample.gpuVendor || ""}
          percent={sample.gpuAvailable ? sample.gpuPercent : 0}
          history={gpuHist.current}
          color="#34d399"
          dim={!sample.gpuAvailable}
        />
      </div>
      <div className="glass px-2.5 py-2 flex items-center gap-3">
        <span className="flex items-center gap-1.5 text-[11px] uppercase tracking-wider opacity-70 shrink-0">
          <HardDrive className="w-3.5 h-3.5" />
          {t("dashboard.metrics.disk")}
        </span>
        <div className="flex-1 h-1.5 rounded-full bg-white/5 overflow-hidden min-w-0">
          <div
            className="h-full rounded-full transition-all"
            style={{
              width: `${Math.min(100, Math.max(0, diskPct))}%`,
              background: diskColor,
              boxShadow: `0 0 8px ${diskColor}80`,
            }}
          />
        </div>
        <span className="text-[11px] font-semibold tabular-nums shrink-0" style={{ color: diskColor }}>
          {pct(diskPct)}
        </span>
        {sample.diskTotalGB > 0 && (
          <span className="text-[10px] opacity-60 tabular-nums shrink-0">
            {formatNumber(sample.diskUsedGB, { minimumFractionDigits: 1, maximumFractionDigits: 1 })} / {formatNumber(sample.diskTotalGB, { minimumFractionDigits: 1, maximumFractionDigits: 1 })} GB
            <span className="opacity-70 ml-1">· {sample.diskPath || "C:\\"}</span>
          </span>
        )}
      </div>
    </div>
  );
}

function Indicator({
  icon, label, value, sub, percent, history, color, dim,
}: {
  icon: React.ReactNode;
  label: string;
  value: string;
  sub: string;
  percent: number;
  history: number[];
  color: string;
  dim?: boolean;
}) {
  return (
    <div className={`glass p-2.5 flex flex-col gap-1.5 min-w-0 ${dim ? "opacity-50" : ""}`}>
      <div className="flex items-center justify-between gap-1.5 text-[11px] uppercase tracking-wider opacity-70 min-w-0">
        <span className="flex items-center gap-1.5 min-w-0 truncate">{icon}{label}</span>
        <span className="font-semibold opacity-100 shrink-0 tabular-nums" style={{ color }}>{value}</span>
      </div>
      <Sparkline data={history} color={color} />
      <div className="flex items-center gap-2">
        <div className="flex-1 h-1 rounded-full bg-white/5 overflow-hidden">
          <div
            className="h-full rounded-full transition-all"
            style={{ width: `${Math.min(100, Math.max(0, percent))}%`, background: color, boxShadow: `0 0 8px ${color}80` }}
          />
        </div>
      </div>
      {sub && <div className="text-[10px] opacity-50 truncate" title={sub}>{sub}</div>}
    </div>
  );
}

function Sparkline({ data, color }: { data: number[]; color: string }) {
  const w = 100;
  const h = 22;
  if (data.length === 0) {
    return <svg viewBox={`0 0 ${w} ${h}`} className="w-full h-[22px]" preserveAspectRatio="none" />;
  }
  const max = 100;
  const step = data.length > 1 ? w / (data.length - 1) : w;
  const pts = data.map((v, i) => {
    const x = i * step;
    const y = h - (Math.max(0, Math.min(max, v)) / max) * h;
    return `${x.toFixed(2)},${y.toFixed(2)}`;
  });
  const linePath = `M ${pts.join(" L ")}`;
  const fillPath = `${linePath} L ${w},${h} L 0,${h} Z`;
  const id = `g-${color.replace("#", "")}`;
  return (
    <svg viewBox={`0 0 ${w} ${h}`} className="w-full h-[22px]" preserveAspectRatio="none">
      <defs>
        <linearGradient id={id} x1="0" x2="0" y1="0" y2="1">
          <stop offset="0%" stopColor={color} stopOpacity="0.4" />
          <stop offset="100%" stopColor={color} stopOpacity="0" />
        </linearGradient>
      </defs>
      <path d={fillPath} fill={`url(#${id})`} />
      <path d={linePath} fill="none" stroke={color} strokeWidth="1.2" strokeLinejoin="round" strokeLinecap="round" />
    </svg>
  );
}

function formatMB(mb: number, formatNumber: (n: number, opts?: Intl.NumberFormatOptions) => string) {
  if (mb >= 1024) {
    return `${formatNumber(mb / 1024, { minimumFractionDigits: 1, maximumFractionDigits: 1 })} GB`;
  }
  return `${formatNumber(mb, { maximumFractionDigits: 0 })} MB`;
}
