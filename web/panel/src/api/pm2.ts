export type PM2Status = {
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
};

type PM2StatusPayload = {
  pm2: PM2Status;
};

async function parsePM2Response(response: Response): Promise<PM2Status> {
  if (!response.ok) {
    let message = `pm2 request failed with status ${response.status}`;

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

  const payload = (await response.json()) as PM2StatusPayload;
  return payload.pm2;
}

export async function fetchPM2Status(): Promise<PM2Status> {
  const response = await fetch("/api/pm2", {
    credentials: "include",
    cache: "no-store",
  });

  return parsePM2Response(response);
}

export async function installPM2(): Promise<PM2Status> {
  const response = await fetch("/api/pm2/install", {
    method: "POST",
    credentials: "include",
  });

  return parsePM2Response(response);
}

export async function removePM2(): Promise<PM2Status> {
  const response = await fetch("/api/pm2/remove", {
    method: "POST",
    credentials: "include",
  });

  return parsePM2Response(response);
}
