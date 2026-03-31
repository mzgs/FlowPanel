import { useEffect, useEffectEvent, useState, type ReactNode } from "react";
import { fetchMariaDBStatus, installMariaDB, type MariaDBStatus } from "@/api/mariadb";
import { fetchPHPStatus, installPHP, type PHPStatus } from "@/api/php";
import { fetchSystemStatus, type SystemStatus } from "@/api/system";
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
  phpStatus: PHPStatus | null;
  systemStatus: SystemStatus | null;
};

async function fetchOverviewData(): Promise<OverviewData> {
  const [mariadbResult, phpResult, systemResult] = await Promise.allSettled([
    fetchMariaDBStatus(),
    fetchPHPStatus(),
    fetchSystemStatus(),
  ]);

  return {
    mariadbStatus: mariadbResult.status === "fulfilled" ? mariadbResult.value : null,
    phpStatus: phpResult.status === "fulfilled" ? phpResult.value : null,
    phpError:
      phpResult.status === "rejected"
        ? getActionError(phpResult.reason, "Failed to inspect the PHP runtime.")
        : null,
    systemStatus: systemResult.status === "fulfilled" ? systemResult.value : null,
  };
}

function SoftwareCard({
  mariadbStatus,
  phpStatus,
  runningAction,
  onInstallMariaDB,
  onInstallPHP,
}: {
  mariadbStatus: MariaDBStatus | null;
  phpStatus: PHPStatus | null;
  runningAction: "install-mariadb" | "install-php" | null;
  onInstallMariaDB: () => Promise<void>;
  onInstallPHP: () => Promise<void>;
}) {
  const mariaDBValue = formatMariaDBValue(mariadbStatus);
  const phpValue = phpStatus?.php_installed ? phpStatus.php_version?.trim() || "Installed" : null;

  return (
    <section className="rounded-2xl border border-[var(--app-border)] bg-[var(--app-surface)] px-5 py-5 shadow-[var(--app-shadow)]">
      <div className="space-y-4">
        <h2 className="text-[15px] font-semibold tracking-tight text-[var(--app-text)]">Software</h2>
        <div className="overflow-hidden rounded-xl border border-[var(--app-border)] bg-[var(--app-surface-muted)]">
          <SoftwareRow
            icon={<TerminalSquare className="h-4 w-4" />}
            label="PHP"
            value={
              phpValue ? (
                <div className="font-mono text-[12px] text-[var(--app-text-muted)]">{phpValue}</div>
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
                  className={mariadbStatus?.ready ? "font-mono text-[12px] text-[var(--app-text)]" : "text-[12px] text-[var(--app-text-muted)]"}
                >
                  {mariaDBValue}
                </div>
              )
            }
          />
          <SoftwareRow
            icon={<LayoutDashboard className="h-4 w-4" />}
            label="phpMyAdmin"
            value={<div className="text-[12px] text-[var(--app-text-muted)]">Status unavailable</div>}
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
    return status.version.trim();
  }

  if (status.service_running) {
    return "Running";
  }

  if (status.server_installed || status.client_installed) {
    return "Installed";
  }

  return "Not installed";
}

export function DashboardPage() {
  const [mariadbStatus, setMariaDBStatus] = useState<MariaDBStatus | null>(null);
  const [phpStatus, setPHPStatus] = useState<PHPStatus | null>(null);
  const [systemStatus, setSystemStatus] = useState<SystemStatus | null>(null);
  const [loading, setLoading] = useState(true);
  const [phpError, setPHPError] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const [runningAction, setRunningAction] = useState<"install-mariadb" | "install-php" | null>(null);

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

  return (
    <>
      <PageHeader title="Overview" />

      <div className="px-4 py-6 sm:px-6 lg:px-8">
        {loading ? (
          <section className="rounded-2xl border border-[var(--app-border)] bg-[var(--app-surface)] px-5 py-8 text-[13px] text-[var(--app-text-muted)] shadow-[var(--app-shadow)]">
            Inspecting local services...
          </section>
        ) : (
          <section className="space-y-5">
            {actionError ? (
              <section className="rounded-xl border border-[var(--app-danger)]/30 bg-[var(--app-danger-soft)] px-4 py-3 text-[13px] text-[var(--app-danger)]">
                {actionError}
              </section>
            ) : null}

            {systemStatus || phpStatus || mariadbStatus ? (
              <div className="grid gap-5 xl:grid-cols-[minmax(0,7fr)_minmax(320px,5fr)]">
                {systemStatus ? (
                  <SystemStatusCard status={systemStatus} />
                ) : null}
                <SoftwareCard
                  mariadbStatus={mariadbStatus}
                  phpStatus={phpStatus}
                  runningAction={runningAction}
                  onInstallMariaDB={handleMariaDBInstall}
                  onInstallPHP={handlePHPInstall}
                />
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
