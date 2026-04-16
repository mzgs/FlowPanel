import { useEffect, useRef, useState, type ReactNode } from "react";
import { fetchCaddyStatus, restartCaddy, type CaddyStatus } from "@/api/caddy";
import {
  fetchDockerStatus,
  installDocker,
  removeDocker,
  restartDocker,
  startDocker,
  stopDocker,
  type DockerStatus,
} from "@/api/docker";
import {
  fetchGolangStatus,
  installGolang,
  removeGolang,
  type GolangStatus,
} from "@/api/golang";
import {
  fetchNodeJSStatus,
  installNodeJS,
  removeNodeJS,
  type NodeJSStatus,
} from "@/api/nodejs";
import {
  fetchPM2ProcessLogs,
  fetchPM2Processes,
  fetchPM2Status,
  installPM2,
  restartPM2Process,
  removePM2,
  startPM2Process,
  stopPM2Process,
  type PM2Process,
  type PM2Status,
} from "@/api/pm2";
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
  fetchMongoDBStatus,
  installMongoDB,
  restartMongoDB,
  removeMongoDB,
  startMongoDB,
  stopMongoDB,
  type MongoDBStatus,
} from "@/api/mongodb";
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
import {
  fetchPostgreSQLStatus,
  installPostgreSQL,
  restartPostgreSQL,
  removePostgreSQL,
  startPostgreSQL,
  stopPostgreSQL,
  type PostgreSQLStatus,
} from "@/api/postgresql";
import {
  fetchRedisStatus,
  installRedis,
  restartRedis,
  removeRedis,
  startRedis,
  stopRedis,
  type RedisStatus,
} from "@/api/redis";
import { ActionConfirmDialog } from "@/components/action-confirm-dialog";
import { MariaDBSettingsDialog } from "@/components/mariadb-settings-dialog";
import { PHPSettingsDialog } from "@/components/php-settings-dialog";
import { PHPMyAdminSettingsDialog } from "@/components/phpmyadmin-settings-dialog";
import {
  Copy,
  ExternalLink,
  List,
  LoaderCircle,
  Package,
  PlayerPlayFilled,
  PlayerStop,
  RefreshCw,
  RotateCcw,
  Server,
  Settings,
  TerminalSquare,
  Trash2,
} from "@/components/icons/tabler-icons";
import { PageHeader } from "@/components/page-header";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
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
const postInstallServiceStartWaitMs = 30_000;
const pm2ProcessesRefreshIntervalMs = 10_000;
const applicationLogoFrameClassName =
  "flex h-11 w-16 shrink-0 items-center justify-center rounded-xl border border-[var(--app-border)] bg-[var(--app-surface-muted)] px-2";

const applicationLogos = {
  php: { src: "/application-icons/php.png", alt: "PHP logo", className: "h-6 w-full" },
  mariadb: { src: "/application-icons/mariadb.png", alt: "MariaDB logo", className: "h-8 w-full" },
  docker: { src: "/application-icons/docker.svg", alt: "Docker logo", className: "h-7 w-full" },
  redis: { src: "/application-icons/redis.svg", alt: "Redis logo", className: "h-7 w-full" },
  mongodb: { src: "/application-icons/mongodb.svg", alt: "MongoDB logo", className: "h-7 w-full" },
  postgresql: {
    src: "/application-icons/postgresql.svg",
    alt: "PostgreSQL logo",
    className: "h-7 w-full",
  },
  phpmyadmin: {
    src: "/application-icons/phpmyadmin.png",
    alt: "phpMyAdmin logo",
    className: "h-7 w-full",
  },
  go: { src: "/application-icons/go.png", alt: "Go logo", className: "h-7 w-full" },
  nodejs: { src: "/application-icons/nodejs.svg", alt: "Node.js logo", className: "h-7 w-full" },
  pm2: { src: "/application-icons/pm2.png", alt: "PM2 logo", className: "h-7 w-full" },
} as const;

type StatusMetaTone = "success" | "danger" | "info";
type RemovableApplication =
  | { kind: "php"; version: string }
  | { kind: "mariadb" }
  | { kind: "docker" }
  | { kind: "redis" }
  | { kind: "mongodb" }
  | { kind: "postgresql" }
  | { kind: "phpmyadmin" }
  | { kind: "golang" }
  | { kind: "nodejs" }
  | { kind: "pm2" };
type RuntimeState = string | null | undefined;
type InstallRemoveRuntimeStatus = {
  installed: boolean;
  binary_path?: string;
  version?: string;
  state: string;
  package_manager?: string;
  install_available: boolean;
  install_label?: string;
  remove_available: boolean;
  remove_label?: string;
};
type ServiceRuntimeStatus = InstallRemoveRuntimeStatus & {
  service_running: boolean;
  start_available: boolean;
  start_label?: string;
  stop_available: boolean;
  stop_label?: string;
  restart_available: boolean;
  restart_label?: string;
};

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

