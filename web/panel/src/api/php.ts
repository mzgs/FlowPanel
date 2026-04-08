export type PHPSettings = {
  max_execution_time?: string;
  max_input_time?: string;
  memory_limit?: string;
  post_max_size?: string;
  file_uploads?: string;
  upload_max_filesize?: string;
  max_file_uploads?: string;
  default_socket_timeout?: string;
  error_reporting?: string;
  display_errors?: string;
};

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
  remove_available: boolean;
  remove_label?: string;
  start_available: boolean;
  start_label?: string;
  stop_available?: boolean;
  stop_label?: string;
  restart_available?: boolean;
  restart_label?: string;
  loaded_config_file?: string;
  scan_dir?: string;
  managed_config_file?: string;
  settings: PHPSettings;
};

type PHPStatusPayload = {
  php: PHPStatus;
};

export type UpdatePHPSettingsInput = {
  max_execution_time: string;
  max_input_time: string;
  memory_limit: string;
  post_max_size: string;
  file_uploads: string;
  upload_max_filesize: string;
  max_file_uploads: string;
  default_socket_timeout: string;
  error_reporting: string;
  display_errors: string;
};

export type PHPApiError = Error & {
  fieldErrors?: Record<string, string>;
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
    cache: "no-store",
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

export async function removePHP(): Promise<PHPStatus> {
  const response = await fetch("/api/php/remove", {
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

export async function stopPHP(): Promise<PHPStatus> {
  const response = await fetch("/api/php/stop", {
    method: "POST",
    credentials: "include",
  });

  return parsePHPResponse(response);
}

export async function restartPHP(): Promise<PHPStatus> {
  const response = await fetch("/api/php/restart", {
    method: "POST",
    credentials: "include",
  });

  return parsePHPResponse(response);
}

export async function updatePHPSettings(
  input: UpdatePHPSettingsInput,
): Promise<PHPStatus> {
  const response = await fetch("/api/php/settings", {
    method: "PUT",
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify(input),
  });

  if (response.ok) {
    return parsePHPResponse(response);
  }

  let message = `php settings request failed with status ${response.status}`;
  let fieldErrors: Record<string, string> | undefined;

  try {
    const payload = (await response.json()) as {
      error?: unknown;
      field_errors?: unknown;
    };
    if (typeof payload.error === "string" && payload.error) {
      message = payload.error;
    }
    if (payload.field_errors && typeof payload.field_errors === "object") {
      fieldErrors = payload.field_errors as Record<string, string>;
    }
  } catch {
    // Keep the default message when the payload is not JSON.
  }

  const error = new Error(message) as PHPApiError;
  error.fieldErrors = fieldErrors;
  throw error;
}
