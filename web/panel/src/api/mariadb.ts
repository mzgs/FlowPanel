export type MariaDBStatus = {
  platform: string;
  package_manager?: string;
  product?: string;
  server_installed: boolean;
  server_path?: string;
  client_installed: boolean;
  client_path?: string;
  version?: string;
  listen_address?: string;
  service_running: boolean;
  ready: boolean;
  state: string;
  message: string;
  issues?: string[];
  install_available: boolean;
  install_label?: string;
};

export type MariaDBDatabase = {
  name: string;
  username: string;
  host: string;
  domain?: string;
  password?: string;
};

export type CreateMariaDBDatabaseInput = {
  name: string;
  username: string;
  password: string;
  domain?: string;
};

export type UpdateMariaDBDatabaseInput = {
  current_username: string;
  username: string;
  password: string;
  domain?: string;
};

export type MariaDBApiError = Error & {
  fieldErrors?: Record<string, string>;
};

export type MariaDBRootPasswordPayload = {
  root_password: string;
  configured: boolean;
};

type MariaDBStatusPayload = {
  mariadb: MariaDBStatus;
};

type MariaDBDatabasesPayload = {
  databases: MariaDBDatabase[];
};

type MariaDBDatabasePayload = {
  database: MariaDBDatabase;
};

async function parseMariaDBResponse(response: Response): Promise<MariaDBStatus> {
  if (!response.ok) {
    throw await readMariaDBApiError(response, "mariadb");
  }

  const payload = (await response.json()) as MariaDBStatusPayload;
  return payload.mariadb;
}

export async function fetchMariaDBStatus(): Promise<MariaDBStatus> {
  const response = await fetch("/api/mariadb", {
    credentials: "include",
  });

  return parseMariaDBResponse(response);
}

export async function installMariaDB(): Promise<MariaDBStatus> {
  const response = await fetch("/api/mariadb/install", {
    method: "POST",
    credentials: "include",
  });

  return parseMariaDBResponse(response);
}

export async function fetchMariaDBRootPassword(): Promise<MariaDBRootPasswordPayload> {
  const response = await fetch("/api/mariadb/root-password", {
    credentials: "include",
  });

  if (!response.ok) {
    throw await readMariaDBApiError(response, "mariadb root password");
  }

  return (await response.json()) as MariaDBRootPasswordPayload;
}

export async function updateMariaDBRootPassword(password: string): Promise<MariaDBRootPasswordPayload> {
  const response = await fetch("/api/mariadb/root-password", {
    method: "PUT",
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify({ password }),
  });

  if (!response.ok) {
    throw await readMariaDBApiError(response, "update mariadb root password");
  }

  return (await response.json()) as MariaDBRootPasswordPayload;
}

export async function fetchMariaDBDatabases(): Promise<MariaDBDatabasesPayload> {
  const response = await fetch("/api/mariadb/databases", {
    credentials: "include",
  });

  if (!response.ok) {
    throw await readMariaDBApiError(response, "list databases");
  }

  return (await response.json()) as MariaDBDatabasesPayload;
}

export async function createMariaDBDatabase(
  input: CreateMariaDBDatabaseInput,
): Promise<MariaDBDatabase> {
  const response = await fetch("/api/mariadb/databases", {
    method: "POST",
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify(input),
  });

  if (!response.ok) {
    throw await readMariaDBApiError(response, "create database");
  }

  const payload = (await response.json()) as MariaDBDatabasePayload;
  return payload.database;
}

export async function updateMariaDBDatabase(
  name: string,
  input: UpdateMariaDBDatabaseInput,
): Promise<MariaDBDatabase> {
  const response = await fetch(`/api/mariadb/databases/${encodeURIComponent(name)}`, {
    method: "PUT",
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify(input),
  });

  if (!response.ok) {
    throw await readMariaDBApiError(response, "update database");
  }

  const payload = (await response.json()) as MariaDBDatabasePayload;
  return payload.database;
}

export async function deleteMariaDBDatabase(
  name: string,
  username?: string,
): Promise<void> {
  const params = new URLSearchParams();
  if (username) {
    params.set("username", username);
  }
  const suffix = params.size ? `?${params.toString()}` : "";

  const response = await fetch(`/api/mariadb/databases/${encodeURIComponent(name)}${suffix}`, {
    method: "DELETE",
    credentials: "include",
  });

  if (!response.ok) {
    throw await readMariaDBApiError(response, "delete database");
  }
}

async function readMariaDBApiError(
  response: Response,
  action: string,
): Promise<MariaDBApiError> {
  let message = `${action} request failed with status ${response.status}`;
  let fieldErrors: Record<string, string> | undefined;

  try {
    const payload = (await response.json()) as {
      error?: unknown;
      field_errors?: unknown;
    };

    if (typeof payload.error === "string" && payload.error) {
      message = payload.error;
    }
    if (payload.field_errors && typeof payload.field_errors === "object") {
      fieldErrors = payload.field_errors as Record<string, string>;
    }
  } catch {
    // Keep default message when the response is not JSON.
  }

  const error = new Error(message) as MariaDBApiError;
  error.fieldErrors = fieldErrors;
  return error;
}
