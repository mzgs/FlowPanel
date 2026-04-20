import { useEffect, useEffectEvent, useState, type ReactNode } from "react";
import { fetchDomains } from "@/api/domains";
import { fetchMariaDBDatabases } from "@/api/mariadb";
import { fetchSystemStatus, type SystemStatus } from "@/api/system";
import { DiskUsageCard } from "@/components/disk-usage-card";
import { Database, Globe } from "@/components/icons/tabler-icons";
import { SystemStatusCard } from "@/components/system-status-card";

type OverviewData = {
  databaseCount: number | null;
  siteCount: number | null;
  systemStatus: SystemStatus | null;
};

async function fetchOverviewData(): Promise<OverviewData> {
  const [databaseResult, domainsResult, systemResult] = await Promise.allSettled([
    fetchMariaDBDatabases(),
    fetchDomains(),
    fetchSystemStatus(),
  ]);

  return {
    databaseCount: databaseResult.status === "fulfilled" ? databaseResult.value.databases.length : null,
    siteCount: domainsResult.status === "fulfilled" ? domainsResult.value.domains.length : null,
    systemStatus: systemResult.status === "fulfilled" ? systemResult.value : null,
  };
}

function OverviewCard({
  databaseCount,
  siteCount,
}: {
  databaseCount: number | null;
  siteCount: number | null;
}) {
  return (
    <section className="rounded-xl border border-[var(--app-border)] bg-[var(--app-bg-2)] px-5 py-5 shadow-[var(--app-shadow)]">
      <div className="text-[15px] font-semibold tracking-tight text-[var(--app-text)]">Overview</div>
      <div className="mt-4 grid gap-px overflow-hidden rounded-lg border border-[var(--app-border)] bg-[var(--app-border)] sm:grid-cols-2">
        <OverviewStat icon={<Globe className="h-4 w-4" />} label="Total sites" value={formatTotalCount(siteCount)} />
        <OverviewStat
          icon={<Database className="h-4 w-4" />}
          label="Total databases"
          value={formatTotalCount(databaseCount)}
        />
      </div>
    </section>
  );
}

function OverviewStat({
  icon,
  label,
  value,
}: {
  icon: ReactNode;
  label: string;
  value: string;
}) {
  return (
    <div className="flex items-center justify-between gap-3 bg-[var(--app-surface-muted)] px-4 py-4">
      <div className="flex items-center gap-3">
        <div className="flex h-9 w-9 items-center justify-center rounded-[10px] border border-[var(--app-border)] bg-[var(--app-surface)] text-[var(--app-text-muted)]">
          {icon}
        </div>
        <div className="text-[14px] font-medium text-[var(--app-text)]">{label}</div>
      </div>
      <div className="text-[22px] font-semibold tracking-tight text-[var(--app-text)]">{value}</div>
    </div>
  );
}

function formatHostname(status: SystemStatus | null) {
  const hostname = status?.hostname?.trim();
  return hostname || "Unavailable";
}

function formatPlatform(status: SystemStatus | null) {
  const name = status?.platform_name?.trim();
  const version = status?.platform_version?.trim();

  if (name && version) {
    return `${name} ${version}`;
  }

  if (name) {
    return name;
  }

  switch (status?.platform) {
    case "darwin":
      return "macOS";
    case "linux":
      return "Linux";
    case "windows":
      return "Windows";
    case "freebsd":
      return "FreeBSD";
    default:
      return status?.platform?.trim() || "Unavailable";
  }
}

function formatTotalCount(value: number | null) {
  if (value === null) {
    return "Unavailable";
  }

  return String(value);
}

function formatPublicIPv4(status: SystemStatus | null) {
  const value = status?.public_ipv4?.trim();
  return value || "Unavailable";
}

function formatCoreCount(status: SystemStatus | null) {
  const cores = status?.cores;
  if (!cores) {
    return "Unavailable";
  }

  return `${cores} ${cores === 1 ? "core" : "cores"}`;
}

function formatMemoryTotal(status: SystemStatus | null) {
  const totalBytes = status?.memory_total_bytes;
  if (totalBytes == null || totalBytes <= 0) {
    return "Unavailable";
  }

  const totalGigabytes = totalBytes / (1024 * 1024 * 1024);
  const roundedGigabytes = Math.round(totalGigabytes * 10) / 10;
  return `${Number.isInteger(roundedGigabytes) ? roundedGigabytes.toFixed(0) : roundedGigabytes.toFixed(1)} GB`;
}

function formatServerTime(status: SystemStatus | null) {
  const displayValue = status?.server_time_display?.trim();
  const timezone = status?.timezone?.trim();

  if (!displayValue) {
    return "Unavailable";
  }

  return timezone ? `${displayValue} ${timezone}` : displayValue;
}

