import { useEffect, useState, type ReactNode } from "react";
import {
  fetchMariaDBStatus,
  installMariaDB,
  removeMariaDB,
  restartMariaDB,
  startMariaDB,
  stopMariaDB,
  type MariaDBStatus,
} from "@/api/mariadb";
import {
  fetchPHPStatus,
  installPHP,
  removePHP,
  restartPHP,
  startPHP,
  stopPHP,
  type PHPRuntimeStatus,
  type PHPStatus,
} from "@/api/php";
import {
  fetchPHPMyAdminStatus,
  installPHPMyAdmin,
  removePHPMyAdmin,
  type PHPMyAdminStatus,
} from "@/api/phpmyadmin";
import { ActionConfirmDialog } from "@/components/action-confirm-dialog";
import { PHPMyAdminSettingsDialog } from "@/components/phpmyadmin-settings-dialog";
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
  Settings,
  TerminalSquare,
  Trash2,
} from "@/components/icons/tabler-icons";
import { PageHeader } from "@/components/page-header";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { cn } from "@/lib/utils";
import Skeleton, { SkeletonTheme } from "react-loading-skeleton";
import { toast } from "sonner";

const compactActionButtonClassName = "h-7 gap-1.5 px-2.5 text-xs";
const statusMetaBadgeClassName = "h-5 rounded-sm px-1.5 py-0 text-[11px] font-medium";
type RemovableApplication =
  | { kind: "php"; version: string }
  | { kind: "mariadb" }
  | { kind: "phpmyadmin" };
type RuntimeState = string | null | undefined;

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

