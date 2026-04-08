import { useEffect, useState, type ReactNode } from "react";
import {
  fetchMariaDBStatus,
  installMariaDB,
  restartMariaDB,
  startMariaDB,
  stopMariaDB,
  type MariaDBStatus,
} from "@/api/mariadb";
import { fetchPHPStatus, installPHP, restartPHP, startPHP, stopPHP, type PHPStatus } from "@/api/php";
import {
  fetchPHPMyAdminStatus,
  installPHPMyAdmin,
  type PHPMyAdminStatus,
} from "@/api/phpmyadmin";
import {
  Database,
  ExternalLink,
  LayoutDashboard,
  LoaderCircle,
  Package,
  PlayerPlayFilled,
  PlayerStop,
  RefreshCw,
  RotateCcw,
  TerminalSquare,
  Trash2,
} from "@/components/icons/tabler-icons";
import { PageHeader } from "@/components/page-header";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { toast } from "sonner";

const compactActionButtonClassName = "h-7 gap-1.5 px-2.5 text-xs";
const statusMetaBadgeClassName = "h-5 rounded-sm px-1.5 py-0 text-[11px] font-medium";

function getErrorMessage(error: unknown, fallback: string) {
  if (error instanceof Error && error.message) {
    return error.message;
  }

  return fallback;
}

function extractVersionNumber(value: string, pattern: RegExp) {
  const match = value.match(pattern);
  return match?.[1] ?? null;
}

function formatPHPVersion(status: PHPStatus | null) {
  if (!status?.php_installed) {
    return "Not installed";
  }

  const version = status.php_version?.trim();
  if (!version) {
    return "Installed";
  }

  return extractVersionNumber(version, /\bPHP\s+(\d+(?:\.\d+)+)\b/i) ?? version;
}

