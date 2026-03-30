import { useEffect, useState } from "react";
import { LoaderCircle, RefreshCw } from "lucide-react";
import { fetchPHPStatus, installPHP, startPHP, type PHPStatus } from "@/api/php";
import { PageHeader } from "@/components/page-header";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";

function getStatusBadge(status: PHPStatus) {
  switch (status.state) {
    case "ready":
      return <Badge variant="success">Ready</Badge>;
    case "stopped":
      return <Badge variant="warning">Needs start</Badge>;
    case "missing":
      return <Badge variant="danger">Missing</Badge>;
    case "missing-fpm":
      return <Badge variant="danger">Missing FPM</Badge>;
    case "misconfigured":
      return <Badge variant="danger">Misconfigured</Badge>;
    default:
      return <Badge variant="neutral">Unknown</Badge>;
  }
}

function getDetailValue(value?: string) {
  return value && value.trim() ? value : "Not detected";
}

function getActionError(error: unknown, fallback: string) {
  if (error instanceof Error && error.message) {
    return error.message;
  }

  return fallback;
}

export function DashboardPage() {
  const [phpStatus, setPHPStatus] = useState<PHPStatus | null>(null);
  const [loading, setLoading] = useState(true);
  const [loadingError, setLoadingError] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const [runningAction, setRunningAction] = useState<"install" | "start" | "refresh" | null>(null);

  useEffect(() => {
    let active = true;

    async function loadStatus() {
      try {
        const nextStatus = await fetchPHPStatus();
        if (!active) {
          return;
        }

        setPHPStatus(nextStatus);
        setLoadingError(null);
      } catch (error) {
        if (!active) {
          return;
        }

        setLoadingError(getActionError(error, "Failed to inspect the PHP runtime."));
      } finally {
        if (active) {
          setLoading(false);
        }
      }
    }

    loadStatus();

    return () => {
      active = false;
    };
  }, []);

  async function handleRefresh() {
    setRunningAction("refresh");
    setActionError(null);

    try {
      const nextStatus = await fetchPHPStatus();
      setPHPStatus(nextStatus);
      setLoadingError(null);
    } catch (error) {
      setActionError(getActionError(error, "Failed to refresh the PHP runtime."));
    } finally {
      setRunningAction(null);
    }
  }

  async function handleInstall() {
    setRunningAction("install");
    setActionError(null);

    try {
      const nextStatus = await installPHP();
      setPHPStatus(nextStatus);
      setLoadingError(null);
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
      setLoadingError(null);
    } catch (error) {
      setActionError(getActionError(error, "Failed to start PHP-FPM."));
    } finally {
      setRunningAction(null);
    }
  }

  const meta = loading
    ? "Inspecting the local PHP runtime."
    : phpStatus?.message ?? "PHP runtime status is unavailable.";

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
      <div className="px-5 py-5 md:px-8">
        {loadingError ? (
          <section className="rounded-[10px] border border-[var(--app-danger)]/40 bg-[var(--app-danger-soft)] px-4 py-3 text-[13px] text-[var(--app-danger)]">
            {loadingError}
          </section>
        ) : null}

        {!loadingError && loading ? (
          <section className="rounded-[10px] border border-[var(--app-border)] bg-[var(--app-surface)] px-5 py-8 text-[13px] text-[var(--app-text-muted)]">
            Inspecting the PHP runtime...
          </section>
        ) : null}

        {!loadingError && !loading && phpStatus ? (
          <section className="space-y-5">
            {actionError ? (
              <section className="rounded-[10px] border border-[var(--app-danger)]/40 bg-[var(--app-danger-soft)] px-4 py-3 text-[13px] text-[var(--app-danger)]">
                {actionError}
              </section>
            ) : null}

            <section className="rounded-[10px] border border-[var(--app-border)] bg-[var(--app-surface)]">
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
                    <Button
                      type="button"
                      onClick={handleInstall}
                      disabled={runningAction !== null}
                    >
                      {runningAction === "install" ? (
                        <LoaderCircle className="h-4 w-4 animate-spin" />
                      ) : null}
                      {phpStatus.install_label ?? "Install PHP"}
                    </Button>
                  ) : null}
                  {phpStatus.start_available ? (
                    <Button
                      type="button"
                      variant="secondary"
                      onClick={handleStart}
                      disabled={runningAction !== null}
                    >
                      {runningAction === "start" ? (
                        <LoaderCircle className="h-4 w-4 animate-spin" />
                      ) : null}
                      {phpStatus.start_label ?? "Start PHP-FPM"}
                    </Button>
                  ) : null}
                </div>
              </div>

              <div className="grid gap-4 px-5 py-5 md:grid-cols-2">
                <div className="space-y-3 rounded-[10px] border border-[var(--app-border)] bg-[var(--app-surface-muted)] p-4">
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

                <div className="space-y-3 rounded-[10px] border border-[var(--app-border)] bg-[var(--app-surface-muted)] p-4">
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

                <div className="space-y-3 rounded-[10px] border border-[var(--app-border)] bg-[var(--app-surface-muted)] p-4">
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

                <div className="space-y-3 rounded-[10px] border border-[var(--app-border)] bg-[var(--app-surface-muted)] p-4">
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
              <section className="rounded-[10px] border border-[var(--app-warning)]/40 bg-[var(--app-warning-soft)] px-4 py-3 text-[13px] text-[var(--app-warning)]">
                {phpStatus.issues.map((issue) => (
                  <p key={issue}>{issue}</p>
                ))}
              </section>
            ) : null}
          </section>
        ) : null}
      </div>
    </>
  );
}
