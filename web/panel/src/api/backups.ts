export type BackupRecord = {
  name: string;
  size: number;
  created_at: string;
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

type BackupsPayload = {
  backups: BackupRecord[];
};

type BackupPayload = {
  backup: BackupRecord;
};

type RestoreBackupPayload = {
  restore: RestoreBackupResult;
};

type BackupApiError = Error;

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

  try {
    const payload = (await response.json()) as { error?: unknown };
    if (typeof payload.error === "string" && payload.error) {
      message = payload.error;
    }
  } catch {
    // Keep default message when the response is not JSON.
  }

  return new Error(message);
}
