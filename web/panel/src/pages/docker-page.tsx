import { type FormEvent, useEffect, useRef, useState } from "react";
import {
  createDockerContainer,
  deleteDockerContainer,
  downloadDockerContainerSnapshot,
  fetchDockerContainerDetails,
  fetchDockerContainerLogs,
  fetchDockerContainers,
  fetchDockerImages,
  fetchDockerStatus,
  recreateDockerContainer,
  restartDockerContainer,
  saveDockerContainerAsImage,
  startDockerContainer,
  stopDockerContainer,
  type DockerContainer,
  type DockerContainerDetails,
  type DockerContainerPortMapping,
  type DockerHubImage,
  type DockerImage,
  type DockerStatus,
  searchDockerHubImages,
} from "@/api/docker";
import {
  ChevronDownIcon,
  Docker,
  Download,
  DotsVertical,
  HardDrive,
  LoaderCircle,
  Package,
  PlayerPlayFilled,
  Plus,
  RefreshCw,
  RotateCcw,
  Search,
  Settings,
  PlayerStop,
  Trash2,
} from "@/components/icons/tabler-icons";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { PageHeader } from "@/components/page-header";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { formatBytes } from "@/lib/format";
import { cn, getErrorMessage } from "@/lib/utils";
import { toast } from "sonner";

type LoadOptions = {
  silent?: boolean;
};

type DockerTab = "containers" | "images";
type DockerContainerAction = "start" | "stop" | "restart";
type DockerContainerMenuAction = "recreate" | "delete" | "snapshot" | "save-image";
type DockerContainerOperation = DockerContainerAction | DockerContainerMenuAction;
type DockerContainerLogsState = {
  output: string;
  loading: boolean;
  refreshing: boolean;
  error: string | null;
};

type DockerContainerResourcesState = {
  details: DockerContainerDetails | null;
  loading: boolean;
  refreshing: boolean;
  error: string | null;
};

type DockerContainerActionErrors = Record<string, string>;

function getContainerStateMeta(state: DockerContainer["state"]) {
  switch (state) {
    case "running":
      return { label: "Running", dotClassName: "bg-[var(--app-ok)]" };
    case "restarting":
      return { label: "Restarting", dotClassName: "bg-[var(--app-warning)]" };
    case "paused":
      return { label: "Paused", dotClassName: "bg-[var(--app-warning)]" };
    case "created":
      return { label: "Stopped", dotClassName: "bg-muted-foreground/60" };
    case "dead":
      return { label: "Dead", dotClassName: "bg-[var(--app-danger)]" };
    case "exited":
      return { label: "Stopped", dotClassName: "bg-muted-foreground/60" };
    default:
      return { label: "Unknown", dotClassName: "bg-muted-foreground/60" };
  }
}

function getPageMeta(
  status: DockerStatus | null,
  containers: DockerContainer[],
  images: DockerImage[],
  activeTab: DockerTab,
) {
  if (!status) {
    if (activeTab === "containers" && containers.length > 0) {
      return `${containers.length} containers found on this node.`;
    }
    if (activeTab === "images" && images.length > 0) {
      return `${images.length} Docker images found on this node.`;
    }
    return activeTab === "containers"
      ? "Container inventory for this node."
      : "Docker image inventory for this node.";
  }

  if (!status.installed) {
    return "Docker is not installed on this node yet.";
  }

  if (!status.service_running) {
    return "Docker is installed, but the daemon is not running.";
  }

  if (activeTab === "containers") {
    const runningCount = containers.filter((container) => container.state === "running").length;
    if (containers.length === 0) {
      return "Docker is running. No containers were found.";
    }
    return `${containers.length} containers found, ${runningCount} running.`;
  }

  if (images.length === 0) {
    return "Docker is running. No images were found.";
  }

  return `${images.length} Docker images found on this node.`;
}

function getContainerLabel(container: DockerContainer) {
  const trimmedName = container.name.trim();
  if (trimmedName) {
    return trimmedName;
  }

  return container.id.slice(0, 12);
}

function isContainerStartable(state: DockerContainer["state"]) {
  switch (state) {
    case "running":
    case "restarting":
    case "paused":
      return false;
    default:
      return true;
  }
}

function getContainerActions(container: DockerContainer) {
  if (isContainerStartable(container.state)) {
    return [{ key: "start", label: "Start", icon: PlayerPlayFilled }] as const;
  }

  return [
    { key: "stop", label: "Stop", icon: PlayerStop },
    { key: "restart", label: "Restart", icon: RotateCcw },
  ] as const;
}

function getContainerActionPendingLabel(action: DockerContainerAction | null) {
  switch (action) {
    case "start":
      return "Starting...";
    case "stop":
      return "Stopping...";
    case "restart":
      return "Restarting...";
    default:
      return "";
  }
}

function getContainerOperationPendingLabel(action: DockerContainerOperation | null) {
  switch (action) {
    case "delete":
      return "Deleting...";
    case "recreate":
      return "Recreating...";
    case "snapshot":
      return "Downloading...";
    case "save-image":
      return "Saving...";
    default:
      return getContainerActionPendingLabel(action);
  }
}

function getSuggestedDockerImageName(container: DockerContainer) {
  const label = getContainerLabel(container)
    .toLowerCase()
    .replace(/[^a-z0-9._-]+/g, "-")
    .replace(/-+/g, "-")
    .replace(/^-|-$/g, "");

  return `${label || "container"}:snapshot`;
}

function getContainerActionSuccessMessage(action: DockerContainerAction, container: DockerContainer) {
  switch (action) {
    case "start":
      return `Started container ${getContainerLabel(container)}.`;
    case "stop":
      return `Stopped container ${getContainerLabel(container)}.`;
    case "restart":
      return `Restarted container ${getContainerLabel(container)}.`;
  }
}

function sortDockerContainers(containers: DockerContainer[]) {
  return [...containers].sort((left, right) =>
    getContainerLabel(left).toLowerCase().localeCompare(getContainerLabel(right).toLowerCase()),
  );
}

function createDockerContainerLogsState(): DockerContainerLogsState {
  return {
    output: "",
    loading: false,
    refreshing: false,
    error: null,
  };
}

function createDockerContainerResourcesState(): DockerContainerResourcesState {
  return {
    details: null,
    loading: false,
    refreshing: false,
    error: null,
  };
}

function clampPercent(value: number | null | undefined) {
  if (value == null || Number.isNaN(value)) {
    return null;
  }

  return Math.max(0, Math.min(100, value));
}

function getResourceBarColor(percent: number | null) {
  if (percent == null) {
    return "var(--app-border-strong)";
  }

  if (percent < 70) {
    return "var(--app-ok)";
  }

  if (percent < 90) {
    return "var(--app-warning)";
  }

  return "var(--app-danger)";
}

function formatDockerResourcePercent(value: number | null | undefined) {
  if (value == null || Number.isNaN(value)) {
    return "—";
  }

  return value >= 10 ? `${Math.round(value)}%` : `${value.toFixed(1)}%`;
}

function formatDockerMemorySummary(details: DockerContainerDetails | null) {
  const used = details?.memory_usage_bytes;
  const limit = details?.memory_limit_bytes;

  if (used == null && limit == null) {
    return "Live usage unavailable";
  }

  if (used != null && limit != null) {
    return `${formatBytes(used)} of ${formatBytes(limit)}`;
  }

  return formatBytes(used ?? limit ?? 0);
}

function formatDockerPortMapping(port: DockerContainerPortMapping) {
  const containerPort = port.container_port.split("/")[0] || port.container_port;

  if (!port.host_port) {
    return containerPort;
  }

  if (port.public) {
    return `${containerPort} to ${port.host_port} (public)`;
  }

  if (port.host_ip) {
    return `${containerPort} to ${port.host_ip}:${port.host_port}`;
  }

  return `${containerPort} to ${port.host_port}`;
}

