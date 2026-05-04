import type { ReactNode } from "react";
import type { SystemStatus } from "@/api/system";
import {
  ChevronDownIcon,
  Check,
  Cpu,
  Database,
  Download,
  HardDrive,
  Info,
  MemoryStick,
  Upload,
  Monitor,
  Server,
  TimerReset,
  World,
} from "@/components/icons/lucide-icons";
import { DiskUsageCard } from "@/components/disk-usage-card";
import type { SystemStatusSample } from "@/components/system-metrics-card";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";

type StatusTone = "cpu" | "memory" | "disk" | "network";

export type OperationalHealth = {
  backup: boolean | null;
  database: boolean | null;
  runtime: boolean | null;
  webServer: boolean | null;
};

type StatusTileProps = {
  detail?: ReactNode;
  icon: ReactNode;
  label: string;
  tone: StatusTone;
  value: string;
  values: Array<number | null>;
};

const statusHistoryLimit = 24;
const sparklineWidth = 150;
const sparklineHeight = 36;
const networkBarCount = 22;

function clampPercent(value: number | null | undefined) {
  if (value == null || Number.isNaN(value)) {
    return null;
  }

  return Math.max(0, Math.min(100, value));
}

function formatPercent(value: number | null) {
  if (value === null) {
    return "--";
  }

  return value >= 10 ? `${Math.round(value)}%` : `${value.toFixed(1)}%`;
}

function formatByteValue(value?: number | null) {
  if (value == null || value < 0) {
    return "Unavailable";
  }

  const units = ["B", "KB", "MB", "GB", "TB", "PB"];
  const exponent = Math.min(Math.floor(Math.log(Math.max(value, 1)) / Math.log(1024)), units.length - 1);
  const size = value / 1024 ** exponent;
  const digits = exponent === 0 || size >= 100 ? 0 : size >= 10 ? 1 : 2;

  return `${size.toFixed(digits)} ${units[exponent]}`;
}

function formatRate(value: number | null) {
  return value === null ? "--" : `${formatByteValue(value)}/s`;
}

function getMemoryPercent(status: Pick<SystemStatus, "memory_total_bytes" | "memory_used_bytes">) {
  if (status.memory_used_bytes == null || status.memory_total_bytes == null || status.memory_total_bytes <= 0) {
    return null;
  }

  return clampPercent((status.memory_used_bytes / status.memory_total_bytes) * 100);
}

function getDiskPercent(status: Pick<SystemStatus, "disk_total_bytes" | "disk_used_bytes">) {
  if (status.disk_used_bytes == null || status.disk_total_bytes == null || status.disk_total_bytes <= 0) {
    return null;
  }

  return clampPercent((status.disk_used_bytes / status.disk_total_bytes) * 100);
}

function toRate(current?: number, previous?: number, deltaMs?: number) {
  if (current == null || previous == null || deltaMs == null || deltaMs <= 0 || current < previous) {
    return null;
  }

  return (current - previous) / (deltaMs / 1000);
}

function latestNetworkRates(history: SystemStatusSample[]) {
  const current = history.at(-1);
  const previous = history.at(-2);
  const deltaMs = current && previous ? current.sampledAt - previous.sampledAt : undefined;

  return {
    receive: toRate(current?.status.network_receive_bytes, previous?.status.network_receive_bytes, deltaMs),
    transmit: toRate(current?.status.network_transmit_bytes, previous?.status.network_transmit_bytes, deltaMs),
  };
}

function buildPercentSeries(history: SystemStatusSample[], status: SystemStatus, getValue: (status: SystemStatusSample["status"] | SystemStatus) => number | null) {
  const values = history.slice(-statusHistoryLimit).map((sample) => getValue(sample.status));
  return values.length > 0 ? values : [getValue(status)];
}

function buildNetworkSeries(history: SystemStatusSample[]) {
  return history.slice(-statusHistoryLimit).map((sample, index, samples) => {
    if (index === 0) {
      return null;
    }

    const previous = samples[index - 1];
    const deltaMs = sample.sampledAt - previous.sampledAt;
    const receive = toRate(sample.status.network_receive_bytes, previous.status.network_receive_bytes, deltaMs) ?? 0;
    const transmit = toRate(sample.status.network_transmit_bytes, previous.status.network_transmit_bytes, deltaMs) ?? 0;

    return receive + transmit;
  });
}

function getToneClassName(tone: StatusTone) {
  switch (tone) {
    case "cpu":
      return "text-teal-400 [--tile-accent:#2dd4bf]";
    case "memory":
      return "text-sky-400 [--tile-accent:#38bdf8]";
    case "disk":
      return "text-green-400 [--tile-accent:#4ade80]";
    case "network":
      return "text-amber-400 [--tile-accent:#fbbf24]";
  }
}

