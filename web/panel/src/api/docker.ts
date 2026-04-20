import { type EnvironmentVariable } from "@/api/domains";

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

export type DockerContainer = {
  id: string;
  name: string;
  image: string;
  status: string;
  state: string;
  ports: DockerContainerPortMapping[];
};

export type DockerContainerPortMapping = {
  container_port: string;
  host_ip: string;
  host_port: string;
  public: boolean;
};

export type DockerContainerVolumeMapping = {
  source: string;
  destination: string;
  read_only: boolean;
};

export type DockerContainerDetails = {
  cpu_percent?: number;
  memory_usage_bytes?: number;
  memory_limit_bytes?: number;
  memory_percent?: number;
  ports: DockerContainerPortMapping[];
};

export type DockerContainerSettings = {
  ports: DockerContainerPortMapping[];
  publish_all_ports: boolean;
  environment: EnvironmentVariable[];
  volumes: DockerContainerVolumeMapping[];
  volume_source_base_path?: string;
};

export type DockerImage = {
  id: string;
  repository: string;
  tag: string;
  size: string;
  created_since: string;
};

export type DockerHubImage = {
  name: string;
  description: string;
  star_count: number;
  is_official: boolean;
};

type DockerStatusPayload = {
  docker: DockerStatus;
};

type DockerContainersPayload = {
  containers: DockerContainer[];
};

type DockerImagesPayload = {
  images: DockerImage[];
};

type DockerHubImagesPayload = {
  results: DockerHubImage[];
};

type DockerContainerPayload = {
  container: DockerContainer;
};

type DockerContainerLogsPayload = {
  output: string;
};

type DockerContainerDetailsPayload = {
  details: DockerContainerDetails;
};

type DockerContainerSettingsPayload = {
  settings: DockerContainerSettings;
};

export type DockerApiError = Error & {
  fieldErrors?: Record<string, string>;
};

function normalizeDockerPortMappings(ports: DockerContainerPortMapping[] | null | undefined): DockerContainerPortMapping[] {
  return Array.isArray(ports) ? ports : [];
}

function normalizeEnvironmentVariables(values: EnvironmentVariable[] | null | undefined): EnvironmentVariable[] {
  return Array.isArray(values) ? values : [];
}

function normalizeDockerVolumeMappings(
  values: DockerContainerVolumeMapping[] | null | undefined,
): DockerContainerVolumeMapping[] {
  return Array.isArray(values) ? values : [];
}

function normalizeDockerContainer(container: DockerContainer): DockerContainer {
  return {
    ...container,
    ports: normalizeDockerPortMappings(container.ports),
  };
}

async function parseDockerError(response: Response): Promise<DockerApiError> {
  let message = `docker request failed with status ${response.status}`;
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
    // Keep the default error message when the payload is not JSON.
  }

  const error = new Error(message) as DockerApiError;
  error.fieldErrors = fieldErrors;
  return error;
}

async function parseDockerResponse(response: Response): Promise<DockerStatus> {
  if (!response.ok) {
    throw await parseDockerError(response);
  }

  const payload = (await response.json()) as DockerStatusPayload;
  return payload.docker;
}

async function parseDockerContainersResponse(response: Response): Promise<DockerContainer[]> {
  if (!response.ok) {
    throw await parseDockerError(response);
  }

  const payload = (await response.json()) as DockerContainersPayload;
  return payload.containers.map(normalizeDockerContainer);
}

async function parseDockerContainerResponse(response: Response): Promise<DockerContainer> {
  if (!response.ok) {
    throw await parseDockerError(response);
  }

  const payload = (await response.json()) as DockerContainerPayload;
  return normalizeDockerContainer(payload.container);
}

