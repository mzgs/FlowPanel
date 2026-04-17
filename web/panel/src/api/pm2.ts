export type PM2Status = {
  platform: string;
  package_manager?: string;
  installed: boolean;
  binary_path?: string;
  version?: string;
  state: string;
  message: string;
  issues?: string[];
  install_available: boolean;
  install_label?: string;
  remove_available: boolean;
  remove_label?: string;
};

export type PM2Process = {
  id: number;
  name: string;
  status: string;
  cpu: number;
  memory_bytes: number;
  restarts: number;
  uptime_unix_milli?: number;
  script_path?: string;
  namespace?: string;
  version?: string;
  exec_mode?: string;
};

export type PM2CreateProcessInput = {
  name?: string;
  script_path: string;
  working_directory?: string;
};

type PM2StatusPayload = {
  pm2: PM2Status;
};

type PM2ProcessesPayload = {
  processes: PM2Process[];
};

type PM2ProcessLogsPayload = {
  output: string;
};

async function parsePM2Response<T>(response: Response): Promise<T> {
  if (!response.ok) {
    let message = `pm2 request failed with status ${response.status}`;

    try {
      const payload = await response.json();
      if (typeof payload.error === "string" && payload.error) {
        message = payload.error;
      }
    } catch {
      // Keep the default error message when the payload is not JSON.
    }

    throw new Error(message);
  }

  return (await response.json()) as T;
}

export async function fetchPM2Status(): Promise<PM2Status> {
  const response = await fetch("/api/pm2", {
    credentials: "include",
    cache: "no-store",
  });

  const payload = await parsePM2Response<PM2StatusPayload>(response);
  return payload.pm2;
}

export async function fetchPM2Processes(): Promise<PM2Process[]> {
  const response = await fetch("/api/pm2/processes", {
    credentials: "include",
    cache: "no-store",
  });

  const payload = await parsePM2Response<PM2ProcessesPayload>(response);
  return payload.processes;
}

export async function createPM2Process(input: PM2CreateProcessInput): Promise<PM2Process[]> {
  const response = await fetch("/api/pm2/processes", {
    method: "POST",
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify(input),
  });

  const payload = await parsePM2Response<PM2ProcessesPayload>(response);
  return payload.processes;
}

export async function fetchPM2ProcessLogs(processID: number): Promise<string> {
  const response = await fetch(`/api/pm2/processes/${processID}/logs`, {
    credentials: "include",
    cache: "no-store",
  });

  const payload = await parsePM2Response<PM2ProcessLogsPayload>(response);
  return payload.output;
}

export async function clearPM2ProcessLogs(processID: number): Promise<void> {
  const response = await fetch(`/api/pm2/processes/${processID}/logs/clear`, {
    method: "POST",
    credentials: "include",
  });

  await parsePM2Response<{ ok: boolean }>(response);
}

async function runPM2ProcessAction(processID: number, action: "start" | "stop" | "restart" | "delete"): Promise<PM2Process[]> {
  const response = await fetch(`/api/pm2/processes/${processID}/${action}`, {
    method: "POST",
    credentials: "include",
  });

  const payload = await parsePM2Response<PM2ProcessesPayload>(response);
  return payload.processes;
}

export function startPM2Process(processID: number): Promise<PM2Process[]> {
  return runPM2ProcessAction(processID, "start");
}

export function stopPM2Process(processID: number): Promise<PM2Process[]> {
  return runPM2ProcessAction(processID, "stop");
}

export function restartPM2Process(processID: number): Promise<PM2Process[]> {
  return runPM2ProcessAction(processID, "restart");
}

export function deletePM2Process(processID: number): Promise<PM2Process[]> {
  return runPM2ProcessAction(processID, "delete");
}

export async function installPM2(): Promise<PM2Status> {
  const response = await fetch("/api/pm2/install", {
    method: "POST",
    credentials: "include",
  });

  const payload = await parsePM2Response<PM2StatusPayload>(response);
  return payload.pm2;
}

export async function removePM2(): Promise<PM2Status> {
  const response = await fetch("/api/pm2/remove", {
    method: "POST",
    credentials: "include",
    keepalive: true,
  });

  const payload = await parsePM2Response<PM2StatusPayload>(response);
  return payload.pm2;
}