function getSparklinePath(values: Array<number | null>) {
  const definedValues = values.filter((value): value is number => value !== null);
  if (definedValues.length === 0) {
    return "";
  }

  const max = Math.max(...definedValues, 1);
  const min = Math.min(...definedValues, 0);
  const range = Math.max(max - min, 1);
  const lastDefined = definedValues[0];
  let current = lastDefined;

  return values
    .map((value, index) => {
      current = value ?? current;
      const x = values.length === 1 ? sparklineWidth : (index / (values.length - 1)) * sparklineWidth;
      const y = sparklineHeight - ((current - min) / range) * (sparklineHeight - 8) - 4;
      return `${index === 0 ? "M" : "L"} ${x.toFixed(1)} ${y.toFixed(1)}`;
    })
    .join(" ");
}

function Sparkline({ values }: { values: Array<number | null> }) {
  const path = getSparklinePath(values);

  return (
    <svg aria-hidden="true" className="h-9 w-full overflow-visible" preserveAspectRatio="none" viewBox={`0 0 ${sparklineWidth} ${sparklineHeight}`}>
      {path ? (
        <>
          <path d={`${path} L ${sparklineWidth} ${sparklineHeight} L 0 ${sparklineHeight} Z`} fill="color-mix(in oklab, var(--tile-accent) 18%, transparent)" />
          <path d={path} fill="none" stroke="var(--tile-accent)" strokeLinecap="round" strokeLinejoin="round" strokeWidth="2.5" />
        </>
      ) : (
        <path d={`M 0 ${sparklineHeight - 5} L ${sparklineWidth} ${sparklineHeight - 5}`} fill="none" stroke="var(--app-border-strong)" strokeLinecap="round" strokeWidth="2" />
      )}
    </svg>
  );
}

function NetworkBars({ values }: { values: Array<number | null> }) {
  const normalized = values.slice(-networkBarCount);
  const max = Math.max(...normalized.map((value) => value ?? 0), 1);

  return (
    <div aria-hidden="true" className="flex h-9 items-end gap-1">
      {Array.from({ length: networkBarCount }).map((_, index) => {
        const value = normalized[index - Math.max(networkBarCount - normalized.length, 0)] ?? 0;
        const height = Math.max(5, (value / max) * 32);

        return (
          <div
            key={index}
            className="w-full rounded-t-[2px] bg-[var(--tile-accent)]"
            style={{ height }}
          />
        );
      })}
    </div>
  );
}

function StatusTile({ detail, icon, label, tone, value, values }: StatusTileProps) {
  const isNetwork = tone === "network";

  return (
    <section className={`rounded-lg border border-[var(--app-border)] bg-[var(--app-bg-2)] px-3 py-2.5 shadow-[var(--app-shadow)] ${getToneClassName(tone)}`}>
      <div className="flex items-center gap-2 text-[13px] font-semibold text-[var(--app-text)]">
        <span className="text-[var(--tile-accent)]">{icon}</span>
        <span>{label}</span>
      </div>
      <div className="mt-2.5 text-[24px] font-semibold leading-none tracking-tight text-[var(--app-text)]">{value}</div>
      <div className="mt-2.5 min-h-9">{isNetwork ? <NetworkBars values={values} /> : <Sparkline values={values} />}</div>
      {detail ? <div className="mt-2 text-[12px] leading-4 text-[var(--app-text-muted)]">{detail}</div> : null}
    </section>
  );
}

function ResourceUsageCard({
  cpuPercent,
  diskPercent,
  memoryPercent,
}: {
  cpuPercent: number | null;
  diskPercent: number | null;
  memoryPercent: number | null;
}) {
  const rows = [
    { icon: <Cpu className="h-4 w-4" />, label: "CPU", percent: cpuPercent, tone: "cpu" as const },
    { icon: <MemoryStick className="h-4 w-4" />, label: "Memory", percent: memoryPercent, tone: "memory" as const },
    { icon: <HardDrive className="h-4 w-4" />, label: "Disk", percent: diskPercent, tone: "disk" as const },
  ];

  return (
    <section className="rounded-lg border border-[var(--app-border)] bg-[var(--app-bg-2)] px-3 py-2.5 shadow-[var(--app-shadow)]">
      <div className="flex items-center justify-between gap-3">
        <div className="text-[13px] font-semibold text-[var(--app-text)]">Resource Usage</div>
        <button
          type="button"
          className="flex h-7 items-center gap-1 rounded-md bg-[var(--app-surface-muted)] px-2 text-[12px] font-semibold text-[var(--app-text)]"
        >
          24h
          <ChevronDownIcon className="h-3.5 w-3.5 text-[var(--app-text-muted)]" />
        </button>
      </div>
      <div className="mt-3 space-y-3.5">
        {rows.map((row) => (
          <div key={row.label} className={`${getToneClassName(row.tone)} grid grid-cols-[84px_minmax(0,1fr)_40px] items-center gap-3`}>
            <div className="flex min-w-0 items-center gap-2 text-[13px] font-semibold text-[var(--app-text)]">
              <span className="text-[var(--tile-accent)]">{row.icon}</span>
              <span className="truncate">{row.label}</span>
            </div>
            <div className="h-2.5 overflow-hidden rounded-full bg-[var(--app-surface)]">
              <div
                className="h-full rounded-full bg-[var(--tile-accent)] transition-[width] duration-200"
                style={{ width: `${row.percent ?? 0}%` }}
              />
            </div>
            <div className="text-right text-[13px] font-semibold text-[var(--app-text)]">{formatPercent(row.percent)}</div>
          </div>
        ))}
      </div>
    </section>
  );
}

