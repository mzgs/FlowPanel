import type { ReactNode } from "react";
import type { PM2Process } from "@/api/pm2";
import {
  LoaderCircle,
  PlayerPlayFilled,
  PlayerStop,
  RotateCcw,
  TerminalSquare,
  Trash2,
} from "@/components/icons/tabler-icons";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { cn } from "@/lib/utils";

type PM2ProcessListProps = {
  mode?: "dialog" | "dashboard";
  processes: PM2Process[];
  error: string | null;
  loading: boolean;
  busy: boolean;
  processActionKey: string | null;
  className?: string;
  emptyState?: ReactNode;
  onProcessAction: (action: "start" | "stop" | "restart" | "delete", process: Pick<PM2Process, "id" | "name">) => void;
  onDelete: (process: Pick<PM2Process, "id" | "name">) => void;
  onOpenLogs: (process: PM2Process) => void;
};

const compactActionButtonClassName = "h-7 gap-1.5 px-2.5 text-xs";

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
    return {
      label: "Online",
      variant: "outline" as const,
      className: "border-[var(--app-ok)]/30 bg-[var(--app-ok-soft)] text-[var(--app-ok)]",
    };
  }
  if (normalized === "stopped") {
    return {
      label: "Stopped",
      variant: "outline" as const,
      className: "border-[var(--app-danger)]/30 bg-[var(--app-danger-soft)] text-[var(--app-danger)]",
    };
  }
  if (normalized === "launching" || normalized === "waiting restart") {
    return { label: formatPM2ProcessStatus(normalized), variant: "secondary" as const, className: "" };
  }
  if (normalized === "errored") {
    return { label: "Errored", variant: "destructive" as const, className: "" };
  }

  return { label: formatPM2ProcessStatus(normalized), variant: "outline" as const, className: "" };
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

function isSavedPM2Process(process: PM2Process) {
  return process.id < 0;
}

function getPM2PrimaryProcessAction(process: PM2Process) {
  if (canStopPM2Process(process)) {
    return {
      action: "stop" as const,
      label: "Stop",
      icon: PlayerStop,
    };
  }

  return {
    action: "start" as const,
    label: "Start",
    icon: PlayerPlayFilled,
  };
}

function PM2ProcessActionButtons({
  process,
  busy,
  processActionKey,
  onDelete,
  onOpenLogs,
  onProcessAction,
}: {
  process: PM2Process;
  busy: boolean;
  processActionKey: string | null;
  onDelete: (process: Pick<PM2Process, "id" | "name">) => void;
  onOpenLogs: (process: PM2Process) => void;
  onProcessAction: (action: "start" | "stop" | "restart" | "delete", process: Pick<PM2Process, "id" | "name">) => void;
}) {
  const activeAction = processActionKey?.endsWith(`:${process.id}`) ? processActionKey.split(":")[0] : null;
  const primaryAction = getPM2PrimaryProcessAction(process);
  const primaryActionDisabled = primaryAction.action === "start" ? !canStartPM2Process(process) : !canStopPM2Process(process);
  const PrimaryActionIcon = primaryAction.icon;

  return (
    <div className="flex flex-wrap justify-end gap-2">
      <Button
        type="button"
        variant="outline"
        size="sm"
        className="h-7 w-7 p-0"
        onClick={() => {
          onProcessAction(primaryAction.action, process);
        }}
        disabled={busy || primaryActionDisabled}
        aria-label={`${primaryAction.label} ${process.name}`}
        title={`${primaryAction.label} ${process.name}`}
      >
        {activeAction === primaryAction.action ? (
          <LoaderCircle className="h-4 w-4 animate-spin" />
        ) : (
          <PrimaryActionIcon className="h-4 w-4" />
        )}
      </Button>

      <Button
        type="button"
        variant="outline"
        size="sm"
        className="h-7 w-7 p-0"
        onClick={() => {
          onProcessAction("restart", process);
        }}
        disabled={busy || !canRestartPM2Process(process)}
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
        className="h-7 w-7 p-0 text-[var(--app-danger)] hover:bg-[var(--app-danger-soft)] hover:text-[var(--app-danger)]"
        onClick={() => {
          onDelete({ id: process.id, name: process.name });
        }}
        disabled={busy}
        aria-label={`Delete ${process.name}`}
        title={`Delete ${process.name}`}
      >
        {activeAction === "delete" ? (
          <LoaderCircle className="h-4 w-4 animate-spin" />
        ) : (
          <Trash2 className="h-4 w-4" />
        )}
      </Button>

      <Button
        type="button"
        variant="outline"
        size="sm"
        className={compactActionButtonClassName}
        onClick={() => {
          onOpenLogs(process);
        }}
        disabled={busy || isSavedPM2Process(process)}
        title={isSavedPM2Process(process) ? "Start the process to view logs." : undefined}
      >
        <TerminalSquare className="h-4 w-4" />
        Logs
      </Button>
    </div>
  );
}

