import { useEffect, useEffectEvent, useRef, useState, type ReactNode } from "react";
import { fetchScheduledBackups } from "@/api/backups";
import { fetchCaddyStatus, restartCaddy, type CaddyStatus } from "@/api/caddy";
import { fetchDomains } from "@/api/domains";
import {
  fetchDockerStatus,
  restartDocker,
  startDocker,
  stopDocker,
  type DockerStatus,
} from "@/api/docker";
import { fetchFFmpegStatus, type FFmpegStatus } from "@/api/ffmpeg";
import { fetchYTDLPStatus, type YTDLPStatus } from "@/api/ytdlp";
import { fetchGolangStatus, type GolangStatus } from "@/api/golang";
import {
  fetchMariaDBDatabases,
  fetchMariaDBStatus,
  restartMariaDB,
  startMariaDB,
  stopMariaDB,
  type MariaDBStatus,
} from "@/api/mariadb";
import {
  fetchMongoDBStatus,
  restartMongoDB,
  startMongoDB,
  stopMongoDB,
  type MongoDBStatus,
} from "@/api/mongodb";
import { fetchNodeJSStatus, type NodeJSStatus } from "@/api/nodejs";
import { fetchPHPStatus, restartPHP, startPHP, stopPHP, type PHPStatus } from "@/api/php";
import { fetchPHPMyAdminStatus, type PHPMyAdminStatus } from "@/api/phpmyadmin";
import {
  clearPM2ProcessLogs,
  createPM2Process,
  deletePM2Process,
  fetchPM2ProcessLogs,
  fetchPM2Processes,
  fetchPM2Status,
  restartPM2Process,
  startPM2Process,
  stopPM2Process,
  updatePM2Process,
  type PM2CreateProcessInput,
  type PM2Process,
  type PM2Status,
} from "@/api/pm2";
import {
  fetchPostgreSQLStatus,
  restartPostgreSQL,
  startPostgreSQL,
  stopPostgreSQL,
  type PostgreSQLStatus,
} from "@/api/postgresql";
import {
  fetchRedisStatus,
  restartRedis,
  startRedis,
  stopRedis,
  type RedisStatus,
} from "@/api/redis";
import { fetchSystemStatus, type SystemStatus } from "@/api/system";
import { ActionConfirmDialog } from "@/components/action-confirm-dialog";
import { LoaderCircle, Plus, Trash2, Database, PlayerPlayFilled, PlayerStop, RefreshCw, World } from "@/components/icons/lucide-icons";
import { PM2ProcessList } from "@/components/pm2-process-list";
import { PM2ProcessDialog } from "@/components/pm2-process-dialog";
import { appendSystemStatusSample, type SystemStatusSample } from "@/components/system-metrics-card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { SystemStatusCard, type OperationalHealth } from "@/components/system-status-card";
import { formatMariaDBVersion, getRuntimeActionLabel } from "@/lib/runtime-status";
import { cn, getErrorMessage } from "@/lib/utils";
import { toast } from "sonner";

const systemStatusRefreshIntervalMs = 5_000;
const pm2ProcessesRefreshIntervalMs = 10_000;
const pm2LogsRefreshIntervalMs = 2_000;
const pm2LogsBottomThresholdPx = 24;

type OverviewData = {
  databaseCount: number | null;
  health: OperationalHealth;
  siteCount: number | null;
  systemStatus: SystemStatus | null;
};

type InstalledAppsData = {
  caddy: CaddyStatus | null;
  docker: DockerStatus | null;
  ffmpeg: FFmpegStatus | null;
  ytdlp: YTDLPStatus | null;
  golang: GolangStatus | null;
  mariadb: MariaDBStatus | null;
  mongodb: MongoDBStatus | null;
  nodejs: NodeJSStatus | null;
  php: PHPStatus | null;
  phpmyadmin: PHPMyAdminStatus | null;
  postgresql: PostgreSQLStatus | null;
  redis: RedisStatus | null;
};

type RuntimeRowAction = {
  key: string;
  label: string;
  icon: "restart" | "start" | "stop";
  run: () => Promise<void>;
};

type RuntimeRow = {
  key: string;
  name: string;
  icon: string;
  iconAlt: string;
  version: string;
  status: string;
  running: boolean;
  state?: string | null;
  actions: RuntimeRowAction[];
};

async function fetchOperationalHealth(): Promise<OperationalHealth> {
  const [caddyResult, mariadbResult, phpResult, scheduledBackupsResult] = await Promise.allSettled([
    fetchCaddyStatus(),
    fetchMariaDBStatus(),
    fetchPHPStatus(),
    fetchScheduledBackups(),
  ]);

  return {
    backup:
      scheduledBackupsResult.status === "fulfilled"
        ? scheduledBackupsResult.value.enabled && scheduledBackupsResult.value.started
        : null,
    database: mariadbResult.status === "fulfilled" ? mariadbResult.value.ready : null,
    runtime: phpResult.status === "fulfilled" ? phpResult.value.ready : null,
    webServer: caddyResult.status === "fulfilled" ? caddyResult.value.started && caddyResult.value.config_loaded : null,
  };
}

