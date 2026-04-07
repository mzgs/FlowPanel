export type FTPAccount = {
  id: string;
  domain_id: string;
  domain_name?: string;
  username: string;
  root_path: string;
  enabled: boolean;
  has_password: boolean;
  created_at: string;
  updated_at: string;
};

export type CreateFTPAccountInput = {
  username: string;
  password: string;
  root_path: string;
  domain_id?: string;
  enabled?: boolean;
};

export type UpdateFTPAccountInput = {
  username: string;
  password?: string;
  root_path: string;
  domain_id?: string;
  enabled?: boolean;
};

export type FTPApiError = Error & {
  fieldErrors?: Record<string, string>;
};

export async function fetchFTPAccounts(): Promise<{ accounts: FTPAccount[] }> {
  const response = await fetch("/api/ftp/accounts", {
    credentials: "include",
  });

  if (!response.ok) {
    throw await readFTPApiError(response, "list ftp accounts");
  }

  return (await response.json()) as { accounts: FTPAccount[] };
}

export async function createFTPAccount(
  input: CreateFTPAccountInput,
): Promise<FTPAccount> {
  const response = await fetch("/api/ftp/accounts", {
    method: "POST",
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify(input),
  });

  if (!response.ok) {
    throw await readFTPApiError(response, "create ftp account");
  }

  const payload = (await response.json()) as { account: FTPAccount };
  return payload.account;
}

export async function updateFTPAccount(
  accountID: string,
  input: UpdateFTPAccountInput,
): Promise<FTPAccount> {
  const response = await fetch(`/api/ftp/accounts/${encodeURIComponent(accountID)}`, {
    method: "PUT",
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify(input),
  });

  if (!response.ok) {
    throw await readFTPApiError(response, "update ftp account");
  }

  const payload = (await response.json()) as { account: FTPAccount };
  return payload.account;
}

export async function deleteFTPAccount(accountID: string): Promise<void> {
  const response = await fetch(`/api/ftp/accounts/${encodeURIComponent(accountID)}`, {
    method: "DELETE",
    credentials: "include",
  });

  if (!response.ok) {
    throw await readFTPApiError(response, "delete ftp account");
  }
}

async function readFTPApiError(
  response: Response,
  action: string,
): Promise<FTPApiError> {
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

  const error = new Error(message) as FTPApiError;
  error.fieldErrors = fieldErrors;
  return error;
}
