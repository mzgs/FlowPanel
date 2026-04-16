export type DockerStatus = {
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

type DockerStatusPayload = {
  docker: DockerStatus;
};

async function parseDockerResponse(response: Response): Promise<DockerStatus> {
  if (!response.ok) {
    let message = `docker request failed with status ${response.status}`;

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

  const payload = (await response.json()) as DockerStatusPayload;
  return payload.docker;
}

export async function fetchDockerStatus(): Promise<DockerStatus> {
  const response = await fetch("/api/docker", {
    credentials: "include",
    cache: "no-store",
  });

  return parseDockerResponse(response);
}

export async function installDocker(): Promise<DockerStatus> {
  const response = await fetch("/api/docker/install", {
    method: "POST",
    credentials: "include",
  });

  return parseDockerResponse(response);
}

export async function removeDocker(): Promise<DockerStatus> {
  const response = await fetch("/api/docker/remove", {
    method: "POST",
    credentials: "include",
    keepalive: true,
  });

  return parseDockerResponse(response);
}

export async function startDocker(): Promise<DockerStatus> {
  const response = await fetch("/api/docker/start", {
    method: "POST",
    credentials: "include",
  });

  return parseDockerResponse(response);
}

export async function stopDocker(): Promise<DockerStatus> {
  const response = await fetch("/api/docker/stop", {
    method: "POST",
    credentials: "include",
  });

  return parseDockerResponse(response);
}

export async function restartDocker(): Promise<DockerStatus> {
  const response = await fetch("/api/docker/restart", {
    method: "POST",
    credentials: "include",
  });

  return parseDockerResponse(response);
}