async function fetchOverviewData(): Promise<OverviewData> {
  const [databaseResult, domainsResult, systemResult, healthResult] = await Promise.allSettled([
    fetchMariaDBDatabases(),
    fetchDomains(),
    fetchSystemStatus(),
    fetchOperationalHealth(),
  ]);

  return {
    databaseCount: databaseResult.status === "fulfilled" ? databaseResult.value.databases.length : null,
    health:
      healthResult.status === "fulfilled"
        ? healthResult.value
        : { backup: null, database: null, runtime: null, webServer: null },
    siteCount: domainsResult.status === "fulfilled" ? domainsResult.value.domains.length : null,
    systemStatus: systemResult.status === "fulfilled" ? systemResult.value : null,
  };
}

async function fetchInstalledAppsData(): Promise<InstalledAppsData> {
  const [
    caddyResult,
    phpResult,
    mariadbResult,
    dockerResult,
    ffmpegResult,
    ytdlpResult,
    redisResult,
    mongoDBResult,
    postgresqlResult,
    phpMyAdminResult,
    golangResult,
    nodeJSResult,
  ] = await Promise.allSettled([
    fetchCaddyStatus(),
    fetchPHPStatus(),
    fetchMariaDBStatus(),
    fetchDockerStatus(),
    fetchFFmpegStatus(),
    fetchYTDLPStatus(),
    fetchRedisStatus(),
    fetchMongoDBStatus(),
    fetchPostgreSQLStatus(),
    fetchPHPMyAdminStatus(),
    fetchGolangStatus(),
    fetchNodeJSStatus(),
  ]);

  return {
    caddy: caddyResult.status === "fulfilled" ? caddyResult.value : null,
    docker: dockerResult.status === "fulfilled" ? dockerResult.value : null,
    ffmpeg: ffmpegResult.status === "fulfilled" ? ffmpegResult.value : null,
    ytdlp: ytdlpResult.status === "fulfilled" ? ytdlpResult.value : null,
    golang: golangResult.status === "fulfilled" ? golangResult.value : null,
    mariadb: mariadbResult.status === "fulfilled" ? mariadbResult.value : null,
    mongodb: mongoDBResult.status === "fulfilled" ? mongoDBResult.value : null,
    nodejs: nodeJSResult.status === "fulfilled" ? nodeJSResult.value : null,
    php: phpResult.status === "fulfilled" ? phpResult.value : null,
    phpmyadmin: phpMyAdminResult.status === "fulfilled" ? phpMyAdminResult.value : null,
    postgresql: postgresqlResult.status === "fulfilled" ? postgresqlResult.value : null,
    redis: redisResult.status === "fulfilled" ? redisResult.value : null,
  };
}

function normalizeVersion(version?: string) {
  const value = version?.trim();
  if (!value || value.includes("/") || value.includes("\\")) {
    return "";
  }

  return value
    .replace(/^v/i, "")
    .replace(/^PHP\s+/i, "")
    .replace(/^go/i, "")
    .split(/\s+/)[0];
}

function runtimeStatusLabel(state: string | null | undefined, running: boolean, installed = true) {
  const actionLabel = getRuntimeActionLabel(state);
  if (actionLabel) {
    return actionLabel.replace("...", "");
  }

  return running ? "Running" : installed ? "Stopped" : "Installed";
}

function runtimeStateBusy(state: string | null | undefined) {
  return Boolean(getRuntimeActionLabel(state));
}

function buildServiceRuntimeActions({
  key,
  restartAvailable,
  startAvailable,
  stopAvailable,
  onRestart,
  onStart,
  onStop,
}: {
  key: string;
  restartAvailable?: boolean;
  startAvailable?: boolean;
  stopAvailable?: boolean;
  onRestart?: () => Promise<void>;
  onStart?: () => Promise<void>;
  onStop?: () => Promise<void>;
}) {
  const actions: RuntimeRowAction[] = [];

  if (restartAvailable && onRestart) {
    actions.push({ key: `restart-${key}`, label: "Restart", icon: "restart", run: onRestart });
  }

  if (stopAvailable && onStop) {
    actions.push({ key: `stop-${key}`, label: "Stop", icon: "stop", run: onStop });
  } else if (startAvailable && onStart) {
    actions.push({ key: `start-${key}`, label: "Start", icon: "start", run: onStart });
  }

  return actions;
}