function formatUptime(status: SystemStatus | null) {
  const totalSeconds = status?.uptime_seconds;
  if (totalSeconds == null || totalSeconds <= 0) {
    return "Unavailable";
  }

  const days = Math.floor(totalSeconds / 86_400);
  const hours = Math.floor((totalSeconds % 86_400) / 3_600);
  const minutes = Math.floor((totalSeconds % 3_600) / 60);

  if (days > 0) {
    return hours > 0 ? `${days}d ${hours}h` : `${days}d`;
  }

  if (hours > 0) {
    return minutes > 0 ? `${hours}h ${minutes}m` : `${hours}h`;
  }

  return `${Math.max(minutes, 1)}m`;
}

function DetailItem({ label, value, valueClassName = "" }: { label: string; value: string; valueClassName?: string }) {
  return (
    <div className="flex min-w-0 items-baseline gap-2">
      <div className="shrink-0 text-[13px] font-semibold tracking-tight text-[var(--app-text)]">{label}:</div>
      <div className={`min-w-0 text-[13px] text-[var(--app-text-muted)] sm:text-[14px] ${valueClassName}`}>{value}</div>
    </div>
  );
}

function SystemInfoCard({ status }: { status: SystemStatus | null }) {
  const details = [
    {
      label: "IPv4 Public IP",
      value: formatPublicIPv4(status),
      valueClassName: "break-all font-mono text-[12px] sm:text-[13px]",
    },
    { label: "OS", value: formatPlatform(status) },
    {
      label: "Hostname",
      value: formatHostname(status),
      valueClassName: "break-all font-mono text-[12px] sm:text-[13px]",
    },
    { label: "CPU", value: formatCoreCount(status) },
    { label: "Memory", value: formatMemoryTotal(status) },
    { label: "Uptime", value: formatUptime(status) },
    { label: "Server time", value: formatServerTime(status) },
  ];

  return (
      <div className="rounded-lg border border-[var(--app-border)] bg-[var(--app-surface-muted)] px-4 py-3.5">
        <div className="flex flex-col gap-3 sm:flex-row sm:flex-wrap sm:items-center sm:gap-x-8">
          {details.map((detail) => (
            <DetailItem
              key={detail.label}
              label={detail.label}
              value={detail.value}
              valueClassName={detail.valueClassName}
            />
          ))}
        </div>
      </div>
  );
}

export function DashboardPage() {
  const [databaseCount, setDatabaseCount] = useState<number | null>(null);
  const [siteCount, setSiteCount] = useState<number | null>(null);
  const [systemStatus, setSystemStatus] = useState<SystemStatus | null>(null);
  const [loading, setLoading] = useState(true);

  const refreshSystemStatus = useEffectEvent(async () => {
    try {
      const nextStatus = await fetchSystemStatus();
      setSystemStatus(nextStatus);
    } catch {
      // Keep the last successful snapshot instead of surfacing transient polling errors.
    }
  });

  useEffect(() => {
    let active = true;

    async function loadStatus() {
      const nextOverview = await fetchOverviewData();
      if (!active) {
        return;
      }

      setDatabaseCount(nextOverview.databaseCount);
      setSiteCount(nextOverview.siteCount);
      setSystemStatus(nextOverview.systemStatus);
      setLoading(false);
    }

    loadStatus();

    return () => {
      active = false;
    };
  }, []);

  useEffect(() => {
    const intervalId = window.setInterval(() => {
      void refreshSystemStatus();
    }, 5_000);

    return () => {
      window.clearInterval(intervalId);
    };
  }, [refreshSystemStatus]);
  const hasTotals = siteCount !== null || databaseCount !== null;
  const showOverview = Boolean(systemStatus || hasTotals);

  return (
    <>
      <div className="px-4 py-6 sm:px-6 lg:px-8">
        <SystemInfoCard status={systemStatus} />
      </div>

      <div className="px-4 py-6 sm:px-6 lg:px-8">
        {loading ? (
          <section className="rounded-2xl border border-[var(--app-border)] bg-[var(--app-bg-2)] px-5 py-8 text-[13px] text-[var(--app-text-muted)] shadow-[var(--app-shadow)]">
            Inspecting local services...
          </section>
        ) : (
          <section className="space-y-5">
            {showOverview ? (
              <div className="space-y-5">
                {hasTotals ? <OverviewCard databaseCount={databaseCount} siteCount={siteCount} /> : null}

                {systemStatus ? (
                  <div className="grid gap-5 xl:grid-cols-[minmax(0,7fr)_minmax(320px,5fr)]">
                    <SystemStatusCard status={systemStatus} />
                    <DiskUsageCard status={systemStatus} />
                  </div>
                ) : null}
              </div>
            ) : null}
          </section>
        )}
      </div>
    </>
  );
}