function ResourceMeter({
  detail,
  label,
  percent,
}: {
  detail: string;
  label: string;
  percent: number | null;
}) {
  const normalized = clampPercent(percent);

  return (
    <div className="space-y-2">
      <div className="flex items-baseline justify-between gap-3">
        <div className="text-sm font-medium text-foreground">{label}</div>
        <div className="text-sm text-muted-foreground">{detail}</div>
      </div>
      <div
        aria-label={`${label} usage`}
        aria-valuemax={100}
        aria-valuemin={0}
        aria-valuenow={normalized == null ? undefined : Math.round(normalized)}
        className="h-2.5 overflow-hidden rounded-md border border-[var(--app-border)] bg-[var(--app-bg-2)]"
        role="progressbar"
      >
        <div
          className="h-full rounded-[3px] transition-[width,background-color] duration-200"
          style={{
            width: `${normalized ?? 0}%`,
            backgroundColor: getResourceBarColor(normalized),
          }}
        />
      </div>
    </div>
  );
}

function ContainersSkeleton() {
  return (
    <div className="overflow-hidden rounded-2xl border border-[var(--app-border)] bg-[var(--app-bg-2)] shadow-[var(--app-shadow)]">
      <div className="hidden grid-cols-[minmax(0,1.05fr)_minmax(0,1.15fr)_minmax(140px,0.55fr)_120px] gap-6 border-b border-[var(--app-border)] px-6 py-4 text-sm text-muted-foreground md:grid">
        <div>Name</div>
        <div>Image</div>
        <div>Status</div>
        <div className="text-right">Actions</div>
      </div>
      {Array.from({ length: 4 }).map((_, index) => (
        <div
          key={index}
          className="grid gap-4 border-b border-[var(--app-border)] px-4 py-4 last:border-b-0 md:grid-cols-[minmax(0,1.05fr)_minmax(0,1.15fr)_minmax(140px,0.55fr)_120px] md:px-6"
        >
          <div className="h-5 w-40 animate-pulse rounded bg-[var(--app-surface)]" />
          <div className="h-5 w-52 animate-pulse rounded bg-[var(--app-surface)]" />
          <div className="h-5 w-24 animate-pulse rounded bg-[var(--app-surface)]" />
          <div className="flex justify-start gap-2 md:justify-end">
            <div className="h-9 w-9 animate-pulse rounded-md bg-[var(--app-surface)]" />
            <div className="h-9 w-9 animate-pulse rounded-md bg-[var(--app-surface)]" />
          </div>
        </div>
      ))}
    </div>
  );
}

function ImagesSkeleton() {
  return (
    <div className="overflow-hidden rounded-2xl border border-[var(--app-border)] bg-[var(--app-bg-2)] shadow-[var(--app-shadow)]">
      <div className="hidden grid-cols-[minmax(0,1.2fr)_160px_140px_140px] gap-6 border-b border-[var(--app-border)] px-6 py-4 text-sm text-muted-foreground md:grid">
        <div>Repository</div>
        <div>Tag</div>
        <div>Size</div>
        <div>Created</div>
      </div>
      {Array.from({ length: 4 }).map((_, index) => (
        <div
          key={index}
          className="grid gap-4 border-b border-[var(--app-border)] px-4 py-4 last:border-b-0 md:grid-cols-[minmax(0,1.2fr)_160px_140px_140px] md:px-6"
        >
          <div className="h-5 w-44 animate-pulse rounded bg-[var(--app-surface)]" />
          <div className="h-5 w-20 animate-pulse rounded bg-[var(--app-surface)]" />
          <div className="h-5 w-16 animate-pulse rounded bg-[var(--app-surface)]" />
          <div className="h-5 w-24 animate-pulse rounded bg-[var(--app-surface)]" />
        </div>
      ))}
    </div>
  );
}

type EmptyStateProps = {
  title: string;
  description: string;
};

function DockerEmptyState({ title, description }: EmptyStateProps) {
  return (
    <div className="flex min-h-[320px] items-center justify-center rounded-2xl border border-[var(--app-border)] bg-[var(--app-bg-2)] px-6 py-10 text-center shadow-[var(--app-shadow)]">
      <div className="max-w-md space-y-4">
        <div className="mx-auto flex h-14 w-14 items-center justify-center rounded-2xl border border-[var(--app-border)] bg-[var(--app-surface)]">
          <Docker className="h-7 w-7" />
        </div>
        <div className="space-y-2">
          <h2 className="text-xl font-semibold tracking-tight text-foreground">{title}</h2>
          <p className="text-sm leading-6 text-muted-foreground">{description}</p>
        </div>
      </div>
    </div>
  );
}

function TabButton({
  active,
  label,
  count,
  onClick,
}: {
  active: boolean;
  label: string;
  count: number;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      role="tab"
      aria-selected={active}
      onClick={onClick}
      className={cn(
        "flex min-w-0 flex-1 items-center justify-between gap-3 rounded-xl px-4 py-3 text-left transition-colors",
        active
          ? "bg-background text-foreground shadow-[var(--app-shadow)]"
          : "text-muted-foreground hover:bg-[var(--app-surface)] hover:text-foreground",
      )}
    >
      <span className="truncate text-sm font-medium">{label}</span>
      <span
        className={cn(
          "rounded-full px-2 py-0.5 text-xs font-medium",
          active ? "bg-[var(--app-surface)] text-foreground" : "bg-[var(--app-surface)]/70",
        )}
      >
        {count}
      </span>
    </button>
  );
}

function ContainerResourcesPanel({
  container,
  resources,
}: {
  container: DockerContainer;
  resources: DockerContainerResourcesState;
}) {
  const cpuPercent = clampPercent(resources.details?.cpu_percent);
  const memoryPercent = clampPercent(resources.details?.memory_percent);
  const portMappings = resources.details?.ports ?? [];
  const metricsUnavailable = cpuPercent == null && memoryPercent == null;

  return (
    <div className="rounded-xl border border-[var(--app-border)] bg-[var(--app-bg-2)] p-4 sm:p-5">
      <div className="flex items-start justify-between gap-3">
        <div className="space-y-1">
          <div className="text-sm font-medium text-foreground">Resources</div>
          <div className="text-xs text-muted-foreground">Live usage and runtime details for this container.</div>
        </div>
      </div>

      {resources.error ? (
        <div className="mt-4 rounded-xl border border-[var(--app-danger-soft)] bg-[var(--app-danger-soft)] px-4 py-3 text-sm text-foreground">
          {resources.error}
        </div>
      ) : null}

      {resources.loading && !resources.details ? (
        <div className="mt-4 space-y-5">
          <div className="space-y-2">
            <div className="flex items-center justify-between gap-3">
              <div className="h-4 w-16 animate-pulse rounded bg-[var(--app-surface)]" />
              <div className="h-4 w-28 animate-pulse rounded bg-[var(--app-surface)]" />
            </div>
            <div className="h-2.5 animate-pulse rounded-md bg-[var(--app-surface)]" />
          </div>
          <div className="space-y-2">
            <div className="flex items-center justify-between gap-3">
              <div className="h-4 w-12 animate-pulse rounded bg-[var(--app-surface)]" />
              <div className="h-4 w-16 animate-pulse rounded bg-[var(--app-surface)]" />
            </div>
            <div className="h-2.5 animate-pulse rounded-md bg-[var(--app-surface)]" />
          </div>
        </div>
      ) : (
        <div className="mt-4 space-y-5">
          <ResourceMeter
            detail={formatDockerMemorySummary(resources.details)}
            label="Memory"
            percent={memoryPercent}
          />
          <ResourceMeter
            detail={formatDockerResourcePercent(cpuPercent)}
            label="CPU"
            percent={cpuPercent}
          />
        </div>
      )}

      {metricsUnavailable && !resources.loading && !resources.error ? (
        <div className="mt-4 rounded-lg border border-[var(--app-border)] bg-[var(--app-surface)] px-3 py-2 text-xs text-muted-foreground">
          {container.state === "running"
            ? "Live metrics are not available for this container right now."
            : "Live metrics appear when the container is running."}
        </div>
      ) : null}

      <div className="mt-5 space-y-4 border-t border-[var(--app-border)] pt-4">
        <div className="space-y-1">
          <div className="text-[11px] font-medium uppercase tracking-[0.18em] text-muted-foreground">
            Container ID
          </div>
          <div className="break-all font-mono text-sm text-foreground">{container.id}</div>
        </div>

        <div className="space-y-2">
          <div className="text-[11px] font-medium uppercase tracking-[0.18em] text-muted-foreground">
            Port mapping
          </div>
          {resources.loading && !resources.details ? (
            <div className="space-y-2">
              <div className="h-4 w-40 animate-pulse rounded bg-[var(--app-surface)]" />
              <div className="h-4 w-32 animate-pulse rounded bg-[var(--app-surface)]" />
            </div>
          ) : resources.error && !resources.details ? (
            <div className="text-sm text-muted-foreground">Port details are unavailable right now.</div>
          ) : portMappings.length > 0 ? (
            <div className="space-y-2">
              {portMappings.map((port) => (
                <div
                  key={`${port.container_port}-${port.host_ip}-${port.host_port}`}
                  className="rounded-lg border border-[var(--app-border)] bg-[var(--app-surface)] px-3 py-2 text-sm text-foreground"
                >
                  {formatDockerPortMapping(port)}
                </div>
              ))}
            </div>
          ) : (
            <div className="text-sm text-muted-foreground">No published ports.</div>
          )}
        </div>
      </div>
    </div>
  );
}

