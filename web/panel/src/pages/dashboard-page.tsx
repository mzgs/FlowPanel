import { useEffect, useEffectEvent, useState } from "react";
import { fetchPHPStatus, installPHP, startPHP, type PHPStatus } from "@/api/php";
import { fetchSystemStatus, type SystemStatus } from "@/api/system";
import { LoaderCircle, RefreshCw } from "@/components/icons/tabler-icons";
import { PageHeader } from "@/components/page-header";
import { SystemStatusCard } from "@/components/system-status-card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";

function getStatusBadge(status: PHPStatus) {
  switch (status.state) {
    case "ready":
      return <Badge>Ready</Badge>;
    case "stopped":
      return <Badge variant="secondary">Needs start</Badge>;
    case "missing":
      return <Badge variant="destructive">Missing</Badge>;
    case "missing-fpm":
      return <Badge variant="destructive">Missing FPM</Badge>;
    case "misconfigured":
      return <Badge variant="destructive">Misconfigured</Badge>;
    default:
      return <Badge variant="outline">Unknown</Badge>;
  }
}

function getDetailValue(value?: string) {
  return value && value.trim() ? value : "Not detected";
}

function getActionError(error: unknown, fallback: string) {
  if (error instanceof Error && error.message && error.message !== "Failed to fetch") {
    return error.message;
  }

  return fallback;
}

type OverviewData = {
  phpError: string | null;
  phpStatus: PHPStatus | null;
  systemStatus: SystemStatus | null;
};

async function fetchOverviewData(): Promise<OverviewData> {
  const [phpResult, systemResult] = await Promise.allSettled([fetchPHPStatus(), fetchSystemStatus()]);

  return {
    phpStatus: phpResult.status === "fulfilled" ? phpResult.value : null,
    phpError:
      phpResult.status === "rejected"
        ? getActionError(phpResult.reason, "Failed to inspect the PHP runtime.")
        : null,
    systemStatus: systemResult.status === "fulfilled" ? systemResult.value : null,
  };
}

function PHPRuntimeCard({
  phpStatus,
  runningAction,
  onInstall,
  onStart,
}: {
  phpStatus: PHPStatus;
  runningAction: "install" | "start" | "refresh" | null;
  onInstall: () => Promise<void>;
  onStart: () => Promise<void>;
}) {
  return (
    <>
      <section className="rounded-2xl border border-[var(--app-border)] bg-[var(--app-surface)] shadow-[var(--app-shadow)]">
        <div className="flex flex-col gap-4 border-b border-[var(--app-border)] px-5 py-4 md:flex-row md:items-start md:justify-between">
          <div className="space-y-2">
            <div className="flex items-center gap-2">
              <h2 className="text-[15px] font-semibold text-[var(--app-text)]">PHP Runtime</h2>
              {getStatusBadge(phpStatus)}
            </div>
            <p className="max-w-2xl text-[13px] leading-6 text-[var(--app-text-muted)]">
              {phpStatus.message}
            </p>
          </div>

          <div className="flex flex-wrap items-center gap-2">
            {phpStatus.install_available ? (
              <Button type="button" onClick={onInstall} disabled={runningAction !== null}>
                {runningAction === "install" ? (
                  <LoaderCircle className="h-4 w-4 animate-spin" />
                ) : null}
                {phpStatus.install_label ?? "Install PHP"}
              </Button>
            ) : null}
            {phpStatus.start_available ? (
              <Button type="button" variant="secondary" onClick={onStart} disabled={runningAction !== null}>
                {runningAction === "start" ? (
                  <LoaderCircle className="h-4 w-4 animate-spin" />
                ) : null}
                {phpStatus.start_label ?? "Start PHP-FPM"}
              </Button>
            ) : null}
          </div>
        </div>

        <div className="grid gap-4 px-5 py-5 md:grid-cols-2">
          <div className="space-y-3 rounded-xl border border-[var(--app-border)] bg-[var(--app-surface-muted)] p-4">
            <div className="text-[12px] font-medium uppercase tracking-[0.08em] text-[var(--app-text-muted)]">
              PHP CLI
            </div>
            <div className="text-[13px] text-[var(--app-text)]">
              {phpStatus.php_installed ? "Installed" : "Not installed"}
            </div>
            <div className="font-mono text-[12px] leading-6 text-[var(--app-text-muted)]">
              {getDetailValue(phpStatus.php_path)}
            </div>
            <div className="text-[12px] leading-6 text-[var(--app-text-muted)]">
              {getDetailValue(phpStatus.php_version)}
            </div>
          </div>

          <div className="space-y-3 rounded-xl border border-[var(--app-border)] bg-[var(--app-surface-muted)] p-4">
            <div className="text-[12px] font-medium uppercase tracking-[0.08em] text-[var(--app-text-muted)]">
              PHP-FPM
            </div>
            <div className="text-[13px] text-[var(--app-text)]">
              {phpStatus.fpm_installed ? "Installed" : "Not installed"}
            </div>
            <div className="font-mono text-[12px] leading-6 text-[var(--app-text-muted)]">
              {getDetailValue(phpStatus.fpm_path)}
            </div>
            <div className="text-[12px] leading-6 text-[var(--app-text-muted)]">
              {phpStatus.service_running ? "Service reachable" : "Service not running"}
            </div>
          </div>

          <div className="space-y-3 rounded-xl border border-[var(--app-border)] bg-[var(--app-surface-muted)] p-4">
            <div className="text-[12px] font-medium uppercase tracking-[0.08em] text-[var(--app-text-muted)]">
              FastCGI
            </div>
            <div className="font-mono text-[12px] leading-6 text-[var(--app-text-muted)]">
              {getDetailValue(phpStatus.listen_address)}
            </div>
            <div className="text-[12px] leading-6 text-[var(--app-text-muted)]">
              Php site domains are proxied to this address through embedded Caddy.
            </div>
          </div>

          <div className="space-y-3 rounded-xl border border-[var(--app-border)] bg-[var(--app-surface-muted)] p-4">
            <div className="text-[12px] font-medium uppercase tracking-[0.08em] text-[var(--app-text-muted)]">
              Package Manager
            </div>
            <div className="text-[13px] text-[var(--app-text)]">
              {getDetailValue(phpStatus.package_manager)}
            </div>
            <div className="text-[12px] leading-6 text-[var(--app-text-muted)]">
              FlowPanel can install PHP automatically when this server exposes a supported package manager.
            </div>
          </div>
        </div>
      </section>

      {phpStatus.issues && phpStatus.issues.length > 0 ? (
        <section className="rounded-xl border border-[var(--app-warning)]/30 bg-[var(--app-warning-soft)] px-4 py-3 text-[13px] text-[var(--app-warning)]">
          {phpStatus.issues.map((issue) => (
            <p key={issue}>{issue}</p>
          ))}
        </section>
      ) : null}
    </>
  );
}