async function parseDockerImagesResponse(response: Response): Promise<DockerImage[]> {
  if (!response.ok) {
    throw await parseDockerError(response);
  }

  const payload = (await response.json()) as DockerImagesPayload;
  return payload.images;
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

export async function fetchDockerContainers(): Promise<DockerContainer[]> {
  const response = await fetch("/api/docker/containers", {
    credentials: "include",
    cache: "no-store",
  });

  return parseDockerContainersResponse(response);
}

export async function fetchDockerContainerLogs(containerID: string, options?: { since?: string }): Promise<string> {
  const suffix = options?.since ? `?since=${encodeURIComponent(options.since)}` : "";
  const response = await fetch(`/api/docker/containers/${encodeURIComponent(containerID)}/logs${suffix}`, {
    credentials: "include",
    cache: "no-store",
  });

  if (!response.ok) {
    throw await parseDockerError(response);
  }

  const payload = (await response.json()) as DockerContainerLogsPayload;
  return payload.output;
}

export async function fetchDockerContainerDetails(containerID: string): Promise<DockerContainerDetails> {
  const response = await fetch(`/api/docker/containers/${encodeURIComponent(containerID)}/details`, {
    credentials: "include",
    cache: "no-store",
  });

  if (!response.ok) {
    throw await parseDockerError(response);
  }

  const payload = (await response.json()) as DockerContainerDetailsPayload;
  return {
    ...payload.details,
    ports: normalizeDockerPortMappings(payload.details.ports),
  };
}

export async function fetchDockerContainerSettings(containerID: string): Promise<DockerContainerSettings> {
  const response = await fetch(`/api/docker/containers/${encodeURIComponent(containerID)}/settings`, {
    credentials: "include",
    cache: "no-store",
  });

  if (!response.ok) {
    throw await parseDockerError(response);
  }

  const payload = (await response.json()) as DockerContainerSettingsPayload;
  return {
    ...payload.settings,
    ports: normalizeDockerPortMappings(payload.settings.ports),
    environment: normalizeEnvironmentVariables(payload.settings.environment),
    volumes: normalizeDockerVolumeMappings(payload.settings.volumes),
  };
}

export async function updateDockerContainerSettings(
  containerID: string,
  input: {
    ports: DockerContainerPortMapping[];
    environment: EnvironmentVariable[];
    volumes: DockerContainerVolumeMapping[];
  },
): Promise<DockerContainer> {
  const response = await fetch(`/api/docker/containers/${encodeURIComponent(containerID)}/settings`, {
    method: "PUT",
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify(input),
  });

  return parseDockerContainerResponse(response);
}

export async function fetchDockerImages(): Promise<DockerImage[]> {
  const response = await fetch("/api/docker/images", {
    credentials: "include",
    cache: "no-store",
  });

  return parseDockerImagesResponse(response);
}

export async function searchDockerHubImages(query: string): Promise<DockerHubImage[]> {
  const response = await fetch(`/api/docker/search-images?query=${encodeURIComponent(query)}`, {
    credentials: "include",
    cache: "no-store",
  });

  if (!response.ok) {
    throw await parseDockerError(response);
  }

  const payload = (await response.json()) as DockerHubImagesPayload;
  return payload.results;
}

export async function createDockerContainer(input: { image: string }): Promise<DockerContainer> {
  const response = await fetch("/api/docker/containers", {
    method: "POST",
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify(input),
  });

  return parseDockerContainerResponse(response);
}

export async function deleteDockerContainer(containerID: string): Promise<void> {
  const response = await fetch(`/api/docker/containers/${encodeURIComponent(containerID)}`, {
    method: "DELETE",
    credentials: "include",
  });

  if (!response.ok) {
    throw await parseDockerError(response);
  }
}

export async function recreateDockerContainer(containerID: string): Promise<DockerContainer> {
  const response = await fetch(`/api/docker/containers/${encodeURIComponent(containerID)}/recreate`, {
    method: "POST",
    credentials: "include",
  });

  return parseDockerContainerResponse(response);
}

export async function downloadDockerContainerSnapshot(containerID: string): Promise<string> {
  const response = await fetch(`/api/docker/containers/${encodeURIComponent(containerID)}/snapshot`, {
    credentials: "include",
  });

  if (!response.ok) {
    throw await parseDockerError(response);
  }

  return triggerDockerDownload(response, `${containerID}.tar`);
}

export async function saveDockerContainerAsImage(containerID: string, image: string): Promise<void> {
  const response = await fetch(`/api/docker/containers/${encodeURIComponent(containerID)}/save-image`, {
    method: "POST",
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify({ image }),
  });

  if (!response.ok) {
    throw await parseDockerError(response);
  }
}

async function runDockerContainerAction(containerID: string, action: "start" | "stop" | "restart"): Promise<DockerContainer> {
  const response = await fetch(`/api/docker/containers/${encodeURIComponent(containerID)}/${action}`, {
    method: "POST",
    credentials: "include",
  });

  return parseDockerContainerResponse(response);
}

export async function startDockerContainer(containerID: string): Promise<DockerContainer> {
  return runDockerContainerAction(containerID, "start");
}

export async function stopDockerContainer(containerID: string): Promise<DockerContainer> {
  return runDockerContainerAction(containerID, "stop");
}

export async function restartDockerContainer(containerID: string): Promise<DockerContainer> {
  return runDockerContainerAction(containerID, "restart");
}

async function triggerDockerDownload(response: Response, fallbackName: string): Promise<string> {
  const blob = await response.blob();
  const downloadURL = window.URL.createObjectURL(blob);
  const fileName = getDockerDownloadFilename(response.headers.get("Content-Disposition"), fallbackName);
  const anchor = document.createElement("a");

  anchor.href = downloadURL;
  anchor.download = fileName;
  anchor.style.display = "none";
  document.body.append(anchor);
  anchor.click();
  anchor.remove();

  window.setTimeout(() => {
    window.URL.revokeObjectURL(downloadURL);
  }, 0);

  return fileName;
}

function getDockerDownloadFilename(contentDisposition: string | null, fallbackName: string): string {
  if (contentDisposition) {
    const encodedMatch = contentDisposition.match(/filename\*=UTF-8''([^;]+)/i);
    if (encodedMatch?.[1]) {
      return decodeURIComponent(encodedMatch[1]);
    }

    const plainMatch = contentDisposition.match(/filename=\"([^\"]+)\"|filename=([^;]+)/i);
    const value = plainMatch?.[1] ?? plainMatch?.[2];
    if (value) {
      return value.trim();
    }
  }

  return fallbackName;
}