function OverviewCard({
  databaseCount,
  siteCount,
}: {
  databaseCount: number | null;
  siteCount: number | null;
}) {
  return (
    <section className="rounded-xl border border-[var(--app-border)] bg-[var(--app-bg-2)] px-5 py-2 shadow-[var(--app-shadow)]">
      <div className="text-[15px] font-semibold tracking-tight text-[var(--app-text)]">Overview</div>
      <div className="mt-4 grid divide-y divide-[var(--app-border)] sm:grid-cols-2 sm:divide-x sm:divide-y-0">
        <OverviewStat icon={<World className="h-4 w-4" />} label="Total sites" value={formatTotalCount(siteCount)} />
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
    <div className="flex items-center justify-between gap-3 px-0 py-2 first:pt-0 last:pb-0 sm:px-4 sm:py-2 sm:first:pt-2 sm:last:pb-2 sm:first:pl-0 sm:last:pr-0">
      <div className="flex items-center gap-3">
        <div className="flex h-9 w-9 items-center justify-center rounded-[10px] bg-[var(--app-surface-muted)] text-[var(--app-text-muted)]">
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
    <div className="flex shrink-0 items-baseline gap-2 whitespace-nowrap">
      <div className="shrink-0 text-[13px] font-semibold tracking-tight text-[var(--app-text)]">{label}:</div>
      <div className={`text-[13px] text-[var(--app-text-muted)] sm:text-[14px] ${valueClassName}`}>{value}</div>
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
    <div className="rounded-lg border border-[var(--app-border)] bg-[var(--app-surface-muted)] px-4 py-2">
      <div className="flex items-center gap-x-8 overflow-x-auto">
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

function RuntimeActionIcon({ icon, spinning = false }: { icon: RuntimeRowAction["icon"]; spinning?: boolean }) {
  if (spinning || icon === "restart") {
    return <RefreshCw className={cn("h-4 w-4", spinning && "animate-spin")} />;
  }

  if (icon === "start") {
    return <PlayerPlayFilled className="h-4 w-4" />;
  }

  return <PlayerStop className="h-3.5 w-3.5 fill-current" />;
}

function RuntimeCard({
  actionKey,
  error,
  loading,
  rows,
  onAction,
}: {
  actionKey: string | null;
  error: string | null;
  loading: boolean;
  rows: RuntimeRow[];
  onAction: (action: RuntimeRowAction, row: RuntimeRow) => void;
}) {
  return (
    <section className="overflow-hidden rounded-xl border border-[var(--app-border)] bg-[var(--app-bg-2)] shadow-[var(--app-shadow)]">
      <div className="border-b border-[var(--app-border)] px-4 py-3">
        <div className="text-[15px] font-semibold tracking-tight text-[var(--app-text)]">Installed apps</div>
      </div>

      {error ? (
        <div className="px-4 py-3 text-[13px] text-[var(--app-danger)]">{error}</div>
      ) : loading ? (
        <div className="px-4 py-5 text-[13px] text-[var(--app-text-muted)]">Inspecting installed apps...</div>
      ) : rows.length === 0 ? (
        <div className="px-4 py-5 text-[13px] text-[var(--app-text-muted)]">No installed runtime apps found.</div>
      ) : (
        <div className="divide-y divide-[var(--app-border)]">
          {rows.map((row) => {
            const busy = actionKey !== null || runtimeStateBusy(row.state);

            return (
              <div key={row.key} className="grid grid-cols-[minmax(0,1fr)_5rem_4.75rem] items-center gap-3 px-4 py-2.5">
                <div className="flex min-w-0 items-center gap-3">
                  <img src={row.icon} alt={row.iconAlt} className="h-8 w-8 shrink-0 object-contain" />
                  <span
                    className={cn(
                      "h-2.5 w-2.5 shrink-0 rounded-full",
                      row.running ? "bg-emerald-500" : "bg-[var(--app-text-muted)]"
                    )}
                    aria-hidden="true"
                  />
                  <div className="min-w-0 flex-1">
                    <div className="truncate text-sm font-medium text-[var(--app-text)]" title={row.name}>
                      {row.name}
                    </div>
                  </div>
                </div>

                <div className="truncate text-sm text-[var(--app-text)]" title={row.version}>
                  {row.version || "\u00a0"}
                </div>
                <div className="flex items-center justify-end gap-2">
                  {row.actions.map((action) => {
                    const actionBusy = actionKey === action.key || runtimeStateBusy(row.state);

                    return (
                      <Button
                        key={action.key}
                        type="button"
                        variant="outline"
                        size="icon"
                        className="h-8 w-8 rounded-md bg-[var(--app-surface-muted)]"
                        aria-label={`${action.label} ${row.name}`}
                        title={`${action.label} ${row.name}`}
                        disabled={busy}
                        onClick={() => onAction(action, row)}
                      >
                        <RuntimeActionIcon icon={action.icon} spinning={actionBusy} />
                      </Button>
                    );
                  })}
                </div>
              </div>
            );
          })}
        </div>
      )}
    </section>
  );
}

export function DashboardPage() {
  const [databaseCount, setDatabaseCount] = useState<number | null>(null);
  const [health, setHealth] = useState<OperationalHealth>({
    backup: null,
    database: null,
    runtime: null,
    webServer: null,
  });
  const [siteCount, setSiteCount] = useState<number | null>(null);
  const [systemStatus, setSystemStatus] = useState<SystemStatus | null>(null);
  const [systemStatusHistory, setSystemStatusHistory] = useState<SystemStatusSample[]>([]);
  const [loading, setLoading] = useState(true);
  const [pm2Status, setPM2Status] = useState<PM2Status | null>(null);
  const [pm2Processes, setPM2Processes] = useState<PM2Process[]>([]);
  const [pm2Loading, setPM2Loading] = useState(true);
  const [pm2Refreshing, setPM2Refreshing] = useState(false);
  const [pm2Error, setPM2Error] = useState<string | null>(null);
  const [pm2ProcessActionKey, setPM2ProcessActionKey] = useState<string | null>(null);
  const [pm2FormOpen, setPM2FormOpen] = useState(false);
  const [pm2FormTarget, setPM2FormTarget] = useState<PM2Process | null>(null);
  const [pm2FormSubmitting, setPM2FormSubmitting] = useState(false);
  const [pm2FormError, setPM2FormError] = useState<string | null>(null);
  const [pm2LogsOpen, setPM2LogsOpen] = useState(false);
  const [pm2LogsTarget, setPM2LogsTarget] = useState<PM2Process | null>(null);
  const [pm2LogsOutput, setPM2LogsOutput] = useState("");
  const [pm2LogsLoading, setPM2LogsLoading] = useState(false);
  const [pm2LogsClearing, setPM2LogsClearing] = useState(false);
  const [pm2LogsError, setPM2LogsError] = useState<string | null>(null);
  const [pm2DeleteCandidate, setPM2DeleteCandidate] = useState<Pick<PM2Process, "id" | "name"> | null>(null);
  const [installedApps, setInstalledApps] = useState<InstalledAppsData | null>(null);
  const [installedAppsLoading, setInstalledAppsLoading] = useState(true);
  const [installedAppsError, setInstalledAppsError] = useState<string | null>(null);
  const [installedAppsActionKey, setInstalledAppsActionKey] = useState<string | null>(null);

  const pm2RequestIdRef = useRef(0);
  const pm2LogsRequestIdRef = useRef(0);
  const pm2LogsContainerRef = useRef<HTMLDivElement | null>(null);
  const pm2LogsAutoScrollRef = useRef(true);

  async function loadInstalledApps(options?: { background?: boolean }) {
    if (!options?.background) {
      setInstalledAppsLoading(true);
    }

    setInstalledAppsError(null);

    try {
      setInstalledApps(await fetchInstalledAppsData());
    } catch (error) {
      setInstalledAppsError(getErrorMessage(error, "Failed to inspect installed apps."));
    } finally {
      setInstalledAppsLoading(false);
    }
  }

  async function handleInstalledAppAction(action: RuntimeRowAction, row: RuntimeRow) {
    if (installedAppsActionKey !== null) {
      return;
    }

    setInstalledAppsActionKey(action.key);
    setInstalledAppsError(null);

    try {
      await action.run();
      const pastTense = action.icon === "restart" ? "restarted" : action.icon === "start" ? "started" : "stopped";
      toast.success(`${row.name} ${pastTense}.`);
      void loadInstalledApps({ background: true });
    } catch (error) {
      const message = getErrorMessage(error, `Failed to ${action.label.toLowerCase()} ${row.name}.`);
      setInstalledAppsError(message);
      toast.error(message);
    } finally {
      setInstalledAppsActionKey((current) => (current === action.key ? null : current));
    }
  }

  function syncSystemStatus(status: SystemStatus, sampledAt = Date.now()) {
    setSystemStatus(status);
    setSystemStatusHistory((current) => appendSystemStatusSample(current, status, sampledAt));
  }

  const refreshSystemStatus = useEffectEvent(async () => {
    try {
      const nextStatus = await fetchSystemStatus();
      syncSystemStatus(nextStatus);
    } catch {
      // Keep the last successful snapshot instead of surfacing transient polling errors.
    }
  });

  const refreshOperationalHealth = useEffectEvent(async () => {
    try {
      setHealth(await fetchOperationalHealth());
    } catch {
      setHealth({ backup: null, database: null, runtime: null, webServer: null });
    }
  });

  function resetPM2LogsState() {
    pm2LogsRequestIdRef.current += 1;
    pm2LogsAutoScrollRef.current = true;
    setPM2LogsOutput("");
    setPM2LogsLoading(false);
    setPM2LogsClearing(false);
    setPM2LogsError(null);
  }

  function isScrolledToBottom(element: HTMLDivElement) {
    return element.scrollHeight - element.scrollTop - element.clientHeight <= pm2LogsBottomThresholdPx;
  }

  function syncPM2Processes(processes: PM2Process[]) {
    setPM2Processes(processes);
    setPM2DeleteCandidate((current) => (current && !processes.some((process) => process.id === current.id) ? null : current));

    let closeLogs = false;
    setPM2LogsTarget((current) => {
      if (current === null) {
        return current;
      }

      const nextTarget = processes.find((process) => process.id === current.id) ?? null;
      if (nextTarget !== null) {
        return nextTarget;
      }

      closeLogs = true;
      return null;
    });

    if (closeLogs) {
      setPM2LogsOpen(false);
      resetPM2LogsState();
    }
  }

  const loadPM2Overview = useEffectEvent(async (options?: { background?: boolean }) => {
    const preserveContent = Boolean(options?.background && pm2Processes.length > 0);
    const requestId = pm2RequestIdRef.current + 1;
    pm2RequestIdRef.current = requestId;

    if (preserveContent) {
      setPM2Refreshing(true);
    } else {
      setPM2Loading(true);
    }

    setPM2Error(null);

    try {
      const nextStatus = await fetchPM2Status();
      if (pm2RequestIdRef.current !== requestId) {
        return;
      }

      setPM2Status(nextStatus);
      if (!nextStatus.installed) {
        syncPM2Processes([]);
        return;
      }

      const nextProcesses = await fetchPM2Processes();
      if (pm2RequestIdRef.current !== requestId) {
        return;
      }

      syncPM2Processes(nextProcesses);
    } catch (error) {
      if (pm2RequestIdRef.current !== requestId) {
        return;
      }

      setPM2Error(getErrorMessage(error, "Failed to load PM2 processes."));
    } finally {
      if (pm2RequestIdRef.current === requestId) {
        setPM2Loading(false);
        setPM2Refreshing(false);
      }
    }
  });

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
    pm2LogsAutoScrollRef.current = true;
    setPM2LogsTarget(process);
    setPM2LogsOpen(true);
    resetPM2LogsState();
    void loadPM2Logs(process);
  }

  function handlePM2FormOpenChange(open: boolean) {
    setPM2FormOpen(open);
    if (!open) {
      setPM2FormTarget(null);
      setPM2FormError(null);
    }
  }

  async function handlePM2FormSubmit(input: PM2CreateProcessInput) {
    const editing = pm2FormTarget !== null;
    setPM2FormSubmitting(true);
    setPM2FormError(null);
    try {
      const nextProcesses = pm2FormTarget
        ? await updatePM2Process(pm2FormTarget.id, input)
        : await createPM2Process(input);
      syncPM2Processes(nextProcesses);
      handlePM2FormOpenChange(false);
      toast.success(`${input.name || input.script_path} ${editing ? "updated" : "created"}.`);
    } catch (error) {
      const message = getErrorMessage(error, `Failed to ${editing ? "update" : "create"} ${input.name || input.script_path}.`);
      setPM2FormError(message);
      toast.error(message);
    } finally {
      setPM2FormSubmitting(false);
    }
  }

  async function handlePM2ClearLogs(process: PM2Process) {
    if (pm2LogsClearing) {
      return;
    }

    const requestId = pm2LogsRequestIdRef.current + 1;
    pm2LogsRequestIdRef.current = requestId;
    setPM2LogsClearing(true);
    setPM2LogsError(null);

    try {
      await clearPM2ProcessLogs(process.id);
      if (pm2LogsRequestIdRef.current !== requestId) {
        return;
      }

      setPM2LogsOutput("");
      toast.success("PM2 logs cleared.");
    } catch (error) {
      if (pm2LogsRequestIdRef.current !== requestId) {
        return;
      }

      const message = getErrorMessage(error, `Failed to clear logs for ${process.name}.`);
      setPM2LogsError(message);
      toast.error(message);
    } finally {
      if (pm2LogsRequestIdRef.current === requestId) {
        setPM2LogsLoading(false);
        setPM2LogsClearing(false);
      }
    }
  }

  async function handlePM2ProcessAction(action: "start" | "stop" | "restart" | "delete", process: Pick<PM2Process, "id" | "name">) {
    const actionKey = `${action}:${process.id}`;
    const processLabel = process.name || `Process ${process.id}`;
    const successMessage =
      action === "start"
        ? `${processLabel} started.`
        : action === "stop"
          ? `${processLabel} stopped.`
          : action === "restart"
            ? `${processLabel} restarted.`
            : `${processLabel} deleted.`;
    const fallbackMessage =
      action === "start"
        ? `Failed to start ${processLabel}.`
        : action === "stop"
          ? `Failed to stop ${processLabel}.`
          : action === "restart"
            ? `Failed to restart ${processLabel}.`
            : `Failed to delete ${processLabel}.`;

    setPM2ProcessActionKey(actionKey);
    setPM2Error(null);

    try {
      const nextProcesses =
        action === "start"
          ? await startPM2Process(process.id)
          : action === "stop"
            ? await stopPM2Process(process.id)
            : action === "restart"
              ? await restartPM2Process(process.id)
              : await deletePM2Process(process.id);

      syncPM2Processes(nextProcesses);
      toast.success(successMessage);
    } catch (error) {
      const message = getErrorMessage(error, fallbackMessage);
      setPM2Error(message);
      toast.error(message);
    } finally {
      setPM2ProcessActionKey((current) => (current === actionKey ? null : current));
    }
  }

  useEffect(() => {
    let active = true;

    async function loadStatus() {
      const nextOverview = await fetchOverviewData();
      if (!active) {
        return;
      }

      setDatabaseCount(nextOverview.databaseCount);
      setHealth(nextOverview.health);
      setSiteCount(nextOverview.siteCount);
      if (nextOverview.systemStatus) {
        syncSystemStatus(nextOverview.systemStatus);
      }
      setLoading(false);
    }

    loadStatus();

    return () => {
      active = false;
    };
  }, []);

  useEffect(() => {
    void loadPM2Overview();

    return () => {
      pm2RequestIdRef.current += 1;
      pm2LogsRequestIdRef.current += 1;
    };
  }, []);

  useEffect(() => {
    void loadInstalledApps();

    const intervalId = window.setInterval(() => {
      void loadInstalledApps({ background: true });
    }, 15_000);

    return () => {
      window.clearInterval(intervalId);
    };
  }, []);

  useEffect(() => {
    const intervalId = window.setInterval(() => {
      void refreshSystemStatus();
      void refreshOperationalHealth();
    }, systemStatusRefreshIntervalMs);

    return () => {
      window.clearInterval(intervalId);
    };
  }, []);

  useEffect(() => {
    const intervalId = window.setInterval(() => {
      if (pm2Loading || pm2Refreshing || pm2ProcessActionKey !== null || pm2FormSubmitting) {
        return;
      }

      void loadPM2Overview({ background: true });
    }, pm2ProcessesRefreshIntervalMs);

    return () => {
      window.clearInterval(intervalId);
    };
  }, [pm2Loading, pm2Refreshing, pm2ProcessActionKey, pm2FormSubmitting]);

  useEffect(() => {
    if (!pm2LogsOpen || pm2LogsTarget === null) {
      return;
    }

    const intervalId = window.setInterval(() => {
      if (pm2LogsLoading || pm2LogsClearing || pm2ProcessActionKey !== null) {
        return;
      }

      void loadPM2Logs(pm2LogsTarget);
    }, pm2LogsRefreshIntervalMs);

    return () => {
      window.clearInterval(intervalId);
    };
  }, [pm2LogsClearing, pm2LogsLoading, pm2LogsOpen, pm2LogsTarget, pm2ProcessActionKey]);

  useEffect(() => {
    if (!pm2LogsOpen || pm2LogsContainerRef.current === null || !pm2LogsAutoScrollRef.current) {
      return;
    }

    const container = pm2LogsContainerRef.current;
    container.scrollTop = container.scrollHeight;
  }, [pm2LogsOpen, pm2LogsOutput]);

  const installedRuntimeRows: RuntimeRow[] = [];
  const addRuntimeRow = (row: RuntimeRow | false | null | undefined) => {
    if (row) {
      installedRuntimeRows.push(row);
    }
  };
  const addServiceRuntimeRow = ({
    key,
    name,
    icon,
    iconAlt,
    status,
    installed,
    running,
    version,
    start,
    stop,
    restart,
  }: {
    key: string;
    name: string;
    icon: string;
    iconAlt: string;
    status?: { state?: string; start_available?: boolean; stop_available?: boolean; restart_available?: boolean } | null;
    installed: boolean;
    running: boolean;
    version?: string;
    start?: () => Promise<unknown>;
    stop?: () => Promise<unknown>;
    restart?: () => Promise<unknown>;
  }) => {
    if (!installed || !status) {
      return;
    }

    addRuntimeRow({
      key,
      name,
      icon,
      iconAlt,
      version: normalizeVersion(version),
      status: runtimeStatusLabel(status.state, running),
      running,
      state: status.state,
      actions: buildServiceRuntimeActions({
        key,
        restartAvailable: status.restart_available,
        startAvailable: status.start_available,
        stopAvailable: status.stop_available,
        onRestart: restart ? async () => void (await restart()) : undefined,
        onStart: start ? async () => void (await start()) : undefined,
        onStop: stop ? async () => void (await stop()) : undefined,
      }),
    });
  };

  if (installedApps?.caddy) {
    addRuntimeRow({
      key: "caddy",
      name: "Caddy",
      icon: "/application-icons/caddy.svg",
      iconAlt: "Caddy logo",
      version: "-",
      status: runtimeStatusLabel(installedApps.caddy.state, installedApps.caddy.started),
      running: installedApps.caddy.started,
      state: installedApps.caddy.state,
      actions: buildServiceRuntimeActions({
        key: "caddy",
        restartAvailable: installedApps.caddy.restart_available,
        onRestart: async () => void (await restartCaddy()),
      }),
    });
  }

  for (const runtime of installedApps?.php?.versions ?? []) {
    const installed = runtime.php_installed || runtime.fpm_installed;
    addServiceRuntimeRow({
      key: `php-${runtime.version}`,
      name: `PHP ${runtime.version}`,
      icon: "/application-icons/php.svg",
      iconAlt: "PHP logo",
      status: runtime,
      installed,
      running: runtime.service_running,
      version: runtime.php_version || runtime.version,
      start: async () => startPHP(runtime.version),
      stop: async () => stopPHP(runtime.version),
      restart: async () => restartPHP(runtime.version),
    });
  }

  addServiceRuntimeRow({
    key: "mariadb",
    name: "MariaDB",
    icon: "/application-icons/mariadb.png",
    iconAlt: "MariaDB logo",
    status: installedApps?.mariadb,
    installed: Boolean(installedApps?.mariadb?.server_installed || installedApps?.mariadb?.client_installed),
    running: Boolean(installedApps?.mariadb?.service_running),
    version: installedApps?.mariadb?.version ? formatMariaDBVersion(installedApps.mariadb.version) : undefined,
    start: startMariaDB,
    stop: stopMariaDB,
    restart: restartMariaDB,
  });
  addServiceRuntimeRow({
    key: "docker",
    name: "Docker",
    icon: "/application-icons/docker.svg",
    iconAlt: "Docker logo",
    status: installedApps?.docker,
    installed: Boolean(installedApps?.docker?.installed),
    running: Boolean(installedApps?.docker?.service_running),
    version: installedApps?.docker?.version,
    start: startDocker,
    stop: stopDocker,
    restart: restartDocker,
  });
  addServiceRuntimeRow({
    key: "redis",
    name: "Redis",
    icon: "/application-icons/redis.svg",
    iconAlt: "Redis logo",
    status: installedApps?.redis,
    installed: Boolean(installedApps?.redis?.installed),
    running: Boolean(installedApps?.redis?.service_running),
    version: installedApps?.redis?.version,
    start: startRedis,
    stop: stopRedis,
    restart: restartRedis,
  });
  addServiceRuntimeRow({
    key: "mongodb",
    name: "MongoDB",
    icon: "/application-icons/mongodb.svg",
    iconAlt: "MongoDB logo",
    status: installedApps?.mongodb,
    installed: Boolean(installedApps?.mongodb?.installed),
    running: Boolean(installedApps?.mongodb?.service_running),
    version: installedApps?.mongodb?.version,
    start: startMongoDB,
    stop: stopMongoDB,
    restart: restartMongoDB,
  });
  addServiceRuntimeRow({
    key: "postgresql",
    name: "PostgreSQL",
    icon: "/application-icons/postgresql.svg",
    iconAlt: "PostgreSQL logo",
    status: installedApps?.postgresql,
    installed: Boolean(installedApps?.postgresql?.installed),
    running: Boolean(installedApps?.postgresql?.service_running),
    version: installedApps?.postgresql?.version,
    start: startPostgreSQL,
    stop: stopPostgreSQL,
    restart: restartPostgreSQL,
  });

  addRuntimeRow(installedApps?.nodejs?.installed && {
    key: "nodejs",
    name: "Node.js",
    icon: "/application-icons/nodejs.svg",
    iconAlt: "Node.js logo",
    version: normalizeVersion(installedApps.nodejs.version),
    status: runtimeStatusLabel(installedApps.nodejs.state, false, false),
    running: true,
    state: installedApps.nodejs.state,
    actions: [],
  });
  addRuntimeRow(installedApps?.golang?.installed && {
    key: "golang",
    name: "Go",
    icon: "/application-icons/go.png",
    iconAlt: "Go logo",
    version: normalizeVersion(installedApps.golang.version),
    status: runtimeStatusLabel(installedApps.golang.state, false, false),
    running: true,
    state: installedApps.golang.state,
    actions: [],
  });
  addRuntimeRow(installedApps?.ffmpeg?.installed && {
    key: "ffmpeg",
    name: "FFmpeg",
    icon: "/application-icons/ffmpeg.svg",
    iconAlt: "FFmpeg logo",
    version: normalizeVersion(installedApps.ffmpeg.version),
    status: runtimeStatusLabel(installedApps.ffmpeg.state, false, false),
    running: true,
    state: installedApps.ffmpeg.state,
    actions: [],
  });
  addRuntimeRow(installedApps?.ytdlp?.installed && {
    key: "ytdlp",
    name: "yt-dlp",
    icon: "/application-icons/ytdlp.svg",
    iconAlt: "yt-dlp logo",
    version: normalizeVersion(installedApps.ytdlp.version),
    status: runtimeStatusLabel(installedApps.ytdlp.state, false, false),
    running: true,
    state: installedApps.ytdlp.state,
    actions: [],
  });
  addRuntimeRow(installedApps?.phpmyadmin?.installed && {
    key: "phpmyadmin",
    name: "phpMyAdmin",
    icon: "/application-icons/phpmyadmin.png",
    iconAlt: "phpMyAdmin logo",
    version: normalizeVersion(installedApps.phpmyadmin.version),
    status: runtimeStatusLabel(installedApps.phpmyadmin.state, true),
    running: true,
    state: installedApps.phpmyadmin.state,
    actions: [],
  });

  const hasTotals = siteCount !== null || databaseCount !== null;
  const showOverview = Boolean(systemStatus || hasTotals);
  const pm2ProcessCountLabel = `${pm2Processes.length} ${pm2Processes.length === 1 ? "process" : "processes"}`;
  const pm2DeleteDialogTitle = pm2DeleteCandidate ? `Delete ${pm2DeleteCandidate.name || `process ${pm2DeleteCandidate.id}`}` : "Delete PM2 process";
  const pm2DeleteDialogDescription = pm2DeleteCandidate
    ? `Delete ${pm2DeleteCandidate.name || `process ${pm2DeleteCandidate.id}`} from PM2? The process will be removed from the runtime list and must be created again to restore it.`
    : "Delete this PM2 process?";
  const pm2ProcessesSection = pm2Status?.installed === false ? null : (
    <section className="rounded-xl border border-[var(--app-border)] bg-[var(--app-bg-2)] px-4 py-4 shadow-[var(--app-shadow)]">
      <div className="flex min-w-0 items-center justify-between gap-3">
        <div className="flex min-w-0 items-center gap-2">
          <div className="text-[15px] font-semibold tracking-tight text-[var(--app-text)]">PM2 processes</div>
          {pm2Status?.installed ? <Badge variant="secondary">{pm2ProcessCountLabel}</Badge> : null}
        </div>
        <Button type="button" variant="outline" size="sm" className="h-7 w-7 shrink-0 p-0" onClick={() => setPM2FormOpen(true)} disabled={!pm2Status?.installed || pm2FormSubmitting} aria-label="Add PM2 process" title="Add PM2 process">
          <Plus className="h-4 w-4" />
        </Button>
      </div>

      <div className="mt-3">
        <PM2ProcessList
          mode="dashboard"
          processes={pm2Processes}
          error={pm2Error}
          loading={pm2Loading}
          busy={pm2ProcessActionKey !== null || pm2FormSubmitting}
          processActionKey={pm2ProcessActionKey}
          onProcessAction={(action, process) => {
            void handlePM2ProcessAction(action, process);
          }}
          onDelete={(process) => {
            setPM2DeleteCandidate(process);
          }}
          onEdit={(process) => {
            setPM2FormTarget(process);
            setPM2FormOpen(true);
          }}
          onOpenLogs={openPM2Logs}
        />
      </div>
    </section>
  );
  const leftDashboardSection = systemStatus ? (
    <div className="space-y-5">
      {pm2ProcessesSection}
      <RuntimeCard
        actionKey={installedAppsActionKey}
        error={installedAppsError}
        loading={installedAppsLoading}
        rows={installedRuntimeRows}
        onAction={handleInstalledAppAction}
      />
    </div>
  ) : null;

  return (
    <>
      <PM2ProcessDialog
        open={pm2FormOpen}
        process={pm2FormTarget}
        submitting={pm2FormSubmitting}
        error={pm2FormError}
        onOpenChange={handlePM2FormOpenChange}
        onSubmit={(input) => void handlePM2FormSubmit(input)}
      />
      <div className="px-4 pb-3 pt-4 sm:px-6 lg:px-8">
        <SystemInfoCard status={systemStatus} />
      </div>

      <div className="px-4 pb-6 pt-3 sm:px-6 lg:px-8">
        {loading ? (
          <section className="rounded-xl border border-[var(--app-border)] bg-[var(--app-bg-2)] px-5 py-8 text-[13px] text-[var(--app-text-muted)] shadow-[var(--app-shadow)]">
            Inspecting local services...
          </section>
        ) : (
          <section className="space-y-5">
            {showOverview ? (
              systemStatus ? (
                <SystemStatusCard
                  databaseCount={databaseCount}
                  health={health}
                  history={systemStatusHistory}
                  leftContent={leftDashboardSection}
                  siteCount={siteCount}
                  status={systemStatus}
                />
              ) : hasTotals ? (
                <OverviewCard databaseCount={databaseCount} siteCount={siteCount} />
              ) : null
            ) : null}

            {systemStatus ? null : (
              <>
                {pm2ProcessesSection}
                <RuntimeCard
                  actionKey={installedAppsActionKey}
                  error={installedAppsError}
                  loading={installedAppsLoading}
                  rows={installedRuntimeRows}
                  onAction={handleInstalledAppAction}
                />
              </>
            )}
          </section>
        )}
      </div>

      <Dialog
        open={pm2LogsOpen}
        onOpenChange={(open) => {
          setPM2LogsOpen(open);
          if (!open) {
            setPM2LogsTarget(null);
            resetPM2LogsState();
          }
        }}
      >
        <DialogContent className="h-[min(80vh,calc(100vh-2rem))] grid-rows-[auto_auto_minmax(0,1fr)] overflow-hidden sm:max-w-4xl">
          <DialogHeader>
            <DialogTitle>{pm2LogsTarget ? `${pm2LogsTarget.name} logs` : "PM2 process logs"}</DialogTitle>
            <DialogDescription>
              {pm2LogsTarget ? `Recent output for process ${pm2LogsTarget.id}.` : "Recent PM2 process output."}
            </DialogDescription>
          </DialogHeader>

          <div className="flex items-center justify-end">
            <Button
              type="button"
              variant="outline"
              size="sm"
              className="text-[var(--app-danger)] hover:bg-[var(--app-danger-soft)] hover:text-[var(--app-danger)]"
              onClick={() => {
                if (pm2LogsTarget) {
                  void handlePM2ClearLogs(pm2LogsTarget);
                }
              }}
              disabled={pm2LogsClearing || pm2LogsTarget === null}
            >
              {pm2LogsClearing ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <Trash2 className="h-4 w-4" />}
              Clear logs
            </Button>
          </div>

          <div
            ref={pm2LogsContainerRef}
            className="min-h-0 overflow-auto rounded-lg border border-[var(--app-border)] bg-[var(--app-surface-muted)]"
            onScroll={(event) => {
              pm2LogsAutoScrollRef.current = isScrolledToBottom(event.currentTarget);
            }}
          >
            {pm2LogsError ? (
              <div className="p-4 text-sm text-[var(--app-danger)]">{pm2LogsError}</div>
            ) : pm2LogsLoading && !pm2LogsOutput ? (
              <div className="flex h-full items-center justify-center gap-2 p-4 text-sm text-[var(--app-text-muted)]">
                <LoaderCircle className="h-4 w-4 animate-spin" />
                Loading logs...
              </div>
            ) : (
              <pre className="whitespace-pre-wrap break-words p-4 font-mono text-xs leading-5 text-[var(--app-text)]">
                {pm2LogsOutput || "No log output available."}
              </pre>
            )}
          </div>
        </DialogContent>
      </Dialog>

      <ActionConfirmDialog
        open={pm2DeleteCandidate !== null}
        onOpenChange={(open) => {
          if (!open) {
            setPM2DeleteCandidate(null);
          }
        }}
        title={pm2DeleteDialogTitle}
        desc={pm2DeleteDialogDescription}
        confirmText="Delete"
        destructive
        isLoading={pm2DeleteCandidate !== null && pm2ProcessActionKey === `delete:${pm2DeleteCandidate.id}`}
        handleConfirm={() => {
          if (pm2DeleteCandidate) {
            const candidate = pm2DeleteCandidate;
            setPM2DeleteCandidate(null);
            void handlePM2ProcessAction("delete", candidate);
          }
        }}
      />
    </>
  );
}
