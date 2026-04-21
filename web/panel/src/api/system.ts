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
