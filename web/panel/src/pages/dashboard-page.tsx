import { useEffect, useEffectEvent, useState, type ReactNode } from "react";
import { fetchDomains } from "@/api/domains";
import { fetchMariaDBDatabases, fetchMariaDBStatus, installMariaDB, type MariaDBStatus } from "@/api/mariadb";
import { fetchPHPStatus, installPHP, type PHPStatus } from "@/api/php";
import { fetchPHPMyAdminStatus, installPHPMyAdmin, type PHPMyAdminStatus } from "@/api/phpmyadmin";
import { fetchSystemStatus, type SystemStatus } from "@/api/system";
import { DiskUsageCard } from "@/components/disk-usage-card";
import { Database, Globe, LayoutDashboard, LoaderCircle, TerminalSquare } from "@/components/icons/tabler-icons";
import { SystemStatusCard } from "@/components/system-status-card";
import { Button } from "@/components/ui/button";

function getActionError(error: unknown, fallback: string) {
  if (error instanceof Error && error.message && error.message !== "Failed to fetch") {
    return error.message;
  }

  return fallback;
}

function getRuntimeActionLabel(state?: string | null) {
  switch (state) {
    case "installing":
      return "Installing...";
    case "removing":
      return "Removing...";
    case "starting":
      return "Starting...";
    case "stopping":
      return "Stopping...";
    case "restarting":
      return "Restarting...";
    default:
      return null;
  }
}

function isRuntimeActionState(state?: string | null) {
  return getRuntimeActionLabel(state) !== null;
}

type OverviewData = {
  databaseCount: number | null;
  mariadbStatus: MariaDBStatus | null;
  phpError: string | null;
  phpMyAdminStatus: PHPMyAdminStatus | null;
  phpStatus: PHPStatus | null;
  siteCount: number | null;
  systemStatus: SystemStatus | null;
};

