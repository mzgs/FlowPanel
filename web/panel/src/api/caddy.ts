export type CaddyStatus = {
  started: boolean;
  config_loaded: boolean;
  admin_listen_addr?: string;
  public_http_addr?: string;
  public_https_addr?: string;
  configured_domains: number;
  state: string;
  message: string;
  restart_available: boolean;
  restart_label?: string;
};

type CaddyStatusPayload = {
  caddy: CaddyStatus;
};

async function parseCaddyResponse(response: Response): Promise<CaddyStatus> {
  if (!response.ok) {
    let message = `caddy request failed with status ${response.status}`;

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

  const payload = (await response.json()) as CaddyStatusPayload;
  return payload.caddy;
}

export async function fetchCaddyStatus(): Promise<CaddyStatus> {
  const response = await fetch("/api/caddy", {
    credentials: "include",
    cache: "no-store",
  });

  return parseCaddyResponse(response);
}

export async function restartCaddy(): Promise<CaddyStatus> {
  const response = await fetch("/api/caddy/restart", {
    method: "POST",
    credentials: "include",
  });

  return parseCaddyResponse(response);
}
