import { useEffect, useEffectEvent, useState, type ReactNode } from "react";
import { fetchMariaDBStatus, installMariaDB, type MariaDBStatus } from "@/api/mariadb";
import { fetchPHPStatus, installPHP, type PHPStatus } from "@/api/php";
import { fetchPHPMyAdminStatus, installPHPMyAdmin, type PHPMyAdminStatus } from "@/api/phpmyadmin";
import { fetchSystemStatus, type SystemStatus } from "@/api/system";
import { DiskUsageCard } from "@/components/disk-usage-card";
import { Database, LayoutDashboard, LoaderCircle, TerminalSquare } from "@/components/icons/tabler-icons";
import { PageHeader } from "@/components/page-header";
import { SystemStatusCard } from "@/components/system-status-card";
import { Button } from "@/components/ui/button";

function getActionError(error: unknown, fallback: string) {
  if (error instanceof Error && error.message && error.message !== "Failed to fetch") {
    return error.message;
  }

  return fallback;
}

type OverviewData = {
  mariadbStatus: MariaDBStatus | null;
  phpError: string | null;
  phpMyAdminStatus: PHPMyAdminStatus | null;
  phpStatus: PHPStatus | null;
  systemStatus: SystemStatus | null;
};

async function fetchOverviewData(): Promise<OverviewData> {
  const [mariadbResult, phpResult, phpMyAdminResult, systemResult] = await Promise.allSettled([
    fetchMariaDBStatus(),
    fetchPHPStatus(),
    fetchPHPMyAdminStatus(),
    fetchSystemStatus(),
  ]);

  return {
    mariadbStatus: mariadbResult.status === "fulfilled" ? mariadbResult.value : null,
    phpMyAdminStatus: phpMyAdminResult.status === "fulfilled" ? phpMyAdminResult.value : null,
    phpStatus: phpResult.status === "fulfilled" ? phpResult.value : null,
    phpError:
      phpResult.status === "rejected"
        ? getActionError(phpResult.reason, "Failed to inspect the PHP runtime.")
        : null,
    systemStatus: systemResult.status === "fulfilled" ? systemResult.value : null,
  };
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
              phpVersion ? (
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
              mariadbStatus?.install_available ? (
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
              phpMyAdminStatus?.install_available ? (
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

function formatMariaDBValue(status: MariaDBStatus | null) {
  if (!status) {
    return "Unavailable";
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

  if (status.installed && status.version?.trim()) {
    return status.version.trim();
  }

  if (status.installed) {
    return "Installed";
  }

  return "Not installed";
}

function formatPHPVersion(status: PHPStatus | null) {
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

export function DashboardPage() {
  const [mariadbStatus, setMariaDBStatus] = useState<MariaDBStatus | null>(null);
  const [phpMyAdminStatus, setPHPMyAdminStatus] = useState<PHPMyAdminStatus | null>(null);
  const [phpStatus, setPHPStatus] = useState<PHPStatus | null>(null);
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
      setMariaDBStatus(nextOverview.mariadbStatus);
      setPHPError(nextOverview.phpError);
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
  const showOverview = Boolean(systemStatus || hasApplications);

  return (
    <>
      <PageHeader title="Overview" />

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
