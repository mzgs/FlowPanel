export type AlertSettings = {
  enabled: boolean;
  webhook_url: string;
  webhook_secret_configured: boolean;
  smtp_host: string;
  smtp_port: number;
  smtp_encryption: "none" | "starttls" | "tls";
  smtp_username: string;
  smtp_password_configured: boolean;
  smtp_from: string;
  smtp_recipients: string;
  disk_warning_percent: number;
  disk_critical_percent: number;
  certificate_warning_days: number;
  login_failure_threshold: number;
  cooldown_minutes: number;
  notify_recovery: boolean;
};

export type UpdateAlertSettingsInput = Omit<
  AlertSettings,
  "webhook_secret_configured" | "smtp_password_configured"
> & {
  webhook_secret: string;
  smtp_password: string;
};

export type AlertSettingsApiError = Error & {
  fieldErrors?: Record<string, string>;
};

type AlertSettingsPayload = { settings: AlertSettings };

export async function fetchAlertSettings(): Promise<AlertSettings> {
  const response = await fetch("/api/alerts/settings", {
    cache: "no-store",
    credentials: "include",
  });
  if (!response.ok) throw await readError(response, "load notification settings");
  return ((await response.json()) as AlertSettingsPayload).settings;
}

export async function updateAlertSettings(input: UpdateAlertSettingsInput): Promise<AlertSettings> {
  const response = await fetch("/api/alerts/settings", {
    method: "PUT",
    credentials: "include",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  });
  if (!response.ok) throw await readError(response, "save notification settings");
  return ((await response.json()) as AlertSettingsPayload).settings;
}

export async function sendTestAlert(): Promise<void> {
  const response = await fetch("/api/alerts/test", { method: "POST", credentials: "include" });
  if (!response.ok) throw await readError(response, "send test notification");
}

async function readError(response: Response, action: string): Promise<AlertSettingsApiError> {
  let message = `${action} request failed with status ${response.status}`;
  let fieldErrors: Record<string, string> | undefined;
  try {
    const payload = (await response.json()) as { error?: unknown; field_errors?: unknown };
    if (typeof payload.error === "string" && payload.error) message = payload.error;
    if (payload.field_errors && typeof payload.field_errors === "object") {
      fieldErrors = payload.field_errors as Record<string, string>;
    }
  } catch {
    // Keep the status-based message.
  }
  const error = new Error(message) as AlertSettingsApiError;
  error.fieldErrors = fieldErrors;
  return error;
}
