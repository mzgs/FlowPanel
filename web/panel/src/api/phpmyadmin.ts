export type PHPMyAdminStatus = {
  platform: string;
  package_manager?: string;
  installed: boolean;
  install_path?: string;
  version?: string;
  state: string;
  message: string;
  issues?: string[];
  install_available: boolean;
  install_label?: string;
};

type PHPMyAdminStatusPayload = {
  phpmyadmin: PHPMyAdminStatus;
};

async function parsePHPMyAdminResponse(response: Response): Promise<PHPMyAdminStatus> {
  if (!response.ok) {
    let message = `phpMyAdmin request failed with status ${response.status}`;

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

  const payload = (await response.json()) as PHPMyAdminStatusPayload;
  return payload.phpmyadmin;
}

export async function fetchPHPMyAdminStatus(): Promise<PHPMyAdminStatus> {
  const response = await fetch("/api/phpmyadmin", {
    credentials: "include",
  });

  return parsePHPMyAdminResponse(response);
}

export async function installPHPMyAdmin(): Promise<PHPMyAdminStatus> {
  const response = await fetch("/api/phpmyadmin/install", {
    method: "POST",
    credentials: "include",
  });

  return parsePHPMyAdminResponse(response);
}
