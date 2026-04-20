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

async function parseDockerError(response: Response): Promise<Error> {
  let message = `docker request failed with status ${response.status}`;

  try {
    const payload = await response.json();
    if (typeof payload.error === "string" && payload.error) {
      message = payload.error;
    }
  } catch {
    // Keep the default error message when the payload is not JSON.
  }

  return new Error(message);
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
  return payload.containers;
}

async function parseDockerContainerResponse(response: Response): Promise<DockerContainer> {
  if (!response.ok) {
    throw await parseDockerError(response);
  }

  const payload = (await response.json()) as DockerContainerPayload;
  return payload.container;
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
