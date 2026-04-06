import {
  useEffect,
  useMemo,
  useRef,
  useState,
  type ChangeEvent,
  type FormEvent,
} from "react";
import {
  disconnectGoogleDrive,
  fetchSettings,
  uploadGoogleDriveOAuthCredentials,
  updateSettings,
  type PanelSettings,
  type SettingsApiError,
} from "@/api/settings";
import {
  GoogleDrive,
  LoaderCircle,
  RefreshCw,
  Upload,
} from "@/components/icons/tabler-icons";
import { PageHeader } from "@/components/page-header";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { toast } from "sonner";

type SettingsFormState = {
  panel_name: string;
  panel_url: string;
  github_token: string;
};

const initialForm: SettingsFormState = {
  panel_name: "",
  panel_url: "",
  github_token: "",
};
const googleDrivePopupMessageType = "flowpanel-google-drive-oauth";

function toFormState(settings: PanelSettings): SettingsFormState {
  return {
    panel_name: settings.panel_name,
    panel_url: settings.panel_url,
    github_token: settings.github_token,
  };
}

function sameFormState(left: SettingsFormState, right: SettingsFormState) {
  return (
    left.panel_name === right.panel_name &&
    left.panel_url === right.panel_url &&
    left.github_token === right.github_token
  );
}

function FieldError({ message }: { message?: string }) {
  if (!message) {
    return null;
  }

  return <p className="text-sm text-destructive">{message}</p>;
}

