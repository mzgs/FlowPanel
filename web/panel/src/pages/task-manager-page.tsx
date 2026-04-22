import { Link } from "@tanstack/react-router";
import { useDeferredValue, useEffect, useEffectEvent, useMemo, useRef, useState, type ReactNode } from "react";
import {
  controlTaskManagerService,
  controlTaskManagerStartupItem,
  fetchTaskManagerSnapshot,
  terminateTaskManagerProcess,
  type TaskManagerProcess,
  type TaskManagerScheduledTask,
  type TaskManagerService,
  type TaskManagerSnapshot,
  type TaskManagerStartupItem,
  type TaskManagerUser,
} from "@/api/task-manager";
import { PageHeader } from "@/components/page-header";
import {
  Clock,
  LoaderCircle,
  Monitor,
  PlayerPlay,
  PlayerStop,
  RefreshCw,
  RotateCcw,
  Search,
  Server,
  Trash2,
  UserCog,
} from "@/components/icons/tabler-icons";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { formatBytes, formatDateTime } from "@/lib/format";
import { cn, getErrorMessage } from "@/lib/utils";
import { toast } from "sonner";

const refreshIntervalMs = 10_000;

type TaskManagerSection = "processes" | "services" | "startup" | "users" | "scheduled";

const sectionMeta: Array<{
  id: TaskManagerSection;
  label: string;
  description: string;
}> = [
  { id: "processes", label: "Processes", description: "Inspect active workloads and terminate stuck tasks." },
  { id: "services", label: "Services", description: "Start, stop, and restart service units from one place." },
  { id: "startup", label: "Startup Items", description: "Control what registers for login or boot." },
  { id: "users", label: "Users", description: "Review local accounts and active sessions." },
  { id: "scheduled", label: "Scheduled Tasks", description: "Track scheduled jobs and recent execution state." },
] as const;

function formatPercent(value?: number) {
  if (value == null || !Number.isFinite(value) || value < 0) {
    return "Unavailable";
  }

  return `${value.toFixed(value >= 10 ? 0 : 1)}%`;
}

function formatValue(value?: string | null) {
  const trimmed = value?.trim();
  return trimmed ? trimmed : "Unavailable";
}

function formatDateTimeValue(value?: string) {
  return value ? formatDateTime(value) : "Unavailable";
}

function matchesSearch(search: string, ...values: Array<string | number | undefined | null>) {
  if (!search) {
    return true;
  }

  const haystack = values
    .map((value) => (value == null ? "" : String(value).toLowerCase()))
    .join(" ");

  return haystack.includes(search);
}

function getStateBadgeVariant(active: string | undefined) {
  switch ((active || "").toLowerCase()) {
    case "active":
    case "running":
    case "enabled":
    case "started":
    case "scheduled":
      return "default" as const;
    case "failed":
    case "dead":
    case "disabled":
    case "stopped":
      return "destructive" as const;
    default:
      return "outline" as const;
  }
}

function StatCard({
  icon,
  label,
  value,
}: {
  icon: ReactNode;
  label: string;
  value: number;
}) {
  return (
    <section className="rounded-xl border border-[var(--app-border)] bg-[var(--app-bg-2)] px-4 py-3 shadow-[var(--app-shadow)]">
      <div className="flex items-center justify-between gap-3">
        <div>
          <div className="text-[12px] font-medium uppercase tracking-[0.14em] text-[var(--app-text-muted)]">{label}</div>
          <div className="mt-1 text-[24px] font-semibold tracking-tight text-[var(--app-text)]">{value.toLocaleString("en-US")}</div>
        </div>
        <div className="flex h-9 w-9 items-center justify-center rounded-lg bg-[var(--app-surface-muted)] text-[var(--app-text-muted)]">
          {icon}
        </div>
      </div>
    </section>
  );
}

function EmptyState({ title, description }: { title: string; description: string }) {
  return (
    <div className="rounded-lg border border-dashed border-[var(--app-border)] px-4 py-8 text-center">
      <div className="text-sm font-medium text-[var(--app-text)]">{title}</div>
      <div className="mt-1 text-sm text-[var(--app-text-muted)]">{description}</div>
    </div>
  );
}

