import { useEffect, useEffectEvent, useRef, useState, type ReactNode } from "react";
import { fetchDomains } from "@/api/domains";
import { fetchMariaDBDatabases } from "@/api/mariadb";
import {
  clearPM2ProcessLogs,
  deletePM2Process,
  fetchPM2ProcessLogs,
  fetchPM2Processes,
  fetchPM2Status,
  restartPM2Process,
  startPM2Process,
  stopPM2Process,
  type PM2Process,
  type PM2Status,
} from "@/api/pm2";
import { fetchSystemStatus, type SystemStatus } from "@/api/system";
import { ActionConfirmDialog } from "@/components/action-confirm-dialog";
import { DiskUsageCard } from "@/components/disk-usage-card";
import { LoaderCircle, Trash2, Database, World } from "@/components/icons/tabler-icons";
import { PM2ProcessList } from "@/components/pm2-process-list";
import { SystemMetricsCard, type SystemStatusSample } from "@/components/system-metrics-card";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { SystemStatusCard } from "@/components/system-status-card";
import { getErrorMessage } from "@/lib/utils";
import { toast } from "sonner";

const systemStatusRefreshIntervalMs = 5_000;
const systemStatusHistoryLimit = 60;
const pm2ProcessesRefreshIntervalMs = 10_000;
const pm2LogsRefreshIntervalMs = 2_000;
const pm2LogsBottomThresholdPx = 24;
const dashboardSplitGridClassName = "grid gap-5 xl:grid-cols-[minmax(0,7fr)_minmax(320px,5fr)]";

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
    <div className="flex min-w-0 items-baseline gap-2">
      <div className="shrink-0 text-[13px] font-semibold tracking-tight text-[var(--app-text)]">{label}:</div>
      <div className={`min-w-0 text-[13px] text-[var(--app-text-muted)] sm:text-[14px] ${valueClassName}`}>{value}</div>
    </div>
  );
}

function formatPM2Meta(status: PM2Status | null, processes: PM2Process[]) {
  const toolchain = status?.binary_path?.trim() || "pm2";
  const countLabel = `${processes.length} ${processes.length === 1 ? "process" : "processes"}`;

  return { countLabel, toolchain };
}

