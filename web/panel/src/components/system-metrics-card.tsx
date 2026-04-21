import { useEffect, useId, useState } from "react";
import { Area, AreaChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import {
  fetchSystemHistory,
  type SystemHistoryRange,
  type SystemHistorySample,
  type SystemStatus,
} from "@/api/system";
import { Download, HardDrive, Monitor, Server } from "@/components/icons/tabler-icons";
import { cn, getErrorMessage } from "@/lib/utils";

type MetricsStatusSnapshot = Pick<
  SystemStatus,
  | "cpu_usage_percent"
  | "disk_free_bytes"
  | "disk_read_bytes"
  | "disk_read_count"
  | "disk_total_bytes"
  | "disk_used_bytes"
  | "disk_write_bytes"
  | "disk_write_count"
  | "memory_total_bytes"
  | "memory_used_bytes"
  | "network_receive_bytes"
  | "network_transmit_bytes"
>;

export type SystemStatusSample = {
  sampledAt: number;
  status: MetricsStatusSnapshot;
};

type MetricsTab = "traffic" | "disk" | "cpu" | "ram";
type MonitorRange = "realtime" | SystemHistoryRange;

type TrendSeries = {
  color: string;
  key: string;
  label: string;
  values: Array<number | null>;
};

type MetricStat = {
  detail?: string;
  label: string;
  value: string;
};

const metricsTabs: Array<{
  description: string;
  icon: typeof Download;
  label: string;
  value: MetricsTab;
}> = [
  {
    value: "traffic",
    label: "Traffic",
    description: "Network throughput",
    icon: Download,
  },
  {
    value: "disk",
    label: "Disk",
    description: "Read and write activity",
    icon: HardDrive,
  },
  {
    value: "cpu",
    label: "CPU",
    description: "Processor usage",
    icon: Monitor,
  },
  {
    value: "ram",
    label: "RAM",
    description: "Memory usage",
    icon: Server,
  },
];

const monitorRanges: Array<{
  detail: string;
  label: string;
  value: MonitorRange;
}> = [
  { value: "realtime", label: "Realtime", detail: "Live 5s" },
  { value: "1h", label: "1 hour", detail: "1-min samples" },
  { value: "6h", label: "6 hours", detail: "1-min samples" },
  { value: "1d", label: "1 day", detail: "1-min samples" },
];

const historicalRefreshIntervalMs = 60_000;

type TrendChartPoint = {
  index: number;
  sampledAt: number | null;
  [key: string]: number | string | null;
};

type TrendChartTooltipEntry = {
  color?: string;
  dataKey?: string | number;
  name?: string;
  payload?: TrendChartPoint;
  value?: number | string;
};

type AxisTickProps = {
  x?: number;
  y?: number;
  payload?: {
    value?: number | string;
  };
  range: MonitorRange;
};

function clampPercent(value: number | null) {
  if (value === null || Number.isNaN(value)) {
    return null;
  }

  return Math.max(0, Math.min(100, value));
}

function getDiskPercent(status: MetricsStatusSnapshot) {
  if (status.disk_used_bytes == null || status.disk_total_bytes == null || status.disk_total_bytes <= 0) {
    return null;
  }

  return clampPercent((status.disk_used_bytes / status.disk_total_bytes) * 100);
}

function getMemoryPercent(status: MetricsStatusSnapshot) {
  if (status.memory_used_bytes == null || status.memory_total_bytes == null || status.memory_total_bytes <= 0) {
    return null;
  }

  return clampPercent((status.memory_used_bytes / status.memory_total_bytes) * 100);
}

function formatPercent(value: number | null) {
  if (value === null) {
    return "Unavailable";
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

function formatBytesPerSecond(value: number | null) {
  if (value === null) {
    return "Unavailable";
  }

  return `${formatByteValue(value)}/s`;
}

function getMemoryFreeBytes(status: MetricsStatusSnapshot) {
  if (status.memory_total_bytes == null || status.memory_used_bytes == null) {
    return null;
  }

  return Math.max(status.memory_total_bytes - status.memory_used_bytes, 0);
}

function averageDefinedValues(values: Array<number | null>) {
  const defined = values.filter((value): value is number => value !== null);
  if (defined.length === 0) {
    return null;
  }

  return defined.reduce((sum, value) => sum + value, 0) / defined.length;
}

function maxDefinedValue(values: Array<number | null>) {
  const defined = values.filter((value): value is number => value !== null);
  if (defined.length === 0) {
    return null;
  }

  return Math.max(...defined);
}

function getLastDefinedValue(values: Array<number | null>) {
  for (let index = values.length - 1; index >= 0; index -= 1) {
    const value = values[index];
    if (value !== null) {
      return value;
    }
  }

  return null;
}

function formatHistoryWindow(history: SystemStatusSample[]) {
  if (history.length < 2) {
    return "Collecting";
  }

  const spanMs = history[history.length - 1].sampledAt - history[0].sampledAt;
  const totalSeconds = Math.max(Math.round(spanMs / 1000), 1);

  if (totalSeconds < 60) {
    return `Last ${totalSeconds}s`;
  }

  const minutes = totalSeconds / 60;
  if (minutes < 10) {
    return `Last ${minutes.toFixed(1)}m`;
  }

  if (minutes < 60) {
    return `Last ${Math.round(minutes)}m`;
  }

  const hours = minutes / 60;
  return hours >= 10 ? `Last ${Math.round(hours)}h` : `Last ${hours.toFixed(1)}h`;
}

function toTrafficRate(current?: number, previous?: number, deltaMs?: number) {
  if (current == null || previous == null || deltaMs == null || deltaMs <= 0 || current < previous) {
    return null;
  }

  return (current - previous) / (deltaMs / 1000);
}

function buildTrafficSeries(history: SystemStatusSample[]) {
  return history.map((sample, index) => {
    if (index === 0) {
      return {
        receiveRate: null,
        transmitRate: null,
      };
    }

    const previous = history[index - 1];
    const deltaMs = sample.sampledAt - previous.sampledAt;

    return {
      receiveRate: toTrafficRate(sample.status.network_receive_bytes, previous.status.network_receive_bytes, deltaMs),
      transmitRate: toTrafficRate(sample.status.network_transmit_bytes, previous.status.network_transmit_bytes, deltaMs),
    };
  });
}

function buildDiskActivitySeries(history: SystemStatusSample[]) {
  return history.map((sample, index) => {
    if (index === 0) {
      return {
        iops: null,
        readRate: null,
        writeRate: null,
      };
    }

    const previous = history[index - 1];
    const deltaMs = sample.sampledAt - previous.sampledAt;
    const readOpsRate = toTrafficRate(sample.status.disk_read_count, previous.status.disk_read_count, deltaMs);
    const writeOpsRate = toTrafficRate(sample.status.disk_write_count, previous.status.disk_write_count, deltaMs);

    return {
      iops: readOpsRate === null && writeOpsRate === null ? null : (readOpsRate ?? 0) + (writeOpsRate ?? 0),
      readRate: toTrafficRate(sample.status.disk_read_bytes, previous.status.disk_read_bytes, deltaMs),
      writeRate: toTrafficRate(sample.status.disk_write_bytes, previous.status.disk_write_bytes, deltaMs),
    };
  });
}

function formatAxisPercent(value: number | null) {
  if (value === null) {
    return "--";
  }

  return `${Math.round(value)}%`;
}

function formatTooltipTimestamp(sampledAt: number | null, range: MonitorRange) {
  if (sampledAt == null || !Number.isFinite(sampledAt)) {
    return "Sample";
  }

  const date = new Date(sampledAt);
  const options: Intl.DateTimeFormatOptions =
    range === "realtime"
      ? { hour: "2-digit", minute: "2-digit", second: "2-digit" }
      : range === "1d"
        ? { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" }
        : { hour: "2-digit", minute: "2-digit" };

  return new Intl.DateTimeFormat(undefined, options).format(date);
}

function formatXAxisTimestamp(sampledAt: number | null, range: MonitorRange) {
  if (sampledAt == null || !Number.isFinite(sampledAt)) {
    return { primary: "", secondary: "" };
  }

  const date = new Date(sampledAt);

  if (range === "1d") {
    return {
      primary: new Intl.DateTimeFormat(undefined, { month: "short", day: "numeric" }).format(date),
      secondary: new Intl.DateTimeFormat(undefined, { hour: "2-digit", minute: "2-digit" }).format(date),
    };
  }

  return {
    primary: new Intl.DateTimeFormat(undefined, { hour: "2-digit", minute: "2-digit" }).format(date),
    secondary: "",
  };
}

function formatUnavailableFallback(value: number | string | null | undefined, formatter: (value: number | null) => string) {
  if (typeof value !== "number" || Number.isNaN(value)) {
    return "Unavailable";
  }

  return formatter(value);
}

function normalizeHistoricalSamples(samples: SystemHistorySample[]): SystemStatusSample[] {
  return samples
    .map((sample) => {
      const sampledAt = Date.parse(sample.sampled_at);
      if (!Number.isFinite(sampledAt)) {
        return null;
      }

      return {
        sampledAt,
        status: {
          cpu_usage_percent: sample.cpu_usage_percent,
          disk_free_bytes: sample.disk_free_bytes,
          disk_read_bytes: sample.disk_read_bytes,
          disk_read_count: sample.disk_read_count,
          disk_total_bytes: sample.disk_total_bytes,
          disk_used_bytes: sample.disk_used_bytes,
          disk_write_bytes: sample.disk_write_bytes,
          disk_write_count: sample.disk_write_count,
          memory_total_bytes: sample.memory_total_bytes,
          memory_used_bytes: sample.memory_used_bytes,
          network_receive_bytes: sample.network_receive_bytes,
          network_transmit_bytes: sample.network_transmit_bytes,
        },
      } satisfies SystemStatusSample;
    })
    .filter((sample): sample is SystemStatusSample => sample !== null);
}

function downsampleHistory(history: SystemStatusSample[], maxPoints: number) {
  if (history.length <= maxPoints) {
    return history;
  }

  return Array.from({ length: maxPoints }, (_, index) => {
    const historyIndex = Math.round((index / (maxPoints - 1)) * (history.length - 1));
    return history[historyIndex];
  });
}

function getChartPointLimit(range: MonitorRange) {
  switch (range) {
    case "realtime":
      return 60;
    case "1h":
      return 60;
    case "6h":
      return 120;
    case "1d":
      return 180;
  }
}

function getHistorySourceDetail(range: MonitorRange) {
  return range === "realtime" ? "Updated every 5s" : "Persisted 1-minute samples";
}

function TrendChartTooltip({
  active,
  payload,
  range,
  valueFormatter,
}: {
  active?: boolean;
  payload?: TrendChartTooltipEntry[];
  range: MonitorRange;
  valueFormatter: (value: number | null) => string;
}) {
  const rows = (payload ?? []).filter((entry) => typeof entry.value === "number" && !Number.isNaN(entry.value));
  if (!active || rows.length === 0) {
    return null;
  }

  const point = rows[0]?.payload;

  return (
    <div className="min-w-36 rounded-lg border border-[var(--app-border)] bg-[var(--app-bg-2)] px-3 py-2 shadow-[0_10px_30px_rgba(15,23,42,0.12)]">
      <div className="text-[11px] font-medium text-[var(--app-text)]">{formatTooltipTimestamp(point?.sampledAt ?? null, range)}</div>
      <div className="mt-2 space-y-1.5">
        {rows.map((entry) => (
          <div key={String(entry.dataKey ?? entry.name)} className="flex items-center justify-between gap-4 text-[11px]">
            <div className="flex items-center gap-2 text-[var(--app-text-muted)]">
              <span className="h-2 w-2 rounded-full" style={{ backgroundColor: entry.color }} />
              <span>{entry.name}</span>
            </div>
            <span className="font-semibold text-[var(--app-text)]">
              {formatUnavailableFallback(entry.value, valueFormatter)}
            </span>
          </div>
        ))}
      </div>
    </div>
  );
}

function TrendChartXAxisTick({ x = 0, y = 0, payload, range }: AxisTickProps) {
  const value = typeof payload?.value === "number" ? payload.value : null;
  const { primary, secondary } = formatXAxisTimestamp(value, range);

  if (!primary) {
    return null;
  }

  return (
    <g transform={`translate(${x},${y})`}>
      <text fill="var(--app-text-muted)" fontSize="10" textAnchor="middle">
        <tspan x="0" dy="12">
          {primary}
        </tspan>
        {secondary ? (
          <tspan x="0" dy="11">
            {secondary}
          </tspan>
        ) : null}
      </text>
    </g>
  );
}

function TrendChart({
  emptyMessage,
  history,
  range,
  series,
  valueFormatter,
  yAxisFormatter,
  yAxisPercent,
}: {
  emptyMessage: string;
  history: SystemStatusSample[];
  range: MonitorRange;
  series: TrendSeries[];
  valueFormatter: (value: number | null) => string;
  yAxisFormatter: (value: number | null) => string;
  yAxisPercent?: boolean;
}) {
  const chartId = useId();
  const pointCount = Math.max(...series.map((item) => item.values.length), 0);
  const validValues = series.flatMap((item) => item.values.filter((value): value is number => value !== null));
  const hasData = pointCount >= 2 && validValues.length >= 2;
  const chartData: TrendChartPoint[] = Array.from({ length: pointCount }, (_, index) => {
    const point: TrendChartPoint = {
      index,
      sampledAt: history[index]?.sampledAt ?? null,
    };

    series.forEach((item) => {
      point[item.key] = item.values[index] ?? null;
    });

    return point;
  });

  return (
    <div className="space-y-3">
      <div
        className="relative h-48 overflow-hidden rounded-xl border border-[var(--app-border)]"
        style={{
          background:
            "linear-gradient(180deg, color-mix(in oklab, var(--app-surface-elev) 92%, var(--app-accent) 3%) 0%, var(--app-bg-2) 100%)",
        }}
      >
        {hasData ? (
          <>
            <div className="pointer-events-none absolute inset-x-0 top-0 h-20 bg-[radial-gradient(circle_at_top_left,color-mix(in_oklab,var(--app-accent)_18%,transparent),transparent_60%)] opacity-80" />
            <ResponsiveContainer width="100%" height="100%">
              <AreaChart data={chartData} margin={{ top: 14, right: 10, bottom: 28, left: 4 }}>
                <defs>
                  {series.map((item) => {
                    const gradientId = `${chartId}-${item.key}`.replace(/:/g, "");
                    return (
                      <linearGradient key={gradientId} id={gradientId} x1="0" x2="0" y1="0" y2="1">
                        <stop offset="0%" stopColor={item.color} stopOpacity={0.28} />
                        <stop offset="70%" stopColor={item.color} stopOpacity={0.08} />
                        <stop offset="100%" stopColor={item.color} stopOpacity={0.01} />
                      </linearGradient>
                    );
                  })}
                </defs>

                <CartesianGrid stroke="var(--app-border)" strokeDasharray="3 5" vertical={false} />
                <XAxis
                  axisLine={false}
                  dataKey="sampledAt"
                  interval="preserveStartEnd"
                  minTickGap={range === "1d" ? 32 : 20}
                  tick={<TrendChartXAxisTick range={range} />}
                  tickLine={false}
                  tickMargin={0}
                />
                <YAxis
                  axisLine={false}
                  domain={yAxisPercent ? [0, 100] : undefined}
                  tick={{ fill: "var(--app-text-muted)", fontSize: 10 }}
                  tickFormatter={(value: number) => yAxisFormatter(value)}
                  tickLine={false}
                  width={56}
                />
                <Tooltip
                  content={<TrendChartTooltip range={range} valueFormatter={valueFormatter} />}
                  cursor={{ stroke: "var(--app-border-strong)", strokeDasharray: "4 4" }}
                />

                {series.map((item) => {
                  const gradientId = `${chartId}-${item.key}`.replace(/:/g, "");

                  return (
                    <Area
                      key={item.key}
                      activeDot={{ fill: item.color, r: 3.5, stroke: "var(--app-bg-2)", strokeWidth: 2 }}
                      animationDuration={300}
                      connectNulls={false}
                      dataKey={item.key}
                      fill={`url(#${gradientId})`}
                      fillOpacity={1}
                      isAnimationActive={pointCount <= 180}
                      name={item.label}
                      stroke={item.color}
                      strokeLinecap="round"
                      strokeLinejoin="round"
                      strokeWidth={2.4}
                      type="monotone"
                    />
                  );
                })}
              </AreaChart>
            </ResponsiveContainer>

            <div className="pointer-events-none absolute left-3 top-3 rounded-md bg-[color-mix(in_oklab,var(--app-bg-2)_88%,transparent)] px-2 py-1 text-[10px] font-medium text-[var(--app-text-muted)]">
              {range === "1d" ? "Date + time" : "Hour"}
            </div>
          </>
        ) : (
          <div className="flex h-full items-center justify-center px-6 text-center text-[12px] text-[var(--app-text-muted)]">
            {emptyMessage}
          </div>
        )}
      </div>

      {series.length > 1 ? (
        <div className="flex flex-wrap gap-3 text-[11px] font-medium text-[var(--app-text-muted)]">
          {series.map((item) => (
            <div key={item.key} className="flex items-center gap-2">
              <span className="h-2.5 w-2.5 rounded-full shadow-[0_0_0_3px_color-mix(in_oklab,var(--app-bg-2)_78%,transparent)]" style={{ backgroundColor: item.color }} />
              <span>{item.label}</span>
            </div>
          ))}
        </div>
      ) : null}
    </div>
  );
}

function MetricStatGrid({ items }: { items: MetricStat[] }) {
  return (
    <div className="grid grid-cols-3 divide-x divide-[var(--app-border)] overflow-hidden rounded-lg border border-[var(--app-border)] bg-[var(--app-surface-muted)]">
      {items.map((item) => (
        <div key={item.label} className="min-w-0 px-3 py-2.5">
          <div className="text-[11px] font-medium text-[var(--app-text-muted)]">{item.label}</div>
          <div className="mt-1 truncate text-[15px] font-semibold tracking-tight text-[var(--app-text)]">{item.value}</div>
          {item.detail ? <div className="mt-0.5 truncate text-[11px] text-[var(--app-text-muted)]">{item.detail}</div> : null}
        </div>
      ))}
    </div>
  );
}

export function SystemMetricsCard({ history: realtimeHistory, status }: { history: SystemStatusSample[]; status: SystemStatus }) {
  const [tab, setTab] = useState<MetricsTab>("traffic");
  const [range, setRange] = useState<MonitorRange>("realtime");
  const [historicalHistory, setHistoricalHistory] = useState<Partial<Record<SystemHistoryRange, SystemStatusSample[]>>>({});
  const [historicalErrors, setHistoricalErrors] = useState<Partial<Record<SystemHistoryRange, string>>>({});
  const [loadingRange, setLoadingRange] = useState<SystemHistoryRange | null>(null);

  useEffect(() => {
    if (range === "realtime") {
      return;
    }

    let active = true;

    async function loadHistoricalRange(nextRange: SystemHistoryRange) {
      setLoadingRange(nextRange);
      setHistoricalErrors((current) => ({ ...current, [nextRange]: null }));

      try {
        const samples = normalizeHistoricalSamples(await fetchSystemHistory(nextRange));
        if (!active) {
          return;
        }

        setHistoricalHistory((current) => ({ ...current, [nextRange]: samples }));
      } catch (error) {
        if (!active) {
          return;
        }

        const message = getErrorMessage(error, "Failed to load system history.");
        setHistoricalErrors((current) => ({ ...current, [nextRange]: message }));
      } finally {
        if (!active) {
          return;
        }

        setLoadingRange((current) => (current === nextRange ? null : current));
      }
    }

    void loadHistoricalRange(range);

    const intervalId = window.setInterval(() => {
      void loadHistoricalRange(range);
    }, historicalRefreshIntervalMs);

    return () => {
      active = false;
      window.clearInterval(intervalId);
    };
  }, [range]);

  const historicalRange = range === "realtime" ? null : range;
  const selectedHistory = range === "realtime" ? realtimeHistory : historicalHistory[range] ?? [];
  const chartHistory = downsampleHistory(selectedHistory, getChartPointLimit(range));
  const displayStatus = selectedHistory[selectedHistory.length - 1]?.status ?? status;
  const cpuValues = chartHistory.map((sample) => clampPercent(sample.status.cpu_usage_percent ?? null));
  const memoryValues = chartHistory.map((sample) => getMemoryPercent(sample.status));
  const trafficSeries = buildTrafficSeries(chartHistory);
  const diskActivitySeries = buildDiskActivitySeries(chartHistory);
  const diskReadRates = diskActivitySeries.map((sample) => sample.readRate);
  const diskWriteRates = diskActivitySeries.map((sample) => sample.writeRate);
  const diskIopsValues = diskActivitySeries.map((sample) => sample.iops);
  const receiveRates = trafficSeries.map((sample) => sample.receiveRate);
  const transmitRates = trafficSeries.map((sample) => sample.transmitRate);
  const activeTab = metricsTabs.find((item) => item.value === tab) ?? metricsTabs[0];
  const activeRange = monitorRanges.find((item) => item.value === range) ?? monitorRanges[0];
  const loading = historicalRange !== null && loadingRange === historicalRange;
  const error = historicalRange ? historicalErrors[historicalRange] ?? null : null;

  let chart = (
    <TrendChart
      emptyMessage={
        error ??
        (loading
          ? `Loading ${activeRange.label.toLowerCase()} history...`
          : range === "realtime"
          ? "Waiting for enough live network samples to draw traffic."
          : `No ${activeRange.label.toLowerCase()} traffic history yet.`)
      }
      history={chartHistory}
      range={range}
      series={[
        { color: "var(--app-accent)", key: "download", label: "Download", values: receiveRates },
        { color: "var(--app-warning)", key: "upload", label: "Upload", values: transmitRates },
      ]}
      valueFormatter={formatBytesPerSecond}
      yAxisFormatter={formatBytesPerSecond}
    />
  );
  let stats: MetricStat[] = [
    {
      label: "Download",
      value: formatBytesPerSecond(getLastDefinedValue(receiveRates)),
      detail: `Peak ${formatBytesPerSecond(maxDefinedValue(receiveRates))}`,
    },
    {
      label: "Upload",
      value: formatBytesPerSecond(getLastDefinedValue(transmitRates)),
      detail: `Peak ${formatBytesPerSecond(maxDefinedValue(transmitRates))}`,
    },
    {
      label: "Window",
      value: formatHistoryWindow(selectedHistory),
      detail: getHistorySourceDetail(range),
    },
  ];

  if (tab === "disk") {
    chart = (
      <TrendChart
        emptyMessage={
          error ??
          (loading
            ? `Loading ${activeRange.label.toLowerCase()} history...`
            : range === "realtime"
              ? "Waiting for enough live disk samples to draw activity."
              : `No ${activeRange.label.toLowerCase()} disk activity history yet.`)
        }
        history={chartHistory}
        range={range}
        series={[
          { color: "var(--app-accent)", key: "read", label: "Read", values: diskReadRates },
          { color: "var(--app-warning)", key: "write", label: "Write", values: diskWriteRates },
        ]}
        valueFormatter={formatBytesPerSecond}
        yAxisFormatter={formatBytesPerSecond}
      />
    );
    stats = [
      {
        label: "Read",
        value: formatBytesPerSecond(getLastDefinedValue(diskReadRates)),
        detail: `Peak ${formatBytesPerSecond(maxDefinedValue(diskReadRates))}`,
      },
      {
        label: "Write",
        value: formatBytesPerSecond(getLastDefinedValue(diskWriteRates)),
        detail: `Peak ${formatBytesPerSecond(maxDefinedValue(diskWriteRates))}`,
      },
      {
        label: "Activity",
        value: (() => {
          const currentIops = getLastDefinedValue(diskIopsValues);
          return currentIops === null ? "Unavailable" : `${Math.round(currentIops)} IOPS`;
        })(),
        detail: `Fullness ${formatPercent(getDiskPercent(displayStatus))}`,
      },
    ];
  }

  if (tab === "cpu") {
    chart = (
      <TrendChart
        emptyMessage={
          error ??
          (loading
            ? `Loading ${activeRange.label.toLowerCase()} history...`
            : range === "realtime"
              ? "Waiting for enough live CPU samples to draw usage."
              : `No ${activeRange.label.toLowerCase()} CPU history yet.`)
        }
        history={chartHistory}
        range={range}
        series={[{ color: "var(--app-accent)", key: "cpu", label: "CPU usage", values: cpuValues }]}
        valueFormatter={formatPercent}
        yAxisFormatter={formatAxisPercent}
        yAxisPercent
      />
    );
    stats = [
      {
        label: "Current",
        value: formatPercent(clampPercent(displayStatus.cpu_usage_percent ?? null)),
      },
      {
        label: "Average",
        value: formatPercent(averageDefinedValues(cpuValues)),
      },
      {
        label: "Peak",
        value: formatPercent(maxDefinedValue(cpuValues)),
      },
    ];
  }

  if (tab === "ram") {
    chart = (
      <TrendChart
        emptyMessage={
          error ??
          (loading
            ? `Loading ${activeRange.label.toLowerCase()} history...`
            : range === "realtime"
              ? "Waiting for enough live RAM samples to draw usage."
              : `No ${activeRange.label.toLowerCase()} RAM history yet.`)
        }
        history={chartHistory}
        range={range}
        series={[{ color: "var(--app-warning)", key: "ram", label: "RAM usage", values: memoryValues }]}
        valueFormatter={formatPercent}
        yAxisFormatter={formatAxisPercent}
        yAxisPercent
      />
    );
    stats = [
      {
        label: "Usage",
        value: formatPercent(getMemoryPercent(displayStatus)),
        detail: `Total ${formatByteValue(displayStatus.memory_total_bytes)}`,
      },
      {
        label: "Used",
        value: formatByteValue(displayStatus.memory_used_bytes),
      },
      {
        label: "Free",
        value: formatByteValue(getMemoryFreeBytes(displayStatus)),
      },
    ];
  }

  return (
    <section className="rounded-xl border border-[var(--app-border)] bg-[var(--app-bg-2)] px-4 py-4 shadow-[var(--app-shadow)]">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="text-[15px] font-semibold tracking-tight text-[var(--app-text)]">System monitor</div>
          <div className="mt-1 text-[12px] text-[var(--app-text-muted)]">{activeTab.description}</div>
        </div>
        <div className="shrink-0 text-right">
          <div className="text-[11px] font-medium text-[var(--app-text)]">{activeRange.label}</div>
          <div className="mt-0.5 text-[11px] text-[var(--app-text-muted)]">{activeRange.detail}</div>
        </div>
      </div>

      <div className="mt-3 space-y-2">
        <div role="tablist" aria-label="System metric tabs">
          <div className="inline-flex rounded-lg border border-[var(--app-border)] bg-[var(--app-surface-muted)] p-1">
            {metricsTabs.map((item) => {
              const active = item.value === tab;
              const Icon = item.icon;

              return (
                <button
                  key={item.value}
                  role="tab"
                  type="button"
                  aria-selected={active}
                  tabIndex={active ? 0 : -1}
                  className={cn(
                    "inline-flex h-8 items-center gap-1.5 rounded-md px-3 text-[12px] font-medium transition-colors duration-150 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--app-accent)] focus-visible:ring-offset-2 focus-visible:ring-offset-[var(--app-bg-2)]",
                    active
                      ? "bg-[var(--app-bg-2)] text-[var(--app-text)] shadow-sm"
                      : "text-[var(--app-text-muted)] hover:text-[var(--app-text)]",
                  )}
                  onClick={() => {
                    setTab(item.value);
                  }}
                >
                  <Icon className="h-3.5 w-3.5" stroke={1.8} />
                  <span>{item.label}</span>
                </button>
              );
            })}
          </div>
        </div>

        <div role="tablist" aria-label="System monitor ranges">
          <div className="inline-flex rounded-lg border border-[var(--app-border)] bg-[var(--app-surface-muted)] p-1">
            {monitorRanges.map((item) => {
              const active = item.value === range;

              return (
                <button
                  key={item.value}
                  role="tab"
                  type="button"
                  aria-selected={active}
                  tabIndex={active ? 0 : -1}
                  className={cn(
                    "inline-flex h-8 items-center rounded-md px-3 text-[12px] font-medium transition-colors duration-150 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--app-accent)] focus-visible:ring-offset-2 focus-visible:ring-offset-[var(--app-bg-2)]",
                    active
                      ? "bg-[var(--app-bg-2)] text-[var(--app-text)] shadow-sm"
                      : "text-[var(--app-text-muted)] hover:text-[var(--app-text)]",
                  )}
                  onClick={() => {
                    setRange(item.value);
                  }}
                >
                  <span>{item.label}</span>
                </button>
              );
            })}
          </div>
        </div>
      </div>

      <div className="mt-4 space-y-4">
        {chart}
        <MetricStatGrid items={stats} />
      </div>
    </section>
  );
}
