export type RedisStatus = {
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

type RedisStatusPayload = {
  redis: RedisStatus;
};

async function parseRedisResponse(response: Response): Promise<RedisStatus> {
  if (!response.ok) {
    let message = `redis request failed with status ${response.status}`;

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

  const payload = (await response.json()) as RedisStatusPayload;
  return payload.redis;
}

export async function fetchRedisStatus(): Promise<RedisStatus> {
  const response = await fetch("/api/redis", {
    credentials: "include",
    cache: "no-store",
  });

  return parseRedisResponse(response);
}

export async function installRedis(): Promise<RedisStatus> {
  const response = await fetch("/api/redis/install", {
    method: "POST",
    credentials: "include",
  });

  return parseRedisResponse(response);
}

export async function removeRedis(): Promise<RedisStatus> {
  const response = await fetch("/api/redis/remove", {
    method: "POST",
    credentials: "include",
  });

  return parseRedisResponse(response);
}

export async function startRedis(): Promise<RedisStatus> {
  const response = await fetch("/api/redis/start", {
    method: "POST",
    credentials: "include",
  });

  return parseRedisResponse(response);
}

export async function stopRedis(): Promise<RedisStatus> {
  const response = await fetch("/api/redis/stop", {
    method: "POST",
    credentials: "include",
  });

  return parseRedisResponse(response);
}

export async function restartRedis(): Promise<RedisStatus> {
  const response = await fetch("/api/redis/restart", {
    method: "POST",
    credentials: "include",
  });

  return parseRedisResponse(response);
}
