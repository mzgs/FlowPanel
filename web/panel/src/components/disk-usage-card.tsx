import type { SystemStatus } from "@/api/system";
import { formatBytes } from "@/lib/format";

type DiskTone = {
  bar: string;
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
    };
  }

  if (percent < 70) {
    return {
      bar: "var(--app-ok)",
    };
  }

  if (percent < 85) {
    return {
      bar: "var(--app-warning)",
    };
  }

  return {
    bar: "var(--app-danger)",
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
          <div className="flex items-start justify-end gap-3">
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
            <div className="flex items-center gap-2">
              <img
                alt=""
                aria-hidden="true"
                className="h-5 w-5 shrink-0 object-contain opacity-80"
                src="/application-icons/hdd.png"
              />
              <div
                aria-label="Disk usage"
                aria-valuemax={100}
                aria-valuemin={0}
                aria-valuenow={diskPercent == null ? undefined : Math.round(diskPercent)}
                className="h-2.5 flex-1 overflow-hidden rounded-md border border-[var(--app-border)] bg-[var(--app-surface)]"
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
