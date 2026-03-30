export type PHPStatus = {
  platform: string;
  package_manager?: string;
  php_installed: boolean;
  php_path?: string;
  php_version?: string;
  fpm_installed: boolean;
  fpm_path?: string;
  listen_address?: string;
  service_running: boolean;
  ready: boolean;
  state: string;
  message: string;
  issues?: string[];
  install_available: boolean;
  install_label?: string;
  start_available: boolean;
  start_label?: string;
};

type PHPStatusPayload = {
  php: PHPStatus;
};

async function parsePHPResponse(response: Response): Promise<PHPStatus> {
  if (!response.ok) {
    let message = `php request failed with status ${response.status}`;

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

  const payload = (await response.json()) as PHPStatusPayload;
  return payload.php;
}

export async function fetchPHPStatus(): Promise<PHPStatus> {
  const response = await fetch("/api/php", {
    credentials: "include",
  });

  return parsePHPResponse(response);
}

export async function installPHP(): Promise<PHPStatus> {
  const response = await fetch("/api/php/install", {
    method: "POST",
    credentials: "include",
  });

  return parsePHPResponse(response);
}

export async function startPHP(): Promise<PHPStatus> {
  const response = await fetch("/api/php/start", {
    method: "POST",
    credentials: "include",
  });

  return parsePHPResponse(response);
}
