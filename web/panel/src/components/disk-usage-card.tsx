import type { SystemStatus } from "@/api/system";
import { HardDrive } from "@/components/icons/tabler-icons";
import { formatBytes } from "@/lib/format";

type DiskTone = {
  bar: string;
  label: string;
  surface: string;
  title: string;
};

function clampPercent(value: number | null) {
  if (value === null || Number.isNaN(value)) {
    return null;
  }

  return Math.max(0, Math.min(100, value));
}

function getDiskPercent(status: SystemStatus) {
  if (status.disk_used_bytes == null || status.disk_total_bytes == null || status.disk_total_bytes <= 0) {
    return null;
  }

  return clampPercent((status.disk_used_bytes / status.disk_total_bytes) * 100);
}

function formatPercent(value: number | null) {
  if (value === null) {
    return "--";
  }

  return value >= 10 ? `${Math.round(value)}%` : `${value.toFixed(1)}%`;
}

function formatFreePercent(value: number | null) {
  if (value === null) {
    return "Free space unavailable";
  }

  const freePercent = clampPercent(100 - value);
  return `${formatPercent(freePercent)} free`;
}

function formatDiskValue(value?: number) {
  if (value == null) {
    return "Unavailable";
  }

  return formatBytes(value);
}

function getDiskTone(percent: number | null): DiskTone {
  if (percent === null) {
    return {
      bar: "var(--app-border-strong)",
      label: "Metrics unavailable",
      surface: "var(--app-surface)",
      title: "Disk capacity",
    };
  }

  if (percent < 70) {
    return {
      bar: "var(--app-ok)",
      label: "Healthy headroom",
      surface: "var(--app-ok-soft)",
      title: "Disk capacity",
    };
  }

  if (percent < 85) {
    return {
      bar: "var(--app-warning)",
      label: "Usage climbing",
      surface: "var(--app-warning-soft)",
      title: "Disk capacity",
    };
  }

  return {
    bar: "var(--app-danger)",
    label: percent >= 92 ? "Space is critical" : "Low free space",
    surface: "var(--app-danger-soft)",
    title: "Disk capacity",
  };
}

function MetricCard({
  label,
  value,
  mono = false,
}: {
  label: string;
  value: string;
  mono?: boolean;
}) {
  return (
    <div className="rounded-lg border border-[var(--app-border)] bg-[var(--app-surface)] px-4 py-3">
      <div className="text-[12px] text-[var(--app-text-muted)]">{label}</div>
      <div
        className={
          mono
            ? "mt-1 font-mono text-[12px] text-[var(--app-text)]"
            : "mt-1 text-[14px] font-semibold tracking-tight text-[var(--app-text)]"
        }
      >
        {value}
      </div>
    </div>
  );
}

export function DiskUsageCard({ status }: { status: SystemStatus }) {
  const diskPercent = getDiskPercent(status);
  const diskTone = getDiskTone(diskPercent);
  const diskUsed = formatDiskValue(status.disk_used_bytes);
  const diskFree = formatDiskValue(status.disk_free_bytes);
  const diskCapacity = formatDiskValue(status.disk_total_bytes);

  return (
    <section className="rounded-2xl border border-[var(--app-border)] bg-[var(--app-bg-2)] px-5 py-5 shadow-[var(--app-shadow)]">
      <div className="space-y-4">
        <h2 className="text-[15px] font-semibold tracking-tight text-[var(--app-text)]">Disk status</h2>

        <div className="rounded-xl border border-[var(--app-border)] bg-[var(--app-surface-muted)] px-4 py-4">
          <div className="flex items-start justify-between gap-4">
            <div className="flex items-center gap-3">
              <div
                className="flex h-10 w-10 items-center justify-center rounded-[10px] border border-[var(--app-border)] text-[var(--app-text-muted)]"
                style={{ backgroundColor: diskTone.surface }}
              >
                <HardDrive className="h-4 w-4" />
              </div>
              <div className="space-y-1">
                <div className="text-[14px] font-medium text-[var(--app-text)]">{diskTone.title}</div>
                <div className="text-[12px] text-[var(--app-text-muted)]">{diskTone.label}</div>
              </div>
            </div>

            <div className="text-right">
              <div className="text-[28px] font-semibold tracking-tight text-[var(--app-text)]">
                {formatPercent(diskPercent)}
              </div>
              <div className="text-[12px] text-[var(--app-text-muted)]">used</div>
              <div className="mt-1 text-[12px] text-[var(--app-text-muted)]">
                {formatFreePercent(diskPercent)}
              </div>
            </div>
          </div>

          <div className="mt-5">
            <div className="mb-2 flex items-center justify-between gap-3 text-[12px] text-[var(--app-text-muted)]">
              <span>Usage</span>
              <span>
                {diskUsed} of {diskCapacity}
              </span>
            </div>
            <div
              aria-label="Disk usage"
              aria-valuemax={100}
              aria-valuemin={0}
              aria-valuenow={diskPercent == null ? undefined : Math.round(diskPercent)}
              className="h-3 overflow-hidden rounded-md border border-[var(--app-border)] bg-[var(--app-surface)]"
              role="progressbar"
            >
              <div
                className="h-full rounded-[3px] transition-[width,background-color] duration-200"
                style={{
                  width: `${diskPercent ?? 0}%`,
                  backgroundColor: diskTone.bar,
                }}
              />
            </div>
          </div>
        </div>

        <div className="grid gap-3 sm:grid-cols-3">
          <MetricCard label="Used" value={diskUsed} />
          <MetricCard label="Free" value={diskFree} />
          <MetricCard label="Capacity" value={diskCapacity} />
        </div>

        {status.disk_total_bytes == null || status.disk_used_bytes == null ? (
          <div className="rounded-lg border border-[var(--app-border)] bg-[var(--app-surface-muted)] px-4 py-3 text-[13px] text-[var(--app-text-muted)]">
            Disk metrics are not available for this host yet.
          </div>
        ) : null}
      </div>
    </section>
  );
}