function ActionButton({
  pending,
  icon,
  children,
  ...props
}: React.ComponentProps<typeof Button> & {
  pending?: boolean;
  icon: ReactNode;
}) {
  return (
    <Button {...props}>
      {pending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : icon}
      {children}
    </Button>
  );
}

function ProcessesTable({
  processes,
  pendingAction,
  onTerminate,
}: {
  processes: TaskManagerProcess[];
  pendingAction: string | null;
  onTerminate: (process: TaskManagerProcess) => void;
}) {
  if (processes.length === 0) {
    return <EmptyState title="No matching processes" description="Adjust the search term or wait for the next refresh." />;
  }

  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Process</TableHead>
          <TableHead>PID</TableHead>
          <TableHead>User</TableHead>
          <TableHead>State</TableHead>
          <TableHead>CPU</TableHead>
          <TableHead>Memory</TableHead>
          <TableHead>Started</TableHead>
          <TableHead className="text-right">Actions</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {processes.map((process) => {
          const actionKey = `process:${process.pid}:terminate`;

          return (
            <TableRow key={process.pid}>
              <TableCell className="max-w-[28rem]">
                <div className="font-medium text-foreground">{process.name}</div>
                <div className="truncate text-xs text-muted-foreground">{formatValue(process.command)}</div>
              </TableCell>
              <TableCell className="font-mono text-xs">{process.pid}</TableCell>
              <TableCell>{formatValue(process.user)}</TableCell>
              <TableCell>
                <Badge variant={getStateBadgeVariant(process.state)}>{formatValue(process.state)}</Badge>
              </TableCell>
              <TableCell>{formatPercent(process.cpu_usage_percent)}</TableCell>
              <TableCell>{process.memory_bytes ? formatBytes(process.memory_bytes) : "Unavailable"}</TableCell>
              <TableCell>{formatDateTimeValue(process.started_at)}</TableCell>
              <TableCell className="text-right">
                <ActionButton
                  variant="destructive"
                  size="sm"
                  disabled={pendingAction === actionKey}
                  pending={pendingAction === actionKey}
                  icon={<Trash2 className="h-4 w-4" />}
                  onClick={() => onTerminate(process)}
                >
                  End
                </ActionButton>
              </TableCell>
            </TableRow>
          );
        })}
      </TableBody>
    </Table>
  );
}

function ServicesTable({
  services,
  pendingAction,
  onAction,
}: {
  services: TaskManagerService[];
  pendingAction: string | null;
  onAction: (service: TaskManagerService, action: "start" | "stop" | "restart") => void;
}) {
  if (services.length === 0) {
    return <EmptyState title="No matching services" description="This platform may not expose service units through the task manager." />;
  }

  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Service</TableHead>
          <TableHead>Manager</TableHead>
          <TableHead>State</TableHead>
          <TableHead>Startup</TableHead>
          <TableHead>User</TableHead>
          <TableHead>File</TableHead>
          <TableHead className="text-right">Actions</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {services.map((service) => {
          const prefix = `service:${service.id}`;

          return (
            <TableRow key={service.id}>
              <TableCell className="max-w-[26rem]">
                <div className="font-medium text-foreground">{service.name}</div>
                <div className="truncate text-xs text-muted-foreground">
                  {service.description || service.command || "No description available"}
                </div>
              </TableCell>
              <TableCell>{service.manager}</TableCell>
              <TableCell>
                <Badge variant={getStateBadgeVariant(service.active_state || (service.running ? "running" : "stopped"))}>
                  {service.active_state || (service.running ? "running" : "stopped")}
                </Badge>
              </TableCell>
              <TableCell>
                <Badge variant={getStateBadgeVariant(service.startup_state)}>{formatValue(service.startup_state)}</Badge>
              </TableCell>
              <TableCell>{formatValue(service.user)}</TableCell>
              <TableCell className="max-w-[18rem] truncate text-xs text-muted-foreground">
                {formatValue(service.file)}
              </TableCell>
              <TableCell className="text-right">
                <div className="flex justify-end gap-2">
                  <ActionButton
                    variant="outline"
                    size="sm"
                    disabled={pendingAction === `${prefix}:start` || service.running}
                    pending={pendingAction === `${prefix}:start`}
                    icon={<PlayerPlay className="h-4 w-4" />}
                    onClick={() => onAction(service, "start")}
                  >
                    Start
                  </ActionButton>
                  <ActionButton
                    variant="outline"
                    size="sm"
                    disabled={pendingAction === `${prefix}:stop` || !service.running}
                    pending={pendingAction === `${prefix}:stop`}
                    icon={<PlayerStop className="h-4 w-4" />}
                    onClick={() => onAction(service, "stop")}
                  >
                    Stop
                  </ActionButton>
                  <ActionButton
                    variant="outline"
                    size="sm"
                    disabled={pendingAction === `${prefix}:restart`}
                    pending={pendingAction === `${prefix}:restart`}
                    icon={<RotateCcw className="h-4 w-4" />}
                    onClick={() => onAction(service, "restart")}
                  >
                    Restart
                  </ActionButton>
                </div>
              </TableCell>
            </TableRow>
          );
        })}
      </TableBody>
    </Table>
  );
}

