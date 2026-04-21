import type { SystemStatus } from "@/api/system";
import { HardDrive } from "@/components/icons/tabler-icons";

type DiskTone = {
  barClassName: string;
  iconClassName: string;
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

  return `${Math.round(value)}%`;
}

function formatDiskValue(value?: number) {
  if (value == null || value < 0) {
    return "Unavailable";
  }

  const units = ["B", "KB", "MB", "GB", "TB", "PB"];
  const exponent = Math.min(Math.floor(Math.log(Math.max(value, 1)) / Math.log(1024)), units.length - 1);
  const size = value / 1024 ** exponent;
  const digits = exponent >= 3 && size < 100 ? 1 : 0;

  return `${size.toFixed(digits)} ${units[exponent]}`;
}

function formatDiskLabel(status: SystemStatus) {
  const name = status.platform_name?.trim();
  if (name) {
    return name;
  }

  switch (status.platform) {
    case "darwin":
      return "macOS";
    case "linux":
      return "Linux";
    case "windows":
      return "Windows";
    case "freebsd":
      return "FreeBSD";
    default:
      return "Disk";
  }
}

function getDiskTone(percent: number | null): DiskTone {
  if (percent === null) {
    return {
      barClassName: "bg-[var(--app-border-strong)]",
      iconClassName: "text-[var(--app-text-muted)]",
    };
  }

  if (percent < 70) {
    return {
      barClassName: "bg-[var(--app-ok)]",
      iconClassName: "text-[var(--app-ok)]",
    };
  }

  if (percent < 85) {
    return {
      barClassName: "bg-[var(--app-warning)]",
      iconClassName: "text-[var(--app-warning)]",
    };
  }

  return {
    barClassName: "bg-[var(--app-danger)]",
    iconClassName: "text-[var(--app-danger)]",
  };
}

export function DiskUsageCard({ status }: { status: SystemStatus }) {
  const diskPercent = getDiskPercent(status);
  const diskTone = getDiskTone(diskPercent);
  const diskUsed = formatDiskValue(status.disk_used_bytes);
  const diskFree = formatDiskValue(status.disk_free_bytes);
  const diskTotal = formatDiskValue(status.disk_total_bytes);
  const diskLabel = formatDiskLabel(status);
  const diskMetricsAvailable = status.disk_total_bytes != null && status.disk_used_bytes != null;

  return (
    <section className="rounded-[20px] border border-[var(--app-border)] bg-[var(--app-bg-2)] px-5 pb-3 pt-2 shadow-[var(--app-shadow)]">
      <div className="space-y-3">
        <div className="text-[15px] font-semibold tracking-tight text-[var(--app-text)]">Disk status</div>
        <div className="flex items-start justify-between gap-4">
          <div className="flex min-w-0 items-center gap-3">
            <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-xl border border-[var(--app-border)] bg-[var(--app-surface)]">
              <HardDrive className={`h-[18px] w-[18px] ${diskTone.iconClassName}`} stroke={1.8} />
            </div>
            <div className="min-w-0">
              <div className="flex min-w-0 items-baseline gap-2">
                <div className="truncate text-[15px] font-semibold tracking-tight text-[var(--app-text)]">{diskLabel}</div>
                <div className="shrink-0 text-[12px] font-medium text-[var(--app-text-muted)]">Total {diskTotal}</div>
              </div>
            </div>
          </div>
          <div className="shrink-0 text-[18px] font-semibold leading-none tracking-tight text-[var(--app-text)]">
            {formatPercent(diskPercent)}
          </div>
        </div>

        <div className="grid grid-cols-2 gap-3">
          <div className="min-w-0">
            <div className="text-[12px] font-medium text-[var(--app-text-muted)]">Used {diskUsed}</div>
          </div>
          <div className="min-w-0 text-right">
            <div className="text-[12px] font-medium text-[var(--app-text-muted)]">Free {diskFree}</div>
          </div>
        </div>

        <div
          aria-label="Disk usage"
          aria-valuemax={100}
          aria-valuemin={0}
          aria-valuenow={diskPercent == null ? undefined : Math.round(diskPercent)}
          className="h-3 overflow-hidden rounded-full bg-[var(--app-surface)]"
          role="progressbar"
        >
          <div
            className={`h-full rounded-full transition-[width] duration-200 ${diskTone.barClassName}`}
            style={{ width: `${diskPercent ?? 0}%` }}
          />
        </div>

        {diskMetricsAvailable ? null : (
          <div className="text-[12px] text-[var(--app-text-muted)]">Disk metrics are not available for this host yet.</div>
        )}
      </div>
    </section>
  );
}
