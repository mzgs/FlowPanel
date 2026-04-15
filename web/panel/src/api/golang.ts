export type GolangStatus = {
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

type GolangStatusPayload = {
  golang: GolangStatus;
};

async function parseGolangResponse(response: Response): Promise<GolangStatus> {
  if (!response.ok) {
    let message = `golang request failed with status ${response.status}`;

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

  const payload = (await response.json()) as GolangStatusPayload;
  return payload.golang;
}

export async function fetchGolangStatus(): Promise<GolangStatus> {
  const response = await fetch("/api/golang", {
    credentials: "include",
    cache: "no-store",
  });

  return parseGolangResponse(response);
}

export async function installGolang(): Promise<GolangStatus> {
  const response = await fetch("/api/golang/install", {
    method: "POST",
    credentials: "include",
  });

  return parseGolangResponse(response);
}

export async function removeGolang(): Promise<GolangStatus> {
  const response = await fetch("/api/golang/remove", {
    method: "POST",
    credentials: "include",
  });

  return parseGolangResponse(response);
}
