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
  panelSettingsQueryKey,
  uploadGoogleDriveOAuthCredentials,
  updateSettings,
  type PanelSettings,
  type SettingsApiError,
} from "@/api/settings";
import { useQueryClient } from "@tanstack/react-query";
import {
  CircleCheck,
  Copy,
  GoogleDrive,
  LoaderCircle,
  RefreshCw,
  ShieldCheck,
  TimerReset,
  Upload,
  World,
  Wrench,
} from "@/components/icons/lucide-icons";
import { FieldError } from "@/components/field-error";
import { NotificationSettings } from "@/components/notification-settings";
import { PageHeader } from "@/components/page-header";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { toast } from "sonner";

type SettingsFormState = {
  panel_name: string;
  panel_url: string;
  github_token: string;
  login_timeout_minutes: string;
  ftp_enabled: boolean;
  ftp_port: string;
  ftp_passive_ports: string;
};

const initialForm: SettingsFormState = {
  panel_name: "",
  panel_url: "",
  github_token: "",
  login_timeout_minutes: "",
  ftp_enabled: false,
  ftp_port: "",
  ftp_passive_ports: "",
};
const googleDrivePopupMessageType = "flowpanel-google-drive-oauth";

function toFormState(settings: PanelSettings): SettingsFormState {
  return {
    panel_name: settings.panel_name,
    panel_url: settings.panel_url,
    github_token: settings.github_token,
    login_timeout_minutes: String(settings.login_timeout_minutes),
    ftp_enabled: settings.ftp_enabled,
    ftp_port: String(settings.ftp_port),
    ftp_passive_ports: settings.ftp_passive_ports,
  };
}

function sameFormState(left: SettingsFormState, right: SettingsFormState) {
  return (
    left.panel_name === right.panel_name &&
    left.panel_url === right.panel_url &&
    left.github_token === right.github_token &&
    left.login_timeout_minutes === right.login_timeout_minutes &&
    left.ftp_enabled === right.ftp_enabled &&
    left.ftp_port === right.ftp_port &&
    left.ftp_passive_ports === right.ftp_passive_ports
  );
}

