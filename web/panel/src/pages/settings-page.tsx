import { useEffect, useMemo, useState, type FormEvent } from "react";
import {
  fetchSettings,
  updateSettings,
  type PanelSettings,
  type SettingsApiError,
} from "@/api/settings";
import { LoaderCircle, RefreshCw } from "@/components/icons/tabler-icons";
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
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});

  async function loadSettings() {
    setLoading(true);
    setLoadError(null);

    try {
      const settings = await fetchSettings();
      const nextForm = toFormState(settings);
      setForm(nextForm);
      setSavedForm(nextForm);
      setFieldErrors({});
    } catch (error) {
      const message =
        error instanceof Error ? error.message : "Failed to load settings.";
      setLoadError(message);
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void loadSettings();
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
      const nextForm = toFormState(settings);
      setForm(nextForm);
      setSavedForm(nextForm);
      toast.success("Settings saved.");
    } catch (error) {
      const settingsError = error as SettingsApiError;
      setFieldErrors(settingsError.fieldErrors ?? {});
      toast.error(
        settingsError.message || "Settings could not be saved.",
      );
    } finally {
      setSaving(false);
    }
  }

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
              <p className="text-sm text-[var(--app-text-muted)]">{loadError}</p>
            </div>
            <div>
              <Button type="button" variant="outline" onClick={() => void loadSettings()}>
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
                Basic identity, public routing, and integration credentials for the panel.
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
                  Optional public hostname or URL for the panel. Example: <span className="font-medium text-[var(--app-text)]">panel.com</span>
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
      </div>
    </>
  );
}
