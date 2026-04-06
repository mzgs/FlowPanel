export type BackupRecord = {
  name: string;
  size: number;
  created_at: string;
};

export type ScheduledBackupRecord = {
  id: string;
  name: string;
  schedule: string;
  created_at: string;
  include_panel_data: boolean;
  include_sites: boolean;
  include_databases: boolean;
};

export type CreateBackupInput = {
  include_panel_data: boolean;
  include_sites: boolean;
  include_databases: boolean;
  site_hostnames?: string[];
  database_names?: string[];
};

export type RestoreBackupResult = {
  restored_panel_files: boolean;
  restored_panel_database: boolean;
  restored_sites?: string[];
  restored_databases?: string[];
};

export type CreateScheduledBackupInput = {
  name: string;
  schedule: string;
  include_panel_data: boolean;
  include_sites: boolean;
  include_databases: boolean;
};

type BackupsPayload = {
  backups: BackupRecord[];
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

export async function importBackup(file: File): Promise<BackupRecord> {
  const formData = new FormData();
  formData.set("backup", file);

  const response = await fetch("/api/backups/import", {
    method: "POST",
    credentials: "include",
    body: formData,
  });

  if (!response.ok) {
    throw await readBackupApiError(response, "import backup");
  }

  const payload = (await response.json()) as BackupPayload;
  return payload.backup;
}

export async function deleteBackup(name: string): Promise<void> {
  const response = await fetch(`/api/backups/${encodeURIComponent(name)}`, {
    method: "DELETE",
    credentials: "include",
  });

  if (!response.ok) {
    throw await readBackupApiError(response, "delete backup");
  }
}

export async function restoreBackup(name: string): Promise<RestoreBackupResult> {
  const response = await fetch(`/api/backups/${encodeURIComponent(name)}/restore`, {
    method: "POST",
    credentials: "include",
  });

  if (!response.ok) {
    throw await readBackupApiError(response, "restore backup");
  }

  const payload = (await response.json()) as RestoreBackupPayload;
  return payload.restore;
}

export function getBackupDownloadUrl(name: string) {
  return `/api/backups/${encodeURIComponent(name)}/download`;
}

async function readBackupApiError(
  response: Response,
  action: string,
): Promise<BackupApiError> {
  let message = `${action} request failed with status ${response.status}`;
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
    // Keep default message when the response is not JSON.
  }

  const error = new Error(message) as BackupApiError;
  error.fieldErrors = fieldErrors;
  return error;
}