function StartupItemsTable({
  items,
  pendingAction,
  onAction,
}: {
  items: TaskManagerStartupItem[];
  pendingAction: string | null;
  onAction: (item: TaskManagerStartupItem, action: "enable" | "disable") => void;
}) {
  if (items.length === 0) {
    return <EmptyState title="No matching startup items" description="Startup registration is unavailable or there are no managed entries." />;
  }

  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Item</TableHead>
          <TableHead>Manager</TableHead>
          <TableHead>State</TableHead>
          <TableHead>Runtime</TableHead>
          <TableHead>User</TableHead>
          <TableHead>File</TableHead>
          <TableHead className="text-right">Actions</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {items.map((item) => {
          const prefix = `startup:${item.id}`;
          const enabled = item.state.toLowerCase().includes("enabled");

          return (
            <TableRow key={item.id}>
              <TableCell className="font-medium text-foreground">{item.name}</TableCell>
              <TableCell>{item.manager}</TableCell>
              <TableCell>
                <Badge variant={getStateBadgeVariant(item.state)}>{item.state}</Badge>
              </TableCell>
              <TableCell>
                <Badge variant={getStateBadgeVariant(item.running ? "running" : "stopped")}>
                  {item.running ? "running" : "stopped"}
                </Badge>
              </TableCell>
              <TableCell>{formatValue(item.user)}</TableCell>
              <TableCell className="max-w-[18rem] truncate text-xs text-muted-foreground">{formatValue(item.file)}</TableCell>
              <TableCell className="text-right">
                <div className="flex justify-end gap-2">
                  <ActionButton
                    variant="outline"
                    size="sm"
                    disabled={pendingAction === `${prefix}:enable` || enabled || !item.available}
                    pending={pendingAction === `${prefix}:enable`}
                    icon={<PlayerPlay className="h-4 w-4" />}
                    onClick={() => onAction(item, "enable")}
                  >
                    Enable
                  </ActionButton>
                  <ActionButton
                    variant="outline"
                    size="sm"
                    disabled={pendingAction === `${prefix}:disable` || !enabled || !item.available}
                    pending={pendingAction === `${prefix}:disable`}
                    icon={<PlayerStop className="h-4 w-4" />}
                    onClick={() => onAction(item, "disable")}
                  >
                    Disable
                  </ActionButton>
                </div>
              </TableCell>
            </TableRow>
          );
        })}
      </TableBody>
    </Table>
  );
}