function appendSystemStatusSample(history: SystemStatusSample[], status: SystemStatus, sampledAt = Date.now()) {
  const nextSample = { sampledAt, status };
  const lastSample = history[history.length - 1];

  if (lastSample && lastSample.status.server_time === status.server_time) {
    return [...history.slice(0, -1), nextSample];
  }

  const nextHistory = [...history, nextSample];
  return nextHistory.length > systemStatusHistoryLimit ? nextHistory.slice(-systemStatusHistoryLimit) : nextHistory;
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
        <div className="flex flex-col gap-3 sm:flex-row sm:flex-wrap sm:items-center sm:gap-x-8">
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

export function DashboardPage() {
  const [databaseCount, setDatabaseCount] = useState<number | null>(null);
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
  const [pm2LogsOpen, setPM2LogsOpen] = useState(false);
  const [pm2LogsTarget, setPM2LogsTarget] = useState<PM2Process | null>(null);
  const [pm2LogsOutput, setPM2LogsOutput] = useState("");
  const [pm2LogsLoading, setPM2LogsLoading] = useState(false);
  const [pm2LogsClearing, setPM2LogsClearing] = useState(false);
  const [pm2LogsError, setPM2LogsError] = useState<string | null>(null);
  const [pm2DeleteCandidate, setPM2DeleteCandidate] = useState<Pick<PM2Process, "id" | "name"> | null>(null);

  const pm2RequestIdRef = useRef(0);
  const pm2LogsRequestIdRef = useRef(0);
  const pm2LogsContainerRef = useRef<HTMLDivElement | null>(null);
  const pm2LogsAutoScrollRef = useRef(true);

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
    const intervalId = window.setInterval(() => {
      void refreshSystemStatus();
    }, systemStatusRefreshIntervalMs);

    return () => {
      window.clearInterval(intervalId);
    };
  }, []);

  useEffect(() => {
    const intervalId = window.setInterval(() => {
      if (pm2Loading || pm2Refreshing || pm2ProcessActionKey !== null) {
        return;
      }

      void loadPM2Overview({ background: true });
    }, pm2ProcessesRefreshIntervalMs);

    return () => {
      window.clearInterval(intervalId);
    };
  }, [pm2Loading, pm2Refreshing, pm2ProcessActionKey]);

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

  const hasTotals = siteCount !== null || databaseCount !== null;
  const showOverview = Boolean(systemStatus || hasTotals);
  const pm2Meta = formatPM2Meta(pm2Status, pm2Processes);
  const pm2DeleteDialogTitle = pm2DeleteCandidate ? `Delete ${pm2DeleteCandidate.name || `process ${pm2DeleteCandidate.id}`}` : "Delete PM2 process";
  const pm2DeleteDialogDescription = pm2DeleteCandidate
    ? `Delete ${pm2DeleteCandidate.name || `process ${pm2DeleteCandidate.id}`} from PM2? The process will be removed from the runtime list and must be created again to restore it.`
    : "Delete this PM2 process?";
  const pm2ProcessesSection = (
    <section className="rounded-xl border border-[var(--app-border)] bg-[var(--app-bg-2)] px-4 py-4 shadow-[var(--app-shadow)]">
      <div className="min-w-0">
        <div className="min-w-0">
          <div className="text-[15px] font-semibold tracking-tight text-[var(--app-text)]">PM2 processes</div>
          <div className="mt-1 flex flex-wrap gap-x-3 gap-y-1 text-[12px] text-[var(--app-text-muted)]">
            <span className="font-mono">{pm2Meta.toolchain}</span>
            {pm2Status?.installed ? <span>{pm2Meta.countLabel}</span> : null}
            {pm2Status && !pm2Status.installed && pm2Status.message ? <span>{pm2Status.message}</span> : null}
          </div>
        </div>
      </div>

      <div className="mt-3">
        {pm2Status && !pm2Status.installed && !pm2Error ? (
          <div className="rounded-md border border-dashed border-[var(--app-border)] bg-[var(--app-surface-muted)] px-4 py-6 text-sm text-[var(--app-text-muted)]">
            PM2 is not installed on this node.
          </div>
        ) : (
          <PM2ProcessList
            mode="dashboard"
            processes={pm2Processes}
            error={pm2Error}
            loading={pm2Loading}
            busy={pm2ProcessActionKey !== null}
            processActionKey={pm2ProcessActionKey}
            onProcessAction={(action, process) => {
              void handlePM2ProcessAction(action, process);
            }}
            onDelete={(process) => {
              setPM2DeleteCandidate(process);
            }}
            onOpenLogs={openPM2Logs}
          />
        )}
      </div>
    </section>
  );

  return (
    <>
      <div className="px-4 pb-3 pt-4 sm:px-6 lg:px-8">
        <SystemInfoCard status={systemStatus} />
      </div>

      <div className="px-4 pb-6 pt-3 sm:px-6 lg:px-8">
        {loading ? (
          <section className="rounded-2xl border border-[var(--app-border)] bg-[var(--app-bg-2)] px-5 py-8 text-[13px] text-[var(--app-text-muted)] shadow-[var(--app-shadow)]">
            Inspecting local services...
          </section>
        ) : (
          <section className="space-y-5">
            {showOverview ? (
              systemStatus ? (
                <div className={dashboardSplitGridClassName}>
                  <SystemStatusCard status={systemStatus} />
                  <div className="space-y-5">
                    <DiskUsageCard status={systemStatus} />
                    {hasTotals ? <OverviewCard databaseCount={databaseCount} siteCount={siteCount} /> : null}
                  </div>
                </div>
              ) : hasTotals ? (
                <OverviewCard databaseCount={databaseCount} siteCount={siteCount} />
              ) : null
            ) : null}

            {systemStatus ? (
              <div className={dashboardSplitGridClassName}>
                {pm2ProcessesSection}
                <SystemMetricsCard history={systemStatusHistory} status={systemStatus} />
              </div>
            ) : (
              pm2ProcessesSection
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
