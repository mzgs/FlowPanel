export type SystemStatus = {
  cores: number;
  cpu_usage_percent?: number;
  disk_free_bytes?: number;
  disk_mount_path?: string;
  disk_total_bytes?: number;
  disk_used_bytes?: number;
  hostname?: string;
  load_1?: number;
  load_5?: number;
  load_15?: number;
  memory_total_bytes?: number;
  memory_used_bytes?: number;
  platform: string;
  platform_name: string;
  platform_version?: string;
  public_ipv4?: string;
  server_time: string;
  server_time_display: string;
  timezone: string;
};

type SystemStatusPayload = {
  system: SystemStatus;
};

export async function fetchSystemStatus(): Promise<SystemStatus> {
  const response = await fetch("/api/system", {
    credentials: "include",
  });

  if (!response.ok) {
    throw new Error(`system request failed with status ${response.status}`);
  }

  const payload = (await response.json()) as SystemStatusPayload;
  return payload.system;
}
