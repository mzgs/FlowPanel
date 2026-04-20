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

export function DiskUsageCard({ status }: { status: SystemStatus }) {
  const diskPercent = getDiskPercent(status);
  const diskTone = getDiskTone(diskPercent);
  const diskUsed = formatDiskValue(status.disk_used_bytes);
  const diskFree = formatDiskValue(status.disk_free_bytes);
  const diskCapacity = formatDiskValue(status.disk_total_bytes);

  return (
    <section className="rounded-xl border border-[var(--app-border)] bg-[var(--app-bg-2)] px-4 py-4 shadow-[var(--app-shadow)]">
      <div className="space-y-3">
        <h2 className="text-[14px] font-semibold tracking-tight text-[var(--app-text)]">Disk status</h2>

        <div className="space-y-3">
          <div className="flex items-start justify-between gap-3">
            <div className="flex min-w-0 items-center gap-2.5">
              <div
                className="flex h-8 w-8 shrink-0 items-center justify-center rounded-[9px] border border-[var(--app-border)] text-[var(--app-text-muted)]"
                style={{ backgroundColor: diskTone.surface }}
              >
                <HardDrive className="h-[15px] w-[15px]" />
              </div>
              <div className="min-w-0 space-y-0.5">
                <div className="text-[13px] font-medium leading-5 text-[var(--app-text)]">{diskTone.title}</div>
                <div className="text-[11px] leading-4 text-[var(--app-text-muted)]">{diskTone.label}</div>
              </div>
            </div>

            <div className="shrink-0 text-right">
              <div className="text-[24px] font-semibold leading-none tracking-tight text-[var(--app-text)]">
                {formatPercent(diskPercent)}
              </div>
              <div className="mt-0.5 text-[11px] text-[var(--app-text-muted)]">used</div>
              <div className="mt-1 text-[11px] leading-4 text-[var(--app-text-muted)]">
                {formatFreePercent(diskPercent)}
              </div>
            </div>
          </div>

          <div className="border-t border-[var(--app-border)] pt-3">
            <div className="mb-1.5 flex items-center justify-between gap-3 text-[11px] text-[var(--app-text-muted)]">
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
              className="h-2.5 overflow-hidden rounded-md border border-[var(--app-border)] bg-[var(--app-surface)]"
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

        <div className="grid gap-px overflow-hidden rounded-md border border-[var(--app-border)] bg-[var(--app-border)] sm:grid-cols-3">
          {[
            { label: "Used", value: diskUsed },
            { label: "Free", value: diskFree },
            { label: "Capacity", value: diskCapacity },
          ].map((metric) => (
            <div key={metric.label} className="bg-[var(--app-surface-muted)] px-3 py-2.5">
              <div className="text-[11px] text-[var(--app-text-muted)]">{metric.label}</div>
              <div className="mt-0.5 text-[13px] font-semibold tracking-tight text-[var(--app-text)]">
                {metric.value}
              </div>
            </div>
          ))}
        </div>

        {status.disk_total_bytes == null || status.disk_used_bytes == null ? (
          <div className="border-t border-[var(--app-border)] pt-2.5 text-[12px] text-[var(--app-text-muted)]">
            Disk metrics are not available for this host yet.
          </div>
        ) : null}
      </div>
    </section>
  );
}