function getRuntimeActionLabel(state: RuntimeState) {
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

function isRuntimeActionState(state: RuntimeState) {
  return getRuntimeActionLabel(state) !== null;
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

  return "";
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

  return "";
}

function getPHPRuntimeBadge(status: PHPRuntimeStatus) {
  const actionLabel = getRuntimeActionLabel(status.state);
  if (actionLabel) {
    return { label: actionLabel.replace("...", ""), variant: "secondary" as const };
  }

  if (status.ready) {
    return { label: "Ready", variant: "default" as const };
  }

  if (status.service_running) {
    return { label: "Running", variant: "secondary" as const };
  }

  if (status.php_installed || status.fpm_installed) {
    return { label: "Installed", variant: "outline" as const };
  }

  return { label: "Not installed", variant: "outline" as const };
}

function getMariaDBBadge(status: MariaDBStatus | null) {
  if (!status) {
    return { label: "Unavailable", variant: "outline" as const };
  }

  const actionLabel = getRuntimeActionLabel(status.state);
  if (actionLabel) {
    return { label: actionLabel.replace("...", ""), variant: "secondary" as const };
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

  const actionLabel = getRuntimeActionLabel(status.state);
  if (actionLabel) {
    return { label: actionLabel.replace("...", ""), variant: "secondary" as const };
  }

  if (status.installed) {
    return { label: "Ready", variant: "default" as const };
  }

  return { label: "Not installed", variant: "outline" as const };
}

function getPHPMyAdminServiceStatus(status: PHPMyAdminStatus | null) {
  const actionLabel = getRuntimeActionLabel(status?.state);
  if (actionLabel) {
    return { value: actionLabel.replace("...", ""), tone: undefined };
  }

  if (status?.installed) {
    return { value: "Running", tone: "success" as const };
  }

  return { value: "Stopped", tone: "danger" as const };
}

function canRemovePHPRuntime(status: PHPRuntimeStatus | null) {
  if (!status) {
    return false;
  }

  return status.remove_available;
}

function canRemoveMariaDB(status: MariaDBStatus | null) {
  if (!status) {
    return false;
  }

  return status.remove_available;
}

function canRemovePHPMyAdmin(status: PHPMyAdminStatus | null) {
  if (!status) {
    return false;
  }

  return status.remove_available;
}

function ApplicationCard({
  icon,
  name,
  summary,
  badge,
  meta,
  actions,
  configAction,
}: {
  icon: ReactNode;
  name: string;
  summary: string;
  badge: { label: string; variant: "default" | "secondary" | "destructive" | "outline" };
  meta: Array<{ label?: string; value: ReactNode; mono?: boolean; tone?: "success" | "danger"; fullWidth?: boolean }>;
  actions: ReactNode;
  configAction?: ReactNode;
}) {
  return (
    <section className="rounded-xl border border-[var(--app-border)] bg-[var(--app-bg-2)]">
      <div className="relative px-4 py-4">
        <div className="flex min-w-0 w-full items-start gap-3">
          <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg border border-[var(--app-border)] bg-[var(--app-surface-muted)] text-[var(--app-text-muted)]">
            {icon}
          </div>
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-center gap-2 pr-10">
              <h2 className="text-sm font-semibold tracking-tight text-[var(--app-text)]">{name}</h2>
              <Badge variant={badge.variant}>{badge.label}</Badge>
            </div>
            {summary ? (
              <div className="mt-1 pr-10 text-sm font-medium text-[var(--app-text)]">{summary}</div>
            ) : null}
            {meta.length > 0 ? (
              <div className="mt-1 flex w-full flex-wrap items-center gap-x-3 gap-y-1 text-xs text-[var(--app-text-muted)]">
                {meta.map((item) => {
                  const Wrapper = item.fullWidth ? "div" : "span";

                  return (
                    <Wrapper
                      key={item.label ?? String(item.value)}
                      className={cn(
                        item.fullWidth ? "block w-full basis-full shrink-0" : "truncate",
                        item.mono && "font-mono"
                      )}
                      title={
                        typeof item.value === "string" && item.label
                          ? `${item.label}: ${item.value}`
                          : typeof item.value === "string"
                            ? item.value
                            : undefined
                      }
                    >
                      {item.label ? `${item.label}: ` : null}
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
                    </Wrapper>
                  );
                })}
              </div>
            ) : null}
          </div>
        </div>
        <div className="absolute right-4 top-4">
          {configAction ?? (
            <Button
              type="button"
              variant="ghost"
              size="icon"
              className="h-8 w-8 rounded-md text-[var(--app-text-muted)]"
              aria-label={`Configure ${name}`}
              title={`Configure ${name}`}
            >
              <Settings className="h-4 w-4" />
            </Button>
          )}
        </div>
      </div>

      <div className="flex flex-wrap items-center gap-2 border-t border-[var(--app-border)] px-4 py-3">
        {actions}
      </div>
    </section>
  );
}

function ApplicationCardSkeleton({ showConfigAction = false }: { showConfigAction?: boolean }) {
  return (
    <section className="rounded-xl border border-[var(--app-border)] bg-[var(--app-bg-2)]">
      <div className="relative px-4 py-4">
        <div className="flex min-w-0 w-full items-start gap-3">
          <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg border border-[var(--app-border)] bg-[var(--app-surface-muted)]">
            <Skeleton circle width={18} height={18} />
          </div>

          <div className="min-w-0 flex-1 pr-10">
            <div className="flex flex-wrap items-center gap-2">
              <Skeleton width={112} height={14} />
              <Skeleton width={74} height={20} borderRadius={6} />
            </div>
            <div className="mt-2">
              <Skeleton width="58%" height={18} />
            </div>
            <div className="mt-2 flex flex-wrap items-center gap-x-3 gap-y-2">
              <Skeleton width={120} height={12} />
              <Skeleton width={96} height={12} />
            </div>
          </div>
        </div>

        {showConfigAction ? (
          <div className="absolute right-4 top-4">
            <Skeleton width={32} height={32} borderRadius={8} />
          </div>
        ) : null}
      </div>

      <div className="flex flex-wrap items-center gap-2 border-t border-[var(--app-border)] px-4 py-3">
        <Skeleton width={104} height={28} borderRadius={8} />
        <Skeleton width={88} height={28} borderRadius={8} />
        <Skeleton width={92} height={28} borderRadius={8} />
      </div>
    </section>
  );
}

function ApplicationsPageSkeleton() {
  return (
    <SkeletonTheme
      baseColor="var(--app-surface-muted)"
      highlightColor="color-mix(in oklab, var(--app-bg-2) 82%, white)"
      borderRadius="0.5rem"
      duration={1.3}
    >
      <div className="space-y-5 px-4 pb-6 sm:px-6 lg:px-8">
        <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
          <ApplicationCardSkeleton />
          <ApplicationCardSkeleton />
          <ApplicationCardSkeleton showConfigAction />
        </div>
      </div>
    </SkeletonTheme>
  );
}

function phpRuntimeActionKey(action: "install" | "remove" | "start" | "stop" | "restart", version: string) {
  return `${action}-php-${version}`;
}

function getAvailablePHPVersions(status: PHPStatus | null) {
  const availableVersions = status?.available_versions?.filter((value) => value.trim().length > 0) ?? [];
  if (availableVersions.length > 0) {
    return availableVersions;
  }

  return (status?.versions ?? []).map((runtime) => runtime.version);
}

function getSelectedPHPRuntime(status: PHPStatus | null, version: string) {
  const runtimes = status?.versions ?? [];
  if (runtimes.length === 0) {
    return null;
  }

  const normalizedVersion = version.trim();
  if (normalizedVersion) {
    const selectedRuntime = runtimes.find((runtime) => runtime.version === normalizedVersion);
    if (selectedRuntime) {
      return selectedRuntime;
    }
  }

  const defaultVersion = status?.default_version?.trim();
  if (defaultVersion) {
    const defaultRuntime = runtimes.find((runtime) => runtime.version === defaultVersion);
    if (defaultRuntime) {
      return defaultRuntime;
    }
  }

  return runtimes[0];
}

function formatInstalledPHPRuntimeVersion(status: PHPRuntimeStatus | null) {
  if (!status?.php_installed) {
    return "\u00a0";
  }

  const version = status.php_version?.trim();
  if (!version) {
    return "\u00a0";
  }

  return extractVersionNumber(version, /\bPHP\s+(\d+(?:\.\d+)+)\b/i) ?? version;
}

function PHPRuntimeCard({
  status,
  availableVersions,
  selectedVersion,
  runningAction,
  disableActions,
  onVersionChange,
  onInstall,
  onStart,
  onStop,
  onRestart,
  onRemove,
}: {
  status: PHPRuntimeStatus | null;
  availableVersions: string[];
  selectedVersion: string;
  runningAction: string | null;
  disableActions: boolean;
  onVersionChange: (version: string) => void;
  onInstall: (version: string) => void;
  onStart: (version: string) => void;
  onStop: (version: string) => void;
  onRestart: (version: string) => void;
  onRemove: (version: string) => void;
}) {
  const badge = status ? getPHPRuntimeBadge(status) : { label: "Unavailable", variant: "outline" as const };
  const busyLabel = getRuntimeActionLabel(status?.state);
  const actionKey = (action: Parameters<typeof phpRuntimeActionKey>[0]) =>
    phpRuntimeActionKey(action, selectedVersion);
  const serviceValue =
    busyLabel?.replace("...", "") ??
    (status?.service_running ? "Running" : status?.fpm_installed ? "Installed" : "Stopped");
  const serviceTone = busyLabel ? undefined : status?.service_running ? "success" : "danger";
  const removeEnabled = canRemovePHPRuntime(status);

  return (
    <ApplicationCard
      icon={<TerminalSquare className="h-5 w-5" />}
      name="PHP"
      summary={formatInstalledPHPRuntimeVersion(status)}
      badge={badge}
      meta={[
        {
          fullWidth: true,
          value: (
            <div className="flex w-full items-center justify-between gap-3">
              <span className="flex items-center gap-1">
                <span>Service:</span>
                {serviceTone ? (
                  <Badge
                    variant="outline"
                    className={cn(
                      statusMetaBadgeClassName,
                      serviceTone === "success" &&
                        "border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-900/60 dark:bg-emerald-950/40 dark:text-emerald-300",
                      serviceTone === "danger" &&
                        "border-red-200 bg-red-50 text-red-700 dark:border-red-900/60 dark:bg-red-950/40 dark:text-red-300"
                    )}
                  >
                    {serviceValue}
                  </Badge>
                ) : (
                  <span>{serviceValue}</span>
                )}
              </span>
              <div className="w-[92px] shrink-0">
                <Select
                  value={selectedVersion}
                  onValueChange={onVersionChange}
                  disabled={availableVersions.length === 0}
                >
                  <SelectTrigger size="xs" className="w-full rounded-md">
                    <SelectValue placeholder="Select PHP" />
                  </SelectTrigger>
                  <SelectContent align="start">
                    {availableVersions.map((version) => (
                      <SelectItem key={version} value={version}>
                        PHP {version}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            </div>
          ),
        },
      ]}
      actions={
        <>
          {busyLabel ? (
            <Button type="button" variant="outline" size="sm" className={compactActionButtonClassName} disabled>
              <LoaderCircle className="h-4 w-4 animate-spin" />
              {busyLabel}
            </Button>
          ) : null}
          {status?.install_available ? (
            <Button
              type="button"
              size="sm"
              className={compactActionButtonClassName}
              onClick={() => onInstall(selectedVersion)}
              disabled={disableActions}
            >
              {runningAction === actionKey("install") ? (
                <LoaderCircle className="h-4 w-4 animate-spin" />
              ) : (
                <Package className="h-4 w-4" />
              )}
              Install
            </Button>
          ) : null}
          {status?.start_available ? (
            <Button
              type="button"
              variant="outline"
              size="sm"
              className={compactActionButtonClassName}
              onClick={() => onStart(selectedVersion)}
              disabled={disableActions}
            >
              {runningAction === actionKey("start") ? (
                <LoaderCircle className="h-4 w-4 animate-spin" />
              ) : (
                <PlayerPlayFilled className="h-4 w-4" />
              )}
              Start
            </Button>
          ) : null}
          {status?.stop_available ? (
            <Button
              type="button"
              variant="outline"
              size="sm"
              className={compactActionButtonClassName}
              onClick={() => onStop(selectedVersion)}
              disabled={disableActions}
            >
              {runningAction === actionKey("stop") ? (
                <LoaderCircle className="h-4 w-4 animate-spin" />
              ) : (
                <PlayerStop className="h-4 w-4" />
              )}
              Stop
            </Button>
          ) : null}
          {status?.restart_available ? (
            <Button
              type="button"
              variant="outline"
              size="sm"
              className={compactActionButtonClassName}
              onClick={() => onRestart(selectedVersion)}
              disabled={disableActions}
            >
              {runningAction === actionKey("restart") ? (
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
            onClick={() => onRemove(selectedVersion)}
            disabled={disableActions || !removeEnabled}
            title={removeEnabled ? undefined : "Runtime removal is only available for installed runtimes."}
          >
            {runningAction === actionKey("remove") ? (
              <LoaderCircle className="h-4 w-4 animate-spin" />
            ) : (
              <Trash2 className="h-4 w-4" />
            )}
            Remove
          </Button>
        </>
      }
    />
  );
}

export function ApplicationsPage() {
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [pageError, setPageError] = useState<string | null>(null);
  const [selectedPHPVersion, setSelectedPHPVersion] = useState("");

  const [phpStatus, setPHPStatus] = useState<PHPStatus | null>(null);
  const [mariadbStatus, setMariaDBStatus] = useState<MariaDBStatus | null>(null);
  const [phpMyAdminStatus, setPHPMyAdminStatus] = useState<PHPMyAdminStatus | null>(null);
  const [removeCandidate, setRemoveCandidate] = useState<RemovableApplication | null>(null);
  const [phpMyAdminSettingsOpen, setPHPMyAdminSettingsOpen] = useState(false);

  const [runningAction, setRunningAction] = useState<string | null>(null);

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
      void loadPage();
    }, 3_000);

    return () => {
      window.clearInterval(intervalId);
    };
  }, [runningAction, phpStatus?.state, mariadbStatus?.state, phpMyAdminStatus?.state]);

  useEffect(() => {
    const availableVersions = getAvailablePHPVersions(phpStatus);
    if (availableVersions.length === 0) {
      if (selectedPHPVersion !== "") {
        setSelectedPHPVersion("");
      }
      return;
    }

    if (availableVersions.includes(selectedPHPVersion)) {
      return;
    }

    const defaultVersion = phpStatus?.default_version?.trim();
    if (defaultVersion && availableVersions.includes(defaultVersion)) {
      setSelectedPHPVersion(defaultVersion);
      return;
    }

    setSelectedPHPVersion(availableVersions[0]);
  }, [phpStatus, selectedPHPVersion]);

  useEffect(() => {
    if (
      runningAction === "install-mariadb" &&
      (mariadbStatus?.server_installed || mariadbStatus?.client_installed)
    ) {
      setRunningAction(null);
      return;
    }
    if (
      runningAction === "remove-mariadb" &&
      mariadbStatus &&
      !mariadbStatus.server_installed &&
      !mariadbStatus.client_installed
    ) {
      setRemoveCandidate((current) => (current === "mariadb" ? null : current));
      setRunningAction(null);
      return;
    }
    if (runningAction === "start-mariadb" && mariadbStatus?.service_running) {
      setRunningAction(null);
      return;
    }
    if (runningAction === "stop-mariadb" && mariadbStatus?.server_installed && !mariadbStatus.service_running) {
      setRunningAction(null);
      return;
    }
    if (runningAction === "restart-mariadb" && mariadbStatus?.service_running) {
      setRunningAction(null);
      return;
    }
    if (runningAction === "install-phpmyadmin" && phpMyAdminStatus?.installed) {
      setRunningAction(null);
      return;
    }
    if (runningAction === "remove-phpmyadmin" && phpMyAdminStatus && !phpMyAdminStatus.installed) {
      setRemoveCandidate((current) => (current?.kind === "phpmyadmin" ? null : current));
      setRunningAction(null);
    }
  }, [runningAction, phpStatus, mariadbStatus, phpMyAdminStatus]);

  const phpMyAdminInstallBlocked = !mariadbStatus?.server_installed;
  const phpMyAdminServiceStatus = getPHPMyAdminServiceStatus(phpMyAdminStatus);
  const phpVersions = getAvailablePHPVersions(phpStatus);
  const selectedPHPRuntime = getSelectedPHPRuntime(phpStatus, selectedPHPVersion);
  const mariaDBRemoveEnabled = canRemoveMariaDB(mariadbStatus);
  const phpMyAdminRemoveEnabled = canRemovePHPMyAdmin(phpMyAdminStatus);
  const mariadbBusyLabel = getRuntimeActionLabel(mariadbStatus?.state);
  const phpMyAdminBusyLabel = getRuntimeActionLabel(phpMyAdminStatus?.state);
  const removeDialogDescription =
    removeCandidate?.kind === "php"
      ? `Remove PHP ${removeCandidate.version} from this node? Domains assigned to PHP ${removeCandidate.version} will stop serving until that runtime is installed again.`
      : removeCandidate?.kind === "mariadb"
        ? "Remove MariaDB from this node? Existing databases may become unavailable until MariaDB is installed again."
        : removeCandidate?.kind === "phpmyadmin"
          ? "Remove phpMyAdmin from this node? The browser database client will no longer be available."
          : "Remove this runtime?";
  const removeDialogTitle =
    removeCandidate?.kind === "php"
      ? `Remove PHP ${removeCandidate.version}`
      : removeCandidate?.kind === "mariadb"
        ? "Remove MariaDB"
        : removeCandidate?.kind === "phpmyadmin"
          ? "Remove phpMyAdmin"
          : "Remove application";
  const removeDialogConfirmText =
    removeCandidate?.kind === "php"
      ? `Remove PHP ${removeCandidate.version}`
      : removeCandidate?.kind === "mariadb"
        ? "Remove MariaDB"
        : removeCandidate?.kind === "phpmyadmin"
          ? "Remove phpMyAdmin"
          : "Remove";

  async function handleRemoveApplication() {
    if (removeCandidate === null) {
      return;
    }

    const target = removeCandidate;
    setRemoveCandidate(null);
    const action =
      target.kind === "php"
        ? phpRuntimeActionKey("remove", target.version)
        : target.kind === "mariadb"
          ? "remove-mariadb"
          : "remove-phpmyadmin";
    setRunningAction(action);
    setPageError(null);

    try {
      if (target.kind === "php") {
        const nextStatus = await removePHP(target.version);
        setPHPStatus(nextStatus);
        toast.success(`PHP ${target.version} removed.`);
      } else if (target.kind === "mariadb") {
        const nextStatus = await removeMariaDB();
        setMariaDBStatus(nextStatus);
        toast.success(
          !nextStatus.server_installed && !nextStatus.client_installed
            ? "MariaDB removed."
            : "MariaDB removal started.",
        );
      } else {
        const nextStatus = await removePHPMyAdmin();
        setPHPMyAdminStatus(nextStatus);
        toast.success(!nextStatus.installed ? "phpMyAdmin removed." : "phpMyAdmin removal started.");
      }
    } catch (error) {
      const fallback =
        target.kind === "php"
          ? `Failed to remove PHP ${target.version}.`
          : target.kind === "mariadb"
            ? "Failed to remove MariaDB."
            : "Failed to remove phpMyAdmin.";
      const message = getErrorMessage(error, fallback);
      setPageError(message);
      toast.error(message);
      setRunningAction(null);
    }
  }

  async function handlePHPInstall(version: string) {
    setRunningAction(phpRuntimeActionKey("install", version));
    setPageError(null);

    try {
      const nextStatus = await installPHP(version);
      setPHPStatus(nextStatus);
      toast.success(`PHP ${version} installed.`);
    } catch (error) {
      const message = getErrorMessage(error, `Failed to install PHP ${version}.`);
      setPageError(message);
      toast.error(message);
    } finally {
      setRunningAction(null);
    }
  }

  async function handlePHPStart(version: string) {
    setRunningAction(phpRuntimeActionKey("start", version));
    setPageError(null);

    try {
      const nextStatus = await startPHP(version);
      setPHPStatus(nextStatus);
      toast.success(`PHP ${version} FPM started.`);
    } catch (error) {
      const message = getErrorMessage(error, `Failed to start PHP ${version} FPM.`);
      setPageError(message);
      toast.error(message);
    } finally {
      setRunningAction(null);
    }
  }

  async function handlePHPStop(version: string) {
    setRunningAction(phpRuntimeActionKey("stop", version));
    setPageError(null);

    try {
      const nextStatus = await stopPHP(version);
      setPHPStatus(nextStatus);
      toast.success(`PHP ${version} FPM stopped.`);
    } catch (error) {
      const message = getErrorMessage(error, `Failed to stop PHP ${version} FPM.`);
      setPageError(message);
      toast.error(message);
    } finally {
      setRunningAction(null);
    }
  }

  async function handlePHPRestart(version: string) {
    setRunningAction(phpRuntimeActionKey("restart", version));
    setPageError(null);

    try {
      const nextStatus = await restartPHP(version);
      setPHPStatus(nextStatus);
      toast.success(`PHP ${version} FPM restarted.`);
    } catch (error) {
      const message = getErrorMessage(error, `Failed to restart PHP ${version} FPM.`);
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
          meta="Install, configure, and monitor the shared runtimes available on this node."
        />
        <ApplicationsPageSkeleton />
      </>
    );
  }

  return (
    <>
      <PHPMyAdminSettingsDialog
        open={phpMyAdminSettingsOpen}
        onOpenChange={setPHPMyAdminSettingsOpen}
        status={phpMyAdminStatus}
        onStatusChange={setPHPMyAdminStatus}
      />

      <ActionConfirmDialog
        open={removeCandidate !== null}
        onOpenChange={(open) => {
          if (!open) {
            setRemoveCandidate(null);
          }
        }}
        title={removeDialogTitle}
        desc={removeDialogDescription}
        confirmText={removeDialogConfirmText}
        destructive
        isLoading={
          (removeCandidate?.kind === "php" &&
            runningAction === phpRuntimeActionKey("remove", removeCandidate.version)) ||
          (removeCandidate?.kind === "mariadb" && runningAction === "remove-mariadb") ||
          (removeCandidate?.kind === "phpmyadmin" && runningAction === "remove-phpmyadmin")
        }
        handleConfirm={() => {
          void handleRemoveApplication();
        }}
        className="sm:max-w-md"
      />

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
          <PHPRuntimeCard
            status={selectedPHPRuntime}
            availableVersions={phpVersions}
            selectedVersion={selectedPHPVersion}
            runningAction={runningAction}
            disableActions={runningAction !== null}
            onVersionChange={setSelectedPHPVersion}
            onInstall={(version) => {
              void handlePHPInstall(version);
            }}
            onStart={(version) => {
              void handlePHPStart(version);
            }}
            onStop={(version) => {
              void handlePHPStop(version);
            }}
            onRestart={(version) => {
              void handlePHPRestart(version);
            }}
            onRemove={(version) => {
              setRemoveCandidate({ kind: "php", version });
            }}
          />

          <ApplicationCard
            icon={<Database className="h-5 w-5" />}
            name="MariaDB"
            summary={formatMariaDBValue(mariadbStatus)}
            badge={getMariaDBBadge(mariadbStatus)}
            meta={[
              {
                label: "Service",
                value:
                  mariadbBusyLabel?.replace("...", "") ??
                  (mariadbStatus?.service_running ? "Running" : "Stopped"),
                tone: mariadbBusyLabel ? undefined : mariadbStatus?.service_running ? "success" : "danger",
              },
            ]}
            actions={
              <>
                {mariadbBusyLabel ? (
                  <Button type="button" variant="outline" size="sm" className={compactActionButtonClassName} disabled>
                    <LoaderCircle className="h-4 w-4 animate-spin" />
                    {mariadbBusyLabel}
                  </Button>
                ) : null}
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
                  onClick={() => {
                    setRemoveCandidate({ kind: "mariadb" });
                  }}
                  disabled={runningAction !== null || !mariaDBRemoveEnabled}
                  title={mariaDBRemoveEnabled ? undefined : "Runtime removal is only available for installed runtimes."}
                >
                  {runningAction === "remove-mariadb" ? (
                    <LoaderCircle className="h-4 w-4 animate-spin" />
                  ) : (
                    <Trash2 className="h-4 w-4" />
                  )}
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
            configAction={
              <Button
                type="button"
                variant="ghost"
                size="icon"
                className="h-8 w-8 rounded-md text-[var(--app-text-muted)]"
                aria-label="Open phpMyAdmin settings"
                title="Open phpMyAdmin settings"
                onClick={() => setPHPMyAdminSettingsOpen(true)}
                disabled={phpMyAdminStatus === null}
              >
                <Settings className="h-4 w-4" />
              </Button>
            }
            actions={
              <>
                {phpMyAdminBusyLabel ? (
                  <Button type="button" variant="outline" size="sm" className={compactActionButtonClassName} disabled>
                    <LoaderCircle className="h-4 w-4 animate-spin" />
                    {phpMyAdminBusyLabel}
                  </Button>
                ) : null}
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
                  onClick={() => {
                    setRemoveCandidate({ kind: "phpmyadmin" });
                  }}
                  disabled={runningAction !== null || !phpMyAdminRemoveEnabled}
                  title={phpMyAdminRemoveEnabled ? undefined : "Runtime removal is only available for installed runtimes."}
                >
                  {runningAction === "remove-phpmyadmin" ? (
                    <LoaderCircle className="h-4 w-4 animate-spin" />
                  ) : (
                    <Trash2 className="h-4 w-4" />
                  )}
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
