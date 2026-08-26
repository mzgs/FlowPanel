export type SystemStatus = {
  cores: number;
  cpu_usage_percent?: number;
  disk_free_bytes?: number;
  disk_mount_path?: string;
  disk_read_bytes?: number;
  disk_read_count?: number;
  disk_total_bytes?: number;
  disk_used_bytes?: number;
  disk_write_bytes?: number;
  disk_write_count?: number;
  hostname?: string;
  load_1?: number;
  load_5?: number;
  load_15?: number;
  memory_total_bytes?: number;
  memory_used_bytes?: number;
  network_receive_bytes?: number;
  network_transmit_bytes?: number;
  platform: string;
  platform_name: string;
  platform_version?: string;
  public_ipv4?: string;
  server_time: string;
  server_time_display: string;
  timezone: string;
  uptime_seconds?: number;
};

export type SystemHistoryRange = "1h" | "6h" | "1d";

export type DiskSnapshot = {
  mounts: Array<{
    device: string;
    mountpoint: string;
    filesystem: string;
    total_bytes: number;
    used_bytes: number;
    free_bytes: number;
    used_percent: number;
  }>;
  largest_files: Array<{
    path: string;
    size_bytes: number;
    modified_at: string;
  }>;
  scanned_path: string;
  scanned_at: string;
  scan_complete: boolean;
};

export type PanelUpdateStatus = {
  current_version: string;
  latest_version?: string;
  update_available: boolean;
  updating?: boolean;
  update_error?: string;
};

export type SystemHistorySample = {
  sampled_at: string;
  cpu_usage_percent?: number;
  disk_free_bytes?: number;
  disk_read_bytes?: number;
  disk_read_count?: number;
  disk_total_bytes?: number;
  disk_used_bytes?: number;
  disk_write_bytes?: number;
  disk_write_count?: number;
  memory_total_bytes?: number;
  memory_used_bytes?: number;
  network_receive_bytes?: number;
  network_transmit_bytes?: number;
};

type SystemStatusPayload = {
  system: SystemStatus;
};

type SystemHistoryPayload = {
  samples: SystemHistorySample[];
};

type PanelUpdatePayload = {
  update: PanelUpdateStatus;
};

async function parseSystemResponse<T>(response: Response): Promise<T> {
  if (!response.ok) {
    let message = `system request failed with status ${response.status}`;

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

export async function fetchSystemStatus(): Promise<SystemStatus> {
  const response = await fetch("/api/system", {
    credentials: "include",
    cache: "no-store",
  });

  const payload = await parseSystemResponse<SystemStatusPayload>(response);
  return payload.system;
}

export async function fetchSystemHistory(range: SystemHistoryRange): Promise<SystemHistorySample[]> {
  const response = await fetch(`/api/system/history?range=${range}`, {
    credentials: "include",
    cache: "no-store",
  });

  const payload = await parseSystemResponse<SystemHistoryPayload>(response);
  return payload.samples;
}

export async function fetchDiskSnapshot(signal?: AbortSignal): Promise<DiskSnapshot> {
  const response = await fetch("/api/system/disk", {
    credentials: "include",
    cache: "no-store",
    signal,
  });

  const payload = await parseSystemResponse<{ disk: DiskSnapshot }>(response);
  return payload.disk;
}

export async function fetchPanelUpdate(): Promise<PanelUpdateStatus> {
  const response = await fetch("/api/panel/update", {
    credentials: "include",
    cache: "no-store",
  });

  const payload = await parseSystemResponse<PanelUpdatePayload>(response);
  return payload.update;
}

export async function updatePanel(): Promise<void> {
  const response = await fetch("/api/panel/update", {
    method: "POST",
    credentials: "include",
  });

  await parseSystemResponse<{ ok: boolean }>(response);
}

async function restart(target: "panel" | "system"): Promise<void> {
  const response = await fetch(`/api/${target}/restart`, {
    method: "POST",
    credentials: "include",
  });

  await parseSystemResponse<{ ok: boolean }>(response);
}

export function restartPanel(): Promise<void> {
  return restart("panel");
}

export function restartServer(): Promise<void> {
  return restart("system");
}
