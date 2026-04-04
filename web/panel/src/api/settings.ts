export type PanelSettings = {
  panel_name: string;
  panel_url: string;
  github_token: string;
};

export type UpdatePanelSettingsInput = PanelSettings;

export type SettingsApiError = Error & {
  fieldErrors?: Record<string, string>;
};

type SettingsPayload = {
  settings: PanelSettings;
};

export async function fetchSettings(): Promise<PanelSettings> {
  const response = await fetch("/api/settings", {
    credentials: "include",
  });

  if (!response.ok) {
    throw await readSettingsApiError(response, "load settings");
  }

  const payload = (await response.json()) as SettingsPayload;
  return payload.settings;
}

export async function updateSettings(
  input: UpdatePanelSettingsInput,
): Promise<PanelSettings> {
  const response = await fetch("/api/settings", {
    method: "PUT",
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify(input),
  });

  if (!response.ok) {
    throw await readSettingsApiError(response, "save settings");
  }

  const payload = (await response.json()) as SettingsPayload;
  return payload.settings;
}

async function readSettingsApiError(
  response: Response,
  action: string,
): Promise<SettingsApiError> {
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
    // Keep the default message when the response body is not valid JSON.
  }

  const error = new Error(message) as SettingsApiError;
  error.fieldErrors = fieldErrors;
  return error;
}
