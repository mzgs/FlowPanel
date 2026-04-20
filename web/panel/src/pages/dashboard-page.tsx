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

function formatServerTime(status: SystemStatus | null) {
  const displayValue = status?.server_time_display?.trim();
  if (displayValue) {
    return displayValue;
  }

  const rawValue = status?.server_time?.trim();
  if (!rawValue) {
    return "Unavailable";
  }

  return rawValue;
}

function formatTimezone(status: SystemStatus | null) {
  const value = status?.timezone?.trim();
  if (!value) {
    return "Local";
  }

  const match = value.match(/^([+-])0?(\d{1,2})(?::?(\d{2}))?$/);
  if (!match) {
    return value;
  }

  const [, sign, hours, minutes] = match;
  if (minutes && minutes !== "00") {
    return `${sign}${Number(hours)}:${minutes}`;
  }

  return `${sign}${Number(hours)}`;
}

function formatTotalCount(value: number | null) {
  if (value === null) {
    return "Unavailable";
  }

  return String(value);
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
        <div className="grid gap-3 sm:grid-cols-2 xl:max-w-5xl xl:grid-cols-3">
          <section className="rounded-xl border border-[var(--app-border)] bg-[var(--app-bg-2)] px-4 py-3 shadow-[var(--app-shadow)]">
            <div className="text-[12px] text-[var(--app-text-muted)]">Operating system</div>
            <div className="mt-1 text-[15px] font-semibold tracking-tight text-[var(--app-text)]">
              {formatPlatform(systemStatus)}
            </div>
          </section>
          <section className="rounded-xl border border-[var(--app-border)] bg-[var(--app-bg-2)] px-4 py-3 shadow-[var(--app-shadow)]">
            <div className="text-[12px] text-[var(--app-text-muted)]">Hostname</div>
            <div className="mt-1 font-mono text-[13px] text-[var(--app-text)]">{formatHostname(systemStatus)}</div>
          </section>
          <section className="rounded-xl border border-[var(--app-border)] bg-[var(--app-bg-2)] px-4 py-3 shadow-[var(--app-shadow)]">
            <div className="text-[12px] text-[var(--app-text-muted)]">Server time</div>
            <div className="mt-1 flex items-baseline gap-2">
              <div className="text-[15px] font-semibold tracking-tight text-[var(--app-text)]">
                {formatServerTime(systemStatus)}
              </div>
              <div className="font-mono text-[12px] text-[var(--app-text-muted)]">
                {formatTimezone(systemStatus)}
              </div>
            </div>
          </section>
        </div>
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
