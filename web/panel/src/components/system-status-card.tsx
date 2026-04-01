import type { SystemStatus } from "@/api/system";

const gaugeSize = 144;
const gaugeRadius = 52;
const gaugeStroke = 10;
const gaugeCircumference = 2 * Math.PI * gaugeRadius;

type GaugeTone = {
  stroke: string;
  text: string;
};

type GaugePanelProps = {
  caption: string;
  detail: string;
  percent: number | null;
  title: string;
  value: string;
};

function clampPercent(value: number | null) {
  if (value === null || Number.isNaN(value)) {
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

function formatLoadSeries(status: SystemStatus) {
  if (status.load_1 == null || status.load_5 == null || status.load_15 == null) {
    return "Load average unavailable";
  }

  return `${status.load_1.toFixed(2)} / ${status.load_5.toFixed(2)} / ${status.load_15.toFixed(2)}`;
}

function formatMemoryLine(status: SystemStatus) {
  if (status.memory_used_bytes == null || status.memory_total_bytes == null) {
    return "Memory unavailable";
  }

  return `${formatMegabytes(status.memory_used_bytes)} / ${formatMegabytes(status.memory_total_bytes)} MB`;
}

function formatMegabytes(bytes: number) {
  return Math.round(bytes / (1024 * 1024)).toString();
}

function getLoadPercent(status: SystemStatus) {
  if (status.load_1 == null || status.cores <= 0) {
    return null;
  }

  return clampPercent((status.load_1 / status.cores) * 100);
}

function getMemoryPercent(status: SystemStatus) {
  if (
    status.memory_used_bytes == null ||
    status.memory_total_bytes == null ||
    status.memory_total_bytes <= 0
  ) {
    return null;
  }

  return clampPercent((status.memory_used_bytes / status.memory_total_bytes) * 100);
}

function getLoadTitle(percent: number | null) {
  if (percent === null) {
    return "System load";
  }

  if (percent < 45) {
    return "Smooth operation";
  }

  if (percent < 80) {
    return "Steady load";
  }

  return "High load";
}

function getGaugeTone(percent: number | null): GaugeTone {
  if (percent === null) {
    return {
      stroke: "var(--app-border-strong)",
      text: "var(--app-text-muted)",
    };
  }

  if (percent < 70) {
    return {
      stroke: "var(--app-ok)",
      text: "var(--app-text)",
    };
  }

  if (percent < 90) {
    return {
      stroke: "var(--app-warning)",
      text: "var(--app-text)",
    };
  }

  return {
    stroke: "var(--app-danger)",
    text: "var(--app-text)",
  };
}

function GaugePanel({ caption, detail, percent, title, value }: GaugePanelProps) {
  const normalized = clampPercent(percent);
  const tone = getGaugeTone(normalized);
  const progressLength =
    normalized === null
      ? gaugeCircumference
      : gaugeCircumference - (normalized / 100) * gaugeCircumference;

  return (
    <div className="rounded-xl border border-[var(--app-border)] bg-[var(--app-surface-muted)] px-4 py-5">
      <div className="flex flex-col items-center gap-4 text-center">
        <div className="relative h-36 w-36">
          <svg
            aria-hidden="true"
            className="h-full w-full -rotate-90"
            viewBox={`0 0 ${gaugeSize} ${gaugeSize}`}
          >
            <circle
              cx={gaugeSize / 2}
              cy={gaugeSize / 2}
              r={gaugeRadius}
              fill="none"
              stroke="var(--app-border)"
              strokeWidth={gaugeStroke}
            />
            <circle
              cx={gaugeSize / 2}
              cy={gaugeSize / 2}
              r={gaugeRadius}
              fill="none"
              stroke={tone.stroke}
              strokeDasharray={gaugeCircumference}
              strokeDashoffset={progressLength}
              strokeLinecap="round"
              strokeWidth={gaugeStroke}
            />
          </svg>
          <div
            className="absolute inset-0 flex items-center justify-center text-3xl font-semibold tracking-tight"
            style={{ color: tone.text }}
          >
            {value}
          </div>
        </div>

        <div className="space-y-1.5">
          <div className="text-[15px] font-semibold leading-5 text-[var(--app-text)]">{title}</div>
          <div className="text-[13px] leading-5 text-[var(--app-text)]">{detail}</div>
          {caption ? (
            <div className="text-[12px] text-[var(--app-text-muted)]">{caption}</div>
          ) : null}
        </div>
      </div>
    </div>
  );
}

export function SystemStatusCard({ status }: { status: SystemStatus }) {
  const loadPercent = getLoadPercent(status);
  const memoryPercent = getMemoryPercent(status);
  const cpuPercent = clampPercent(status.cpu_usage_percent ?? null);

  return (
    <section className="rounded-2xl border border-[var(--app-border)] bg-[var(--app-bg-2)] px-5 py-5 shadow-[var(--app-shadow)]">
      <div className="space-y-4">
        <h2 className="text-[15px] font-semibold tracking-tight text-[var(--app-text)]">System status</h2>
        <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
          <GaugePanel
            caption="Load average"
            detail={formatLoadSeries(status)}
            percent={loadPercent}
            title={getLoadTitle(loadPercent)}
            value={formatPercent(loadPercent)}
          />
          <GaugePanel
            caption=""
            detail="CPU usage"
            percent={cpuPercent}
            title={`${status.cores} ${status.cores === 1 ? "core" : "cores"}`}
            value={formatPercent(cpuPercent)}
          />
          <GaugePanel
            caption=""
            detail="RAM usage"
            percent={memoryPercent}
            title={formatMemoryLine(status)}
            value={formatPercent(memoryPercent)}
          />
        </div>
      </div>
    </section>
  );
}
