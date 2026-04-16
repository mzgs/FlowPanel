export type MongoDBStatus = {
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

type MongoDBStatusPayload = {
  mongodb: MongoDBStatus;
};

async function parseMongoDBResponse(response: Response): Promise<MongoDBStatus> {
  if (!response.ok) {
    let message = `mongodb request failed with status ${response.status}`;

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

  const payload = (await response.json()) as MongoDBStatusPayload;
  return payload.mongodb;
}

export async function fetchMongoDBStatus(): Promise<MongoDBStatus> {
  const response = await fetch("/api/mongodb", {
    credentials: "include",
    cache: "no-store",
  });

  return parseMongoDBResponse(response);
}

export async function installMongoDB(): Promise<MongoDBStatus> {
  const response = await fetch("/api/mongodb/install", {
    method: "POST",
    credentials: "include",
  });

  return parseMongoDBResponse(response);
}

export async function removeMongoDB(): Promise<MongoDBStatus> {
  const response = await fetch("/api/mongodb/remove", {
    method: "POST",
    credentials: "include",
    keepalive: true,
  });

  return parseMongoDBResponse(response);
}

export async function startMongoDB(): Promise<MongoDBStatus> {
  const response = await fetch("/api/mongodb/start", {
    method: "POST",
    credentials: "include",
  });

  return parseMongoDBResponse(response);
}

export async function stopMongoDB(): Promise<MongoDBStatus> {
  const response = await fetch("/api/mongodb/stop", {
    method: "POST",
    credentials: "include",
  });

  return parseMongoDBResponse(response);
}

export async function restartMongoDB(): Promise<MongoDBStatus> {
  const response = await fetch("/api/mongodb/restart", {
    method: "POST",
    credentials: "include",
  });

  return parseMongoDBResponse(response);
}
