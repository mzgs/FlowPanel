export type PostgreSQLStatus = {
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
  service_running: boolean;
  start_available: boolean;
  start_label?: string;
  stop_available: boolean;
  stop_label?: string;
  restart_available: boolean;
  restart_label?: string;
};

type PostgreSQLStatusPayload = {
  postgresql: PostgreSQLStatus;
};

async function parsePostgreSQLResponse(response: Response): Promise<PostgreSQLStatus> {
  if (!response.ok) {
    let message = `postgresql request failed with status ${response.status}`;

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

  const payload = (await response.json()) as PostgreSQLStatusPayload;
  return payload.postgresql;
}

export async function fetchPostgreSQLStatus(): Promise<PostgreSQLStatus> {
  const response = await fetch("/api/postgresql", {
    credentials: "include",
    cache: "no-store",
  });

  return parsePostgreSQLResponse(response);
}

export async function installPostgreSQL(): Promise<PostgreSQLStatus> {
  const response = await fetch("/api/postgresql/install", {
    method: "POST",
    credentials: "include",
  });

  return parsePostgreSQLResponse(response);
}

export async function removePostgreSQL(): Promise<PostgreSQLStatus> {
  const response = await fetch("/api/postgresql/remove", {
    method: "POST",
    credentials: "include",
  });

  return parsePostgreSQLResponse(response);
}

export async function startPostgreSQL(): Promise<PostgreSQLStatus> {
  const response = await fetch("/api/postgresql/start", {
    method: "POST",
    credentials: "include",
  });

  return parsePostgreSQLResponse(response);
}

export async function stopPostgreSQL(): Promise<PostgreSQLStatus> {
  const response = await fetch("/api/postgresql/stop", {
    method: "POST",
    credentials: "include",
  });

  return parsePostgreSQLResponse(response);
}

export async function restartPostgreSQL(): Promise<PostgreSQLStatus> {
  const response = await fetch("/api/postgresql/restart", {
    method: "POST",
    credentials: "include",
  });

  return parsePostgreSQLResponse(response);
}
