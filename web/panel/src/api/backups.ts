export type BackupRecord = {
  id: string;
  name: string;
  size: number;
  created_at: string;
  location: "local" | "google_drive";
};

export type ScheduledBackupRecord = {
  id: string;
  name: string;
  schedule: string;
  created_at: string;
  include_panel_data: boolean;
  include_docker_data: boolean;
  include_sites: boolean;
  include_databases: boolean;
  location: "local" | "google_drive";
};

export type CreateBackupInput = {
  include_panel_data: boolean;
  include_docker_data: boolean;
  include_sites: boolean;
  include_databases: boolean;
  site_hostnames?: string[];
  database_names?: string[];
  location?: "local" | "google_drive";
};

export type RestoreBackupResult = {
  restored_panel_files: boolean;
  restored_panel_database: boolean;
  restored_docker_data: boolean;
  restored_docker_containers?: string[];
  restored_sites?: string[];
  restored_databases?: string[];
  warnings?: string[];
};

export type CreateScheduledBackupInput = {
  name: string;
  schedule: string;
  include_panel_data: boolean;
  include_docker_data: boolean;
  include_sites: boolean;
  include_databases: boolean;
  location?: "local" | "google_drive";
};

type BackupsPayload = {
  backups: BackupRecord[];
  directory: string;
};

type ScheduledBackupsPayload = {
  enabled: boolean;
  started: boolean;
  schedules: ScheduledBackupRecord[];
};

type BackupPayload = {
  backup: BackupRecord;
};

type ScheduledBackupPayload = {
  schedule: ScheduledBackupRecord;
};

type RestoreBackupPayload = {
  restore: RestoreBackupResult;
};

type BackupApiError = Error & {
  fieldErrors?: Record<string, string>;
};

export type BackupUploadProgress = {
  loaded: number;
  total: number;
};

export type BackupRestoreProgress = {
  label: string;
  percent: number;
};

export async function fetchBackups(): Promise<BackupsPayload> {
  const response = await fetch("/api/backups", {
    credentials: "include",
  });

  if (!response.ok) {
    throw await readBackupApiError(response, "list backups");
  }

  return (await response.json()) as BackupsPayload;
}

export async function createBackup(input: CreateBackupInput): Promise<BackupRecord> {
  const response = await fetch("/api/backups", {
    method: "POST",
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify(input),
  });

  if (!response.ok) {
    throw await readBackupApiError(response, "create backup");
  }

  const payload = (await response.json()) as BackupPayload;
  return payload.backup;
}

export async function fetchScheduledBackups(): Promise<ScheduledBackupsPayload> {
  const response = await fetch("/api/backups/schedules", {
    credentials: "include",
  });

  if (!response.ok) {
    throw await readBackupApiError(response, "list scheduled backups");
  }

  return (await response.json()) as ScheduledBackupsPayload;
}

export async function createScheduledBackup(
  input: CreateScheduledBackupInput,
): Promise<ScheduledBackupRecord> {
  const response = await fetch("/api/backups/schedules", {
    method: "POST",
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify(input),
  });

  if (!response.ok) {
    throw await readBackupApiError(response, "create scheduled backup");
  }

  const payload = (await response.json()) as ScheduledBackupPayload;
  return payload.schedule;
}

export async function deleteScheduledBackup(id: string): Promise<void> {
  const response = await fetch(`/api/backups/schedules/${encodeURIComponent(id)}`, {
    method: "DELETE",
    credentials: "include",
  });

  if (!response.ok) {
    throw await readBackupApiError(response, "delete scheduled backup");
  }
}

export function importBackup(
  file: File,
  onProgress?: (progress: BackupUploadProgress) => void,
): Promise<BackupRecord> {
  const formData = new FormData();
  formData.set("backup", file);

  return new Promise((resolve, reject) => {
    const request = new XMLHttpRequest();
    request.open("POST", "/api/backups/import");
    request.withCredentials = true;
    request.upload.onprogress = (event) => {
      onProgress?.({
        loaded: event.loaded,
        total: event.lengthComputable ? event.total : 0,
      });
    };
    request.onerror = () =>
      reject(new Error("Backup upload failed because of a network error."));
    request.onload = () => {
      if (request.status >= 200 && request.status < 300) {
        try {
          resolve((JSON.parse(request.responseText) as BackupPayload).backup);
        } catch {
          reject(new Error("Import backup returned an invalid response."));
        }
        return;
      }
      reject(parseBackupApiError(request.status, request.responseText, "import backup"));
    };
    request.send(formData);
  });
}

export async function deleteBackup(id: string, location: BackupRecord["location"]): Promise<void> {
  const response = await fetch(
    `/api/backups/${encodeURIComponent(id)}?location=${encodeURIComponent(location)}`,
    {
      method: "DELETE",
      credentials: "include",
    },
  );

  if (!response.ok) {
    throw await readBackupApiError(response, "delete backup");
  }
}

export async function restoreBackup(
  id: string,
  location: BackupRecord["location"],
  onProgress?: (progress: BackupRestoreProgress) => void,
): Promise<RestoreBackupResult> {
  const response = await fetch(
    `/api/backups/${encodeURIComponent(id)}/restore-progress?location=${encodeURIComponent(location)}`,
    {
      method: "POST",
      credentials: "include",
    },
  );

  if (!response.ok) {
    throw await readBackupApiError(response, "restore backup");
  }

  if (!response.body) {
    throw new Error("Restore backup returned an empty response.");
  }

  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  let result: RestoreBackupResult | undefined;
  const consumeLine = (line: string) => {
    if (!line.trim()) return;
    const event = JSON.parse(line) as RestoreBackupPayload & {
      progress?: BackupRestoreProgress;
      error?: string;
    };
    if (event.error) throw new Error(event.error);
    if (event.progress) onProgress?.(event.progress);
    if (event.restore) result = event.restore;
  };

  while (true) {
    const { done, value } = await reader.read();
    buffer += decoder.decode(value, { stream: !done });
    const lines = buffer.split("\n");
    buffer = lines.pop() ?? "";
    lines.forEach(consumeLine);
    if (done) break;
  }
  consumeLine(buffer);
  if (!result) throw new Error("Restore backup returned an invalid response.");
  return result;
}

export function getBackupDownloadUrl(id: string, location: BackupRecord["location"]) {
  return `/api/backups/${encodeURIComponent(id)}/download?location=${encodeURIComponent(location)}`;
}

async function readBackupApiError(
  response: Response,
  action: string,
): Promise<BackupApiError> {
  return parseBackupApiError(response.status, await response.text(), action);
}

function parseBackupApiError(
  status: number,
  responseText: string,
  action: string,
): BackupApiError {
  let message = `${action} request failed with status ${status}`;
  let fieldErrors: Record<string, string> | undefined;

  try {
    const payload = JSON.parse(responseText) as {
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
    // Keep the status-based message when the response is not JSON.
  }

  const error = new Error(message) as BackupApiError;
  error.fieldErrors = fieldErrors;
  return error;
}