async function fetchOverviewData(): Promise<OverviewData> {
  const [databaseResult, domainsResult, mariadbResult, phpResult, phpMyAdminResult, systemResult] = await Promise.allSettled([
    fetchMariaDBDatabases(),
    fetchDomains(),
    fetchMariaDBStatus(),
    fetchPHPStatus(),
    fetchPHPMyAdminStatus(),
    fetchSystemStatus(),
  ]);

  return {
    databaseCount: databaseResult.status === "fulfilled" ? databaseResult.value.databases.length : null,
    mariadbStatus: mariadbResult.status === "fulfilled" ? mariadbResult.value : null,
    phpMyAdminStatus: phpMyAdminResult.status === "fulfilled" ? phpMyAdminResult.value : null,
    phpStatus: phpResult.status === "fulfilled" ? phpResult.value : null,
    phpError:
      phpResult.status === "rejected"
        ? getActionError(phpResult.reason, "Failed to inspect the PHP runtime.")
        : null,
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

function ApplicationsCard({
  mariadbStatus,
  phpMyAdminStatus,
  phpStatus,
  runningAction,
  onInstallMariaDB,
  onInstallPHP,
  onInstallPHPMyAdmin,
}: {
  mariadbStatus: MariaDBStatus | null;
  phpMyAdminStatus: PHPMyAdminStatus | null;
  phpStatus: PHPStatus | null;
  runningAction: "install-mariadb" | "install-php" | "install-phpmyadmin" | null;
  onInstallMariaDB: () => Promise<void>;
  onInstallPHP: () => Promise<void>;
  onInstallPHPMyAdmin: () => Promise<void>;
}) {
  const mariaDBValue = formatMariaDBValue(mariadbStatus);
  const phpVersion = formatPHPVersion(phpStatus);
  const phpBusyLabel = getRuntimeActionLabel(phpStatus?.state);
  const mariadbBusyLabel = getRuntimeActionLabel(mariadbStatus?.state);
  const phpMyAdminBusyLabel = getRuntimeActionLabel(phpMyAdminStatus?.state);
  const phpMyAdminInstallBlocked = !mariadbStatus?.server_installed;

  return (
    <section className="rounded-2xl border border-[var(--app-border)] bg-[var(--app-bg-2)] px-5 py-5 shadow-[var(--app-shadow)]">
      <div className="space-y-4">
        <h2 className="text-[15px] font-semibold tracking-tight text-[var(--app-text)]">Applications</h2>
        <div className="overflow-hidden rounded-xl border border-[var(--app-border)] bg-[var(--app-surface-muted)]">
          <SoftwareRow
            icon={<TerminalSquare className="h-4 w-4" />}
            label="PHP"
            value={
              phpBusyLabel ? (
                <div className="flex items-center gap-2 text-[12px] text-[var(--app-text-muted)]">
                  <LoaderCircle className="h-4 w-4 animate-spin" />
                  {phpBusyLabel}
                </div>
              ) : phpVersion ? (
                <div
                  className="max-w-[13rem] truncate text-right font-mono text-[12px] text-[var(--app-text)] sm:max-w-[18rem]"
                  title={phpVersion.full}
                >
                  {phpVersion.short}
                </div>
              ) : phpStatus?.install_available ? (
                <Button type="button" size="sm" onClick={onInstallPHP} disabled={runningAction !== null}>
                  {runningAction === "install-php" ? (
                    <LoaderCircle className="h-4 w-4 animate-spin" />
                  ) : null}
                  {phpStatus.install_label ?? "Install"}
                </Button>
              ) : (
                <div className="text-[12px] text-[var(--app-text-muted)]">Not installed</div>
              )
            }
          />
          <SoftwareRow
            icon={<Database className="h-4 w-4" />}
            label="MariaDB"
            value={
              mariadbBusyLabel ? (
                <div className="flex items-center gap-2 text-[12px] text-[var(--app-text-muted)]">
                  <LoaderCircle className="h-4 w-4 animate-spin" />
                  {mariadbBusyLabel}
                </div>
              ) : mariadbStatus?.install_available ? (
                <Button type="button" size="sm" onClick={onInstallMariaDB} disabled={runningAction !== null}>
                  {runningAction === "install-mariadb" ? (
                    <LoaderCircle className="h-4 w-4 animate-spin" />
                  ) : null}
                  {mariadbStatus.install_label ?? "Install"}
                </Button>
              ) : (
                <div
                  className={
                    mariadbStatus?.ready
                      ? "max-w-[13rem] truncate text-right font-mono text-[12px] text-[var(--app-text)] sm:max-w-[18rem]"
                      : "text-[12px] text-[var(--app-text-muted)]"
                  }
                  title={mariadbStatus?.ready && mariadbStatus.version?.trim() ? mariadbStatus.version.trim() : undefined}
                >
                  {mariaDBValue}
                </div>
              )
            }
          />
          <SoftwareRow
            icon={<LayoutDashboard className="h-4 w-4" />}
            label="phpMyAdmin"
            value={
              phpMyAdminBusyLabel ? (
                <div className="flex items-center gap-2 text-[12px] text-[var(--app-text-muted)]">
                  <LoaderCircle className="h-4 w-4 animate-spin" />
                  {phpMyAdminBusyLabel}
                </div>
              ) : phpMyAdminStatus?.install_available ? (
                <Button
                  type="button"
                  size="sm"
                  onClick={onInstallPHPMyAdmin}
                  disabled={runningAction !== null || phpMyAdminInstallBlocked}
                  title={phpMyAdminInstallBlocked ? "Install MariaDB first." : undefined}
                >
                  {runningAction === "install-phpmyadmin" ? (
                    <LoaderCircle className="h-4 w-4 animate-spin" />
                  ) : null}
                  {phpMyAdminStatus.install_label ?? "Install"}
                </Button>
              ) : (
                <div
                  className={
                    phpMyAdminStatus?.installed
                      ? "max-w-[13rem] truncate text-right font-mono text-[12px] text-[var(--app-text)] sm:max-w-[18rem]"
                      : "text-[12px] text-[var(--app-text-muted)]"
                  }
                  title={[phpMyAdminStatus?.version, phpMyAdminStatus?.install_path].filter(Boolean).join(" • ")}
                >
                  {formatPHPMyAdminValue(phpMyAdminStatus)}
                </div>
              )
            }
          />
        </div>
      </div>
    </section>
  );
}

function SoftwareRow({
  icon,
  label,
  value,
}: {
  icon: ReactNode;
  label: string;
  value: ReactNode;
}) {
  return (
    <div className="flex items-center justify-between gap-3 border-b border-[var(--app-border)] px-4 py-3 last:border-b-0">
      <div className="flex items-center gap-3">
        <div className="flex h-9 w-9 items-center justify-center rounded-[10px] border border-[var(--app-border)] bg-[var(--app-surface)] text-[var(--app-text-muted)]">
          {icon}
        </div>
        <div className="text-[14px] font-medium text-[var(--app-text)]">{label}</div>
      </div>
      {value}
    </div>
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

function formatMariaDBValue(status: MariaDBStatus | null) {
  if (!status) {
    return "Unavailable";
  }

  const actionLabel = getRuntimeActionLabel(status.state);
  if (actionLabel) {
    return actionLabel;
  }

  if (status.ready && status.version?.trim()) {
    return formatMariaDBVersion(status.version.trim());
  }

  if (status.service_running) {
    return "Running";
  }

  if (status.server_installed || status.client_installed) {
    return "Installed";
  }

  return "Not installed";
}

function formatPHPMyAdminValue(status: PHPMyAdminStatus | null) {
  if (!status) {
    return "Unavailable";
  }

  const actionLabel = getRuntimeActionLabel(status.state);
  if (actionLabel) {
    return actionLabel;
  }

  if (status.installed && status.version?.trim()) {
    return status.version.trim();
  }

  if (status.installed) {
    return "Installed";
  }

  return "Not installed";
}

function formatPHPVersion(status: PHPStatus | null) {
  const actionLabel = getRuntimeActionLabel(status?.state);
  if (actionLabel) {
    return {
      full: actionLabel,
      short: actionLabel,
    };
  }

  if (!status?.php_installed) {
    return null;
  }

  const version = status.php_version?.trim();
  if (!version) {
    return {
      full: "Installed",
      short: "Installed",
    };
  }

  return {
    full: version,
    short: extractVersionNumber(version, /\bPHP\s+(\d+(?:\.\d+)+)\b/i) ?? version,
  };
}

function formatMariaDBVersion(version: string) {
  return (
    extractVersionNumber(version, /\bDistrib\s+(\d+(?:\.\d+)+)(?:-[A-Za-z0-9._-]+)?/i) ??
    extractVersionNumber(version, /\bVer\s+(\d+(?:\.\d+)+)\b/i) ??
    extractVersionNumber(version, /\b(\d+(?:\.\d+)+)\b/) ??
    version
  );
}

function extractVersionNumber(value: string, pattern: RegExp) {
  const match = value.match(pattern);
  return match?.[1] ?? null;
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
  const [mariadbStatus, setMariaDBStatus] = useState<MariaDBStatus | null>(null);
  const [phpMyAdminStatus, setPHPMyAdminStatus] = useState<PHPMyAdminStatus | null>(null);
  const [phpStatus, setPHPStatus] = useState<PHPStatus | null>(null);
  const [siteCount, setSiteCount] = useState<number | null>(null);
  const [systemStatus, setSystemStatus] = useState<SystemStatus | null>(null);
  const [loading, setLoading] = useState(true);
  const [phpError, setPHPError] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const [runningAction, setRunningAction] = useState<
    "install-mariadb" | "install-php" | "install-phpmyadmin" | null
  >(null);

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

      setPHPStatus(nextOverview.phpStatus);
      setPHPMyAdminStatus(nextOverview.phpMyAdminStatus);
      setDatabaseCount(nextOverview.databaseCount);
      setMariaDBStatus(nextOverview.mariadbStatus);
      setPHPError(nextOverview.phpError);
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

  const refreshOverviewStatuses = useEffectEvent(async () => {
    const nextOverview = await fetchOverviewData();
    setPHPStatus(nextOverview.phpStatus);
    setPHPMyAdminStatus(nextOverview.phpMyAdminStatus);
    setDatabaseCount(nextOverview.databaseCount);
    setMariaDBStatus(nextOverview.mariadbStatus);
    setPHPError(nextOverview.phpError);
    setSiteCount(nextOverview.siteCount);
  });

  useEffect(() => {
    if (
      runningAction === null &&
      !isRuntimeActionState(phpStatus?.state) &&
      !isRuntimeActionState(mariadbStatus?.state) &&
      !isRuntimeActionState(phpMyAdminStatus?.state)
    ) {
      return;
    }

    const intervalId = window.setInterval(() => {
      void refreshOverviewStatuses();
    }, 3_000);

    return () => {
      window.clearInterval(intervalId);
    };
  }, [runningAction, mariadbStatus?.state, phpMyAdminStatus?.state, phpStatus?.state, refreshOverviewStatuses]);

  useEffect(() => {
    if (runningAction === "install-php" && phpStatus?.php_installed && phpStatus?.fpm_installed) {
      setRunningAction(null);
      return;
    }
    if (
      runningAction === "install-mariadb" &&
      (mariadbStatus?.server_installed || mariadbStatus?.client_installed)
    ) {
      setRunningAction(null);
      return;
    }
    if (runningAction === "install-phpmyadmin" && phpMyAdminStatus?.installed) {
      setRunningAction(null);
    }
  }, [runningAction, phpStatus, mariadbStatus, phpMyAdminStatus]);

  async function handleMariaDBInstall() {
    setRunningAction("install-mariadb");
    setActionError(null);

    try {
      const nextStatus = await installMariaDB();
      setMariaDBStatus(nextStatus);
    } catch (error) {
      setActionError(getActionError(error, "Failed to install MariaDB."));
    } finally {
      setRunningAction(null);
    }
  }

  async function handlePHPInstall() {
    setRunningAction("install-php");
    setActionError(null);

    try {
      const nextStatus = await installPHP();
      setPHPStatus(nextStatus);
      setPHPError(null);
    } catch (error) {
      setActionError(getActionError(error, "Failed to install PHP."));
    } finally {
      setRunningAction(null);
    }
  }

  async function handlePHPMyAdminInstall() {
    setRunningAction("install-phpmyadmin");
    setActionError(null);

    try {
      const nextStatus = await installPHPMyAdmin();
      setPHPMyAdminStatus(nextStatus);
    } catch (error) {
      setActionError(getActionError(error, "Failed to install phpMyAdmin."));
    } finally {
      setRunningAction(null);
    }
  }

  const hasApplications = Boolean(phpStatus || mariadbStatus || phpMyAdminStatus);
  const hasTotals = siteCount !== null || databaseCount !== null;
  const showOverview = Boolean(systemStatus || hasApplications || hasTotals);

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
            {actionError ? (
              <section className="rounded-xl border border-[var(--app-danger)]/30 bg-[var(--app-danger-soft)] px-4 py-3 text-[13px] text-[var(--app-danger)]">
                {actionError}
              </section>
            ) : null}

            {showOverview ? (
              <div className="space-y-5">
                {hasTotals ? <OverviewCard databaseCount={databaseCount} siteCount={siteCount} /> : null}

                {systemStatus ? (
                  <div className="grid gap-5 xl:grid-cols-[minmax(0,7fr)_minmax(320px,5fr)]">
                    <SystemStatusCard status={systemStatus} />
                    <DiskUsageCard status={systemStatus} />
                  </div>
                ) : null}

                {hasApplications ? (
                  <ApplicationsCard
                    mariadbStatus={mariadbStatus}
                    phpMyAdminStatus={phpMyAdminStatus}
                    phpStatus={phpStatus}
                    runningAction={runningAction}
                    onInstallMariaDB={handleMariaDBInstall}
                    onInstallPHP={handlePHPInstall}
                    onInstallPHPMyAdmin={handlePHPMyAdminInstall}
                  />
                ) : null}
              </div>
            ) : null}

            {phpError ? (
              <section className="rounded-xl border border-[var(--app-danger)]/30 bg-[var(--app-danger-soft)] px-4 py-3 text-[13px] text-[var(--app-danger)]">
                {phpError}
              </section>
            ) : null}
          </section>
        )}
      </div>
    </>
  );
}