function SystemOperationalCard({
  health,
  history,
  status,
}: {
  health: OperationalHealth;
  history: SystemStatusSample[];
  status: SystemStatus;
}) {
  const items = [
    {
      fix: "Open Applications > Caddy, review the config state, then start or restart Caddy.",
      icon: <Server className="h-4 w-4" />,
      label: "Web Server",
      problem: "Caddy is stopped or its configuration is not loaded.",
      ready: health.webServer,
    },
    {
      fix: "Open Applications > MariaDB and start or restart the database service.",
      icon: <Database className="h-4 w-4" />,
      label: "Database",
      problem: "MariaDB is not ready.",
      ready: health.database,
    },
    {
      fix: "Open Applications > PHP and install, start, or restart PHP-FPM.",
      icon: <Cpu className="h-4 w-4" />,
      label: "Runtime",
      problem: "The PHP runtime is not ready.",
      ready: health.runtime,
    },
    {
      fix: "Open Backups and enable scheduled backups, then start the backup scheduler.",
      icon: <TimerReset className="h-4 w-4" />,
      label: "Backup",
      problem: "Scheduled backups are disabled or the scheduler is stopped.",
      ready: health.backup,
    },
    {
      fix: "Wait for the next system sample. If it stays empty, check that FlowPanel is running normally.",
      icon: <Monitor className="h-4 w-4" />,
      label: "Monitoring",
      problem: "No system monitoring samples have been recorded yet.",
      ready: history.length > 0,
    },
  ];
  const known = items.filter((item) => item.ready !== null);
  const issueItems = known.filter((item) => item.ready === false);
  const allReady = known.length === items.length && known.every((item) => item.ready);
  const hasIssue = issueItems.length > 0;
  const headline = allReady
    ? "All systems operational"
    : hasIssue
      ? "Action needed"
      : "Status partially unavailable";
  const series = buildPercentSeries(history, status, (sample) => clampPercent(sample.cpu_usage_percent));

  return (
    <section className="overflow-hidden rounded-lg border border-[var(--app-border)] bg-[var(--app-bg-2)] px-3 py-2.5 shadow-[var(--app-shadow)]">
      <div className="text-[13px] font-semibold text-[var(--app-text)]">System Status</div>
      <div className="mt-2 flex items-center gap-2 text-[12px] font-medium text-[var(--app-text)]">
        <span className={`flex h-5 w-5 items-center justify-center rounded-full ${allReady ? "bg-green-500 text-white" : hasIssue ? "bg-[var(--app-danger)] text-white" : "bg-[var(--app-surface-muted)] text-[var(--app-text-muted)]"}`}>
          <Check className="h-3.5 w-3.5" />
        </span>
        <span>{headline}</span>
        {hasIssue ? (
          <Tooltip>
            <TooltipTrigger asChild>
              <button
                type="button"
                aria-label="Show system status problems and fixes"
                className="inline-flex h-5 w-5 items-center justify-center rounded-full text-[var(--app-text-muted)] transition hover:bg-[var(--app-surface-muted)] hover:text-[var(--app-text)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--app-ring)]"
              >
                <Info className="h-3.5 w-3.5" />
              </button>
            </TooltipTrigger>
            <TooltipContent sideOffset={6} className="max-w-80 text-left">
              <div className="space-y-2">
                {issueItems.map((item) => (
                  <div key={item.label}>
                    <div className="font-semibold">{item.label}</div>
                    <div className="mt-0.5 opacity-90">{item.problem}</div>
                    <div className="mt-0.5 opacity-80">Fix: {item.fix}</div>
                  </div>
                ))}
              </div>
            </TooltipContent>
          </Tooltip>
        ) : null}
      </div>
      <div className="mt-3 divide-y divide-[var(--app-border)] border-t border-[var(--app-border)]">
        {items.map((item) => (
          <div key={item.label} className="flex items-center gap-2 py-2 text-[12px] font-semibold text-[var(--app-text)]">
            <span className="text-[var(--app-text-muted)]">{item.icon}</span>
            <span className="min-w-0 flex-1 truncate">{item.label}</span>
            <span
              className={`h-2.5 w-2.5 rounded-full ${
                item.ready === null
                  ? "bg-[var(--app-text-muted)] opacity-60"
                  : item.ready
                    ? "bg-green-400 shadow-[0_0_10px_rgba(74,222,128,0.55)]"
                    : "bg-[var(--app-danger)]"
              }`}
            />
          </div>
        ))}
      </div>
      <div className="mt-2 [--tile-accent:#4ade80]">
        <Sparkline values={series} />
      </div>
    </section>
  );
}