function ContainerList({
  containers,
  activeContainerID,
  pendingOperation,
  actionErrors,
  expandedContainerID,
  containerLogs,
  containerResources,
  onAction,
  onMenuAction,
  onToggleExpandedContainer,
  onClearContainerLogs,
}: {
  containers: DockerContainer[];
  activeContainerID: string | null;
  pendingOperation: DockerContainerOperation | null;
  actionErrors: DockerContainerActionErrors;
  expandedContainerID: string | null;
  containerLogs: DockerContainerLogsState;
  containerResources: DockerContainerResourcesState;
  onAction: (container: DockerContainer, action: DockerContainerAction) => void;
  onMenuAction: (container: DockerContainer, action: DockerContainerMenuAction) => void;
  onToggleExpandedContainer: (container: DockerContainer) => void;
  onClearContainerLogs: (container: DockerContainer) => void;
}) {
  const expandedContainerLogsViewportRef = useRef<HTMLPreElement | null>(null);
  const shouldAutoScrollExpandedLogsRef = useRef(true);

  useEffect(() => {
    shouldAutoScrollExpandedLogsRef.current = true;
  }, [expandedContainerID]);

  useEffect(() => {
    if (expandedContainerID === null || !shouldAutoScrollExpandedLogsRef.current) {
      return;
    }

    const animationFrameID = window.requestAnimationFrame(() => {
      const viewport = expandedContainerLogsViewportRef.current;
      if (!viewport) {
        return;
      }

      viewport.scrollTop = viewport.scrollHeight;
    });

    return () => {
      window.cancelAnimationFrame(animationFrameID);
    };
  }, [expandedContainerID, containerLogs.output]);

  return (
    <div className="overflow-hidden rounded-2xl border border-[var(--app-border)] bg-[var(--app-bg-2)] shadow-[var(--app-shadow)]">
      <div className="hidden grid-cols-[minmax(0,1.05fr)_minmax(0,1.15fr)_minmax(140px,0.55fr)_120px] items-center gap-6 border-b border-[var(--app-border)] px-6 py-5 text-sm text-muted-foreground md:grid">
        <div className="flex items-center gap-3">
          <ChevronDownIcon className="h-4 w-4 text-muted-foreground/70" />
          <span>Name</span>
        </div>
        <div>Image ↑</div>
        <div>Status</div>
        <div className="text-right">Actions</div>
      </div>

      {containers.map((container) => {
        const stateMeta = getContainerStateMeta(container.state);
        const busy = activeContainerID === container.id;
        const actions = getContainerActions(container);
        const pendingLabel = busy ? getContainerOperationPendingLabel(pendingOperation) : null;
        const expanded = expandedContainerID === container.id;
        const statusBusy =
          busy &&
          (pendingOperation === "start" ||
            pendingOperation === "stop" ||
            pendingOperation === "restart");
        const logRegionID = `docker-container-logs-${container.id}`;
        const actionError = actionErrors[container.id];

        return (
          <div
            key={container.id || `${container.name}-${container.image}`}
            className="border-b border-[var(--app-border)] last:border-b-0"
          >
            <div className="grid gap-4 px-4 py-4 md:grid-cols-[minmax(0,1.05fr)_minmax(0,1.15fr)_minmax(140px,0.55fr)_120px] md:px-6 md:py-5">
              <div className="space-y-1">
                <div className="text-[11px] font-medium uppercase tracking-[0.18em] text-muted-foreground md:hidden">
                  Name
                </div>
                <div className="flex min-w-0 items-center gap-3">
                  <button
                    type="button"
                    aria-expanded={expanded}
                    aria-controls={logRegionID}
                    aria-label={`${expanded ? "Collapse" : "Expand"} logs for ${getContainerLabel(container)}`}
                    title={`${expanded ? "Collapse" : "Expand"} logs for ${getContainerLabel(container)}`}
                    onClick={() => {
                      onToggleExpandedContainer(container);
                    }}
                    className="inline-flex h-7 w-7 shrink-0 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-[var(--app-surface)] hover:text-foreground"
                  >
                    <ChevronDownIcon
                      className={cn("h-4 w-4 transition-transform", expanded && "rotate-180")}
                    />
                  </button>
                  <div className="truncate text-[15px] font-medium text-foreground">{getContainerLabel(container)}</div>
                </div>
              </div>

              <div className="space-y-1">
                <div className="text-[11px] font-medium uppercase tracking-[0.18em] text-muted-foreground md:hidden">
                  Image
                </div>
                <div className="flex min-w-0 items-center gap-2.5 text-[15px] text-foreground">
                  <Docker className="h-4 w-4 shrink-0 text-muted-foreground" />
                  <span className="truncate">{container.image}</span>
                </div>
              </div>

              <div className="space-y-1">
                <div className="text-[11px] font-medium uppercase tracking-[0.18em] text-muted-foreground md:hidden">
                  Status
                </div>
                <DropdownMenu modal={false}>
                  <DropdownMenuTrigger asChild>
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      disabled={busy}
                      className="h-auto w-fit max-w-full justify-start px-0 py-0 text-[15px] font-medium text-foreground hover:bg-transparent hover:text-foreground"
                      title={container.status}
                    >
                      <span className="inline-flex min-w-0 items-center gap-2">
                        {statusBusy ? (
                          <LoaderCircle className="h-4 w-4 shrink-0 animate-spin text-muted-foreground" />
                        ) : (
                          <span className={`h-2.5 w-2.5 shrink-0 rounded-full ${stateMeta.dotClassName}`} />
                        )}
                        <span className="truncate">{pendingLabel || stateMeta.label}</span>
                      </span>
                    </Button>
                  </DropdownMenuTrigger>
                  <DropdownMenuContent
                    align="start"
                    className="w-40 border-[var(--app-border)] bg-[var(--app-surface)] p-1 text-[var(--app-text)] shadow-[0_12px_30px_rgba(15,23,42,0.16)]"
                  >
                    {actions.map((action) => {
                      const Icon = action.icon;

                      return (
                        <DropdownMenuItem
                          key={action.key}
                          onSelect={() => {
                            onAction(container, action.key);
                          }}
                        >
                          <Icon className="h-4 w-4" />
                          {action.label}
                        </DropdownMenuItem>
                      );
                    })}
                  </DropdownMenuContent>
                </DropdownMenu>
              </div>

              <div className="space-y-1">
                <div className="text-[11px] font-medium uppercase tracking-[0.18em] text-muted-foreground md:hidden">
                  Actions
                </div>
                <div className="flex items-center gap-1 md:justify-end">
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon"
                    disabled={busy}
                    className="h-9 w-9 rounded-md text-muted-foreground hover:text-foreground"
                    aria-label={`Open settings for ${getContainerLabel(container)}`}
                    title={`Open settings for ${getContainerLabel(container)}`}
                  >
                    <Settings className="h-4 w-4" />
                  </Button>
                  <DropdownMenu modal={false}>
                    <DropdownMenuTrigger asChild>
                      <Button
                        type="button"
                        variant="ghost"
                        size="icon"
                        disabled={busy}
                        className="h-9 w-9 rounded-md text-muted-foreground hover:text-foreground"
                        aria-label={`Open more options for ${getContainerLabel(container)}`}
                        title={`Open more options for ${getContainerLabel(container)}`}
                      >
                        {busy && !statusBusy ? (
                          <LoaderCircle className="h-4 w-4 animate-spin" />
                        ) : (
                          <DotsVertical className="h-4 w-4" />
                        )}
                      </Button>
                    </DropdownMenuTrigger>
                    <DropdownMenuContent
                      align="end"
                      className="w-52 border-[var(--app-border)] bg-[var(--app-surface)] p-1 text-[var(--app-text)] shadow-[0_12px_30px_rgba(15,23,42,0.16)]"
                    >
                      <DropdownMenuItem
                        onSelect={() => {
                          onMenuAction(container, "recreate");
                        }}
                      >
                        <RotateCcw className="h-4 w-4" />
                        Recreate
                      </DropdownMenuItem>
                      <DropdownMenuItem
                        onSelect={() => {
                          onMenuAction(container, "snapshot");
                        }}
                      >
                        <Download className="h-4 w-4" />
                        Download Snapshot
                      </DropdownMenuItem>
                      <DropdownMenuItem
                        onSelect={() => {
                          onMenuAction(container, "save-image");
                        }}
                      >
                        <HardDrive className="h-4 w-4" />
                        Save as Image
                      </DropdownMenuItem>
                      <DropdownMenuSeparator />
                      <DropdownMenuItem
                        variant="destructive"
                        onSelect={() => {
                          onMenuAction(container, "delete");
                        }}
                      >
                        <Trash2 className="h-4 w-4" />
                        Delete
                      </DropdownMenuItem>
                    </DropdownMenuContent>
                  </DropdownMenu>
                </div>
              </div>
            </div>

            {actionError ? (
              <div className="px-4 pb-4 md:px-6">
                <div className="rounded-xl border border-[var(--app-danger)]/30 bg-[var(--app-danger-soft)] px-4 py-3 text-sm text-[var(--app-danger)]">
                  {actionError}
                </div>
              </div>
            ) : null}

            {expanded ? (
              <div id={logRegionID} className="border-t border-[var(--app-border)] bg-[var(--app-surface)]/55 px-4 py-4 md:px-6">
                <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_320px]">
                  <div className="min-w-0 rounded-xl border border-[var(--app-border)] bg-[var(--app-bg-2)] p-4 sm:p-5">
                    <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
                      <div className="space-y-1">
                        <div className="text-sm font-medium text-foreground">Console Log</div>
                        <div className="text-xs text-muted-foreground">
                          Last 200 lines from `docker logs --tail 200 {getContainerLabel(container)}`.
                        </div>
                      </div>
                      <div className="flex flex-wrap items-center gap-2">
                        <Button
                          type="button"
                          variant="outline"
                          size="sm"
                          disabled={containerLogs.loading || containerLogs.refreshing}
                          onClick={() => {
                            onClearContainerLogs(container);
                          }}
                        >
                          <Trash2 className="h-4 w-4" />
                          Clear logs
                        </Button>
                      </div>
                    </div>

                    {containerLogs.error ? (
                      <div className="mt-4 rounded-xl border border-[var(--app-danger-soft)] bg-[var(--app-danger-soft)] px-4 py-3 text-sm text-foreground">
                        {containerLogs.error}
                      </div>
                    ) : null}

                    {containerLogs.loading && !containerLogs.output ? (
                      <div className="mt-4 flex items-center gap-2 rounded-xl border border-[var(--app-border)] bg-[var(--app-bg-2)] px-4 py-4 text-sm text-muted-foreground">
                        <LoaderCircle className="h-4 w-4 animate-spin" />
                        Loading logs...
                      </div>
                    ) : null}

                    {!containerLogs.loading && !containerLogs.output && !containerLogs.error ? (
                      <div className="mt-4 rounded-xl border border-[var(--app-border)] bg-[var(--app-bg-2)] px-4 py-4 text-sm text-muted-foreground">
                        No logs were returned for this container.
                      </div>
                    ) : null}

                    {containerLogs.output ? (
                      <div className="mt-4 overflow-hidden rounded-xl border border-[var(--app-border)] bg-[var(--app-bg-2)]">
                        <pre
                          ref={expanded ? expandedContainerLogsViewportRef : null}
                          onScroll={(event) => {
                            const viewport = event.currentTarget;
                            shouldAutoScrollExpandedLogsRef.current =
                              viewport.scrollHeight - viewport.scrollTop - viewport.clientHeight < 24;
                          }}
                          className="max-h-[360px] overflow-auto p-5 font-mono text-xs leading-5 whitespace-pre-wrap break-words text-[var(--app-text)] sm:p-6"
                        >
                          {containerLogs.output}
                        </pre>
                      </div>
                    ) : null}
                  </div>

                  <ContainerResourcesPanel container={container} resources={containerResources} />
                </div>
              </div>
            ) : null}
          </div>
        );
      })}
    </div>
  );
}