export function DashboardPage() {
  const [phpStatus, setPHPStatus] = useState<PHPStatus | null>(null);
  const [systemStatus, setSystemStatus] = useState<SystemStatus | null>(null);
  const [loading, setLoading] = useState(true);
  const [phpError, setPHPError] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const [runningAction, setRunningAction] = useState<"install" | "start" | "refresh" | null>(null);

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

  async function handleRefresh() {
    setRunningAction("refresh");
    setActionError(null);

    const nextOverview = await fetchOverviewData();
    setPHPStatus(nextOverview.phpStatus);
    setPHPError(nextOverview.phpError);
    setSystemStatus(nextOverview.systemStatus);
    setRunningAction(null);
  }

  async function handleInstall() {
    setRunningAction("install");
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

  async function handleStart() {
    setRunningAction("start");
    setActionError(null);

    try {
      const nextStatus = await startPHP();
      setPHPStatus(nextStatus);
      setPHPError(null);
    } catch (error) {
      setActionError(getActionError(error, "Failed to start PHP-FPM."));
    } finally {
      setRunningAction(null);
    }
  }

  const meta = loading ? "Inspecting local services." : phpStatus?.message ?? "Overview is ready.";

  return (
    <>
      <PageHeader
        title="Overview"
        meta={meta}
        actions={
          <Button
            type="button"
            variant="secondary"
            onClick={handleRefresh}
            disabled={runningAction !== null}
          >
            {runningAction === "refresh" ? (
              <LoaderCircle className="h-4 w-4 animate-spin" />
            ) : (
              <RefreshCw className="h-4 w-4" />
            )}
            Refresh
          </Button>
        }
      />

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

            {systemStatus ? (
              <div className="xl:w-7/12">
                <SystemStatusCard status={systemStatus} />
              </div>
            ) : null}

            {phpError ? (
              <section className="rounded-xl border border-[var(--app-danger)]/30 bg-[var(--app-danger-soft)] px-4 py-3 text-[13px] text-[var(--app-danger)]">
                {phpError}
              </section>
            ) : null}

            {phpStatus ? (
              <PHPRuntimeCard
                phpStatus={phpStatus}
                runningAction={runningAction}
                onInstall={handleInstall}
                onStart={handleStart}
              />
            ) : null}
          </section>
        )}
      </div>
    </>
  );
}
