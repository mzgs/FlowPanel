import type { SystemStatus } from "@/api/system";
import { HardDrive } from "@/components/icons/tabler-icons";
import { formatBytes } from "@/lib/format";

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

function formatDiskSummary(status: SystemStatus) {
  if (status.disk_used_bytes == null || status.disk_total_bytes == null) {
    return "Disk metrics unavailable";
  }

  return `${formatBytes(status.disk_used_bytes)} used of ${formatBytes(status.disk_total_bytes)}`;
}

function formatDiskValue(value?: number) {
  if (value == null) {
    return "Unavailable";
  }

  return formatBytes(value);
}

function getDiskTitle(percent: number | null) {
  if (percent === null) {
    return "Filesystem";
  }

  if (percent < 70) {
    return "Comfortable headroom";
  }

  if (percent < 85) {
    return "Watching capacity";
  }

  return "Low free space";
}

function DetailRow({
  label,
  value,
  mono = false,
}: {
  label: string;
  value: string;
  mono?: boolean;
}) {
  return (
    <div className="flex items-center justify-between gap-3 border-b border-[var(--app-border)] px-4 py-3 last:border-b-0">
      <div className="text-[13px] text-[var(--app-text-muted)]">{label}</div>
      <div className={mono ? "font-mono text-[12px] text-[var(--app-text)]" : "text-[13px] font-medium text-[var(--app-text)]"}>
        {value}
      </div>
    </div>
  );
}

export function DiskUsageCard({ status }: { status: SystemStatus }) {
  const diskPercent = getDiskPercent(status);

  return (
    <section className="rounded-2xl border border-[var(--app-border)] bg-[var(--app-bg-2)] px-5 py-5 shadow-[var(--app-shadow)]">
      <div className="space-y-4">
        <h2 className="text-[15px] font-semibold tracking-tight text-[var(--app-text)]">Disk usage</h2>

        <div className="rounded-xl border border-[var(--app-border)] bg-[var(--app-surface-muted)] px-4 py-4">
          <div className="flex items-start justify-between gap-3">
            <div className="flex items-center gap-3">
              <div className="flex h-10 w-10 items-center justify-center rounded-[10px] border border-[var(--app-border)] bg-[var(--app-surface)] text-[var(--app-text-muted)]">
                <HardDrive className="h-4 w-4" />
              </div>
              <div className="space-y-1">
                <div className="text-[14px] font-medium text-[var(--app-text)]">{getDiskTitle(diskPercent)}</div>
                <div className="font-mono text-[12px] text-[var(--app-text-muted)]">
                  {status.disk_mount_path || "Filesystem"}
                </div>
              </div>
            </div>

            <div className="text-right">
              <div className="text-[28px] font-semibold tracking-tight text-[var(--app-text)]">
                {formatPercent(diskPercent)}
              </div>
              <div className="text-[12px] text-[var(--app-text-muted)]">used</div>
            </div>
          </div>

          <div className="mt-4 text-[13px] text-[var(--app-text)]">{formatDiskSummary(status)}</div>
        </div>

        <div className="overflow-hidden rounded-xl border border-[var(--app-border)] bg-[var(--app-surface-muted)]">
          <DetailRow label="Used" value={formatDiskValue(status.disk_used_bytes)} />
          <DetailRow label="Available" value={formatDiskValue(status.disk_free_bytes)} />
          <DetailRow label="Capacity" value={formatDiskValue(status.disk_total_bytes)} />
          {status.disk_mount_path ? <DetailRow label="Mount" value={status.disk_mount_path} mono /> : null}
        </div>
      </div>
    </section>
  );
}