function ImageList({ images }: { images: DockerImage[] }) {
  return (
    <div className="overflow-hidden rounded-2xl border border-[var(--app-border)] bg-[var(--app-bg-2)] shadow-[var(--app-shadow)]">
      <div className="hidden grid-cols-[minmax(0,1.2fr)_160px_140px_140px] items-center gap-6 border-b border-[var(--app-border)] px-6 py-5 text-sm text-muted-foreground md:grid">
        <div className="flex items-center gap-3">
          <Package className="h-4 w-4 text-muted-foreground/70" />
          <span>Repository</span>
        </div>
        <div>Tag</div>
        <div>Size</div>
        <div>Created</div>
      </div>

      {images.map((image) => {
        const repository = image.repository || "<none>";
        const tag = image.tag || "<none>";

        return (
          <div
            key={`${image.id}-${repository}-${tag}`}
            className="grid gap-4 border-b border-[var(--app-border)] px-4 py-4 last:border-b-0 md:grid-cols-[minmax(0,1.2fr)_160px_140px_140px] md:px-6 md:py-5"
          >
            <div className="space-y-1">
              <div className="text-[11px] font-medium uppercase tracking-[0.18em] text-muted-foreground md:hidden">
                Repository
              </div>
              <div className="flex min-w-0 items-center gap-2.5 text-[15px] text-foreground">
                <Package className="h-4 w-4 shrink-0 text-muted-foreground" />
                <span className="truncate">{repository}</span>
              </div>
            </div>

            <div className="space-y-1">
              <div className="text-[11px] font-medium uppercase tracking-[0.18em] text-muted-foreground md:hidden">
                Tag
              </div>
              <div className="truncate text-[15px] text-foreground">{tag}</div>
            </div>

            <div className="space-y-1">
              <div className="text-[11px] font-medium uppercase tracking-[0.18em] text-muted-foreground md:hidden">
                Size
              </div>
              <div className="truncate text-[15px] text-foreground">{image.size || "—"}</div>
            </div>

            <div className="space-y-1">
              <div className="text-[11px] font-medium uppercase tracking-[0.18em] text-muted-foreground md:hidden">
                Created
              </div>
              <div className="truncate text-[15px] text-foreground">{image.created_since || "—"}</div>
            </div>
          </div>
        );
      })}
    </div>
  );
}

type AddDockerContainerDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCreated: (container: DockerContainer) => void;
};

function AddDockerContainerDialog({
  open,
  onOpenChange,
  onCreated,
}: AddDockerContainerDialogProps) {
  const [query, setQuery] = useState("");
  const [results, setResults] = useState<DockerHubImage[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [creatingImage, setCreatingImage] = useState<string | null>(null);
  const requestIDRef = useRef(0);
  const trimmedQuery = query.trim();

  useEffect(() => {
    requestIDRef.current += 1;
    setQuery("");
    setResults([]);
    setLoading(false);
    setError(null);
    setCreatingImage(null);
  }, [open]);

  useEffect(() => {
    if (!open) {
      return;
    }

    if (trimmedQuery.length === 0) {
      setResults([]);
      setLoading(false);
      setError(null);
      return;
    }

    if (trimmedQuery.length < 2) {
      setResults([]);
      setLoading(false);
      setError(null);
      return;
    }

    const requestID = requestIDRef.current + 1;
    requestIDRef.current = requestID;
    setLoading(true);
    setError(null);

    const timeoutID = window.setTimeout(async () => {
      try {
        const nextResults = await searchDockerHubImages(trimmedQuery);
        if (requestIDRef.current !== requestID) {
          return;
        }

        setResults(nextResults);
      } catch (searchError) {
        if (requestIDRef.current !== requestID) {
          return;
        }

        setResults([]);
        setError(getErrorMessage(searchError, "Failed to search Docker Hub."));
      } finally {
        if (requestIDRef.current === requestID) {
          setLoading(false);
        }
      }
    }, 250);

    return () => {
      window.clearTimeout(timeoutID);
    };
  }, [open, trimmedQuery]);

  async function handleCreate(image: string) {
    if (creatingImage !== null) {
      return;
    }

    setCreatingImage(image);
    setError(null);

    try {
      const container = await createDockerContainer({ image });
      onCreated(container);
      onOpenChange(false);
    } catch (createError) {
      const message = getErrorMessage(createError, `Failed to create a container from ${image}.`);
      setError(message);
      toast.error(message);
    } finally {
      setCreatingImage(null);
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(nextOpen) => {
        if (!nextOpen && creatingImage !== null) {
          return;
        }
        onOpenChange(nextOpen);
      }}
    >
      <DialogContent className="gap-4 sm:max-w-3xl" showCloseButton={creatingImage === null}>
        <DialogHeader>
          <DialogTitle>Add Container</DialogTitle>
          <DialogDescription>
            Search Docker Hub and create a stopped container from a selected image. FlowPanel pulls the
            image first if it is missing locally.
          </DialogDescription>
        </DialogHeader>

        <section className="space-y-3">
          <label htmlFor="docker-image-search" className="text-sm font-medium text-foreground">
            Search Docker Hub images
          </label>
          <div className="relative">
            <Search className="pointer-events-none absolute top-1/2 left-3 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              id="docker-image-search"
              value={query}
              onChange={(event) => {
                setQuery(event.target.value);
              }}
              placeholder="Search images like nginx, redis, postgres..."
              className="pl-9"
              autoComplete="off"
              disabled={creatingImage !== null}
            />
          </div>
        </section>

        {error ? (
          <div className="rounded-xl border border-[var(--app-danger-soft)] bg-[var(--app-danger-soft)] px-4 py-3 text-sm text-foreground">
            {error}
          </div>
        ) : null}

        <section className="overflow-hidden rounded-2xl border border-[var(--app-border)] bg-[var(--app-bg-2)] shadow-[var(--app-shadow)]">
          <div className="flex items-center gap-2 border-b border-[var(--app-border)] px-4 py-3 text-sm text-muted-foreground">
            <Docker className="h-4 w-4" />
            <span>Docker Hub results</span>
          </div>

          <div className="max-h-[420px] overflow-y-auto">
            {loading ? (
              <div className="flex items-center gap-2 px-4 py-5 text-sm text-muted-foreground">
                <LoaderCircle className="h-4 w-4 animate-spin" />
                Searching Docker Hub...
              </div>
            ) : trimmedQuery.length < 2 ? (
              <div className="px-4 py-5 text-sm text-muted-foreground">
                Enter at least 2 characters to search Docker Hub.
              </div>
            ) : results.length === 0 ? (
              <div className="px-4 py-5 text-sm text-muted-foreground">
                No images matched "{trimmedQuery}".
              </div>
            ) : (
              <div className="divide-y divide-[var(--app-border)]">
                {results.map((result) => {
                  const busy = creatingImage === result.name;

                  return (
                    <div
                      key={result.name}
                      className="grid gap-3 px-4 py-4 md:grid-cols-[minmax(0,1fr)_auto]"
                    >
                      <div className="min-w-0 space-y-2">
                        <div className="flex min-w-0 flex-wrap items-center gap-2">
                          <div className="truncate text-sm font-medium text-foreground" title={result.name}>
                            {result.name}
                          </div>
                          {result.is_official ? (
                            <span className="rounded-full border border-[var(--app-border)] bg-[var(--app-surface)] px-2 py-0.5 text-[11px] font-medium text-muted-foreground">
                              Official
                            </span>
                          ) : null}
                          <span className="text-xs text-muted-foreground">
                            {result.star_count.toLocaleString()} stars
                          </span>
                        </div>
                        <p className="text-sm leading-6 text-muted-foreground">
                          {result.description || "No Docker Hub description was provided for this image."}
                        </p>
                        <div className="font-mono text-[12px] text-muted-foreground">
                          docker pull {result.name}
                        </div>
                      </div>

                      <div className="flex items-start md:justify-end">
                        <Button
                          type="button"
                          size="sm"
                          disabled={creatingImage !== null}
                          onClick={() => {
                            void handleCreate(result.name);
                          }}
                        >
                          {busy ? (
                            <>
                              <LoaderCircle className="h-4 w-4 animate-spin" />
                              Creating...
                            </>
                          ) : (
                            <>
                              <Plus className="h-4 w-4" />
                              Create Container
                            </>
                          )}
                        </Button>
                      </div>
                    </div>
                  );
                })}
              </div>
            )}
          </div>
        </section>
      </DialogContent>
    </Dialog>
  );
}

type SaveDockerContainerImageDialogProps = {
  open: boolean;
  container: DockerContainer | null;
  saving: boolean;
  onOpenChange: (open: boolean) => void;
  onSave: (image: string) => Promise<void>;
};

function SaveDockerContainerImageDialog({
  open,
  container,
  saving,
  onOpenChange,
  onSave,
}: SaveDockerContainerImageDialogProps) {
  const [image, setImage] = useState("");
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!open || !container) {
      setImage("");
      setError(null);
      return;
    }

    setImage(getSuggestedDockerImageName(container));
    setError(null);
  }, [container, open]);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!container || saving) {
      return;
    }

    const trimmedImage = image.trim();
    if (trimmedImage === "") {
      setError("Image name is required.");
      return;
    }

    setError(null);

    try {
      await onSave(trimmedImage);
    } catch (saveError) {
      setError(getErrorMessage(saveError, `Failed to save ${getContainerLabel(container)} as an image.`));
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(nextOpen) => {
        if (!nextOpen && saving) {
          return;
        }
        onOpenChange(nextOpen);
      }}
    >
      <DialogContent className="gap-4 sm:max-w-lg" showCloseButton={!saving}>
        <DialogHeader>
          <DialogTitle>Save as Image</DialogTitle>
          <DialogDescription>
            Save the current filesystem state of {container ? getContainerLabel(container) : "this container"} as a
            new Docker image.
          </DialogDescription>
        </DialogHeader>

        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="space-y-2">
            <label htmlFor="docker-save-image-name" className="text-sm font-medium text-foreground">
              Image name
            </label>
            <Input
              id="docker-save-image-name"
              value={image}
              onChange={(event) => {
                setImage(event.target.value);
              }}
              placeholder="my-app:snapshot"
              autoComplete="off"
              disabled={saving}
            />
            <p className="text-xs text-muted-foreground">Use a Docker image reference like `my-app:snapshot`.</p>
          </div>

          {error ? (
            <div className="rounded-xl border border-[var(--app-danger-soft)] bg-[var(--app-danger-soft)] px-4 py-3 text-sm text-foreground">
              {error}
            </div>
          ) : null}

          <div className="flex items-center justify-end gap-3">
            <Button
              type="button"
              variant="outline"
              onClick={() => onOpenChange(false)}
              disabled={saving}
            >
              Cancel
            </Button>
            <Button type="submit" disabled={saving}>
              {saving ? (
                <>
                  <LoaderCircle className="h-4 w-4 animate-spin" />
                  Saving...
                </>
              ) : (
                <>
                  <HardDrive className="h-4 w-4" />
                  Save Image
                </>
              )}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}

