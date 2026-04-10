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

export type PHPExtensionCatalogEntry = {
  id: string;
  label: string;
  aliases?: string[];
  install_id?: string;
  install_package_managers?: string[];
};

export type PHPRuntimeStatus = {
  version: string;
  platform: string;
  package_manager?: string;
  php_installed: boolean;
  php_path?: string;
  php_version?: string;
  extensions?: string[];
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

export type PHPStatus = {
  platform: string;
  package_manager?: string;
  default_version?: string;
  available_versions?: string[];
  versions?: PHPRuntimeStatus[];
  extension_catalog?: PHPExtensionCatalogEntry[];
  php_installed: boolean;
  php_path?: string;
  php_version?: string;
  extensions?: string[];
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

function withVersion(path: string, version?: string): string {
  if (!version) {
    return path;
  }

  return `${path}?version=${encodeURIComponent(version)}`;
}

export async function installPHP(version?: string): Promise<PHPStatus> {
  const response = await fetch(withVersion("/api/php/install", version), {
    method: "POST",
    credentials: "include",
  });

  return parsePHPResponse(response);
}

export async function setDefaultPHPVersion(version: string): Promise<PHPStatus> {
  const response = await fetch(withVersion("/api/php/default", version), {
    method: "PUT",
    credentials: "include",
  });

  return parsePHPResponse(response);
}

export async function installPHPExtension(extension: string, version?: string): Promise<PHPStatus> {
  const params = new URLSearchParams();
  if (version) {
    params.set("version", version);
  }
  params.set("extension", extension);

  const response = await fetch(`/api/php/extensions/install?${params.toString()}`, {
    method: "POST",
    credentials: "include",
  });

  return parsePHPResponse(response);
}

export async function removePHP(version?: string): Promise<PHPStatus> {
  const response = await fetch(withVersion("/api/php/remove", version), {
    method: "POST",
    credentials: "include",
  });

  return parsePHPResponse(response);
}

export async function startPHP(version?: string): Promise<PHPStatus> {
  const response = await fetch(withVersion("/api/php/start", version), {
    method: "POST",
    credentials: "include",
  });

  return parsePHPResponse(response);
}

export async function stopPHP(version?: string): Promise<PHPStatus> {
  const response = await fetch(withVersion("/api/php/stop", version), {
    method: "POST",
    credentials: "include",
  });

  return parsePHPResponse(response);
}

export async function restartPHP(version?: string): Promise<PHPStatus> {
  const response = await fetch(withVersion("/api/php/restart", version), {
    method: "POST",
    credentials: "include",
  });

  return parsePHPResponse(response);
}

export async function updatePHPSettings(
  input: UpdatePHPSettingsInput,
  version?: string,
): Promise<PHPStatus> {
  const response = await fetch(withVersion("/api/php/settings", version), {
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