export function SettingsPage() {
  const queryClient = useQueryClient();
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
    queryClient.setQueryData(panelSettingsQueryKey, nextSettings);
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

  function discardChanges() {
    if (savedForm) {
      setForm(savedForm);
      setFieldErrors({});
    }
  }

  async function copyGoogleDriveRedirectURL() {
    try {
      await navigator.clipboard.writeText(googleDriveRedirectURL);
      toast.success("Redirect URI copied.");
    } catch {
      toast.error("Could not copy the redirect URI.");
    }
  }

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setSaving(true);
    setFieldErrors({});

    try {
      const settings = await updateSettings({
        panel_name: form.panel_name,
        panel_url: form.panel_url,
        github_token: form.github_token,
        login_timeout_minutes:
          Number.parseInt(form.login_timeout_minutes, 10) || 0,
        ftp_enabled: form.ftp_enabled,
        ftp_port: Number.parseInt(form.ftp_port, 10) || 0,
        ftp_passive_ports: form.ftp_passive_ports,
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
      <div className="mx-auto max-w-6xl">
        <PageHeader
          title="Settings"
          meta="Manage your panel, security, and connected services."
          actions={
            <div className="flex flex-wrap items-center justify-end gap-2">
              {isDirty ? (
                <Badge variant="secondary" className="gap-1.5">
                  <span className="h-1.5 w-1.5 rounded-full bg-amber-500" />
                  Unsaved changes
                </Badge>
              ) : null}
              {isDirty ? (
                <Button type="button" variant="ghost" onClick={discardChanges} disabled={saving}>
                  Discard
                </Button>
              ) : null}
              <Button
                type="submit"
                form="settings-form"
                disabled={loading || Boolean(loadError) || saving || !isDirty}
              >
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
          }
        />
      </div>

      <div className="mx-auto max-w-6xl px-4 pb-8 sm:px-6 lg:px-8">
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
          <form id="settings-form" onSubmit={handleSubmit} className="grid gap-4 lg:grid-cols-[minmax(0,1.45fr)_minmax(18rem,0.75fr)]">
            <section className="rounded-lg border border-[var(--app-border)] bg-[var(--app-surface)] shadow-sm">
              <div className="flex gap-3 border-b border-[var(--app-border)] px-5 py-4">
                <div className="mt-0.5 flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-primary/10 text-primary">
                  <World className="h-4 w-4" />
                </div>
                <div>
                  <h2 className="text-sm font-semibold text-[var(--app-text)]">Panel details</h2>
                  <p className="mt-0.5 text-xs text-[var(--app-text-muted)]">Identity, public address, and GitHub access.</p>
                </div>
              </div>

              <div className="grid gap-4 px-5 py-4 sm:grid-cols-2">
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
                  <p className="text-xs text-[var(--app-text-muted)]">Shown in the navigation and browser title.</p>
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
                  <p className="text-xs text-[var(--app-text-muted)]">Public hostname, for example panel.example.com.</p>
                  <FieldError message={fieldErrors.panel_url} />
                </div>

                <div className="space-y-2 sm:col-span-2">
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
                    autoComplete="new-password"
                    spellCheck={false}
                    aria-invalid={fieldErrors.github_token ? true : undefined}
                  />
                  <p className="text-xs text-[var(--app-text-muted)]">Used only for private repository operations.</p>
                  <FieldError message={fieldErrors.github_token} />
                </div>
              </div>
            </section>

            <section className="rounded-lg border border-[var(--app-border)] bg-[var(--app-surface)] shadow-sm">
              <div className="flex gap-3 border-b border-[var(--app-border)] px-5 py-4">
                <div className="mt-0.5 flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-primary/10 text-primary">
                  <ShieldCheck className="h-4 w-4" />
                </div>
                <div>
                  <h2 className="text-sm font-semibold text-[var(--app-text)]">Session security</h2>
                  <p className="mt-0.5 text-xs text-[var(--app-text-muted)]">Automatically sign out inactive sessions.</p>
                </div>
              </div>

              <div className="px-5 py-4">
                <div className="space-y-2">
                  <Label htmlFor="login_timeout_minutes">Inactivity timeout</Label>
                  <div className="relative">
                    <Input
                      id="login_timeout_minutes"
                      type="number"
                      min="5"
                      max="10080"
                      value={form.login_timeout_minutes}
                      onChange={(event) => setForm((current) => ({ ...current, login_timeout_minutes: event.target.value }))}
                      className="pr-20"
                      aria-invalid={fieldErrors.login_timeout_minutes ? true : undefined}
                    />
                    <span className="pointer-events-none absolute inset-y-0 right-3 flex items-center text-xs text-[var(--app-text-muted)]">minutes</span>
                  </div>
                  <p className="text-xs text-[var(--app-text-muted)]">Between 5 minutes and 7 days.</p>
                  <FieldError message={fieldErrors.login_timeout_minutes} />
                </div>
              </div>
            </section>

            <section className="rounded-lg border border-[var(--app-border)] bg-[var(--app-surface)] shadow-sm lg:col-span-2">
              <div className="flex items-center justify-between gap-4 border-b border-[var(--app-border)] px-5 py-4">
                <div className="flex gap-3">
                  <div className="mt-0.5 flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-primary/10 text-primary">
                    <Wrench className="h-4 w-4" />
                  </div>
                  <div>
                    <div className="flex items-center gap-2">
                      <h2 className="text-sm font-semibold text-[var(--app-text)]">FTP server</h2>
                      <Badge variant={form.ftp_enabled ? "default" : "secondary"}>{form.ftp_enabled ? "Enabled" : "Disabled"}</Badge>
                    </div>
                    <p className="mt-0.5 text-xs text-[var(--app-text-muted)]">Shared listener for domain FTP accounts.</p>
                  </div>
                </div>
                <Switch
                  id="ftp_enabled"
                  checked={form.ftp_enabled}
                  onCheckedChange={(checked) => setForm((current) => ({ ...current, ftp_enabled: checked }))}
                  aria-label="Enable FTP server"
                />
              </div>

              <div className="grid gap-4 px-5 py-4 md:grid-cols-2">
                <div className="space-y-2">
                  <Label htmlFor="ftp_port">FTP port</Label>
                  <Input
                    id="ftp_port"
                    type="number"
                    min="1"
                    max="65535"
                    value={form.ftp_port}
                    disabled={!form.ftp_enabled}
                    onChange={(event) =>
                      setForm((current) => ({
                        ...current,
                        ftp_port: event.target.value,
                      }))
                    }
                    placeholder="2121"
                    aria-invalid={fieldErrors.ftp_port ? true : undefined}
                  />
                  <p className="text-xs text-[var(--app-text-muted)]">Listens on all interfaces; the default is 2121.</p>
                  <FieldError message={fieldErrors.ftp_port} />
                </div>

                <div className="space-y-2">
                  <Label htmlFor="ftp_passive_ports">Passive port range</Label>
                  <Input
                    id="ftp_passive_ports"
                    value={form.ftp_passive_ports}
                    disabled={!form.ftp_enabled}
                    onChange={(event) =>
                      setForm((current) => ({
                        ...current,
                        ftp_passive_ports: event.target.value,
                      }))
                    }
                    placeholder="30000-30100"
                    autoComplete="off"
                    spellCheck={false}
                    aria-invalid={fieldErrors.ftp_passive_ports ? true : undefined}
                  />
                  <p className="text-xs text-[var(--app-text-muted)]">One inclusive range, for example 30000-30100.</p>
                  <FieldError message={fieldErrors.ftp_passive_ports} />
                </div>
              </div>

              <div className="flex gap-2 border-t border-[var(--app-border)] bg-amber-500/5 px-5 py-3 text-xs text-[var(--app-text-muted)]">
                <TimerReset className="mt-0.5 h-3.5 w-3.5 shrink-0 text-amber-600" />
                Plain FTP is not encrypted. Use it only on trusted networks or behind a secure tunnel.
              </div>
            </section>
          </form>
        )}

        {!loading && !loadError ? (
          <section className="mt-4 rounded-lg border border-[var(--app-border)] bg-[var(--app-surface)] shadow-sm">
            <div className="flex items-center justify-between gap-4 border-b border-[var(--app-border)] px-5 py-4">
              <div className="flex gap-3">
                <div className="mt-0.5 flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-primary/10 text-primary">
                  <GoogleDrive className="h-4 w-4" />
                </div>
                <div>
                  <div className="flex items-center gap-2">
                    <h2 className="text-sm font-semibold text-[var(--app-text)]">Google Drive</h2>
                    <Badge variant={googleDriveConnected ? "default" : "secondary"}>
                      {googleDriveConnected ? "Connected" : googleDriveReady ? "Ready to connect" : "Setup required"}
                    </Badge>
                  </div>
                  <p className="mt-0.5 text-xs text-[var(--app-text-muted)]">Store and restore backups from your Drive.</p>
                </div>
              </div>
              {googleDriveConnected ? <CircleCheck className="h-5 w-5 shrink-0 text-emerald-500" /> : null}
            </div>

            <div className="grid gap-5 px-5 py-4 lg:grid-cols-[minmax(0,1fr)_auto] lg:items-end">
              <div className="space-y-3">
                <div className="space-y-2">
                  <Label htmlFor="google_drive_redirect_url">
                    Authorized redirect URI
                  </Label>
                  <div className="flex gap-2">
                    <Input
                      id="google_drive_redirect_url"
                      value={googleDriveRedirectURL}
                      readOnly
                      spellCheck={false}
                      className="font-mono text-xs"
                    />
                    <Button type="button" variant="outline" size="icon" onClick={() => void copyGoogleDriveRedirectURL()} aria-label="Copy redirect URI">
                      <Copy className="h-4 w-4" />
                    </Button>
                  </div>
                  <p className="text-xs text-[var(--app-text-muted)]">Add this exact URL to the authorized redirect URIs in Google Cloud.</p>
                </div>

                {googleDriveConnected ? (
                  <p className="text-xs text-[var(--app-text-muted)]">Connected as <span className="font-medium text-[var(--app-text)]">{googleDriveEmail || "Google account"}</span></p>
                ) : null}
                {!googleDriveReady ? (
                  <p className="text-xs text-[var(--app-text-muted)]">Upload an OAuth client JSON file to enable account connection.</p>
                ) : null}
              </div>

              <div className="flex flex-col gap-2 sm:flex-row sm:flex-wrap lg:justify-end">
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
                      Upload credentials
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
                    "Reconnect"
                  ) : (
                    "Connect account"
                  )}
                </Button>
              </div>
            </div>
          </section>
        ) : null}

        {!loading && !loadError ? <NotificationSettings /> : null}
      </div>
    </>
  );
}