export function DockerPage() {
  const [activeTab, setActiveTab] = useState<DockerTab>("containers");
  const [status, setStatus] = useState<DockerStatus | null>(null);
  const [containers, setContainers] = useState<DockerContainer[]>([]);
  const [images, setImages] = useState<DockerImage[]>([]);
  const [createDialogOpen, setCreateDialogOpen] = useState(false);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [statusError, setStatusError] = useState<string | null>(null);
  const [containersError, setContainersError] = useState<string | null>(null);
  const [imagesError, setImagesError] = useState<string | null>(null);
  const [containerActionErrors, setContainerActionErrors] = useState<DockerContainerActionErrors>({});
  const [activeContainerID, setActiveContainerID] = useState<string | null>(null);
  const [pendingOperation, setPendingOperation] = useState<DockerContainerOperation | null>(null);
  const [expandedContainerID, setExpandedContainerID] = useState<string | null>(null);
  const [expandedContainerLogs, setExpandedContainerLogs] = useState<DockerContainerLogsState>(
    createDockerContainerLogsState,
  );
  const [expandedContainerResources, setExpandedContainerResources] = useState<DockerContainerResourcesState>(
    createDockerContainerResourcesState,
  );
  const [confirmAction, setConfirmAction] = useState<{
    action: Extract<DockerContainerMenuAction, "delete" | "recreate">;
    container: DockerContainer;
  } | null>(null);
  const [saveImageContainer, setSaveImageContainer] = useState<DockerContainer | null>(null);
  const latestRequestRef = useRef(0);
  const expandedContainerIDRef = useRef<string | null>(null);
  const expandedContainerLogsRequestIdRef = useRef(0);
  const expandedContainerLogsBusyRef = useRef(false);
  const expandedContainerLogsSinceRef = useRef<string | null>(null);
  const expandedContainerResourcesRequestIdRef = useRef(0);
  const expandedContainerResourcesBusyRef = useRef(false);
  const latestDataRef = useRef<{
    status: DockerStatus | null;
    containers: DockerContainer[];
    images: DockerImage[];
  }>({
    status: null,
    containers: [],
    images: [],
  });

  useEffect(() => {
    latestDataRef.current = { status, containers, images };
  }, [status, containers, images]);

  useEffect(() => {
    expandedContainerIDRef.current = expandedContainerID;
  }, [expandedContainerID]);

  function resetExpandedContainerPanel(nextExpandedContainerID: string | null = null) {
    expandedContainerLogsRequestIdRef.current += 1;
    expandedContainerLogsBusyRef.current = false;
    expandedContainerLogsSinceRef.current = null;
    expandedContainerResourcesRequestIdRef.current += 1;
    expandedContainerResourcesBusyRef.current = false;
    expandedContainerIDRef.current = nextExpandedContainerID;
    setExpandedContainerID(nextExpandedContainerID);
    setExpandedContainerLogs(createDockerContainerLogsState());
    setExpandedContainerResources(createDockerContainerResourcesState());
  }

  function clearContainerActionError(containerID: string) {
    setContainerActionErrors((current) => {
      if (!(containerID in current)) {
        return current;
      }

      const nextErrors = { ...current };
      delete nextErrors[containerID];
      return nextErrors;
    });
  }

  async function loadContainerLogs(
    container: DockerContainer,
    options?: { preserveOutput?: boolean; background?: boolean },
  ) {
    if (expandedContainerLogsBusyRef.current) {
      return;
    }

    const requestId = expandedContainerLogsRequestIdRef.current + 1;
    expandedContainerLogsRequestIdRef.current = requestId;
    expandedContainerLogsBusyRef.current = true;
    expandedContainerIDRef.current = container.id;
    setExpandedContainerID(container.id);
    setExpandedContainerLogs((current) => ({
      output: options?.preserveOutput ? current.output : "",
      loading: options?.background ? false : true,
      refreshing: Boolean(options?.background),
      error: null,
    }));

    try {
      const output = await fetchDockerContainerLogs(container.id, {
        since: expandedContainerLogsSinceRef.current ?? undefined,
      });
      if (
        expandedContainerLogsRequestIdRef.current !== requestId ||
        expandedContainerIDRef.current !== container.id
      ) {
        return;
      }

      setExpandedContainerLogs({
        output: output.trim(),
        loading: false,
        refreshing: false,
        error: null,
      });
    } catch (error) {
      if (
        expandedContainerLogsRequestIdRef.current !== requestId ||
        expandedContainerIDRef.current !== container.id
      ) {
        return;
      }

      setExpandedContainerLogs((current) => ({
        output: options?.preserveOutput ? current.output : "",
        loading: false,
        refreshing: false,
        error: getErrorMessage(error, `Failed to load logs for ${getContainerLabel(container)}.`),
      }));
    } finally {
      if (expandedContainerLogsRequestIdRef.current === requestId) {
        expandedContainerLogsBusyRef.current = false;
      }
    }
  }

  async function loadContainerResources(
    container: DockerContainer,
    options?: { preserveDetails?: boolean; background?: boolean },
  ) {
    if (expandedContainerResourcesBusyRef.current) {
      return;
    }

    const requestId = expandedContainerResourcesRequestIdRef.current + 1;
    expandedContainerResourcesRequestIdRef.current = requestId;
    expandedContainerResourcesBusyRef.current = true;
    expandedContainerIDRef.current = container.id;
    setExpandedContainerID(container.id);
    setExpandedContainerResources((current) => ({
      details: options?.preserveDetails ? current.details : null,
      loading: options?.background ? false : true,
      refreshing: Boolean(options?.background),
      error: null,
    }));

    try {
      const details = await fetchDockerContainerDetails(container.id);
      if (
        expandedContainerResourcesRequestIdRef.current !== requestId ||
        expandedContainerIDRef.current !== container.id
      ) {
        return;
      }

      setExpandedContainerResources({
        details,
        loading: false,
        refreshing: false,
        error: null,
      });
    } catch (error) {
      if (
        expandedContainerResourcesRequestIdRef.current !== requestId ||
        expandedContainerIDRef.current !== container.id
      ) {
        return;
      }

      setExpandedContainerResources((current) => ({
        details: options?.preserveDetails ? current.details : null,
        loading: false,
        refreshing: false,
        error: getErrorMessage(error, `Failed to load resources for ${getContainerLabel(container)}.`),
      }));
    } finally {
      if (expandedContainerResourcesRequestIdRef.current === requestId) {
        expandedContainerResourcesBusyRef.current = false;
      }
    }
  }

  async function loadDocker(options: LoadOptions = {}) {
    const requestId = latestRequestRef.current + 1;
    latestRequestRef.current = requestId;

    if (options.silent) {
      setRefreshing(true);
    } else {
      setLoading(true);
    }

    const [statusResult, containersResult, imagesResult] = await Promise.allSettled([
      fetchDockerStatus(),
      fetchDockerContainers(),
      fetchDockerImages(),
    ]);

    if (latestRequestRef.current !== requestId) {
      return;
    }

    const nextStatus =
      statusResult.status === "fulfilled"
        ? statusResult.value
        : options.silent
          ? latestDataRef.current.status
          : null;
    const nextContainers =
      containersResult.status === "fulfilled"
        ? containersResult.value
        : options.silent
          ? latestDataRef.current.containers
          : [];
    const nextImages =
      imagesResult.status === "fulfilled"
        ? imagesResult.value
        : options.silent
          ? latestDataRef.current.images
          : [];

    setStatus(nextStatus);
    setContainers(nextContainers);
    setImages(nextImages);
    setStatusError(statusResult.status === "rejected" ? getErrorMessage(statusResult.reason, "Failed to inspect Docker.") : null);
    setContainerActionErrors((current) => {
      if (Object.keys(current).length === 0) {
        return current;
      }

      if (!nextStatus?.installed || !nextStatus.service_running) {
        return {};
      }

      const visibleContainerIDs = new Set(nextContainers.map((container) => container.id));
      const nextErrors = Object.fromEntries(
        Object.entries(current).filter(([containerID]) => visibleContainerIDs.has(containerID)),
      );

      return Object.keys(nextErrors).length === Object.keys(current).length ? current : nextErrors;
    });

    if (nextStatus && (!nextStatus.installed || !nextStatus.service_running)) {
      setContainersError(null);
      setImagesError(null);
    } else {
      setContainersError(
        containersResult.status === "rejected"
          ? getErrorMessage(containersResult.reason, "Failed to load Docker containers.")
          : null,
      );
      setImagesError(
        imagesResult.status === "rejected"
          ? getErrorMessage(imagesResult.reason, "Failed to load Docker images.")
          : null,
      );
    }

    setLoading(false);
    setRefreshing(false);
  }

  useEffect(() => {
    void loadDocker();

    return () => {
      latestRequestRef.current += 1;
      expandedContainerLogsRequestIdRef.current += 1;
      expandedContainerResourcesRequestIdRef.current += 1;
    };
  }, []);

  useEffect(() => {
    if (activeTab !== "containers") {
      return;
    }

    if (expandedContainerIDRef.current === null) {
      return;
    }

    const expandedContainer = containers.find((container) => container.id === expandedContainerIDRef.current);
    if (!expandedContainer) {
      resetExpandedContainerPanel();
      return;
    }

    void loadContainerLogs(expandedContainer, { preserveOutput: true });
    void loadContainerResources(expandedContainer, { preserveDetails: true });
  }, [activeTab, containers]);

  useEffect(() => {
    if (activeTab !== "containers" || expandedContainerID === null) {
      return;
    }

    const intervalID = window.setInterval(() => {
      const expandedContainer = latestDataRef.current.containers.find(
        (container) => container.id === expandedContainerIDRef.current,
      );
      if (!expandedContainer) {
        resetExpandedContainerPanel();
        return;
      }

      void loadContainerLogs(expandedContainer, {
        preserveOutput: true,
        background: true,
      });
      void loadContainerResources(expandedContainer, {
        preserveDetails: true,
        background: true,
      });
    }, 10000);

    return () => {
      window.clearInterval(intervalID);
    };
  }, [activeTab, expandedContainerID]);

  async function handleContainerAction(container: DockerContainer, action: DockerContainerAction) {
    if (activeContainerID !== null) {
      return;
    }

    const runAction =
      action === "start"
        ? startDockerContainer
        : action === "stop"
          ? stopDockerContainer
          : restartDockerContainer;

    setActiveContainerID(container.id);
    setPendingOperation(action);
    clearContainerActionError(container.id);

    try {
      const nextContainer = await runAction(container.id);
      clearContainerActionError(container.id);
      setContainers((current) =>
        current.map((item) => (item.id === container.id ? nextContainer : item)),
      );
      toast.success(getContainerActionSuccessMessage(action, nextContainer));
      void loadDocker({ silent: true });
    } catch (error) {
      const message = getErrorMessage(error, `Failed to ${action} container ${getContainerLabel(container)}.`);
      setContainerActionErrors((current) => ({ ...current, [container.id]: message }));
      toast.error(message);
    } finally {
      setActiveContainerID(null);
      setPendingOperation(null);
    }
  }

  async function handleContainerSnapshot(container: DockerContainer) {
    if (activeContainerID !== null) {
      return;
    }

    setActiveContainerID(container.id);
    setPendingOperation("snapshot");

    try {
      const fileName = await downloadDockerContainerSnapshot(container.id);
      toast.success(`Downloaded ${fileName}.`);
    } catch (error) {
      toast.error(
        getErrorMessage(error, `Failed to download a snapshot for ${getContainerLabel(container)}.`),
      );
    } finally {
      setActiveContainerID(null);
      setPendingOperation(null);
    }
  }

  async function handleSaveContainerImage(image: string) {
    if (!saveImageContainer || activeContainerID !== null) {
      return;
    }

    const container = saveImageContainer;
    setActiveContainerID(container.id);
    setPendingOperation("save-image");

    try {
      await saveDockerContainerAsImage(container.id, image);
      setSaveImageContainer(null);
      toast.success(`Saved image ${image}.`);
      void loadDocker({ silent: true });
    } catch (error) {
      toast.error(
        getErrorMessage(error, `Failed to save ${getContainerLabel(container)} as image ${image}.`),
      );
      throw error;
    } finally {
      setActiveContainerID(null);
      setPendingOperation(null);
    }
  }

  async function handleConfirmedContainerAction(container: DockerContainer, action: "delete" | "recreate") {
    if (activeContainerID !== null) {
      return;
    }

    setActiveContainerID(container.id);
    setPendingOperation(action);
    clearContainerActionError(container.id);

    try {
      if (action === "delete") {
        await deleteDockerContainer(container.id);
        clearContainerActionError(container.id);
        setContainers((current) => current.filter((item) => item.id !== container.id));
        setConfirmAction(null);
        toast.success(`Deleted container ${getContainerLabel(container)}.`);
      } else {
        const nextContainer = await recreateDockerContainer(container.id);
        clearContainerActionError(container.id);
        setContainers((current) =>
          sortDockerContainers([
            ...current.filter((item) => item.id !== container.id),
            nextContainer,
          ]),
        );
        setConfirmAction(null);
        toast.success(`Recreated container ${getContainerLabel(nextContainer)}.`);
      }

      void loadDocker({ silent: true });
    } catch (error) {
      const message = getErrorMessage(error, `Failed to ${action} container ${getContainerLabel(container)}.`);
      setContainerActionErrors((current) => ({ ...current, [container.id]: message }));
      toast.error(message);
    } finally {
      setActiveContainerID(null);
      setPendingOperation(null);
    }
  }

  function handleContainerMenuAction(container: DockerContainer, action: DockerContainerMenuAction) {
    if (action === "snapshot") {
      void handleContainerSnapshot(container);
      return;
    }

    if (action === "save-image") {
      setSaveImageContainer(container);
      return;
    }

    setConfirmAction({ action, container });
  }

  function handleToggleContainerLogs(container: DockerContainer) {
    if (expandedContainerID === container.id) {
      resetExpandedContainerPanel();
      return;
    }

    resetExpandedContainerPanel(container.id);
    void loadContainerLogs(container);
    void loadContainerResources(container);
  }

  function handleClearContainerLogs(container: DockerContainer) {
    if (expandedContainerID !== container.id) {
      return;
    }

    expandedContainerLogsRequestIdRef.current += 1;
    expandedContainerLogsBusyRef.current = false;
    expandedContainerLogsSinceRef.current = new Date().toISOString();
    setExpandedContainerLogs({
      output: "",
      loading: false,
      refreshing: false,
      error: null,
    });
    toast.success(`Cleared logs for ${getContainerLabel(container)}.`);
  }

  const canCreateContainer = Boolean(status?.installed && status.service_running);
  const actions = (
    <>
      <Button
        size="sm"
        onClick={() => setCreateDialogOpen(true)}
        disabled={!canCreateContainer || loading || activeContainerID !== null}
      >
        <Plus className="h-4 w-4" />
        Add Container
      </Button>
      <Button
        variant="outline"
        size="sm"
        onClick={() => void loadDocker({ silent: true })}
        disabled={loading || refreshing || activeContainerID !== null}
      >
        {refreshing ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <RefreshCw className="h-4 w-4" />}
        Refresh
      </Button>
    </>
  );

  const activeDataCount = activeTab === "containers" ? containers.length : images.length;
  const activeTabError = activeTab === "containers" ? containersError : imagesError;
  const headerMeta = getPageMeta(status, containers, images, activeTab);
  const statusUnavailable = status ? !status.installed || !status.service_running : false;

  return (
    <div>
      <PageHeader title="Docker" meta={headerMeta} actions={actions} />
      <AddDockerContainerDialog
        open={createDialogOpen}
        onOpenChange={setCreateDialogOpen}
        onCreated={(container) => {
          toast.success(`Created container ${getContainerLabel(container)}.`);
          void loadDocker({ silent: true });
        }}
      />
      <SaveDockerContainerImageDialog
        open={saveImageContainer !== null}
        container={saveImageContainer}
        saving={
          saveImageContainer !== null &&
          activeContainerID === saveImageContainer.id &&
          pendingOperation === "save-image"
        }
        onOpenChange={(open) => {
          if (!open && activeContainerID === null) {
            setSaveImageContainer(null);
          }
        }}
        onSave={handleSaveContainerImage}
      />
      <ConfirmDialog
        open={confirmAction !== null}
        onOpenChange={(open) => {
          if (!open && activeContainerID === null) {
            setConfirmAction(null);
          }
        }}
        title={
          confirmAction?.action === "delete"
            ? `Delete ${confirmAction ? getContainerLabel(confirmAction.container) : "container"}?`
            : `Recreate ${confirmAction ? getContainerLabel(confirmAction.container) : "container"}?`
        }
        desc={
          confirmAction?.action === "delete"
            ? "This removes the container immediately. Any stopped or running state for this container will be lost."
            : "This removes the current container and creates a new one from the same Docker configuration. Running containers are started again after recreation."
        }
        confirmText={confirmAction?.action === "delete" ? "Delete container" : "Recreate container"}
        destructive={confirmAction?.action === "delete"}
        isLoading={
          confirmAction !== null &&
          activeContainerID === confirmAction.container.id &&
          pendingOperation === confirmAction.action
        }
        handleConfirm={() => {
          if (!confirmAction) {
            return;
          }

          void handleConfirmedContainerAction(confirmAction.container, confirmAction.action);
        }}
      />

      <section className="px-4 sm:px-6 lg:px-8">
        <div className="space-y-4">
          <div
            className="flex flex-col gap-2 rounded-2xl border border-[var(--app-border)] bg-[var(--app-bg-2)] p-2 shadow-[var(--app-shadow)] sm:flex-row"
            role="tablist"
            aria-label="Docker inventory tabs"
          >
            <TabButton
              active={activeTab === "containers"}
              label="Containers"
              count={containers.length}
              onClick={() => setActiveTab("containers")}
            />
            <TabButton
              active={activeTab === "images"}
              label="Images"
              count={images.length}
              onClick={() => setActiveTab("images")}
            />
          </div>

          {statusError ? (
            <div className="rounded-xl border border-[var(--app-danger-soft)] bg-[var(--app-danger-soft)] px-4 py-3 text-sm text-foreground">
              {statusError}
            </div>
          ) : null}

          {!statusUnavailable && activeTabError ? (
            <div className="rounded-xl border border-[var(--app-danger-soft)] bg-[var(--app-danger-soft)] px-4 py-3 text-sm text-foreground">
              {activeTabError}
            </div>
          ) : null}

          {loading ? activeTab === "containers" ? <ContainersSkeleton /> : <ImagesSkeleton /> : null}

          {!loading && status && !status.installed ? (
            <DockerEmptyState
              title="Docker is not installed"
              description="Install Docker from the Applications page first, then container and image inventory will appear here."
            />
          ) : null}

          {!loading && status && status.installed && !status.service_running ? (
            <DockerEmptyState
              title="Docker daemon is offline"
              description="The Docker service is installed but not running, so container and image inventory are unavailable right now."
            />
          ) : null}

          {!loading && !status && statusError && activeDataCount === 0 ? (
            <DockerEmptyState
              title={`Docker ${activeTab} are unavailable`}
              description="FlowPanel could not inspect Docker right now. Try refreshing after Docker becomes reachable again."
            />
          ) : null}

          {!loading && activeTab === "containers" && !statusUnavailable && containersError && containers.length === 0 ? (
            <DockerEmptyState
              title="Docker containers are unavailable"
              description="FlowPanel could not read the container inventory right now. Try refreshing after Docker becomes reachable again."
            />
          ) : null}

          {!loading && activeTab === "images" && !statusUnavailable && imagesError && images.length === 0 ? (
            <DockerEmptyState
              title="Docker images are unavailable"
              description="FlowPanel could not read the image inventory right now. Try refreshing after Docker becomes reachable again."
            />
          ) : null}

          {!loading && activeTab === "containers" && !containersError && !statusUnavailable && containers.length === 0 ? (
            <DockerEmptyState
              title="No containers found"
              description="Containers will appear here as soon as Docker workloads are created on this node."
            />
          ) : null}

          {!loading && activeTab === "images" && !imagesError && !statusUnavailable && images.length === 0 ? (
            <DockerEmptyState
              title="No images found"
              description="Pulled and built Docker images will appear here as soon as Docker starts caching them on this node."
            />
          ) : null}

          {!loading && activeTab === "containers" && containers.length > 0 ? (
            <ContainerList
              containers={containers}
              activeContainerID={activeContainerID}
              pendingOperation={pendingOperation}
              actionErrors={containerActionErrors}
              expandedContainerID={expandedContainerID}
              containerLogs={expandedContainerLogs}
              containerResources={expandedContainerResources}
              onAction={handleContainerAction}
              onMenuAction={handleContainerMenuAction}
              onToggleExpandedContainer={handleToggleContainerLogs}
              onClearContainerLogs={handleClearContainerLogs}
            />
          ) : null}
          {!loading && activeTab === "images" && images.length > 0 ? <ImageList images={images} /> : null}
        </div>
      </section>
    </div>
  );
}