function PM2ProcessDashboardRows({
  processes,
  busy,
  processActionKey,
  onDelete,
  onOpenLogs,
  onProcessAction,
}: Omit<PM2ProcessListProps, "mode" | "error" | "loading" | "className" | "emptyState">) {
  return (
    <div className="min-h-0 overflow-auto border-y border-[var(--app-border)]">
      {processes.map((process) => {
        const statusBadge = getPM2ProcessStatusBadge(process.status);

        return (
          <div
            key={process.id}
            className="grid gap-3 border-b border-[var(--app-border)] py-3 last:border-b-0 xl:grid-cols-[minmax(0,1fr)_auto]"
          >
            <div className="min-w-0">
              <div className="flex flex-wrap items-center gap-2">
                <div className="truncate text-sm font-medium text-[var(--app-text)]">{process.name}</div>
                <Badge variant={statusBadge.variant} className={statusBadge.className}>
                  {statusBadge.label}
                </Badge>
                <span className="text-[11px] text-[var(--app-text-muted)]">
                  {isSavedPM2Process(process) ? "Saved" : `ID ${process.id}`}
                </span>
              </div>

              <div className="mt-1.5 flex flex-wrap gap-x-3 gap-y-1 text-[12px] text-[var(--app-text-muted)]">
                <span>CPU {formatPM2ProcessCPU(process.cpu)}</span>
                <span>Memory {formatPM2ProcessMemory(process.memory_bytes)}</span>
                <span>Restarts {process.restarts}</span>
                <span>Uptime {formatPM2ProcessUptime(process)}</span>
              </div>

              <div className="mt-1.5 truncate font-mono text-[11px] text-[var(--app-text-muted)]">
                {process.script_path?.trim() || "-"}
              </div>
            </div>

            <PM2ProcessActionButtons
              process={process}
              busy={busy}
              processActionKey={processActionKey}
              onDelete={onDelete}
              onOpenLogs={onOpenLogs}
              onProcessAction={onProcessAction}
            />
          </div>
        );
      })}
    </div>
  );
}

function PM2ProcessDialogTable({
  processes,
  busy,
  processActionKey,
  onDelete,
  onOpenLogs,
  onProcessAction,
}: Omit<PM2ProcessListProps, "mode" | "error" | "loading" | "className" | "emptyState">) {
  return (
    <div className="min-h-0 overflow-auto rounded-lg bg-[var(--app-surface)]">
      <Table className="min-w-[1100px]">
        <TableHeader className="sticky top-0 z-10 bg-[var(--app-surface)] [&_tr]:border-[var(--app-border)]">
          <TableRow className="hover:bg-transparent">
            <TableHead className="px-4">Name</TableHead>
            <TableHead>ID</TableHead>
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
          {processes.map((process) => {
            const statusBadge = getPM2ProcessStatusBadge(process.status);

            return (
              <TableRow key={process.id} className="align-top">
                <TableCell className="px-4 py-3">
                  <div className="font-medium text-[var(--app-text)]">{process.name}</div>
                </TableCell>
                <TableCell className="py-3 text-[13px] text-[var(--app-text-muted)]">
                  {isSavedPM2Process(process) ? "Saved" : process.id}
                </TableCell>
                <TableCell className="py-3">
                  <Badge variant={statusBadge.variant} className={statusBadge.className}>
                    {statusBadge.label}
                  </Badge>
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
                  <PM2ProcessActionButtons
                    process={process}
                    busy={busy}
                    processActionKey={processActionKey}
                    onDelete={onDelete}
                    onOpenLogs={onOpenLogs}
                    onProcessAction={onProcessAction}
                  />
                </TableCell>
              </TableRow>
            );
          })}
        </TableBody>
      </Table>
    </div>
  );
}

export function PM2ProcessList({
  mode = "dialog",
  processes,
  error,
  loading,
  busy,
  processActionKey,
  className,
  emptyState,
  onProcessAction,
  onDelete,
  onOpenLogs,
}: PM2ProcessListProps) {
  const initialLoading = loading && processes.length === 0;
  const isDashboard = mode === "dashboard";

  return (
    <div
      className={cn(
        "flex min-h-0 flex-col",
        !isDashboard && "rounded-lg border border-[var(--app-border)] bg-[var(--app-surface-muted)]",
        className
      )}
    >
      {error && processes.length > 0 ? (
        <div
          className={cn(
            "text-sm text-[var(--app-danger)]",
            isDashboard
              ? "mb-3 rounded-md border border-[var(--app-danger)]/30 bg-[var(--app-danger-soft)] px-3 py-2"
              : "border-b border-[var(--app-danger)]/20 bg-[var(--app-danger-soft)] px-4 py-3 sm:px-5"
          )}
        >
          {error}
        </div>
      ) : null}

      {error && processes.length === 0 ? (
        <div className={cn("flex h-full items-center justify-center", isDashboard ? "py-2" : "p-5 sm:p-6")}>
          <div
            className={cn(
              "max-w-xl rounded-lg border border-[var(--app-danger)]/30 bg-[var(--app-danger-soft)] px-4 py-3 text-sm text-[var(--app-danger)]",
              isDashboard && "w-full max-w-none"
            )}
          >
            {error}
          </div>
        </div>
      ) : initialLoading ? (
        <div
          className={cn(
            "flex h-full items-center justify-center gap-2 text-sm text-[var(--app-text-muted)]",
            isDashboard ? "py-6" : "px-5 sm:px-6"
          )}
        >
          <LoaderCircle className="h-4 w-4 animate-spin" />
          Loading PM2 processes...
        </div>
      ) : processes.length === 0 ? (
        emptyState ?? (
          <div className={cn(isDashboard ? "py-2" : "p-5 sm:p-6")}>
            <div
              className={cn(
                "rounded-md border border-dashed border-[var(--app-border)] bg-[var(--app-surface)] px-4 text-sm text-[var(--app-text-muted)]",
                isDashboard ? "py-6" : "py-10"
              )}
            >
              No PM2 processes found.
            </div>
          </div>
        )
      ) : isDashboard ? (
        <PM2ProcessDashboardRows
          processes={processes}
          busy={busy}
          processActionKey={processActionKey}
          onDelete={onDelete}
          onOpenLogs={onOpenLogs}
          onProcessAction={onProcessAction}
        />
      ) : (
        <PM2ProcessDialogTable
          processes={processes}
          busy={busy}
          processActionKey={processActionKey}
          onDelete={onDelete}
          onOpenLogs={onOpenLogs}
          onProcessAction={onProcessAction}
        />
      )}
    </div>
  );
}