function formatMariaDBVersion(version: string) {
  return (
    extractVersionNumber(version, /\bDistrib\s+(\d+(?:\.\d+)+)(?:-[A-Za-z0-9._-]+)?/i) ??
    extractVersionNumber(version, /\bVer\s+(\d+(?:\.\d+)+)\b/i) ??
    extractVersionNumber(version, /\b(\d+(?:\.\d+)+)\b/) ??
    version
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

function getPHPBadge(status: PHPStatus | null) {
  if (!status) {
    return { label: "Unavailable", variant: "outline" as const };
  }

  if (status.ready) {
    return { label: "Ready", variant: "default" as const };
  }

  if (status.service_running) {
    return { label: "Running", variant: "secondary" as const };
  }

  if (status.php_installed) {
    return { label: "Installed", variant: "outline" as const };
  }

  return { label: "Not installed", variant: "outline" as const };
}

function getMariaDBBadge(status: MariaDBStatus | null) {
  if (!status) {
    return { label: "Unavailable", variant: "outline" as const };
  }

  if (status.ready) {
    return { label: "Ready", variant: "default" as const };
  }

  if (status.service_running) {
    return { label: "Running", variant: "secondary" as const };
  }

  if (status.server_installed || status.client_installed) {
    return { label: "Installed", variant: "outline" as const };
  }

  return { label: "Not installed", variant: "outline" as const };
}

function getPHPMyAdminBadge(status: PHPMyAdminStatus | null) {
  if (!status) {
    return { label: "Unavailable", variant: "outline" as const };
  }

  if (status.installed) {
    return { label: "Installed", variant: "default" as const };
  }

  return { label: "Not installed", variant: "outline" as const };
}

function getPHPMyAdminServiceStatus(status: PHPMyAdminStatus | null) {
  if (status?.installed) {
    return { value: "Running", tone: "success" as const };
  }

  return { value: "Stopped", tone: "danger" as const };
}

function SectionCard({
  title,
  description,
  actions,
  children,
}: {
  title: string;
  description?: string;
  actions?: ReactNode;
  children: ReactNode;
}) {
  return (
    <section className="rounded-xl border border-[var(--app-border)] bg-[var(--app-bg-2)]">
      <div className="flex flex-col gap-3 border-b border-[var(--app-border)] px-5 py-4 sm:flex-row sm:items-start sm:justify-between">
        <div className="space-y-1">
          <h2 className="text-base font-semibold tracking-tight text-[var(--app-text)]">{title}</h2>
          {description ? (
            <p className="max-w-2xl text-sm text-[var(--app-text-muted)]">{description}</p>
          ) : null}
        </div>
        {actions ? <div className="flex flex-wrap items-center gap-2">{actions}</div> : null}
      </div>
      <div className="px-5 py-5">{children}</div>
    </section>
  );
}

function ApplicationCard({
  icon,
  name,
  summary,
  badge,
  meta,
  actions,
}: {
  icon: ReactNode;
  name: string;
  summary: string;
  badge: { label: string; variant: "default" | "secondary" | "destructive" | "outline" };
  meta: Array<{ label: string; value: string; mono?: boolean; tone?: "success" | "danger" }>;
  actions: ReactNode;
}) {
  return (
    <section className="rounded-xl border border-[var(--app-border)] bg-[var(--app-bg-2)]">
        <div className="flex min-w-0 items-start gap-3 px-4 py-4">
        <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg border border-[var(--app-border)] bg-[var(--app-surface-muted)] text-[var(--app-text-muted)]">
          {icon}
        </div>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <h2 className="text-sm font-semibold tracking-tight text-[var(--app-text)]">{name}</h2>
            <Badge variant={badge.variant}>{badge.label}</Badge>
          </div>
          <div className="mt-1 text-sm font-medium text-[var(--app-text)]">{summary}</div>
          {meta.length > 0 ? (
            <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-[var(--app-text-muted)]">
              {meta.map((item) => (
                <span
                  key={item.label}
                  className={cn("truncate", item.mono && "font-mono")}
                  title={`${item.label}: ${item.value}`}
                >
                  {item.label}:{" "}
                  {item.tone ? (
                    <Badge
                      variant="outline"
                      className={cn(
                        statusMetaBadgeClassName,
                        item.tone === "success" &&
                          "border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-900/60 dark:bg-emerald-950/40 dark:text-emerald-300",
                        item.tone === "danger" &&
                          "border-red-200 bg-red-50 text-red-700 dark:border-red-900/60 dark:bg-red-950/40 dark:text-red-300"
                      )}
                    >
                      {item.value}
                    </Badge>
                  ) : (
                    item.value
                  )}
                </span>
              ))}
            </div>
          ) : null}
        </div>
      </div>

      <div className="flex flex-wrap items-center gap-2 border-t border-[var(--app-border)] px-4 py-3">
        {actions}
      </div>
    </section>
  );
}

export function ApplicationsPage() {
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [pageError, setPageError] = useState<string | null>(null);

  const [phpStatus, setPHPStatus] = useState<PHPStatus | null>(null);
  const [mariadbStatus, setMariaDBStatus] = useState<MariaDBStatus | null>(null);
  const [phpMyAdminStatus, setPHPMyAdminStatus] = useState<PHPMyAdminStatus | null>(null);

  const [runningAction, setRunningAction] = useState<
    | "install-mariadb"
    | "install-php"
    | "install-phpmyadmin"
    | "start-mariadb"
    | "stop-mariadb"
    | "restart-mariadb"
    | "start-php"
    | "stop-php"
    | "restart-php"
    | null
  >(null);

  async function loadPage(options?: {
    showLoading?: boolean;
    showRefreshToast?: boolean;
    ignoreIfUnmounted?: () => boolean;
  }) {
    const showLoading = options?.showLoading ?? false;
    if (showLoading) {
      setLoading(true);
    } else {
      setRefreshing(true);
    }

    setPageError(null);

    const nextErrors: string[] = [];
    const [phpResult, mariadbResult, phpMyAdminResult] = await Promise.allSettled([
      fetchPHPStatus(),
      fetchMariaDBStatus(),
      fetchPHPMyAdminStatus(),
    ]);
    if (options?.ignoreIfUnmounted?.()) {
      return;
    }

    if (phpResult.status === "fulfilled") {
      setPHPStatus(phpResult.value);
    } else {
      setPHPStatus(null);
      nextErrors.push(getErrorMessage(phpResult.reason, "Failed to inspect PHP."));
    }

    if (mariadbResult.status === "fulfilled") {
      setMariaDBStatus(mariadbResult.value);
    } else {
      setMariaDBStatus(null);
      nextErrors.push(getErrorMessage(mariadbResult.reason, "Failed to inspect MariaDB."));
    }

    if (phpMyAdminResult.status === "fulfilled") {
      setPHPMyAdminStatus(phpMyAdminResult.value);
    } else {
      setPHPMyAdminStatus(null);
      nextErrors.push(getErrorMessage(phpMyAdminResult.reason, "Failed to inspect phpMyAdmin."));
    }

    setPageError(nextErrors.length > 0 ? nextErrors.join(" ") : null);

    if (options?.showRefreshToast) {
      if (nextErrors.length > 0) {
        toast.error("Applications page refreshed with warnings.");
      } else {
        toast.success("Applications refreshed.");
      }
    }

    if (showLoading) {
      setLoading(false);
    } else {
      setRefreshing(false);
    }
  }

  useEffect(() => {
    let unmounted = false;

    void loadPage({
      showLoading: true,
      ignoreIfUnmounted: () => unmounted,
    });

    return () => {
      unmounted = true;
    };
  }, []);

  const phpMyAdminInstallBlocked = !mariadbStatus?.server_installed;
  const phpMyAdminServiceStatus = getPHPMyAdminServiceStatus(phpMyAdminStatus);

  async function handlePHPInstall() {
    setRunningAction("install-php");
    setPageError(null);

    try {
      const nextStatus = await installPHP();
      setPHPStatus(nextStatus);
      toast.success("PHP installed.");
    } catch (error) {
      const message = getErrorMessage(error, "Failed to install PHP.");
      setPageError(message);
      toast.error(message);
    } finally {
      setRunningAction(null);
    }
  }

  async function handlePHPStart() {
    setRunningAction("start-php");
    setPageError(null);

    try {
      const nextStatus = await startPHP();
      setPHPStatus(nextStatus);
      toast.success("PHP-FPM started.");
    } catch (error) {
      const message = getErrorMessage(error, "Failed to start PHP-FPM.");
      setPageError(message);
      toast.error(message);
    } finally {
      setRunningAction(null);
    }
  }

  async function handlePHPStop() {
    setRunningAction("stop-php");
    setPageError(null);

    try {
      const nextStatus = await stopPHP();
      setPHPStatus(nextStatus);
      toast.success("PHP-FPM stopped.");
    } catch (error) {
      const message = getErrorMessage(error, "Failed to stop PHP-FPM.");
      setPageError(message);
      toast.error(message);
    } finally {
      setRunningAction(null);
    }
  }

  async function handlePHPRestart() {
    setRunningAction("restart-php");
    setPageError(null);

    try {
      const nextStatus = await restartPHP();
      setPHPStatus(nextStatus);
      toast.success("PHP-FPM restarted.");
    } catch (error) {
      const message = getErrorMessage(error, "Failed to restart PHP-FPM.");
      setPageError(message);
      toast.error(message);
    } finally {
      setRunningAction(null);
    }
  }

  async function handleMariaDBInstall() {
    setRunningAction("install-mariadb");
    setPageError(null);

    try {
      const nextStatus = await installMariaDB();
      setMariaDBStatus(nextStatus);
      toast.success("MariaDB installed.");
    } catch (error) {
      const message = getErrorMessage(error, "Failed to install MariaDB.");
      setPageError(message);
      toast.error(message);
    } finally {
      setRunningAction(null);
    }
  }

  async function handleMariaDBStart() {
    setRunningAction("start-mariadb");
    setPageError(null);

    try {
      const nextStatus = await startMariaDB();
      setMariaDBStatus(nextStatus);
      toast.success("MariaDB started.");
    } catch (error) {
      const message = getErrorMessage(error, "Failed to start MariaDB.");
      setPageError(message);
      toast.error(message);
    } finally {
      setRunningAction(null);
    }
  }

  async function handleMariaDBStop() {
    setRunningAction("stop-mariadb");
    setPageError(null);

    try {
      const nextStatus = await stopMariaDB();
      setMariaDBStatus(nextStatus);
      toast.success("MariaDB stopped.");
    } catch (error) {
      const message = getErrorMessage(error, "Failed to stop MariaDB.");
      setPageError(message);
      toast.error(message);
    } finally {
      setRunningAction(null);
    }
  }

  async function handleMariaDBRestart() {
    setRunningAction("restart-mariadb");
    setPageError(null);

    try {
      const nextStatus = await restartMariaDB();
      setMariaDBStatus(nextStatus);
      toast.success("MariaDB restarted.");
    } catch (error) {
      const message = getErrorMessage(error, "Failed to restart MariaDB.");
      setPageError(message);
      toast.error(message);
    } finally {
      setRunningAction(null);
    }
  }

  async function handlePHPMyAdminInstall() {
    setRunningAction("install-phpmyadmin");
    setPageError(null);

    try {
      const nextStatus = await installPHPMyAdmin();
      setPHPMyAdminStatus(nextStatus);
      toast.success("phpMyAdmin installed.");
    } catch (error) {
      const message = getErrorMessage(error, "Failed to install phpMyAdmin.");
      setPageError(message);
      toast.error(message);
    } finally {
      setRunningAction(null);
    }
  }

  if (loading) {
    return (
      <>
        <PageHeader
          title="Applications"
          meta="Install and configure the local runtimes FlowPanel manages."
        />
        <div className="px-4 pb-6 sm:px-6 lg:px-8">
          <section className="rounded-xl border border-[var(--app-border)] bg-[var(--app-bg-2)] px-5 py-8 text-sm text-[var(--app-text-muted)]">
            <div className="flex items-center gap-2">
              <LoaderCircle className="h-4 w-4 animate-spin" />
              Inspecting application runtimes...
            </div>
          </section>
        </div>
      </>
    );
  }

  return (
    <>
      <PageHeader
        title="Applications"
        meta="Install, configure, and monitor the shared runtimes available on this node."
        actions={
          <Button
            type="button"
            variant="outline"
            onClick={() => {
              void loadPage({ showRefreshToast: true });
            }}
            disabled={refreshing || runningAction !== null}
          >
            {refreshing ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <RefreshCw className="h-4 w-4" />}
            Refresh
          </Button>
        }
      />

      <div className="space-y-5 px-4 pb-6 sm:px-6 lg:px-8">
        {pageError ? (
          <section className="rounded-xl border border-[var(--app-danger)]/30 bg-[var(--app-danger-soft)] px-4 py-3 text-[13px] text-[var(--app-danger)]">
            {pageError}
          </section>
        ) : null}

        <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
          <ApplicationCard
            icon={<TerminalSquare className="h-5 w-5" />}
            name="PHP"
            summary={formatPHPVersion(phpStatus)}
            badge={getPHPBadge(phpStatus)}
            meta={[
              {
                label: "Service",
                value: phpStatus?.service_running ? "Running" : "Stopped",
                tone: phpStatus?.service_running ? "success" : "danger",
              },
            ]}
            actions={
              <>
                {phpStatus?.install_available ? (
                  <Button
                    type="button"
                    size="sm"
                    className={compactActionButtonClassName}
                    onClick={() => {
                      void handlePHPInstall();
                    }}
                    disabled={runningAction !== null}
                  >
                    {runningAction === "install-php" ? (
                      <LoaderCircle className="h-4 w-4 animate-spin" />
                    ) : (
                      <Package className="h-4 w-4" />
                    )}
                    {phpStatus.install_label ?? "Install PHP"}
                  </Button>
                ) : null}
                {phpStatus?.start_available ? (
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    className={compactActionButtonClassName}
                    onClick={() => {
                      void handlePHPStart();
                    }}
                    disabled={runningAction !== null}
                  >
                    {runningAction === "start-php" ? (
                      <LoaderCircle className="h-4 w-4 animate-spin" />
                    ) : (
                      <PlayerPlayFilled className="h-4 w-4" />
                    )}
                    Start
                  </Button>
                ) : null}
                {phpStatus?.stop_available ? (
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    className={compactActionButtonClassName}
                    onClick={() => {
                      void handlePHPStop();
                    }}
                    disabled={runningAction !== null}
                  >
                    {runningAction === "stop-php" ? (
                      <LoaderCircle className="h-4 w-4 animate-spin" />
                    ) : (
                      <PlayerStop className="h-4 w-4" />
                    )}
                    Stop
                  </Button>
                ) : null}
                {phpStatus?.restart_available ? (
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    className={compactActionButtonClassName}
                    onClick={() => {
                      void handlePHPRestart();
                    }}
                    disabled={runningAction !== null}
                  >
                    {runningAction === "restart-php" ? (
                      <LoaderCircle className="h-4 w-4 animate-spin" />
                    ) : (
                      <RotateCcw className="h-4 w-4" />
                    )}
                    Restart
                  </Button>
                ) : null}
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  className={compactActionButtonClassName}
                  disabled
                  title="Runtime removal is not supported yet."
                >
                  <Trash2 className="h-4 w-4" />
                  Remove
                </Button>
              </>
            }
          />

          <ApplicationCard
            icon={<Database className="h-5 w-5" />}
            name="MariaDB"
            summary={formatMariaDBValue(mariadbStatus)}
            badge={getMariaDBBadge(mariadbStatus)}
            meta={[
              {
                label: "Service",
                value: mariadbStatus?.service_running ? "Running" : "Stopped",
                tone: mariadbStatus?.service_running ? "success" : "danger",
              },
            ]}
            actions={
              <>
                {mariadbStatus?.install_available ? (
                  <Button
                    type="button"
                    size="sm"
                    className={compactActionButtonClassName}
                    onClick={() => {
                      void handleMariaDBInstall();
                    }}
                    disabled={runningAction !== null}
                  >
                    {runningAction === "install-mariadb" ? (
                      <LoaderCircle className="h-4 w-4 animate-spin" />
                    ) : (
                      <Package className="h-4 w-4" />
                    )}
                    {mariadbStatus.install_label ?? "Install MariaDB"}
                  </Button>
                ) : null}
                {mariadbStatus?.start_available ? (
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    className={compactActionButtonClassName}
                    onClick={() => {
                      void handleMariaDBStart();
                    }}
                    disabled={runningAction !== null}
                  >
                    {runningAction === "start-mariadb" ? (
                      <LoaderCircle className="h-4 w-4 animate-spin" />
                    ) : (
                      <PlayerPlayFilled className="h-4 w-4" />
                    )}
                    Start
                  </Button>
                ) : null}
                {mariadbStatus?.stop_available ? (
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    className={compactActionButtonClassName}
                    onClick={() => {
                      void handleMariaDBStop();
                    }}
                    disabled={runningAction !== null}
                  >
                    {runningAction === "stop-mariadb" ? (
                      <LoaderCircle className="h-4 w-4 animate-spin" />
                    ) : (
                      <PlayerStop className="h-4 w-4" />
                    )}
                    Stop
                  </Button>
                ) : null}
                {mariadbStatus?.restart_available ? (
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    className={compactActionButtonClassName}
                    onClick={() => {
                      void handleMariaDBRestart();
                    }}
                    disabled={runningAction !== null}
                  >
                    {runningAction === "restart-mariadb" ? (
                      <LoaderCircle className="h-4 w-4 animate-spin" />
                    ) : (
                      <RotateCcw className="h-4 w-4" />
                    )}
                    Restart
                  </Button>
                ) : null}
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  className={compactActionButtonClassName}
                  disabled
                  title="Runtime removal is not supported yet."
                >
                  <Trash2 className="h-4 w-4" />
                  Remove
                </Button>
              </>
            }
          />

          <ApplicationCard
            icon={<LayoutDashboard className="h-5 w-5" />}
            name="phpMyAdmin"
            summary={formatPHPMyAdminValue(phpMyAdminStatus)}
            badge={getPHPMyAdminBadge(phpMyAdminStatus)}
            meta={[{ label: "Service status", value: phpMyAdminServiceStatus.value, tone: phpMyAdminServiceStatus.tone }]}
            actions={
              <>
                {phpMyAdminStatus?.install_available ? (
                  <Button
                    type="button"
                    size="sm"
                    className={compactActionButtonClassName}
                    onClick={() => {
                      void handlePHPMyAdminInstall();
                    }}
                    disabled={runningAction !== null || phpMyAdminInstallBlocked}
                    title={phpMyAdminInstallBlocked ? "Install MariaDB first." : undefined}
                  >
                    {runningAction === "install-phpmyadmin" ? (
                      <LoaderCircle className="h-4 w-4 animate-spin" />
                    ) : (
                      <Package className="h-4 w-4" />
                    )}
                    {phpMyAdminStatus.install_label ?? "Install phpMyAdmin"}
                  </Button>
                ) : null}
                {phpMyAdminStatus?.installed ? (
                  <Button asChild type="button" variant="outline" size="sm" className={compactActionButtonClassName}>
                    <a href="/phpmyadmin/" target="_blank" rel="noreferrer">
                      <ExternalLink className="h-4 w-4" />
                      Open
                    </a>
                  </Button>
                ) : (
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    className={compactActionButtonClassName}
                    disabled
                  >
                    <ExternalLink className="h-4 w-4" />
                    Open
                  </Button>
                )}
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  className={compactActionButtonClassName}
                  disabled
                  title="Runtime removal is not supported yet."
                >
                  <Trash2 className="h-4 w-4" />
                  Remove
                </Button>
              </>
            }
          />
        </div>

      </div>
    </>
  );
}