export function SettingsPage() {
  const [form, setForm] = useState<SettingsFormState>(initialForm);
  const [savedForm, setSavedForm] = useState<SettingsFormState | null>(null);
  const [settings, setSettings] = useState<PanelSettings | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [uploadingGoogleDriveCredentials, setUploadingGoogleDriveCredentials] =
    useState(false);
  const [connectingGoogleDrive, setConnectingGoogleDrive] = useState(false);
  const [disconnectingGoogleDrive, setDisconnectingGoogleDrive] =
    useState(false);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});
  const googleDriveCredentialsInputRef = useRef<HTMLInputElement | null>(null);
  const googleDriveConnectResolvedRef = useRef(false);

  function applySettings(nextSettings: PanelSettings) {
    const nextForm = toFormState(nextSettings);
    setSettings(nextSettings);
    setForm(nextForm);
    setSavedForm(nextForm);
  }

  async function loadSettings(options?: { showLoading?: boolean }) {
    const showLoading = options?.showLoading ?? true;
    if (showLoading) {
      setLoading(true);
      setLoadError(null);
    }

    try {
      const nextSettings = await fetchSettings();
      applySettings(nextSettings);
      setFieldErrors({});
    } catch (error) {
      if (showLoading) {
        const message =
          error instanceof Error ? error.message : "Failed to load settings.";
        setLoadError(message);
      }
    } finally {
      if (showLoading) {
        setLoading(false);
      }
    }
  }

  useEffect(() => {
    void loadSettings();
  }, []);

  useEffect(() => {
    function handleGoogleDrivePopupMessage(event: MessageEvent) {
      if (event.origin !== window.location.origin) {
        return;
      }

      const payload = event.data as {
        type?: string;
        status?: "success" | "error";
        message?: string;
        email?: string;
      };
      if (payload.type !== googleDrivePopupMessageType) {
        return;
      }

      googleDriveConnectResolvedRef.current = true;
      setConnectingGoogleDrive(false);
      if (payload.status === "success") {
        window.location.reload();
        return;
      }

      toast.error(payload.message || "Google Drive connection failed.");
    }

    window.addEventListener("message", handleGoogleDrivePopupMessage);
    return () => {
      window.removeEventListener("message", handleGoogleDrivePopupMessage);
    };
  }, []);

  const isDirty = useMemo(
    () => (savedForm ? !sameFormState(form, savedForm) : false),
    [form, savedForm],
  );

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setSaving(true);
    setFieldErrors({});

    try {
      const settings = await updateSettings({
        panel_name: form.panel_name,
        panel_url: form.panel_url,
        github_token: form.github_token,
      });
      applySettings(settings);
      toast.success("Settings saved.");
    } catch (error) {
      const settingsError = error as SettingsApiError;
      setFieldErrors(settingsError.fieldErrors ?? {});
      toast.error(settingsError.message || "Settings could not be saved.");
    } finally {
      setSaving(false);
    }
  }

  async function handleGoogleDriveCredentialsSelection(
    event: ChangeEvent<HTMLInputElement>,
  ) {
    const file = event.target.files?.[0];
    event.target.value = "";
    if (!file) {
      return;
    }

    setUploadingGoogleDriveCredentials(true);

    try {
      const nextSettings = await uploadGoogleDriveOAuthCredentials(file);
      applySettings(nextSettings);
      toast.success("Google Drive OAuth credentials saved.");
    } catch (error) {
      const message =
        error instanceof Error
          ? error.message
          : "Failed to upload Google Drive OAuth credentials.";
      toast.error(message);
    } finally {
      setUploadingGoogleDriveCredentials(false);
    }
  }

  function handleGoogleDriveConnect() {
    googleDriveConnectResolvedRef.current = false;
    const popup = window.open(
      "/api/settings/google-drive/connect",
      "flowpanel-google-drive",
      "popup=yes,width=560,height=720",
    );
    if (!popup) {
      toast.error("Allow pop-ups to connect Google Drive.");
      return;
    }

    setConnectingGoogleDrive(true);
    popup.focus();
    const interval = window.setInterval(() => {
      if (!popup.closed) {
        return;
      }

      window.clearInterval(interval);
      setConnectingGoogleDrive(false);
      if (!googleDriveConnectResolvedRef.current) {
        window.location.reload();
      }
    }, 400);
  }

  async function handleGoogleDriveDisconnect() {
    setDisconnectingGoogleDrive(true);

    try {
      const nextSettings = await disconnectGoogleDrive();
      applySettings(nextSettings);
      toast.success("Google Drive disconnected.");
    } catch (error) {
      const message =
        error instanceof Error
          ? error.message
          : "Failed to disconnect Google Drive.";
      toast.error(message);
    } finally {
      setDisconnectingGoogleDrive(false);
    }
  }

  const googleDriveReady = settings?.google_drive_available ?? false;
  const googleDriveConnected = settings?.google_drive_connected ?? false;
  const googleDriveEmail = settings?.google_drive_email ?? "";
  const googleDriveRedirectURL =
    typeof window === "undefined"
      ? "/api/settings/google-drive/callback"
      : `${window.location.origin}/api/settings/google-drive/callback`;

  return (
    <>
      <PageHeader
        title="Settings"
        meta="Store panel identity, the optional public panel URL, and GitHub credentials in SQLite. These values persist across restarts."
      />

      <div className="px-4 pb-8 sm:px-6 lg:px-8">
        {loading ? (
          <div className="flex min-h-56 items-center justify-center rounded-lg border border-[var(--app-border)] bg-[var(--app-surface)]">
            <div className="flex items-center gap-3 text-sm text-[var(--app-text-muted)]">
              <LoaderCircle className="h-4 w-4 animate-spin" />
              Loading settings
            </div>
          </div>
        ) : loadError ? (
          <section className="flex flex-col gap-4 rounded-lg border border-[var(--app-border)] bg-[var(--app-surface)] p-6">
            <div className="space-y-1">
              <h2 className="text-base font-semibold text-[var(--app-text)]">
                Settings unavailable
              </h2>
              <p className="text-sm text-[var(--app-text-muted)]">
                {loadError}
              </p>
            </div>
            <div>
              <Button
                type="button"
                variant="outline"
                onClick={() => void loadSettings()}
              >
                <RefreshCw className="h-4 w-4" />
                Retry
              </Button>
            </div>
          </section>
        ) : (
          <form
            onSubmit={handleSubmit}
            className="rounded-lg border border-[var(--app-border)] bg-[var(--app-surface)]"
          >
            <div className="border-b border-[var(--app-border)] px-6 py-4">
              <h2 className="text-base font-semibold text-[var(--app-text)]">
                General
              </h2>
              <p className="mt-1 text-sm text-[var(--app-text-muted)]">
                Basic identity, public routing, and integration credentials for
                the panel.
              </p>
            </div>

            <div className="grid gap-5 px-6 py-5 md:grid-cols-2">
              <div className="space-y-2">
                <Label htmlFor="panel_name">Panel name</Label>
                <Input
                  id="panel_name"
                  value={form.panel_name}
                  onChange={(event) =>
                    setForm((current) => ({
                      ...current,
                      panel_name: event.target.value,
                    }))
                  }
                  aria-invalid={fieldErrors.panel_name ? true : undefined}
                />
                <FieldError message={fieldErrors.panel_name} />
              </div>

              <div className="space-y-2">
                <Label htmlFor="panel_url">Panel URL</Label>
                <Input
                  id="panel_url"
                  value={form.panel_url}
                  onChange={(event) =>
                    setForm((current) => ({
                      ...current,
                      panel_url: event.target.value,
                    }))
                  }
                  placeholder="panel.example.com"
                  autoComplete="off"
                  spellCheck={false}
                  aria-invalid={fieldErrors.panel_url ? true : undefined}
                />
                <p className="text-sm text-[var(--app-text-muted)]">
                  Optional public hostname or URL for the panel. Example:{" "}
                  <span className="font-medium text-[var(--app-text)]">
                    panel.com
                  </span>
                </p>
                <FieldError message={fieldErrors.panel_url} />
              </div>

              <div className="space-y-2">
                <Label htmlFor="github_token">GitHub token</Label>
                <Input
                  id="github_token"
                  type="password"
                  value={form.github_token}
                  onChange={(event) =>
                    setForm((current) => ({
                      ...current,
                      github_token: event.target.value,
                    }))
                  }
                  placeholder="github_pat_..."
                  autoComplete="off"
                  spellCheck={false}
                  aria-invalid={fieldErrors.github_token ? true : undefined}
                />
                <FieldError message={fieldErrors.github_token} />
              </div>
            </div>

            <div className="flex items-center justify-between gap-4 border-t border-[var(--app-border)] px-6 py-4">
              <div className="text-sm text-[var(--app-text-muted)]">
                {isDirty ? "You have unsaved changes." : ""}
              </div>
              <div className="flex items-center gap-3">
                <Button type="submit" disabled={saving || !isDirty}>
                  {saving ? (
                    <>
                      <LoaderCircle className="h-4 w-4 animate-spin" />
                      Saving
                    </>
                  ) : (
                    "Save changes"
                  )}
                </Button>
              </div>
            </div>
          </form>
        )}

        {!loading && !loadError ? (
          <section className="mt-6 rounded-lg border border-[var(--app-border)] bg-[var(--app-surface)]">
            <div className="border-b border-[var(--app-border)] px-6 py-4">
              <h2 className="flex items-center gap-2 text-base font-semibold text-[var(--app-text)]">
                <GoogleDrive className="h-4 w-4" />
                Google Drive
              </h2>
              <p className="mt-1 text-sm text-[var(--app-text-muted)]">
                Connect Google Drive for backups.
              </p>
            </div>

            <div className="flex flex-col gap-4 px-6 py-5 md:flex-row md:items-center md:justify-between">
              <div className="space-y-4">
                <div className="space-y-2">
                  <Label htmlFor="google_drive_redirect_url">
                    Authorized redirect URI
                  </Label>
                  <Input
                    id="google_drive_redirect_url"
                    value={googleDriveRedirectURL}
                    readOnly
                    spellCheck={false}
                  />
                  <p className="text-sm text-[var(--app-text-muted)]">
                    Add this exact URL to your Google OAuth client.
                  </p>
                </div>

                {googleDriveConnected ? (
                  <p className="text-sm font-medium text-[var(--app-text)]">
                    Connected as {googleDriveEmail || "Google account"}
                  </p>
                ) : null}
                {!googleDriveReady ? (
                  <p className="text-sm text-[var(--app-text-muted)]">
                    Upload the OAuth client JSON here or set
                    FLOWPANEL_GOOGLE_DRIVE_CLIENT_ID and
                    FLOWPANEL_GOOGLE_DRIVE_CLIENT_SECRET.
                  </p>
                ) : null}
              </div>

              <div className="flex items-center gap-3">
                <input
                  ref={googleDriveCredentialsInputRef}
                  type="file"
                  accept=".json,application/json"
                  className="hidden"
                  onChange={(event) =>
                    void handleGoogleDriveCredentialsSelection(event)
                  }
                />
                <Button
                  type="button"
                  variant="outline"
                  onClick={() =>
                    googleDriveCredentialsInputRef.current?.click()
                  }
                  disabled={uploadingGoogleDriveCredentials}
                >
                  {uploadingGoogleDriveCredentials ? (
                    <>
                      <LoaderCircle className="h-4 w-4 animate-spin" />
                      Uploading
                    </>
                  ) : (
                    <>
                      <Upload className="h-4 w-4" />
                      Upload OAuth Credential JSON
                    </>
                  )}
                </Button>

                {googleDriveConnected ? (
                  <Button
                    type="button"
                    variant="outline"
                    onClick={() => void handleGoogleDriveDisconnect()}
                    disabled={disconnectingGoogleDrive}
                  >
                    {disconnectingGoogleDrive ? (
                      <>
                        <LoaderCircle className="h-4 w-4 animate-spin" />
                        Disconnecting
                      </>
                    ) : (
                      "Disconnect"
                    )}
                  </Button>
                ) : null}

                <Button
                  type="button"
                  onClick={handleGoogleDriveConnect}
                  disabled={!googleDriveReady || connectingGoogleDrive}
                >
                  {connectingGoogleDrive ? (
                    <>
                      <LoaderCircle className="h-4 w-4 animate-spin" />
                      Waiting for Google
                    </>
                  ) : googleDriveConnected ? (
                    "Reconnect account"
                  ) : (
                    "Connect Google account"
                  )}
                </Button>
              </div>
            </div>
          </section>
        ) : null}
      </div>
    </>
  );
}