function getApplicationActionLabel(action: "install" | "start" | "stop" | "restart" | "remove") {
  switch (action) {
    case "install":
      return "Install";
    case "start":
      return "Start";
    case "stop":
      return "Stop";
    case "restart":
      return "Restart";
    case "remove":
      return "Remove";
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

function formatInstallRemoveRuntimeValue(status: InstallRemoveRuntimeStatus | null) {
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

function formatGolangValue(status: GolangStatus | null) {
  return formatInstallRemoveRuntimeValue(status);
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

function getInstallRemoveRuntimeBadge(status: InstallRemoveRuntimeStatus | null) {
  if (!status) {
    return { label: "Unavailable", variant: "outline" as const };
  }

  const actionLabel = getRuntimeActionLabel(status.state);
  if (actionLabel) {
    return { label: actionLabel.replace("...", ""), variant: "secondary" as const };
  }

  if (status.installed) {
    return { label: "Installed", variant: "default" as const };
  }

  return { label: "Not installed", variant: "outline" as const };
}

function getServiceRuntimeBadge(status: ServiceRuntimeStatus | null) {
  if (!status) {
    return { label: "Unavailable", variant: "outline" as const };
  }

  const actionLabel = getRuntimeActionLabel(status.state);
  if (actionLabel) {
    return { label: actionLabel.replace("...", ""), variant: "secondary" as const };
  }

  if (status.service_running) {
    return { label: "Running", variant: "default" as const };
  }

  if (status.installed) {
    return { label: "Installed", variant: "outline" as const };
  }

  return { label: "Not installed", variant: "outline" as const };
}

function getServiceRuntimeMeta(status: ServiceRuntimeStatus | null) {
  const actionLabel = getRuntimeActionLabel(status?.state);
  const value =
    actionLabel?.replace("...", "") ??
    (status?.service_running ? "Running" : status?.installed ? "Stopped" : "Not installed");
  const tone: StatusMetaTone | undefined = actionLabel
    ? undefined
    : status?.service_running
      ? "success"
      : status?.installed
        ? "danger"
        : undefined;

  return { value, tone };
}

function getGolangBadge(status: GolangStatus | null) {
  return getInstallRemoveRuntimeBadge(status);
}

function formatCaddyValue(status: CaddyStatus | null) {
  if (!status) {
    return "Unavailable";
  }

  const actionLabel = getRuntimeActionLabel(status.state);
  if (actionLabel) {
    return actionLabel;
  }

  if (!status.started) {
    return "Stopped";
  }

  if (status.configured_domains === 0) {
    return "No domains configured";
  }

  return `${status.configured_domains} domain${status.configured_domains === 1 ? "" : "s"} configured`;
}

function getCaddyBadge(status: CaddyStatus | null) {
  if (!status) {
    return { label: "Unavailable", variant: "outline" as const };
  }

  const actionLabel = getRuntimeActionLabel(status.state);
  if (actionLabel) {
    return { label: actionLabel.replace("...", ""), variant: "secondary" as const };
  }

  if (status.started) {
    return { label: "Running", variant: "default" as const };
  }

  return { label: "Stopped", variant: "outline" as const };
}

function getCaddyServiceMeta(status: CaddyStatus | null) {
  const actionLabel = getRuntimeActionLabel(status?.state);
  const value = actionLabel?.replace("...", "") ?? (status?.started ? "Running" : "Stopped");
  const tone: StatusMetaTone | undefined = actionLabel ? undefined : status?.started ? "success" : "danger";

  return { value, tone };
}

function formatNodeJSValue(status: NodeJSStatus | null) {
  return formatInstallRemoveRuntimeValue(status);
}

function getNodeJSBadge(status: NodeJSStatus | null) {
  return getInstallRemoveRuntimeBadge(status);
}

function formatPM2Value(status: PM2Status | null) {
  return formatInstallRemoveRuntimeValue(status);
}

function getPM2Badge(status: PM2Status | null) {
  return getInstallRemoveRuntimeBadge(status);
}

function formatPM2ProcessStatus(status: string) {
  const normalized = status.trim().toLowerCase();
  if (!normalized) {
    return "Unknown";
  }

  return normalized
    .split(/[\s_-]+/)
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(" ");
}

function getPM2ProcessStatusBadge(status: string) {
  const normalized = status.trim().toLowerCase();

  if (normalized === "online") {
    return { label: "Online", variant: "default" as const };
  }
  if (normalized === "launching" || normalized === "waiting restart") {
    return { label: formatPM2ProcessStatus(normalized), variant: "secondary" as const };
  }
  if (normalized === "errored") {
    return { label: "Errored", variant: "destructive" as const };
  }

  return { label: formatPM2ProcessStatus(normalized), variant: "outline" as const };
}

function formatPM2ProcessCPU(value: number) {
  if (!Number.isFinite(value) || value <= 0) {
    return "0%";
  }

  return `${value >= 10 ? value.toFixed(0) : value.toFixed(1)}%`;
}

function formatPM2ProcessMemory(bytes: number) {
  if (!Number.isFinite(bytes) || bytes <= 0) {
    return "0 B";
  }

  const units = ["B", "KB", "MB", "GB", "TB"];
  let value = bytes;
  let unitIndex = 0;

  while (value >= 1024 && unitIndex < units.length - 1) {
    value /= 1024;
    unitIndex += 1;
  }

  return `${value >= 10 || unitIndex === 0 ? value.toFixed(0) : value.toFixed(1)} ${units[unitIndex]}`;
}

function formatPM2ProcessUptime(process: PM2Process) {
  const status = process.status.trim().toLowerCase();
  if (status !== "online" && status !== "launching" && status !== "waiting restart") {
    return "-";
  }

  if (!process.uptime_unix_milli || process.uptime_unix_milli <= 0) {
    return "-";
  }

  const elapsed = Date.now() - process.uptime_unix_milli;
  if (!Number.isFinite(elapsed) || elapsed <= 0) {
    return "Just now";
  }

  const minute = 60_000;
  const hour = 60 * minute;
  const day = 24 * hour;

  if (elapsed >= day) {
    const days = Math.floor(elapsed / day);
    const hours = Math.floor((elapsed % day) / hour);
    return hours > 0 ? `${days}d ${hours}h` : `${days}d`;
  }
  if (elapsed >= hour) {
    const hours = Math.floor(elapsed / hour);
    const minutes = Math.floor((elapsed % hour) / minute);
    return minutes > 0 ? `${hours}h ${minutes}m` : `${hours}h`;
  }
  if (elapsed >= minute) {
    return `${Math.floor(elapsed / minute)}m`;
  }

  return `${Math.max(1, Math.floor(elapsed / 1000))}s`;
}

function canStartPM2Process(process: PM2Process) {
  const status = process.status.trim().toLowerCase();
  return status !== "online" && status !== "launching";
}

function canStopPM2Process(process: PM2Process) {
  const status = process.status.trim().toLowerCase();
  return status === "online" || status === "launching" || status === "waiting restart";
}

function canRestartPM2Process(process: PM2Process) {
  const status = process.status.trim().toLowerCase();
  return status === "online" || status === "launching" || status === "waiting restart";
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

function canRemoveInstallRemoveRuntime(status: InstallRemoveRuntimeStatus | null) {
  if (!status) {
    return false;
  }

  return status.remove_available;
}

function canRemoveGolang(status: GolangStatus | null) {
  return canRemoveInstallRemoveRuntime(status);
}

function canRemoveNodeJS(status: NodeJSStatus | null) {
  return canRemoveInstallRemoveRuntime(status);
}

function canRemovePM2(status: PM2Status | null) {
  return canRemoveInstallRemoveRuntime(status);
}

function ApplicationLogo({ app }: { app: keyof typeof applicationLogos }) {
  const logo = applicationLogos[app];

  return <img src={logo.src} alt={logo.alt} className={cn("object-contain", logo.className)} />;
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
  meta: Array<{ label?: string; value: ReactNode; mono?: boolean; tone?: StatusMetaTone; fullWidth?: boolean }>;
  actions: ReactNode;
  configAction?: ReactNode;
}) {
  const hasConfigAction = configAction !== null;

  return (
    <section className="rounded-xl border border-[var(--app-border)] bg-[var(--app-bg-2)]">
      <div className="relative px-4 py-4">
        <div className="flex min-w-0 w-full items-start gap-3">
          <div className={applicationLogoFrameClassName}>
            {icon}
          </div>
          <div className="min-w-0 flex-1">
            <div className={cn("flex flex-wrap items-center gap-2", hasConfigAction && "pr-10")}>
              <h2 className="text-sm font-semibold tracking-tight text-[var(--app-text)]">{name}</h2>
              <Badge variant={badge.variant}>{badge.label}</Badge>
            </div>
            {summary ? (
              <div className={cn("mt-1 text-sm font-medium text-[var(--app-text)]", hasConfigAction && "pr-10")}>
                {summary}
              </div>
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
                              "border-red-200 bg-red-50 text-red-700 dark:border-red-900/60 dark:bg-red-950/40 dark:text-red-300",
                            item.tone === "info" &&
                              "border-sky-200 bg-sky-50 text-sky-700 dark:border-sky-900/60 dark:bg-sky-950/40 dark:text-sky-300"
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
        {hasConfigAction ? (
          <div className="absolute right-4 top-4">
            {configAction === undefined ? (
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
            ) : (
              configAction
            )}
          </div>
        ) : null}
      </div>

      <div className="flex flex-wrap items-center gap-2 border-t border-[var(--app-border)] px-4 py-3">
        {actions}
      </div>
    </section>
  );
}

function InstallRemoveApplicationCard({
  app,
  name,
  status,
  runningAction,
  installActionKey,
  removeActionKey,
  meta,
  removeTitle,
  onInstall,
  onRemove,
}: {
  app: keyof typeof applicationLogos;
  name: string;
  status: InstallRemoveRuntimeStatus | null;
  runningAction: string | null;
  installActionKey: string;
  removeActionKey: string;
  meta: Array<{ label?: string; value: ReactNode; mono?: boolean; tone?: StatusMetaTone; fullWidth?: boolean }>;
  removeTitle: string;
  onInstall: () => void;
  onRemove: () => void;
}) {
  const busyLabel = getRuntimeActionLabel(status?.state);
  const removeEnabled = canRemoveInstallRemoveRuntime(status);

  return (
    <ApplicationCard
      icon={<ApplicationLogo app={app} />}
      name={name}
      summary={formatInstallRemoveRuntimeValue(status)}
      badge={getInstallRemoveRuntimeBadge(status)}
      meta={meta}
      configAction={null}
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
              onClick={onInstall}
              disabled={runningAction !== null}
            >
              {runningAction === installActionKey ? (
                <LoaderCircle className="h-4 w-4 animate-spin" />
              ) : (
                <Package className="h-4 w-4" />
              )}
              {getApplicationActionLabel("install")}
            </Button>
          ) : null}
          <Button
            type="button"
            variant="outline"
            size="sm"
            className={compactActionButtonClassName}
            onClick={onRemove}
            disabled={runningAction !== null || !removeEnabled}
            title={removeEnabled ? undefined : removeTitle}
          >
            {runningAction === removeActionKey ? (
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

function ServiceApplicationCard({
  app,
  name,
  status,
  runningAction,
  actionKeyPrefix,
  meta,
  removeTitle,
  onInstall,
  onStart,
  onStop,
  onRestart,
  onRemove,
}: {
  app: keyof typeof applicationLogos;
  name: string;
  status: ServiceRuntimeStatus | null;
  runningAction: string | null;
  actionKeyPrefix: string;
  meta: Array<{ label?: string; value: ReactNode; mono?: boolean; tone?: StatusMetaTone; fullWidth?: boolean }>;
  removeTitle: string;
  onInstall: () => void;
  onStart: () => void;
  onStop: () => void;
  onRestart: () => void;
  onRemove: () => void;
}) {
  const busyLabel = getRuntimeActionLabel(status?.state);
  const removeEnabled = canRemoveInstallRemoveRuntime(status);
  const serviceMeta = getServiceRuntimeMeta(status);

  return (
    <ApplicationCard
      icon={<ApplicationLogo app={app} />}
      name={name}
      summary={formatInstallRemoveRuntimeValue(status)}
      badge={getServiceRuntimeBadge(status)}
      meta={[
        {
          label: "Service",
          value: serviceMeta.value,
          tone: serviceMeta.tone,
        },
        ...meta,
      ]}
      configAction={null}
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
              onClick={onInstall}
              disabled={runningAction !== null}
            >
              {runningAction === `install-${actionKeyPrefix}` ? (
                <LoaderCircle className="h-4 w-4 animate-spin" />
              ) : (
                <Package className="h-4 w-4" />
              )}
              {getApplicationActionLabel("install")}
            </Button>
          ) : null}
          {status?.start_available ? (
            <Button
              type="button"
              variant="outline"
              size="sm"
              className={compactActionButtonClassName}
              onClick={onStart}
              disabled={runningAction !== null}
            >
              {runningAction === `start-${actionKeyPrefix}` ? (
                <LoaderCircle className="h-4 w-4 animate-spin" />
              ) : (
                <PlayerPlayFilled className="h-4 w-4" />
              )}
              {getApplicationActionLabel("start")}
            </Button>
          ) : null}
          {status?.stop_available ? (
            <Button
              type="button"
              variant="outline"
              size="sm"
              className={compactActionButtonClassName}
              onClick={onStop}
              disabled={runningAction !== null}
            >
              {runningAction === `stop-${actionKeyPrefix}` ? (
                <LoaderCircle className="h-4 w-4 animate-spin" />
              ) : (
                <PlayerStop className="h-4 w-4" />
              )}
              {getApplicationActionLabel("stop")}
            </Button>
          ) : null}
          {status?.restart_available ? (
            <Button
              type="button"
              variant="outline"
              size="sm"
              className={compactActionButtonClassName}
              onClick={onRestart}
              disabled={runningAction !== null}
            >
              {runningAction === `restart-${actionKeyPrefix}` ? (
                <LoaderCircle className="h-4 w-4 animate-spin" />
              ) : (
                <RotateCcw className="h-4 w-4" />
              )}
              {getApplicationActionLabel("restart")}
            </Button>
          ) : null}
          <Button
            type="button"
            variant="outline"
            size="sm"
            className={compactActionButtonClassName}
            onClick={onRemove}
            disabled={runningAction !== null || !removeEnabled}
            title={removeEnabled ? undefined : removeTitle}
          >
            {runningAction === `remove-${actionKeyPrefix}` ? (
              <LoaderCircle className="h-4 w-4 animate-spin" />
            ) : (
              <Trash2 className="h-4 w-4" />
            )}
            {getApplicationActionLabel("remove")}
          </Button>
        </>
      }
    />
  );
}

function ApplicationCardSkeleton({ showConfigAction = false }: { showConfigAction?: boolean }) {
  return (
    <section className="rounded-xl border border-[var(--app-border)] bg-[var(--app-bg-2)]">
      <div className="relative px-4 py-4">
        <div className="flex min-w-0 w-full items-start gap-3">
          <div className={applicationLogoFrameClassName}>
            <Skeleton width="100%" height={18} borderRadius={6} />
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
          <ApplicationCardSkeleton showConfigAction />
          <ApplicationCardSkeleton showConfigAction />
          <ApplicationCardSkeleton />
          <ApplicationCardSkeleton />
          <ApplicationCardSkeleton />
          <ApplicationCardSkeleton />
          <ApplicationCardSkeleton showConfigAction />
          <ApplicationCardSkeleton />
          <ApplicationCardSkeleton />
        </div>
      </div>
    </SkeletonTheme>
  );
}

function phpRuntimeActionKey(action: "install" | "remove" | "start" | "stop" | "restart", version: string) {
  return `${action}-php-${version}`;
}

function getPHPInstallActionVersion(runningAction: string | null) {
  const prefix = "install-php-";
  if (!runningAction?.startsWith(prefix)) {
    return null;
  }

  return runningAction.slice(prefix.length);
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

function getPHPRuntimeByVersion(status: PHPStatus | null, version: string) {
  const normalizedVersion = version.trim();
  if (!normalizedVersion) {
    return null;
  }

  return (status?.versions ?? []).find((runtime) => runtime.version === normalizedVersion) ?? null;
}

function shouldWaitForPHPServiceAfterInstall(status: PHPStatus, version: string) {
  const runtime = getPHPRuntimeByVersion(status, version);
  return Boolean(runtime?.fpm_installed && !runtime.service_running);
}

function shouldWaitForMariaDBServiceAfterInstall(status: MariaDBStatus) {
  return status.server_installed && !status.service_running;
}

function getPostInstallServiceStartingAction(
  runningAction: string | null,
  phpStatus: PHPStatus | null,
  mariadbStatus: MariaDBStatus | null,
) {
  if (runningAction === "install-mariadb" && mariadbStatus && shouldWaitForMariaDBServiceAfterInstall(mariadbStatus)) {
    return runningAction;
  }

  const installingPHPVersion = getPHPInstallActionVersion(runningAction);
  if (!installingPHPVersion) {
    return null;
  }

  const installingRuntime = getPHPRuntimeByVersion(phpStatus, installingPHPVersion);
  if (installingRuntime?.fpm_installed && !installingRuntime.service_running) {
    return runningAction;
  }

  return null;
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
  settingsDisabled,
  onVersionChange,
  onOpenSettings,
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
  settingsDisabled: boolean;
  onVersionChange: (version: string) => void;
  onOpenSettings: () => void;
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
  const serviceStartingAfterInstall =
    runningAction === actionKey("install") && Boolean(status?.fpm_installed) && !status?.service_running;
  const serviceValue =
    serviceStartingAfterInstall
      ? "Service starting..."
      : busyLabel?.replace("...", "") ??
        (status?.service_running ? "Running" : status?.fpm_installed ? "Installed" : "Stopped");
  const serviceTone: StatusMetaTone | undefined = serviceStartingAfterInstall
    ? "info"
    : busyLabel
      ? undefined
      : status?.service_running
        ? "success"
        : "danger";
  const removeEnabled = canRemovePHPRuntime(status);

  return (
    <ApplicationCard
      icon={<ApplicationLogo app="php" />}
      name="PHP"
      summary={formatInstalledPHPRuntimeVersion(status)}
      badge={badge}
      configAction={
        <Button
          type="button"
          variant="ghost"
          size="icon"
          className="h-8 w-8 rounded-md text-[var(--app-text-muted)]"
          aria-label="Open PHP settings"
          title="Open PHP settings"
          onClick={onOpenSettings}
          disabled={settingsDisabled}
        >
          <Settings className="h-4 w-4" />
        </Button>
      }
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
                        "border-red-200 bg-red-50 text-red-700 dark:border-red-900/60 dark:bg-red-950/40 dark:text-red-300",
                      serviceTone === "info" &&
                        "border-sky-200 bg-sky-50 text-sky-700 dark:border-sky-900/60 dark:bg-sky-950/40 dark:text-sky-300"
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

  const [caddyStatus, setCaddyStatus] = useState<CaddyStatus | null>(null);
  const [phpStatus, setPHPStatus] = useState<PHPStatus | null>(null);
  const [mariadbStatus, setMariaDBStatus] = useState<MariaDBStatus | null>(null);
  const [dockerStatus, setDockerStatus] = useState<DockerStatus | null>(null);
  const [redisStatus, setRedisStatus] = useState<RedisStatus | null>(null);
  const [mongoDBStatus, setMongoDBStatus] = useState<MongoDBStatus | null>(null);
  const [postgresqlStatus, setPostgreSQLStatus] = useState<PostgreSQLStatus | null>(null);
  const [phpMyAdminStatus, setPHPMyAdminStatus] = useState<PHPMyAdminStatus | null>(null);
  const [golangStatus, setGolangStatus] = useState<GolangStatus | null>(null);
  const [nodeJSStatus, setNodeJSStatus] = useState<NodeJSStatus | null>(null);
  const [pm2Status, setPM2Status] = useState<PM2Status | null>(null);
  const [pm2ListOpen, setPM2ListOpen] = useState(false);
  const [pm2Processes, setPM2Processes] = useState<PM2Process[]>([]);
  const [pm2ProcessesLoading, setPM2ProcessesLoading] = useState(false);
  const [pm2ProcessesError, setPM2ProcessesError] = useState<string | null>(null);
  const [pm2ProcessActionKey, setPM2ProcessActionKey] = useState<string | null>(null);
  const [pm2LogsOpen, setPM2LogsOpen] = useState(false);
  const [pm2LogsTarget, setPM2LogsTarget] = useState<PM2Process | null>(null);
  const [pm2LogsOutput, setPM2LogsOutput] = useState("");
  const [pm2LogsLoading, setPM2LogsLoading] = useState(false);
  const [pm2LogsError, setPM2LogsError] = useState<string | null>(null);
  const [removeCandidate, setRemoveCandidate] = useState<RemovableApplication | null>(null);
  const [phpSettingsOpen, setPHPSettingsOpen] = useState(false);
  const [mariaDBSettingsOpen, setMariaDBSettingsOpen] = useState(false);
  const [phpMyAdminSettingsOpen, setPHPMyAdminSettingsOpen] = useState(false);

  const [runningAction, setRunningAction] = useState<string | null>(null);
  const pm2ProcessListRequestIdRef = useRef(0);
  const pm2LogsRequestIdRef = useRef(0);
  const postInstallServiceStartingAction = getPostInstallServiceStartingAction(
    runningAction,
    phpStatus,
    mariadbStatus,
  );

  function resetPM2LogsState() {
    pm2LogsRequestIdRef.current += 1;
    setPM2LogsLoading(false);
    setPM2LogsError(null);
    setPM2LogsOutput("");
  }

  function handlePM2LogsOpenChange(open: boolean) {
    setPM2LogsOpen(open);
    if (!open) {
      setPM2LogsTarget(null);
      resetPM2LogsState();
    }
  }

  function resetPM2ProcessesState() {
    pm2ProcessListRequestIdRef.current += 1;
    setPM2ProcessesLoading(false);
    setPM2ProcessesError(null);
    setPM2ProcessActionKey(null);
    setPM2Processes([]);
    handlePM2LogsOpenChange(false);
  }

  function handlePM2ListOpenChange(open: boolean) {
    setPM2ListOpen(open);
    if (!open) {
      resetPM2ProcessesState();
    }
  }

  async function loadPM2Processes() {
    if (!pm2Status?.installed) {
      setPM2Processes([]);
      setPM2ProcessesError("PM2 is not installed.");
      return;
    }

    const requestId = pm2ProcessListRequestIdRef.current + 1;
    pm2ProcessListRequestIdRef.current = requestId;
    setPM2ProcessesLoading(true);
    setPM2ProcessesError(null);

    try {
      const processes = await fetchPM2Processes();
      if (pm2ProcessListRequestIdRef.current !== requestId) {
        return;
      }

      setPM2Processes(processes);
      setPM2LogsTarget((current) => {
        if (current === null) {
          return current;
        }

        return processes.find((process) => process.id === current.id) ?? current;
      });
    } catch (error) {
      if (pm2ProcessListRequestIdRef.current !== requestId) {
        return;
      }

      setPM2Processes([]);
      setPM2ProcessesError(getErrorMessage(error, "Failed to load PM2 processes."));
    } finally {
      if (pm2ProcessListRequestIdRef.current === requestId) {
        setPM2ProcessesLoading(false);
      }
    }
  }

  async function loadPM2Logs(process: PM2Process) {
    const requestId = pm2LogsRequestIdRef.current + 1;
    pm2LogsRequestIdRef.current = requestId;
    setPM2LogsLoading(true);
    setPM2LogsError(null);

    try {
      const output = await fetchPM2ProcessLogs(process.id);
      if (pm2LogsRequestIdRef.current !== requestId) {
        return;
      }

      setPM2LogsOutput(output.trim());
    } catch (error) {
      if (pm2LogsRequestIdRef.current !== requestId) {
        return;
      }

      setPM2LogsOutput("");
      setPM2LogsError(getErrorMessage(error, `Failed to load logs for ${process.name}.`));
    } finally {
      if (pm2LogsRequestIdRef.current === requestId) {
        setPM2LogsLoading(false);
      }
    }
  }

  function openPM2Logs(process: PM2Process) {
    setPM2LogsTarget(process);
    setPM2LogsOpen(true);
    resetPM2LogsState();
    void loadPM2Logs(process);
  }

  async function handlePM2ProcessAction(action: "start" | "stop" | "restart", process: PM2Process) {
    const actionKey = `${action}:${process.id}`;
    setPM2ProcessActionKey(actionKey);
    setPM2ProcessesError(null);

    const processLabel = process.name || `Process ${process.id}`;
    const successMessage =
      action === "start"
        ? `${processLabel} started.`
        : action === "stop"
          ? `${processLabel} stopped.`
          : `${processLabel} restarted.`;
    const fallbackMessage =
      action === "start"
        ? `Failed to start ${processLabel}.`
        : action === "stop"
          ? `Failed to stop ${processLabel}.`
          : `Failed to restart ${processLabel}.`;

    try {
      const processes =
        action === "start"
          ? await startPM2Process(process.id)
          : action === "stop"
            ? await stopPM2Process(process.id)
            : await restartPM2Process(process.id);
      setPM2Processes(processes);
      setPM2LogsTarget((current) => {
        if (current === null) {
          return current;
        }

        return processes.find((item) => item.id === current.id) ?? current;
      });
      toast.success(successMessage);
    } catch (error) {
      const message = getErrorMessage(error, fallbackMessage);
      setPM2ProcessesError(message);
      toast.error(message);
    } finally {
      setPM2ProcessActionKey((current) => (current === actionKey ? null : current));
    }
  }

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
    const [
      caddyResult,
      phpResult,
      mariadbResult,
      dockerResult,
      redisResult,
      mongoDBResult,
      postgresqlResult,
      phpMyAdminResult,
      golangResult,
      nodeJSResult,
      pm2Result,
    ] = await Promise.allSettled([
      fetchCaddyStatus(),
      fetchPHPStatus(),
      fetchMariaDBStatus(),
      fetchDockerStatus(),
      fetchRedisStatus(),
      fetchMongoDBStatus(),
      fetchPostgreSQLStatus(),
      fetchPHPMyAdminStatus(),
      fetchGolangStatus(),
      fetchNodeJSStatus(),
      fetchPM2Status(),
    ]);
    if (options?.ignoreIfUnmounted?.()) {
      return;
    }

    if (caddyResult.status === "fulfilled") {
      setCaddyStatus(caddyResult.value);
    } else {
      setCaddyStatus(null);
      nextErrors.push(getErrorMessage(caddyResult.reason, "Failed to inspect Caddy server."));
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

    if (dockerResult.status === "fulfilled") {
      setDockerStatus(dockerResult.value);
    } else {
      setDockerStatus(null);
      nextErrors.push(getErrorMessage(dockerResult.reason, "Failed to inspect Docker."));
    }

    if (redisResult.status === "fulfilled") {
      setRedisStatus(redisResult.value);
    } else {
      setRedisStatus(null);
      nextErrors.push(getErrorMessage(redisResult.reason, "Failed to inspect Redis."));
    }

    if (mongoDBResult.status === "fulfilled") {
      setMongoDBStatus(mongoDBResult.value);
    } else {
      setMongoDBStatus(null);
      nextErrors.push(getErrorMessage(mongoDBResult.reason, "Failed to inspect MongoDB."));
    }

    if (postgresqlResult.status === "fulfilled") {
      setPostgreSQLStatus(postgresqlResult.value);
    } else {
      setPostgreSQLStatus(null);
      nextErrors.push(getErrorMessage(postgresqlResult.reason, "Failed to inspect PostgreSQL."));
    }

    if (phpMyAdminResult.status === "fulfilled") {
      setPHPMyAdminStatus(phpMyAdminResult.value);
    } else {
      setPHPMyAdminStatus(null);
      nextErrors.push(getErrorMessage(phpMyAdminResult.reason, "Failed to inspect phpMyAdmin."));
    }

    if (golangResult.status === "fulfilled") {
      setGolangStatus(golangResult.value);
    } else {
      setGolangStatus(null);
      nextErrors.push(getErrorMessage(golangResult.reason, "Failed to inspect Go."));
    }

    if (nodeJSResult.status === "fulfilled") {
      setNodeJSStatus(nodeJSResult.value);
    } else {
      setNodeJSStatus(null);
      nextErrors.push(getErrorMessage(nodeJSResult.reason, "Failed to inspect Node.js."));
    }

    if (pm2Result.status === "fulfilled") {
      setPM2Status(pm2Result.value);
    } else {
      setPM2Status(null);
      nextErrors.push(getErrorMessage(pm2Result.reason, "Failed to inspect PM2."));
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
    return () => {
      pm2ProcessListRequestIdRef.current += 1;
      pm2LogsRequestIdRef.current += 1;
    };
  }, []);

  useEffect(() => {
    if (
      runningAction === null &&
      !isRuntimeActionState(caddyStatus?.state) &&
      !isRuntimeActionState(phpStatus?.state) &&
      !isRuntimeActionState(mariadbStatus?.state) &&
      !isRuntimeActionState(dockerStatus?.state) &&
      !isRuntimeActionState(redisStatus?.state) &&
      !isRuntimeActionState(mongoDBStatus?.state) &&
      !isRuntimeActionState(postgresqlStatus?.state) &&
      !isRuntimeActionState(phpMyAdminStatus?.state) &&
      !isRuntimeActionState(golangStatus?.state) &&
      !isRuntimeActionState(nodeJSStatus?.state) &&
      !isRuntimeActionState(pm2Status?.state)
    ) {
      return;
    }

    const intervalId = window.setInterval(() => {
      void loadPage();
    }, 3_000);

    return () => {
      window.clearInterval(intervalId);
    };
  }, [
    runningAction,
    caddyStatus?.state,
    phpStatus?.state,
    mariadbStatus?.state,
    dockerStatus?.state,
    redisStatus?.state,
    mongoDBStatus?.state,
    postgresqlStatus?.state,
    phpMyAdminStatus?.state,
    golangStatus?.state,
    nodeJSStatus?.state,
    pm2Status?.state,
  ]);

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
    if (!postInstallServiceStartingAction) {
      return;
    }

    const timeoutId = window.setTimeout(() => {
      setRunningAction((current) => (current === postInstallServiceStartingAction ? null : current));
    }, postInstallServiceStartWaitMs);

    return () => {
      window.clearTimeout(timeoutId);
    };
  }, [postInstallServiceStartingAction]);

  useEffect(() => {
    if (
      runningAction === "install-mariadb" &&
      mariadbStatus?.service_running
    ) {
      setRunningAction(null);
      return;
    }
    const installingPHPVersion = getPHPInstallActionVersion(runningAction);
    if (installingPHPVersion) {
      const installingRuntime = getPHPRuntimeByVersion(phpStatus, installingPHPVersion);
      if (installingRuntime?.service_running || installingRuntime?.ready) {
        setRunningAction(null);
      }
      return;
    }
    if (
      runningAction === "remove-mariadb" &&
      mariadbStatus &&
      !mariadbStatus.server_installed &&
      !mariadbStatus.client_installed
    ) {
      setRemoveCandidate((current) => (current?.kind === "mariadb" ? null : current));
      setRunningAction(null);
      return;
    }
    if (runningAction === "install-redis" && redisStatus?.installed) {
      setRunningAction(null);
      return;
    }
    if (runningAction === "install-docker" && dockerStatus?.installed) {
      setRunningAction(null);
      return;
    }
    if (runningAction === "remove-docker" && dockerStatus && !dockerStatus.installed) {
      setRemoveCandidate((current) => (current?.kind === "docker" ? null : current));
      setRunningAction(null);
      return;
    }
    if (runningAction === "remove-redis" && redisStatus && !redisStatus.installed) {
      setRemoveCandidate((current) => (current?.kind === "redis" ? null : current));
      setRunningAction(null);
      return;
    }
    if (runningAction === "install-mongodb" && mongoDBStatus?.installed) {
      setRunningAction(null);
      return;
    }
    if (runningAction === "remove-mongodb" && mongoDBStatus && !mongoDBStatus.installed) {
      setRemoveCandidate((current) => (current?.kind === "mongodb" ? null : current));
      setRunningAction(null);
      return;
    }
    if (runningAction === "install-postgresql" && postgresqlStatus?.installed) {
      setRunningAction(null);
      return;
    }
    if (runningAction === "remove-postgresql" && postgresqlStatus && !postgresqlStatus.installed) {
      setRemoveCandidate((current) => (current?.kind === "postgresql" ? null : current));
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
      return;
    }
    if (runningAction === "install-golang" && golangStatus?.installed) {
      setRunningAction(null);
      return;
    }
    if (runningAction === "remove-golang" && golangStatus && !golangStatus.installed) {
      setRemoveCandidate((current) => (current?.kind === "golang" ? null : current));
      setRunningAction(null);
      return;
    }
    if (runningAction === "install-nodejs" && nodeJSStatus?.installed) {
      setRunningAction(null);
      return;
    }
    if (runningAction === "remove-nodejs" && nodeJSStatus && !nodeJSStatus.installed) {
      setRemoveCandidate((current) => (current?.kind === "nodejs" ? null : current));
      setRunningAction(null);
      return;
    }
    if (runningAction === "install-pm2" && pm2Status?.installed) {
      setRunningAction(null);
      return;
    }
    if (runningAction === "remove-pm2" && pm2Status && !pm2Status.installed) {
      setRemoveCandidate((current) => (current?.kind === "pm2" ? null : current));
      setRunningAction(null);
    }
  }, [
    runningAction,
    phpStatus,
    mariadbStatus,
    dockerStatus,
    redisStatus,
    mongoDBStatus,
    postgresqlStatus,
    phpMyAdminStatus,
    golangStatus,
    nodeJSStatus,
    pm2Status,
  ]);

  useEffect(() => {
    if (!pm2ListOpen || !pm2Status?.installed) {
      return;
    }

    void loadPM2Processes();
  }, [pm2ListOpen, pm2Status?.installed]);

  useEffect(() => {
    if (!pm2ListOpen || !pm2Status?.installed) {
      return;
    }

    const intervalId = window.setInterval(() => {
      if (pm2ProcessesLoading || pm2ProcessActionKey !== null) {
        return;
      }

      void loadPM2Processes();
    }, pm2ProcessesRefreshIntervalMs);

    return () => {
      window.clearInterval(intervalId);
    };
  }, [pm2ListOpen, pm2Status?.installed, pm2ProcessesLoading, pm2ProcessActionKey]);

  useEffect(() => {
    if (!pm2LogsOpen || pm2LogsTarget === null || !pm2Status?.installed) {
      return;
    }

    const intervalId = window.setInterval(() => {
      if (pm2LogsLoading || pm2ProcessActionKey !== null) {
        return;
      }

      void loadPM2Logs(pm2LogsTarget);
    }, pm2ProcessesRefreshIntervalMs);

    return () => {
      window.clearInterval(intervalId);
    };
  }, [pm2LogsOpen, pm2LogsTarget, pm2LogsLoading, pm2ProcessActionKey, pm2Status?.installed]);

  useEffect(() => {
    if (!pm2ListOpen || pm2Status === null || pm2Status.installed) {
      return;
    }

    handlePM2ListOpenChange(false);
  }, [pm2ListOpen, pm2Status]);

  const phpMyAdminInstallBlocked = !mariadbStatus?.server_installed;
  const caddyServiceMeta = getCaddyServiceMeta(caddyStatus);
  const phpMyAdminServiceStatus = getPHPMyAdminServiceStatus(phpMyAdminStatus);
  const phpVersions = getAvailablePHPVersions(phpStatus);
  const selectedPHPRuntime = getSelectedPHPRuntime(phpStatus, selectedPHPVersion);
  const mariaDBRemoveEnabled = canRemoveMariaDB(mariadbStatus);
  const phpMyAdminRemoveEnabled = canRemovePHPMyAdmin(phpMyAdminStatus);
  const golangRemoveEnabled = canRemoveGolang(golangStatus);
  const nodeJSRemoveEnabled = canRemoveNodeJS(nodeJSStatus);
  const pm2RemoveEnabled = canRemovePM2(pm2Status);
  const mariadbBusyLabel = getRuntimeActionLabel(mariadbStatus?.state);
  const phpMyAdminBusyLabel = getRuntimeActionLabel(phpMyAdminStatus?.state);
  const golangBusyLabel = getRuntimeActionLabel(golangStatus?.state);
  const nodeJSBusyLabel = getRuntimeActionLabel(nodeJSStatus?.state);
  const pm2BusyLabel = getRuntimeActionLabel(pm2Status?.state);
  const pm2InstallDisabled = runningAction !== null || !pm2Status?.install_available;
  const pm2NodeJSRequired = !nodeJSStatus?.installed;
  const pm2LogsLineCount = pm2LogsOutput ? pm2LogsOutput.split(/\r?\n/).length : 0;
  const mariaDBServiceStartingAfterInstall =
    runningAction === "install-mariadb" &&
    Boolean(mariadbStatus?.server_installed) &&
    !mariadbStatus?.service_running;
  const removeDialogDescription =
    removeCandidate?.kind === "php"
      ? `Remove PHP ${removeCandidate.version} from this node? Domains assigned to PHP ${removeCandidate.version} will stop serving until that runtime is installed again.`
      : removeCandidate?.kind === "mariadb"
        ? "Remove MariaDB from this node? Existing databases may become unavailable until MariaDB is installed again."
        : removeCandidate?.kind === "docker"
          ? "Remove Docker from this node? Container workloads and image builds will stop working until Docker is installed again."
        : removeCandidate?.kind === "redis"
          ? "Remove Redis from this node? Services and jobs that rely on Redis caching or queues will stop working until Redis is installed again."
          : removeCandidate?.kind === "mongodb"
            ? "Remove MongoDB from this node? Existing MongoDB databases will become unavailable until MongoDB is installed again."
            : removeCandidate?.kind === "postgresql"
              ? "Remove PostgreSQL from this node? Existing PostgreSQL databases will become unavailable until PostgreSQL is installed again."
        : removeCandidate?.kind === "phpmyadmin"
          ? "Remove phpMyAdmin from this node? The browser database client will no longer be available."
        : removeCandidate?.kind === "golang"
          ? "Remove Go from this node? Deployments and scripts that rely on the Go toolchain will stop working until it is installed again."
          : removeCandidate?.kind === "nodejs"
            ? "Remove Node.js from this node? Applications and build steps that rely on the Node.js runtime will stop working until it is installed again."
            : removeCandidate?.kind === "pm2"
              ? "Remove PM2 from this node? Node.js process management and PM2 log rotation will stop working until PM2 is installed again."
          : "Remove this runtime?";
  const removeDialogTitle =
    removeCandidate?.kind === "php"
      ? `Remove PHP ${removeCandidate.version}`
      : removeCandidate?.kind === "mariadb"
        ? "Remove MariaDB"
        : removeCandidate?.kind === "docker"
          ? "Remove Docker"
        : removeCandidate?.kind === "redis"
          ? "Remove Redis"
          : removeCandidate?.kind === "mongodb"
            ? "Remove MongoDB"
            : removeCandidate?.kind === "postgresql"
              ? "Remove PostgreSQL"
        : removeCandidate?.kind === "phpmyadmin"
          ? "Remove phpMyAdmin"
        : removeCandidate?.kind === "golang"
          ? "Remove Go"
          : removeCandidate?.kind === "nodejs"
            ? "Remove Node.js"
            : removeCandidate?.kind === "pm2"
              ? "Remove PM2"
          : "Remove application";
  const removeDialogConfirmText = getApplicationActionLabel("remove");

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
          : target.kind === "docker"
            ? "remove-docker"
          : target.kind === "redis"
            ? "remove-redis"
            : target.kind === "mongodb"
              ? "remove-mongodb"
              : target.kind === "postgresql"
                ? "remove-postgresql"
                : target.kind === "phpmyadmin"
                  ? "remove-phpmyadmin"
                  : target.kind === "golang"
                    ? "remove-golang"
                    : target.kind === "nodejs"
                      ? "remove-nodejs"
                      : "remove-pm2";
    setRunningAction(action);
    setPageError(null);

    try {
      if (target.kind === "php") {
        const nextStatus = await removePHP(target.version);
        setPHPStatus(nextStatus);
        toast.success(
          getPHPRuntimeByVersion(nextStatus, target.version) === null
            ? `PHP ${target.version} removed.`
            : `PHP ${target.version} removal started.`,
        );
      } else if (target.kind === "mariadb") {
        const nextStatus = await removeMariaDB();
        setMariaDBStatus(nextStatus);
        toast.success(
          !nextStatus.server_installed && !nextStatus.client_installed
            ? "MariaDB removed."
            : "MariaDB removal started.",
        );
      } else if (target.kind === "docker") {
        const nextStatus = await removeDocker();
        setDockerStatus(nextStatus);
        toast.success(!nextStatus.installed ? "Docker removed." : "Docker removal started.");
      } else if (target.kind === "redis") {
        const nextStatus = await removeRedis();
        setRedisStatus(nextStatus);
        toast.success(!nextStatus.installed ? "Redis removed." : "Redis removal started.");
      } else if (target.kind === "mongodb") {
        const nextStatus = await removeMongoDB();
        setMongoDBStatus(nextStatus);
        toast.success(!nextStatus.installed ? "MongoDB removed." : "MongoDB removal started.");
      } else if (target.kind === "postgresql") {
        const nextStatus = await removePostgreSQL();
        setPostgreSQLStatus(nextStatus);
        toast.success(!nextStatus.installed ? "PostgreSQL removed." : "PostgreSQL removal started.");
      } else {
        if (target.kind === "phpmyadmin") {
          const nextStatus = await removePHPMyAdmin();
          setPHPMyAdminStatus(nextStatus);
          toast.success(!nextStatus.installed ? "phpMyAdmin removed." : "phpMyAdmin removal started.");
        } else if (target.kind === "golang") {
          const nextStatus = await removeGolang();
          setGolangStatus(nextStatus);
          toast.success(!nextStatus.installed ? "Go removed." : "Go removal started.");
        } else if (target.kind === "nodejs") {
          const nextStatus = await removeNodeJS();
          setNodeJSStatus(nextStatus);
          toast.success(!nextStatus.installed ? "Node.js removed." : "Node.js removal started.");
        } else {
          const nextStatus = await removePM2();
          setPM2Status(nextStatus);
          toast.success(!nextStatus.installed ? "PM2 removed." : "PM2 removal started.");
        }
      }
    } catch (error) {
      const fallback =
        target.kind === "php"
          ? `Failed to remove PHP ${target.version}.`
          : target.kind === "mariadb"
            ? "Failed to remove MariaDB."
            : target.kind === "docker"
              ? "Failed to remove Docker."
            : target.kind === "redis"
              ? "Failed to remove Redis."
              : target.kind === "mongodb"
                ? "Failed to remove MongoDB."
                : target.kind === "postgresql"
                  ? "Failed to remove PostgreSQL."
                  : target.kind === "phpmyadmin"
                    ? "Failed to remove phpMyAdmin."
                    : target.kind === "golang"
                      ? "Failed to remove Go."
                      : target.kind === "nodejs"
                        ? "Failed to remove Node.js."
                        : "Failed to remove PM2.";
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
      if (!shouldWaitForPHPServiceAfterInstall(nextStatus, version)) {
        setRunningAction(null);
      }
    } catch (error) {
      const message = getErrorMessage(error, `Failed to install PHP ${version}.`);
      setPageError(message);
      toast.error(message);
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
      if (!shouldWaitForMariaDBServiceAfterInstall(nextStatus)) {
        setRunningAction(null);
      }
    } catch (error) {
      const message = getErrorMessage(error, "Failed to install MariaDB.");
      setPageError(message);
      toast.error(message);
      setRunningAction(null);
    }
  }

  async function handleRedisInstall() {
    setRunningAction("install-redis");
    setPageError(null);

    try {
      const nextStatus = await installRedis();
      setRedisStatus(nextStatus);
      toast.success("Redis installed.");
    } catch (error) {
      const message = getErrorMessage(error, "Failed to install Redis.");
      setPageError(message);
      toast.error(message);
    } finally {
      setRunningAction(null);
    }
  }

  async function handleDockerInstall() {
    setRunningAction("install-docker");
    setPageError(null);

    try {
      const nextStatus = await installDocker();
      setDockerStatus(nextStatus);
      toast.success("Docker installed.");
    } catch (error) {
      const message = getErrorMessage(error, "Failed to install Docker.");
      setPageError(message);
      toast.error(message);
    } finally {
      setRunningAction(null);
    }
  }

  async function handleMongoDBInstall() {
    setRunningAction("install-mongodb");
    setPageError(null);

    try {
      const nextStatus = await installMongoDB();
      setMongoDBStatus(nextStatus);
      toast.success("MongoDB installed.");
    } catch (error) {
      const message = getErrorMessage(error, "Failed to install MongoDB.");
      setPageError(message);
      toast.error(message);
    } finally {
      setRunningAction(null);
    }
  }

  async function handlePostgreSQLInstall() {
    setRunningAction("install-postgresql");
    setPageError(null);

    try {
      const nextStatus = await installPostgreSQL();
      setPostgreSQLStatus(nextStatus);
      toast.success("PostgreSQL installed.");
    } catch (error) {
      const message = getErrorMessage(error, "Failed to install PostgreSQL.");
      setPageError(message);
      toast.error(message);
    } finally {
      setRunningAction(null);
    }
  }

  async function handleServiceRuntimeAction<TStatus>({
    actionKey,
    run,
    setStatus,
    successMessage,
    fallbackMessage,
  }: {
    actionKey: string;
    run: () => Promise<TStatus>;
    setStatus: (status: TStatus) => void;
    successMessage: string;
    fallbackMessage: string;
  }) {
    setRunningAction(actionKey);
    setPageError(null);

    try {
      const nextStatus = await run();
      setStatus(nextStatus);
      toast.success(successMessage);
    } catch (error) {
      const message = getErrorMessage(error, fallbackMessage);
      setPageError(message);
      toast.error(message);
    } finally {
      setRunningAction(null);
    }
  }

  async function handleCaddyRestart() {
    await handleServiceRuntimeAction({
      actionKey: "restart-caddy",
      run: restartCaddy,
      setStatus: (status) => setCaddyStatus(status),
      successMessage: "Caddy restarted and domains refreshed.",
      fallbackMessage: "Failed to restart Caddy and refresh domains.",
    });
  }

  async function handleRedisStart() {
    await handleServiceRuntimeAction({
      actionKey: "start-redis",
      run: startRedis,
      setStatus: (status) => setRedisStatus(status),
      successMessage: "Redis started.",
      fallbackMessage: "Failed to start Redis.",
    });
  }

  async function handleDockerStart() {
    await handleServiceRuntimeAction({
      actionKey: "start-docker",
      run: startDocker,
      setStatus: (status) => setDockerStatus(status),
      successMessage: "Docker started.",
      fallbackMessage: "Failed to start Docker.",
    });
  }

  async function handleDockerStop() {
    await handleServiceRuntimeAction({
      actionKey: "stop-docker",
      run: stopDocker,
      setStatus: (status) => setDockerStatus(status),
      successMessage: "Docker stopped.",
      fallbackMessage: "Failed to stop Docker.",
    });
  }

  async function handleDockerRestart() {
    await handleServiceRuntimeAction({
      actionKey: "restart-docker",
      run: restartDocker,
      setStatus: (status) => setDockerStatus(status),
      successMessage: "Docker restarted.",
      fallbackMessage: "Failed to restart Docker.",
    });
  }

  async function handleRedisStop() {
    await handleServiceRuntimeAction({
      actionKey: "stop-redis",
      run: stopRedis,
      setStatus: (status) => setRedisStatus(status),
      successMessage: "Redis stopped.",
      fallbackMessage: "Failed to stop Redis.",
    });
  }

  async function handleRedisRestart() {
    await handleServiceRuntimeAction({
      actionKey: "restart-redis",
      run: restartRedis,
      setStatus: (status) => setRedisStatus(status),
      successMessage: "Redis restarted.",
      fallbackMessage: "Failed to restart Redis.",
    });
  }

  async function handleMongoDBStart() {
    await handleServiceRuntimeAction({
      actionKey: "start-mongodb",
      run: startMongoDB,
      setStatus: (status) => setMongoDBStatus(status),
      successMessage: "MongoDB started.",
      fallbackMessage: "Failed to start MongoDB.",
    });
  }

  async function handleMongoDBStop() {
    await handleServiceRuntimeAction({
      actionKey: "stop-mongodb",
      run: stopMongoDB,
      setStatus: (status) => setMongoDBStatus(status),
      successMessage: "MongoDB stopped.",
      fallbackMessage: "Failed to stop MongoDB.",
    });
  }

  async function handleMongoDBRestart() {
    await handleServiceRuntimeAction({
      actionKey: "restart-mongodb",
      run: restartMongoDB,
      setStatus: (status) => setMongoDBStatus(status),
      successMessage: "MongoDB restarted.",
      fallbackMessage: "Failed to restart MongoDB.",
    });
  }

  async function handlePostgreSQLStart() {
    await handleServiceRuntimeAction({
      actionKey: "start-postgresql",
      run: startPostgreSQL,
      setStatus: (status) => setPostgreSQLStatus(status),
      successMessage: "PostgreSQL started.",
      fallbackMessage: "Failed to start PostgreSQL.",
    });
  }

  async function handlePostgreSQLStop() {
    await handleServiceRuntimeAction({
      actionKey: "stop-postgresql",
      run: stopPostgreSQL,
      setStatus: (status) => setPostgreSQLStatus(status),
      successMessage: "PostgreSQL stopped.",
      fallbackMessage: "Failed to stop PostgreSQL.",
    });
  }

  async function handlePostgreSQLRestart() {
    await handleServiceRuntimeAction({
      actionKey: "restart-postgresql",
      run: restartPostgreSQL,
      setStatus: (status) => setPostgreSQLStatus(status),
      successMessage: "PostgreSQL restarted.",
      fallbackMessage: "Failed to restart PostgreSQL.",
    });
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

  async function handleGolangInstall() {
    setRunningAction("install-golang");
    setPageError(null);

    try {
      const nextStatus = await installGolang();
      setGolangStatus(nextStatus);
      toast.success("Go installed.");
    } catch (error) {
      const message = getErrorMessage(error, "Failed to install Go.");
      setPageError(message);
      toast.error(message);
    } finally {
      setRunningAction(null);
    }
  }

  async function handleNodeJSInstall() {
    setRunningAction("install-nodejs");
    setPageError(null);

    try {
      const nextStatus = await installNodeJS();
      setNodeJSStatus(nextStatus);
      toast.success("Node.js installed.");
    } catch (error) {
      const message = getErrorMessage(error, "Failed to install Node.js.");
      setPageError(message);
      toast.error(message);
    } finally {
      setRunningAction(null);
    }
  }

  async function handlePM2Install() {
    setRunningAction("install-pm2");
    setPageError(null);

    try {
      const nextStatus = await installPM2();
      setPM2Status(nextStatus);
      toast.success("PM2 installed. Log rotation is limited to 100 MB.");
    } catch (error) {
      const message = getErrorMessage(error, "Failed to install PM2.");
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
      <MariaDBSettingsDialog
        open={mariaDBSettingsOpen}
        onOpenChange={setMariaDBSettingsOpen}
        status={mariadbStatus}
      />

      <PHPSettingsDialog
        open={phpSettingsOpen}
        onOpenChange={setPHPSettingsOpen}
        status={selectedPHPRuntime}
        extensionCatalog={phpStatus?.extension_catalog ?? []}
        version={selectedPHPVersion}
        defaultVersion={phpStatus?.default_version}
        onStatusChange={setPHPStatus}
      />

      <PHPMyAdminSettingsDialog
        open={phpMyAdminSettingsOpen}
        onOpenChange={setPHPMyAdminSettingsOpen}
        status={phpMyAdminStatus}
        onStatusChange={setPHPMyAdminStatus}
      />

      <Dialog open={pm2ListOpen} onOpenChange={handlePM2ListOpenChange}>
        <DialogContent className="h-[min(85vh,calc(100vh-2rem))] grid-rows-[auto_minmax(0,1fr)] gap-0 overflow-hidden p-0 sm:max-w-6xl">
          <DialogHeader className="border-b border-[var(--app-border)] bg-[var(--app-surface)] px-6 py-5">
            <div className="flex items-start justify-between gap-4">
              <div className="min-w-0">
                <DialogTitle>PM2 processes</DialogTitle>
                <DialogDescription>Manage runtime processes with start, stop, restart, and log actions.</DialogDescription>
              </div>

              <Button
                type="button"
                variant="outline"
                size="sm"
                className="shrink-0 border-[var(--app-border)] bg-[var(--app-surface-muted)] text-[var(--app-text)] hover:bg-[var(--app-bg-2)]"
                onClick={() => {
                  void loadPM2Processes();
                }}
                disabled={pm2ProcessesLoading || pm2ProcessActionKey !== null || !pm2Status?.installed}
              >
                {pm2ProcessesLoading ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <RefreshCw className="h-4 w-4" />}
                Refresh
              </Button>
            </div>
          </DialogHeader>

          <div className="min-h-0 bg-[var(--app-surface)] text-[var(--app-text)]">
            {pm2ProcessesError ? (
              <div className="flex h-full items-center justify-center p-6">
                <div className="max-w-xl rounded-lg border border-[var(--app-danger)]/30 bg-[var(--app-danger-soft)] px-4 py-3 text-sm text-[var(--app-danger)]">
                  {pm2ProcessesError}
                </div>
              </div>
            ) : pm2ProcessesLoading ? (
              <div className="flex h-full items-center justify-center gap-2 px-6 text-sm text-[var(--app-text-muted)]">
                <LoaderCircle className="h-4 w-4 animate-spin" />
                Loading PM2 processes...
              </div>
            ) : pm2Processes.length === 0 ? (
              <div className="px-6 py-6">
                <div className="rounded-md border border-dashed border-[var(--app-border)] bg-[var(--app-surface-muted)] px-4 py-10 text-sm text-[var(--app-text-muted)]">
                  No PM2 processes found.
                </div>
              </div>
            ) : (
              <div className="h-full overflow-auto">
                <Table className="min-w-[1100px]">
                  <TableHeader className="sticky top-0 z-10 bg-[var(--app-surface)] [&_tr]:border-[var(--app-border)]">
                    <TableRow className="hover:bg-transparent">
                      <TableHead className="px-4">Name</TableHead>
                      <TableHead>Status</TableHead>
                      <TableHead>CPU</TableHead>
                      <TableHead>Memory</TableHead>
                      <TableHead>Restarts</TableHead>
                      <TableHead>Uptime</TableHead>
                      <TableHead className="min-w-[280px]">Script</TableHead>
                      <TableHead className="w-[300px] text-right">Actions</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {pm2Processes.map((process) => {
                      const statusBadge = getPM2ProcessStatusBadge(process.status);
                      const activeAction = pm2ProcessActionKey?.endsWith(`:${process.id}`) ? pm2ProcessActionKey.split(":")[0] : null;
                      const actionsDisabled = pm2ProcessActionKey !== null;

                      return (
                        <TableRow key={process.id} className="align-top">
                          <TableCell className="px-4 py-3">
                            <div className="font-medium text-[var(--app-text)]">{process.name}</div>
                            <div className="mt-1 text-xs text-[var(--app-text-muted)]">
                              ID {process.id}
                              {process.namespace ? ` • ${process.namespace}` : ""}
                              {process.exec_mode ? ` • ${process.exec_mode}` : ""}
                              {process.version ? ` • v${process.version}` : ""}
                            </div>
                          </TableCell>
                          <TableCell className="py-3">
                            <Badge variant={statusBadge.variant}>{statusBadge.label}</Badge>
                          </TableCell>
                          <TableCell className="py-3 text-[13px] text-[var(--app-text-muted)]">
                            {formatPM2ProcessCPU(process.cpu)}
                          </TableCell>
                          <TableCell className="py-3 text-[13px] text-[var(--app-text-muted)]">
                            {formatPM2ProcessMemory(process.memory_bytes)}
                          </TableCell>
                          <TableCell className="py-3 text-[13px] text-[var(--app-text-muted)]">
                            {process.restarts}
                          </TableCell>
                          <TableCell className="py-3 text-[13px] text-[var(--app-text-muted)]">
                            {formatPM2ProcessUptime(process)}
                          </TableCell>
                          <TableCell className="max-w-0 py-3">
                            <div className="whitespace-normal break-all font-mono text-xs text-[var(--app-text-muted)]">
                              {process.script_path?.trim() || "-"}
                            </div>
                          </TableCell>
                          <TableCell className="py-3 text-right">
                            <div className="flex flex-wrap justify-end gap-2">
                              <Button
                                type="button"
                                variant="outline"
                                size="sm"
                                className="h-7 w-7 p-0"
                                onClick={() => {
                                  void handlePM2ProcessAction("start", process);
                                }}
                                disabled={actionsDisabled || !canStartPM2Process(process)}
                                aria-label={`Start ${process.name}`}
                                title={`Start ${process.name}`}
                              >
                                {activeAction === "start" ? (
                                  <LoaderCircle className="h-4 w-4 animate-spin" />
                                ) : (
                                  <PlayerPlayFilled className="h-4 w-4" />
                                )}
                              </Button>
                              <Button
                                type="button"
                                variant="outline"
                                size="sm"
                                className="h-7 w-7 p-0"
                                onClick={() => {
                                  void handlePM2ProcessAction("stop", process);
                                }}
                                disabled={actionsDisabled || !canStopPM2Process(process)}
                                aria-label={`Stop ${process.name}`}
                                title={`Stop ${process.name}`}
                              >
                                {activeAction === "stop" ? (
                                  <LoaderCircle className="h-4 w-4 animate-spin" />
                                ) : (
                                  <PlayerStop className="h-4 w-4" />
                                )}
                              </Button>
                              <Button
                                type="button"
                                variant="outline"
                                size="sm"
                                className="h-7 w-7 p-0"
                                onClick={() => {
                                  void handlePM2ProcessAction("restart", process);
                                }}
                                disabled={actionsDisabled || !canRestartPM2Process(process)}
                                aria-label={`Restart ${process.name}`}
                                title={`Restart ${process.name}`}
                              >
                                {activeAction === "restart" ? (
                                  <LoaderCircle className="h-4 w-4 animate-spin" />
                                ) : (
                                  <RotateCcw className="h-4 w-4" />
                                )}
                              </Button>
                              <Button
                                type="button"
                                variant="outline"
                                size="sm"
                                className={compactActionButtonClassName}
                                onClick={() => {
                                  openPM2Logs(process);
                                }}
                                disabled={actionsDisabled}
                              >
                                <TerminalSquare className="h-4 w-4" />
                                Logs
                              </Button>
                            </div>
                          </TableCell>
                        </TableRow>
                      );
                    })}
                  </TableBody>
                </Table>
              </div>
            )}
          </div>
        </DialogContent>
      </Dialog>

      <Dialog open={pm2LogsOpen} onOpenChange={handlePM2LogsOpenChange}>
        <DialogContent className="h-[min(80vh,calc(100vh-2rem))] grid-rows-[auto_minmax(0,1fr)] gap-0 overflow-hidden p-0 sm:max-w-5xl">
          <DialogHeader className="border-b border-[var(--app-border)] bg-[var(--app-surface)] px-6 py-5">
            <div className="flex items-start justify-between gap-4">
              <div className="min-w-0">
                <DialogTitle>{pm2LogsTarget ? `${pm2LogsTarget.name} logs` : "PM2 process logs"}</DialogTitle>
                <DialogDescription>
                  {pm2LogsTarget
                    ? `pm2 logs ${pm2LogsTarget.id} --lines 200 --nostream --raw`
                    : "Recent PM2 process output."}
                </DialogDescription>
              </div>

              <div className="flex items-center gap-2">
                {pm2LogsOutput ? (
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    className="shrink-0 border-[var(--app-border)] bg-[var(--app-surface-muted)] text-[var(--app-text)] hover:bg-[var(--app-bg-2)]"
                    onClick={() => {
                      if (!pm2LogsOutput) {
                        return;
                      }

                      void navigator.clipboard.writeText(pm2LogsOutput).then(
                        () => {
                          toast.success("PM2 logs copied.");
                        },
                        () => {
                          toast.error("Failed to copy PM2 logs.");
                        },
                      );
                    }}
                  >
                    <Copy className="h-4 w-4" />
                    Copy logs
                  </Button>
                ) : null}
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  className="shrink-0 border-[var(--app-border)] bg-[var(--app-surface-muted)] text-[var(--app-text)] hover:bg-[var(--app-bg-2)]"
                  onClick={() => {
                    if (pm2LogsTarget) {
                      void loadPM2Logs(pm2LogsTarget);
                    }
                  }}
                  disabled={pm2LogsLoading || pm2LogsTarget === null}
                >
                  {pm2LogsLoading ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <RefreshCw className="h-4 w-4" />}
                  Refresh
                </Button>
              </div>
            </div>
          </DialogHeader>

          <div className="flex min-h-0 flex-col bg-[var(--app-surface)]">
            {pm2LogsTarget ? (
              <>
                <div className="border-b border-[var(--app-border)] px-6 py-4 text-sm text-[var(--app-text-muted)]">
                  <span className="font-medium text-[var(--app-text)]">{pm2LogsTarget.name}</span>
                  {" • "}
                  {pm2LogsLineCount > 0
                    ? `${pm2LogsLineCount} ${pm2LogsLineCount === 1 ? "line" : "lines"}`
                    : "No captured output"}
                </div>

                {pm2LogsError ? (
                  <div className="flex h-full items-center justify-center p-6">
                    <div className="max-w-xl rounded-lg border border-[var(--app-danger)]/30 bg-[var(--app-danger-soft)] px-4 py-3 text-sm text-[var(--app-danger)]">
                      {pm2LogsError}
                    </div>
                  </div>
                ) : pm2LogsLoading ? (
                  <div className="flex h-full items-center justify-center gap-2 px-6 text-sm text-[var(--app-text-muted)]">
                    <LoaderCircle className="h-4 w-4 animate-spin" />
                    Loading PM2 logs...
                  </div>
                ) : pm2LogsOutput ? (
                  <ScrollArea className="min-h-0 flex-1 bg-[var(--app-surface)]">
                    <pre className="p-6 font-mono text-xs leading-5 whitespace-pre-wrap break-words text-[var(--app-text)]">
                      {pm2LogsOutput}
                    </pre>
                  </ScrollArea>
                ) : (
                  <div className="flex h-full items-center justify-center px-6 text-sm text-[var(--app-text-muted)]">
                    No log output returned for this process.
                  </div>
                )}
              </>
            ) : null}
          </div>
        </DialogContent>
      </Dialog>

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
          (removeCandidate?.kind === "docker" && runningAction === "remove-docker") ||
          (removeCandidate?.kind === "redis" && runningAction === "remove-redis") ||
          (removeCandidate?.kind === "mongodb" && runningAction === "remove-mongodb") ||
          (removeCandidate?.kind === "postgresql" && runningAction === "remove-postgresql") ||
          (removeCandidate?.kind === "phpmyadmin" && runningAction === "remove-phpmyadmin") ||
          (removeCandidate?.kind === "golang" && runningAction === "remove-golang") ||
          (removeCandidate?.kind === "nodejs" && runningAction === "remove-nodejs") ||
          (removeCandidate?.kind === "pm2" && runningAction === "remove-pm2")
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
          <ApplicationCard
            icon={<Server className="h-5 w-5 text-[var(--app-text)]" />}
            name="Caddy server"
            summary={formatCaddyValue(caddyStatus)}
            badge={getCaddyBadge(caddyStatus)}
            meta={[
              {
                label: "Service",
                value: caddyServiceMeta.value,
                tone: caddyServiceMeta.tone,
              },
              { label: "Domains", value: String(caddyStatus?.configured_domains ?? 0) },
              { label: "HTTP", value: caddyStatus?.public_http_addr?.trim() || "Disabled", mono: true },
              { label: "HTTPS", value: caddyStatus?.public_https_addr?.trim() || "Disabled", mono: true },
            ]}
            configAction={null}
            actions={
              <Button
                type="button"
                variant="outline"
                size="sm"
                className={compactActionButtonClassName}
                onClick={() => {
                  void handleCaddyRestart();
                }}
                disabled={runningAction !== null || !caddyStatus?.restart_available}
                title={caddyStatus?.restart_available ? undefined : "Caddy runtime is unavailable."}
              >
                {runningAction === "restart-caddy" ? (
                  <LoaderCircle className="h-4 w-4 animate-spin" />
                ) : (
                  <RotateCcw className="h-4 w-4" />
                )}
                {caddyStatus?.restart_label ?? "Restart & sync"}
              </Button>
            }
          />

          <PHPRuntimeCard
            status={selectedPHPRuntime}
            availableVersions={phpVersions}
            selectedVersion={selectedPHPVersion}
            runningAction={runningAction}
            disableActions={runningAction !== null}
            settingsDisabled={selectedPHPRuntime === null}
            onVersionChange={setSelectedPHPVersion}
            onOpenSettings={() => {
              setPHPSettingsOpen(true);
            }}
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
            icon={<ApplicationLogo app="mariadb" />}
            name="MariaDB"
            summary={formatMariaDBValue(mariadbStatus)}
            badge={getMariaDBBadge(mariadbStatus)}
            meta={[
              {
                label: "Service",
                value: mariaDBServiceStartingAfterInstall
                  ? "Service starting..."
                  : mariadbBusyLabel?.replace("...", "") ??
                    (mariadbStatus?.service_running ? "Running" : "Stopped"),
                tone: mariaDBServiceStartingAfterInstall
                  ? "info"
                  : mariadbBusyLabel
                    ? undefined
                    : mariadbStatus?.service_running
                      ? "success"
                      : "danger",
              },
            ]}
            configAction={
              <Button
                type="button"
                variant="ghost"
                size="icon"
                className="h-8 w-8 rounded-md text-[var(--app-text-muted)]"
                aria-label="Open MariaDB settings"
                title="Open MariaDB settings"
                onClick={() => {
                  setMariaDBSettingsOpen(true);
                }}
              >
                <Settings className="h-4 w-4" />
              </Button>
            }
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
                    {getApplicationActionLabel("install")}
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

          <ServiceApplicationCard
            app="docker"
            name="Docker"
            status={dockerStatus}
            runningAction={runningAction}
            actionKeyPrefix="docker"
            removeTitle="Automatic Docker removal is only available for installed runtimes supported by this environment."
            meta={[
              { label: "Binary", value: dockerStatus?.binary_path?.trim() || "docker", mono: true },
              ...(dockerStatus?.package_manager
                ? [{ label: "Package manager", value: dockerStatus.package_manager }]
                : []),
            ]}
            onInstall={() => {
              void handleDockerInstall();
            }}
            onStart={() => {
              void handleDockerStart();
            }}
            onStop={() => {
              void handleDockerStop();
            }}
            onRestart={() => {
              void handleDockerRestart();
            }}
            onRemove={() => {
              setRemoveCandidate({ kind: "docker" });
            }}
          />

          <ServiceApplicationCard
            app="redis"
            name="Redis"
            status={redisStatus}
            runningAction={runningAction}
            actionKeyPrefix="redis"
            removeTitle="Automatic Redis removal is only available for installed runtimes supported by this environment."
            meta={[
              { label: "Binary", value: redisStatus?.binary_path?.trim() || "redis-server", mono: true },
              ...(redisStatus?.package_manager
                ? [{ label: "Package manager", value: redisStatus.package_manager }]
                : []),
            ]}
            onInstall={() => {
              void handleRedisInstall();
            }}
            onStart={() => {
              void handleRedisStart();
            }}
            onStop={() => {
              void handleRedisStop();
            }}
            onRestart={() => {
              void handleRedisRestart();
            }}
            onRemove={() => {
              setRemoveCandidate({ kind: "redis" });
            }}
          />

          <ServiceApplicationCard
            app="mongodb"
            name="MongoDB"
            status={mongoDBStatus}
            runningAction={runningAction}
            actionKeyPrefix="mongodb"
            removeTitle="Automatic MongoDB removal is only available for installed runtimes supported by this environment."
            meta={[
              { label: "Binary", value: mongoDBStatus?.binary_path?.trim() || "mongod", mono: true },
              ...(mongoDBStatus?.package_manager
                ? [{ label: "Package manager", value: mongoDBStatus.package_manager }]
                : []),
            ]}
            onInstall={() => {
              void handleMongoDBInstall();
            }}
            onStart={() => {
              void handleMongoDBStart();
            }}
            onStop={() => {
              void handleMongoDBStop();
            }}
            onRestart={() => {
              void handleMongoDBRestart();
            }}
            onRemove={() => {
              setRemoveCandidate({ kind: "mongodb" });
            }}
          />

          <ServiceApplicationCard
            app="postgresql"
            name="PostgreSQL"
            status={postgresqlStatus}
            runningAction={runningAction}
            actionKeyPrefix="postgresql"
            removeTitle="Automatic PostgreSQL removal is only available for installed runtimes supported by this environment."
            meta={[
              { label: "Binary", value: postgresqlStatus?.binary_path?.trim() || "postgres", mono: true },
              ...(postgresqlStatus?.package_manager
                ? [{ label: "Package manager", value: postgresqlStatus.package_manager }]
                : []),
            ]}
            onInstall={() => {
              void handlePostgreSQLInstall();
            }}
            onStart={() => {
              void handlePostgreSQLStart();
            }}
            onStop={() => {
              void handlePostgreSQLStop();
            }}
            onRestart={() => {
              void handlePostgreSQLRestart();
            }}
            onRemove={() => {
              setRemoveCandidate({ kind: "postgresql" });
            }}
          />

          <ApplicationCard
            icon={<ApplicationLogo app="phpmyadmin" />}
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
                    {getApplicationActionLabel("install")}
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

          <ApplicationCard
            icon={<ApplicationLogo app="go" />}
            name="Go"
            summary={formatGolangValue(golangStatus)}
            badge={getGolangBadge(golangStatus)}
            meta={[
              { label: "Toolchain", value: golangStatus?.binary_path?.trim() || "go", mono: true },
              ...(golangStatus?.package_manager
                ? [{ label: "Package manager", value: golangStatus.package_manager }]
                : []),
            ]}
            configAction={null}
            actions={
              <>
                {golangBusyLabel ? (
                  <Button type="button" variant="outline" size="sm" className={compactActionButtonClassName} disabled>
                    <LoaderCircle className="h-4 w-4 animate-spin" />
                    {golangBusyLabel}
                  </Button>
                ) : null}
                {golangStatus?.install_available ? (
                  <Button
                    type="button"
                    size="sm"
                    className={compactActionButtonClassName}
                    onClick={() => {
                      void handleGolangInstall();
                    }}
                    disabled={runningAction !== null}
                  >
                    {runningAction === "install-golang" ? (
                      <LoaderCircle className="h-4 w-4 animate-spin" />
                    ) : (
                      <Package className="h-4 w-4" />
                    )}
                    {getApplicationActionLabel("install")}
                  </Button>
                ) : null}
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  className={compactActionButtonClassName}
                  onClick={() => {
                    setRemoveCandidate({ kind: "golang" });
                  }}
                  disabled={runningAction !== null || !golangRemoveEnabled}
                  title={golangRemoveEnabled ? undefined : "Automatic Go removal is only available for installed runtimes supported by this environment."}
                >
                  {runningAction === "remove-golang" ? (
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
            icon={<ApplicationLogo app="nodejs" />}
            name="Node.js"
            summary={formatNodeJSValue(nodeJSStatus)}
            badge={getNodeJSBadge(nodeJSStatus)}
            meta={[
              { label: "Toolchain", value: nodeJSStatus?.binary_path?.trim() || "node", mono: true },
              ...(nodeJSStatus?.npm_path?.trim()
                ? [{ label: "NPM", value: nodeJSStatus.npm_path.trim(), mono: true }]
                : []),
              ...(nodeJSStatus?.package_manager
                ? [{ label: "Package manager", value: nodeJSStatus.package_manager }]
                : []),
            ]}
            configAction={null}
            actions={
              <>
                {nodeJSBusyLabel ? (
                  <Button type="button" variant="outline" size="sm" className={compactActionButtonClassName} disabled>
                    <LoaderCircle className="h-4 w-4 animate-spin" />
                    {nodeJSBusyLabel}
                  </Button>
                ) : null}
                {nodeJSStatus?.install_available ? (
                  <Button
                    type="button"
                    size="sm"
                    className={compactActionButtonClassName}
                    onClick={() => {
                      void handleNodeJSInstall();
                    }}
                    disabled={runningAction !== null}
                  >
                    {runningAction === "install-nodejs" ? (
                      <LoaderCircle className="h-4 w-4 animate-spin" />
                    ) : (
                      <Package className="h-4 w-4" />
                    )}
                    {getApplicationActionLabel("install")}
                  </Button>
                ) : null}
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  className={compactActionButtonClassName}
                  onClick={() => {
                    setRemoveCandidate({ kind: "nodejs" });
                  }}
                  disabled={runningAction !== null || !nodeJSRemoveEnabled}
                  title={nodeJSRemoveEnabled ? undefined : "Automatic Node.js removal is only available for installed runtimes supported by this environment."}
                >
                  {runningAction === "remove-nodejs" ? (
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
            icon={<ApplicationLogo app="pm2" />}
            name="PM2"
            summary={formatPM2Value(pm2Status)}
            badge={getPM2Badge(pm2Status)}
            meta={[
              { label: "Toolchain", value: pm2Status?.binary_path?.trim() || "pm2", mono: true },
              ...(pm2Status?.package_manager
                ? [{ label: "Package manager", value: pm2Status.package_manager }]
                : []),
              ...(pm2Status?.installed ? [{ label: "Log rotation", value: "100 MB max" }] : []),
            ]}
            configAction={null}
            actions={
              <>
                {pm2BusyLabel ? (
                  <Button type="button" variant="outline" size="sm" className={compactActionButtonClassName} disabled>
                    <LoaderCircle className="h-4 w-4 animate-spin" />
                    {pm2BusyLabel}
                  </Button>
                ) : null}
                {!pm2Status?.installed ? (
                  pm2NodeJSRequired ? (
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <span className="inline-flex">
                          <Button
                            type="button"
                            size="sm"
                            className={compactActionButtonClassName}
                            onClick={() => {
                              void handlePM2Install();
                            }}
                            disabled
                          >
                            <Package className="h-4 w-4" />
                            {getApplicationActionLabel("install")}
                          </Button>
                        </span>
                      </TooltipTrigger>
                      <TooltipContent>
                        <p>Node.js required.</p>
                      </TooltipContent>
                    </Tooltip>
                  ) : (
                    <Button
                      type="button"
                      size="sm"
                      className={compactActionButtonClassName}
                      onClick={() => {
                        void handlePM2Install();
                      }}
                      disabled={pm2InstallDisabled}
                    >
                      {runningAction === "install-pm2" ? (
                        <LoaderCircle className="h-4 w-4 animate-spin" />
                      ) : (
                        <Package className="h-4 w-4" />
                      )}
                      {getApplicationActionLabel("install")}
                    </Button>
                  )
                ) : null}
                {pm2Status?.installed ? (
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    className={compactActionButtonClassName}
                    onClick={() => {
                      handlePM2ListOpenChange(true);
                    }}
                    disabled={runningAction === "remove-pm2"}
                  >
                    <List className="h-4 w-4" />
                    List
                  </Button>
                ) : null}
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  className={compactActionButtonClassName}
                  onClick={() => {
                    setRemoveCandidate({ kind: "pm2" });
                  }}
                  disabled={runningAction !== null || !pm2RemoveEnabled}
                  title={pm2RemoveEnabled ? undefined : "Automatic PM2 removal is only available when PM2 and npm are installed."}
                >
                  {runningAction === "remove-pm2" ? (
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