function formatTotal(value: number | null) {
  return value === null ? "--" : String(value);
}

function TotalsCard({
  databaseCount,
  siteCount,
}: {
  databaseCount: number | null;
  siteCount: number | null;
}) {
  const rows = [
    { icon: <World className="h-4 w-4" />, label: "Total sites", value: formatTotal(siteCount) },
    { icon: <Database className="h-4 w-4" />, label: "Total databases", value: formatTotal(databaseCount) },
  ];

  return (
    <section className="rounded-lg border border-[var(--app-border)] bg-[var(--app-bg-2)] px-3 py-2.5 shadow-[var(--app-shadow)]">
      <div className="text-[13px] font-semibold text-[var(--app-text)]">Totals</div>
      <div className="mt-2 divide-y divide-[var(--app-border)] border-t border-[var(--app-border)]">
        {rows.map((row) => (
          <div key={row.label} className="flex items-center gap-2 py-2 text-[12px] text-[var(--app-text)]">
            <span className="text-[var(--app-text-muted)]">{row.icon}</span>
            <span className="min-w-0 flex-1 truncate">{row.label}</span>
            <span className="text-[18px] font-semibold leading-none tracking-tight text-[var(--app-text)]">{row.value}</span>
          </div>
        ))}
      </div>
    </section>
  );
}

export function SystemStatusCard({
  databaseCount,
  health,
  history,
  leftContent,
  siteCount,
  status,
}: {
  databaseCount: number | null;
  health: OperationalHealth;
  history: SystemStatusSample[];
  leftContent?: ReactNode;
  siteCount: number | null;
  status: SystemStatus;
}) {
  const cpuPercent = clampPercent(status.cpu_usage_percent);
  const memoryPercent = getMemoryPercent(status);
  const diskPercent = getDiskPercent(status);
  const networkRates = latestNetworkRates(history);
  const networkSeries = buildNetworkSeries(history);
  const networkTotalRate =
    networkRates.receive === null && networkRates.transmit === null
      ? null
      : (networkRates.receive ?? 0) + (networkRates.transmit ?? 0);

  return (
    <div className="grid gap-3 xl:grid-cols-[minmax(0,1fr)_minmax(260px,320px)]">
      <div className="min-w-0 space-y-5">
        <div className="grid gap-2.5 sm:grid-cols-2 2xl:grid-cols-4">
          <StatusTile
            icon={<Cpu className="h-4 w-4" />}
            label="CPU"
            tone="cpu"
            value={formatPercent(cpuPercent)}
            values={buildPercentSeries(history, status, (sample) => clampPercent(sample.cpu_usage_percent))}
          />
          <StatusTile
            icon={<MemoryStick className="h-4 w-4" />}
            label="Memory"
            tone="memory"
            value={formatPercent(memoryPercent)}
            values={buildPercentSeries(history, status, getMemoryPercent)}
          />
          <StatusTile
            icon={<HardDrive className="h-4 w-4" />}
            label="Disk"
            tone="disk"
            value={formatPercent(diskPercent)}
            values={buildPercentSeries(history, status, getDiskPercent)}
          />
          <StatusTile
            detail={
              <div className="grid grid-cols-2 gap-2">
                <span className="flex min-w-0 items-center gap-1">
                  <Download className="h-3.5 w-3.5 shrink-0" />
                  <span className="truncate">{formatRate(networkRates.receive)}</span>
                </span>
                <span className="flex min-w-0 items-center gap-1">
                  <Upload className="h-3.5 w-3.5 shrink-0" />
                  <span className="truncate">{formatRate(networkRates.transmit)}</span>
                </span>
              </div>
            }
            icon={<Upload className="h-4 w-4" />}
            label="Network"
            tone="network"
            value={formatRate(networkTotalRate)}
            values={networkSeries.length > 1 ? networkSeries : [null]}
          />
        </div>
        {leftContent}
      </div>
      <div className="space-y-2.5">
        <SystemOperationalCard health={health} history={history} status={status} />
        <ResourceUsageCard cpuPercent={cpuPercent} diskPercent={diskPercent} memoryPercent={memoryPercent} />
        <DiskUsageCard status={status} />
        <TotalsCard databaseCount={databaseCount} siteCount={siteCount} />
      </div>
    </div>
  );
}
