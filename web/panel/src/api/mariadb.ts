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

type MariaDBStatusPayload = {
  mariadb: MariaDBStatus;
};

async function parseMariaDBResponse(response: Response): Promise<MariaDBStatus> {
  if (!response.ok) {
    let message = `mariadb request failed with status ${response.status}`;

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