function UsersTable({ users }: { users: TaskManagerUser[] }) {
  if (users.length === 0) {
    return <EmptyState title="No matching users" description="No local users or active sessions matched the current filter." />;
  }

  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>User</TableHead>
          <TableHead>UID</TableHead>
          <TableHead>Home</TableHead>
          <TableHead>Shell</TableHead>
          <TableHead>Sessions</TableHead>
          <TableHead>Status</TableHead>
          <TableHead>Last Seen</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {users.map((user) => (
          <TableRow key={user.username}>
            <TableCell>
              <div className="font-medium text-foreground">{user.username}</div>
              <div className="truncate text-xs text-muted-foreground">
                {user.terminals?.length ? user.terminals.join(", ") : "No active terminal"}
              </div>
            </TableCell>
            <TableCell className="font-mono text-xs">{formatValue(user.uid)}</TableCell>
            <TableCell className="max-w-[18rem] truncate text-xs text-muted-foreground">
              {formatValue(user.home_directory)}
            </TableCell>
            <TableCell className="max-w-[14rem] truncate text-xs text-muted-foreground">{formatValue(user.shell)}</TableCell>
            <TableCell>{user.session_count}</TableCell>
            <TableCell>
              <Badge variant={getStateBadgeVariant(user.logged_in ? "running" : "stopped")}>
                {user.logged_in ? "active" : "idle"}
              </Badge>
            </TableCell>
            <TableCell>{formatDateTimeValue(user.last_seen_at)}</TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}

function ScheduledTasksTable({ tasks }: { tasks: TaskManagerScheduledTask[] }) {
  if (tasks.length === 0) {
    return <EmptyState title="No scheduled tasks" description="Create a task from the Cron page to have it appear here." />;
  }

  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Task</TableHead>
          <TableHead>State</TableHead>
          <TableHead>Schedule</TableHead>
          <TableHead>Last Result</TableHead>
          <TableHead>Last Run</TableHead>
          <TableHead>Next Run</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {tasks.map((task) => (
          <TableRow key={task.id}>
            <TableCell className="max-w-[28rem]">
              <div className="font-medium text-foreground">{task.name}</div>
              <div className="truncate text-xs text-muted-foreground">{task.command}</div>
            </TableCell>
            <TableCell>
              <Badge variant={getStateBadgeVariant(task.state)}>{task.state}</Badge>
            </TableCell>
            <TableCell className="font-mono text-xs">{task.schedule}</TableCell>
            <TableCell>
              <Badge variant={getStateBadgeVariant(task.last_status)}>{task.last_status || "waiting"}</Badge>
            </TableCell>
            <TableCell>{formatDateTimeValue(task.last_run_at)}</TableCell>
            <TableCell>{formatDateTimeValue(task.next_run_at)}</TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}

export function TaskManagerPage() {
  const [snapshot, setSnapshot] = useState<TaskManagerSnapshot | null>(null);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [pendingAction, setPendingAction] = useState<string | null>(null);
  const [activeSection, setActiveSection] = useState<TaskManagerSection>("processes");
  const [search, setSearch] = useState("");
  const requestRef = useRef<AbortController | null>(null);
  const mountedRef = useRef(false);

  const deferredSearch = useDeferredValue(search.trim().toLowerCase());

  const loadSnapshot = useEffectEvent(async (background = false) => {
    if (requestRef.current) {
      return;
    }

    const controller = new AbortController();
    requestRef.current = controller;

    if (background) {
      setRefreshing(true);
    } else if (snapshot === null) {
      setLoading(true);
    } else {
      setRefreshing(true);
    }

    try {
      const nextSnapshot = await fetchTaskManagerSnapshot(controller.signal);
      if (!mountedRef.current) {
        return;
      }
      setSnapshot(nextSnapshot);
      setError(null);
    } catch (nextError) {
      if (!mountedRef.current || controller.signal.aborted) {
        return;
      }
      setError(getErrorMessage(nextError, "Task manager could not be loaded."));
    } finally {
      if (requestRef.current === controller) {
        requestRef.current = null;
      }
      if (!mountedRef.current || controller.signal.aborted) {
        return;
      }
      setLoading(false);
      setRefreshing(false);
    }
  });

  useEffect(() => {
    mountedRef.current = true;
    void loadSnapshot(false);

    const timer = window.setInterval(() => {
      if (document.hidden) {
        return;
      }
      void loadSnapshot(true);
    }, refreshIntervalMs);

    return () => {
      mountedRef.current = false;
      window.clearInterval(timer);
      requestRef.current?.abort();
      requestRef.current = null;
    };
  }, []);

  const filtered = useMemo(
    () => ({
      processes: (snapshot?.processes || []).filter((process) =>
        matchesSearch(deferredSearch, process.name, process.pid, process.user, process.state, process.command),
      ),
      services: (snapshot?.services || []).filter((service) =>
        matchesSearch(
          deferredSearch,
          service.name,
          service.manager,
          service.description,
          service.active_state,
          service.startup_state,
          service.user,
          service.file,
        ),
      ),
      startup: (snapshot?.startup_items || []).filter((item) =>
        matchesSearch(deferredSearch, item.name, item.manager, item.state, item.user, item.file),
      ),
      users: (snapshot?.users || []).filter((user) =>
        matchesSearch(
          deferredSearch,
          user.username,
          user.uid,
          user.home_directory,
          user.shell,
          user.terminals?.join(" "),
        ),
      ),
      scheduled: (snapshot?.scheduled_tasks || []).filter((task) =>
        matchesSearch(deferredSearch, task.name, task.schedule, task.command, task.state, task.last_status),
      ),
    }),
    [deferredSearch, snapshot],
  );

  const counts = {
    processes: snapshot?.processes.length || 0,
    services: snapshot?.services.length || 0,
    startup: snapshot?.startup_items.length || 0,
    users: snapshot?.users.length || 0,
    scheduled: snapshot?.scheduled_tasks.length || 0,
  };

  async function runSnapshotAction(
    actionKey: string,
    action: () => Promise<TaskManagerSnapshot>,
    successMessage: string,
    failureMessage: string,
  ) {
    setPendingAction(actionKey);

    try {
      const nextSnapshot = await action();
      setSnapshot(nextSnapshot);
      setError(null);
      toast.success(successMessage);
    } catch (nextError) {
      toast.error(getErrorMessage(nextError, failureMessage));
    } finally {
      setPendingAction((current) => (current === actionKey ? null : current));
    }
  }

  function handleTerminate(process: TaskManagerProcess) {
    const actionKey = `process:${process.pid}:terminate`;
    void runSnapshotAction(
      actionKey,
      () => terminateTaskManagerProcess(process.pid),
      `Terminated ${process.name}.`,
      "Process could not be terminated.",
    );
  }

  function handleServiceAction(service: TaskManagerService, action: "start" | "stop" | "restart") {
    const actionKey = `service:${service.id}:${action}`;
    const pastTense = action === "start" ? "started" : action === "stop" ? "stopped" : "restarted";
    void runSnapshotAction(
      actionKey,
      () => controlTaskManagerService(service.id, action),
      `${service.name} ${pastTense}.`,
      `Service could not be ${action}.`,
    );
  }

  function handleStartupAction(item: TaskManagerStartupItem, action: "enable" | "disable") {
    const actionKey = `startup:${item.id}:${action}`;
    const pastTense = action === "enable" ? "enabled" : "disabled";
    void runSnapshotAction(
      actionKey,
      () => controlTaskManagerStartupItem(item.id, action),
      `${item.name} ${pastTense}.`,
      "Startup item could not be updated.",
    );
  }

  const currentSection = sectionMeta.find((section) => section.id === activeSection) ?? sectionMeta[0];
  const sectionContent: Record<TaskManagerSection, ReactNode> = {
    processes: <ProcessesTable processes={filtered.processes} pendingAction={pendingAction} onTerminate={handleTerminate} />,
    services: <ServicesTable services={filtered.services} pendingAction={pendingAction} onAction={handleServiceAction} />,
    startup: <StartupItemsTable items={filtered.startup} pendingAction={pendingAction} onAction={handleStartupAction} />,
    users: <UsersTable users={filtered.users} />,
    scheduled: <ScheduledTasksTable tasks={filtered.scheduled} />,
  };

  return (
    <div className="min-h-[calc(100vh-var(--app-navbar-height))]">
      <PageHeader
        title="Task Manager"
        meta="Manage live processes, services, startup registration, users, and scheduled tasks from a single node view."
        actions={
          <Button variant="outline" size="sm" onClick={() => void loadSnapshot(false)} disabled={refreshing}>
            {refreshing ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <RefreshCw className="h-4 w-4" />}
            Refresh
          </Button>
        }
      />

      <div className="space-y-5 px-4 pb-8 sm:px-6 lg:px-8">
        {snapshot ? (
          <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-5">
            {[
              { key: "processes", label: "Processes", value: counts.processes, icon: <Monitor className="h-4 w-4" /> },
              { key: "services", label: "Services", value: counts.services, icon: <Server className="h-4 w-4" /> },
              { key: "startup", label: "Startup Items", value: counts.startup, icon: <PlayerPlay className="h-4 w-4" /> },
              { key: "users", label: "Users", value: counts.users, icon: <UserCog className="h-4 w-4" /> },
              { key: "scheduled", label: "Scheduled Tasks", value: counts.scheduled, icon: <Clock className="h-4 w-4" /> },
            ].map((card) => (
              <StatCard key={card.key} icon={card.icon} label={card.label} value={card.value} />
            ))}
          </div>
        ) : null}

        {snapshot?.notices?.length ? (
          <section className="rounded-xl border border-amber-500/30 bg-amber-500/10 px-4 py-3 text-sm text-amber-100 shadow-[var(--app-shadow)]">
            {snapshot.notices.map((notice) => (
              <div key={notice}>{notice}</div>
            ))}
          </section>
        ) : null}

        {error && snapshot ? (
          <section className="rounded-xl border border-destructive/40 bg-destructive/10 px-4 py-3 text-sm text-destructive shadow-[var(--app-shadow)]">
            {error}
          </section>
        ) : null}

        <section className="rounded-xl border border-[var(--app-border)] bg-[var(--app-bg-2)] shadow-[var(--app-shadow)]">
          <div className="flex flex-col gap-3 border-b border-[var(--app-border)] px-4 py-3 lg:flex-row lg:items-center lg:justify-between">
            <div className="min-w-0">
              <div className="text-[15px] font-semibold tracking-tight text-[var(--app-text)]">{currentSection.label}</div>
              <div className="text-sm text-[var(--app-text-muted)]">{currentSection.description}</div>
            </div>
            <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
              <label className="relative min-w-0 sm:w-[20rem]">
                <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
                <Input
                  value={search}
                  onChange={(event) => setSearch(event.target.value)}
                  placeholder={`Search ${currentSection.label.toLowerCase()}`}
                  className="pl-9"
                />
              </label>
              {activeSection === "scheduled" ? (
                <Button asChild variant="outline" size="sm">
                  <Link to="/cron">Open Cron</Link>
                </Button>
              ) : null}
            </div>
          </div>

          <div className="border-b border-[var(--app-border)] px-4 py-3">
            <div className="flex flex-wrap gap-2">
              {sectionMeta.map((section) => {
                return (
                  <button
                    key={section.id}
                    type="button"
                    onClick={() => setActiveSection(section.id)}
                    className={cn(
                      "inline-flex items-center gap-2 rounded-md border px-3 py-1.5 text-sm transition-colors",
                      activeSection === section.id
                        ? "border-primary bg-primary text-primary-foreground"
                        : "border-[var(--app-border)] bg-[var(--app-surface)] text-[var(--app-text-muted)] hover:text-[var(--app-text)]",
                    )}
                  >
                    <span>{section.label}</span>
                    <span className="rounded bg-black/10 px-1.5 py-0.5 text-[11px]">{counts[section.id]}</span>
                  </button>
                );
              })}
            </div>
          </div>

          <div className="px-4 py-3">
            {loading && !snapshot ? (
              <div className="flex items-center gap-3 py-8 text-sm text-muted-foreground">
                <LoaderCircle className="h-4 w-4 animate-spin" />
                Loading task manager data...
              </div>
            ) : error && !snapshot ? (
              <div className="flex flex-col gap-3 py-6">
                <div className="text-sm text-destructive">{error}</div>
                <div>
                  <Button variant="outline" size="sm" onClick={() => void loadSnapshot(false)}>
                    Retry
                  </Button>
                </div>
              </div>
            ) : (
              sectionContent[activeSection]
            )}
          </div>
        </section>
      </div>
    </div>
  );
}
