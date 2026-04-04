export type DomainLogType = "all" | "access" | "error";

export type DomainLogRecord = {
  hostname: string;
  type: Exclude<DomainLogType, "all">;
  path: string;
  available: boolean;
  modified_at?: string;
  size_bytes: number;
  total_matches: number;
  truncated: boolean;
  read_error?: string;
  lines: string[];
};

export type DomainLogsPayload = {
  hostnames: string[];
  filters: {
    hostname: string;
    type: DomainLogType;
    search: string;
    limit: number;
  };
  logs: DomainLogRecord[];
};

export type FetchDomainLogsInput = {
  hostname?: string;
  type?: DomainLogType;
  search?: string;
  limit?: number;
};

export async function fetchDomainLogs(input: FetchDomainLogsInput = {}): Promise<DomainLogsPayload> {
  const query = new URLSearchParams();
  if (input.hostname) {
    query.set("hostname", input.hostname);
  }
  if (input.type) {
    query.set("type", input.type);
  }
  if (typeof input.search === "string" && input.search.trim() !== "") {
    query.set("search", input.search.trim());
  }
  if (typeof input.limit === "number") {
    query.set("limit", String(input.limit));
  }

  const suffix = query.size > 0 ? `?${query.toString()}` : "";
  const response = await fetch(`/api/domains/logs${suffix}`, {
    credentials: "include",
  });

  if (!response.ok) {
    let message = `domain logs request failed with status ${response.status}`;
    try {
      const payload = (await response.json()) as { error?: unknown };
      if (typeof payload.error === "string" && payload.error) {
        message = payload.error;
      }
    } catch {
      // Ignore invalid JSON responses.
    }
    throw new Error(message);
  }

  return response.json();
}
