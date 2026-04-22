export type TaskManagerProcess = {
  pid: number;
  name: string;
  user?: string;
  state?: string;
  command?: string;
  cpu_usage_percent?: number;
  memory_bytes?: number;
  started_at?: string;
};

export type TaskManagerService = {
  id: string;
  name: string;
  manager: string;
  description?: string;
  active_state?: string;
  sub_state?: string;
  startup_state?: string;
  user?: string;
  file?: string;
  command?: string;
  running: boolean;
};

export type TaskManagerStartupItem = {
  id: string;
  name: string;
  manager: string;
  state: string;
  user?: string;
  file?: string;
  running: boolean;
  available: boolean;
};

export type TaskManagerUser = {
  username: string;
  uid?: string;
  gid?: string;
  home_directory?: string;
  shell?: string;
  logged_in: boolean;
  session_count: number;
  terminals?: string[];
  last_seen_at?: string;
};

export type TaskManagerScheduledTask = {
  id: string;
  name: string;
  source: string;
  schedule: string;
  command: string;
  state: string;
  last_status?: string;
  last_run_at?: string;
  next_run_at?: string;
};

export type TaskManagerSnapshot = {
  platform: string;
  notices?: string[];
  processes: TaskManagerProcess[];
  services: TaskManagerService[];
  startup_items: TaskManagerStartupItem[];
  users: TaskManagerUser[];
  scheduled_tasks: TaskManagerScheduledTask[];
};

type TaskManagerPayload = {
  snapshot: TaskManagerSnapshot;
};

type ServiceAction = "start" | "stop" | "restart";
type StartupAction = "enable" | "disable";

async function parseTaskManagerResponse(response: Response, action: string): Promise<TaskManagerSnapshot> {
  if (!response.ok) {
    let message = `${action} request failed with status ${response.status}`;

    try {
      const payload = (await response.json()) as { error?: unknown };
      if (typeof payload.error === "string" && payload.error) {
        message = payload.error;
      }
    } catch {
      // Keep the default error message when the payload is not valid JSON.
    }

    throw new Error(message);
  }

  const payload = (await response.json()) as TaskManagerPayload;
  return payload.snapshot;
}

export async function fetchTaskManagerSnapshot(signal?: AbortSignal): Promise<TaskManagerSnapshot> {
  const response = await fetch("/api/task-manager", {
    credentials: "include",
    cache: "no-store",
    signal,
  });

  return parseTaskManagerResponse(response, "load task manager");
}

export async function terminateTaskManagerProcess(pid: number): Promise<TaskManagerSnapshot> {
  const response = await fetch(`/api/task-manager/processes/${pid}/terminate`, {
    method: "POST",
    credentials: "include",
  });

  return parseTaskManagerResponse(response, "terminate process");
}

export async function controlTaskManagerService(
  serviceID: string,
  action: ServiceAction,
): Promise<TaskManagerSnapshot> {
  const response = await fetch(`/api/task-manager/services/${encodeURIComponent(serviceID)}/${action}`, {
    method: "POST",
    credentials: "include",
  });

  return parseTaskManagerResponse(response, `${action} service`);
}

export async function controlTaskManagerStartupItem(
  startupID: string,
  action: StartupAction,
): Promise<TaskManagerSnapshot> {
  const response = await fetch(`/api/task-manager/startup-items/${encodeURIComponent(startupID)}/${action}`, {
    method: "POST",
    credentials: "include",
  });

  return parseTaskManagerResponse(response, `${action} startup item`);
}
