import { useEffect, useState } from "react";
import {
  updatePHPSettings,
  type PHPApiError,
  type PHPSettings,
  type PHPStatus,
  type PHPRuntimeStatus,
  type UpdatePHPSettingsInput,
} from "@/api/php";
import { LoaderCircle } from "@/components/icons/tabler-icons";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { cn } from "@/lib/utils";
import { toast } from "sonner";

const phpErrorReportingOptions = [
  { value: "E_ALL", label: "E_ALL" },
  { value: "E_ALL & ~E_NOTICE", label: "E_ALL & ~E_NOTICE" },
  { value: "E_ALL & ~E_DEPRECATED", label: "E_ALL & ~E_DEPRECATED" },
  {
    value: "E_ALL & ~E_NOTICE & ~E_DEPRECATED",
    label: "E_ALL & ~E_NOTICE & ~E_DEPRECATED",
  },
] as const;

const phpSettingsSections = [
  {
    id: "runtime-settings",
    label: "Runtime settings",
  },
  {
    id: "runtime-info",
    label: "Runtime info",
  },
] as const;

type PHPSettingsSectionId = (typeof phpSettingsSections)[number]["id"];

type PHPSettingsDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  status: PHPRuntimeStatus | null;
  version: string;
  onStatusChange: (status: PHPStatus) => void;
};

type PHPSettingsFormState = UpdatePHPSettingsInput;

const emptyForm: PHPSettingsFormState = {
  max_execution_time: "",
  max_input_time: "",
  memory_limit: "",
  post_max_size: "",
  file_uploads: "On",
  upload_max_filesize: "",
  max_file_uploads: "",
  default_socket_timeout: "",
  error_reporting: "E_ALL",
  display_errors: "Off",
};

function toFormState(settings?: PHPSettings): PHPSettingsFormState {
  return {
    max_execution_time: settings?.max_execution_time ?? "",
    max_input_time: settings?.max_input_time ?? "",
    memory_limit: settings?.memory_limit ?? "",
    post_max_size: settings?.post_max_size ?? "",
    file_uploads: settings?.file_uploads ?? "On",
    upload_max_filesize: settings?.upload_max_filesize ?? "",
    max_file_uploads: settings?.max_file_uploads ?? "",
    default_socket_timeout: settings?.default_socket_timeout ?? "",
    error_reporting: settings?.error_reporting ?? "E_ALL",
    display_errors: settings?.display_errors ?? "Off",
  };
}

function sameFormState(left: PHPSettingsFormState, right: PHPSettingsFormState) {
  return (
    left.max_execution_time === right.max_execution_time &&
    left.max_input_time === right.max_input_time &&
    left.memory_limit === right.memory_limit &&
    left.post_max_size === right.post_max_size &&
    left.file_uploads === right.file_uploads &&
    left.upload_max_filesize === right.upload_max_filesize &&
    left.max_file_uploads === right.max_file_uploads &&
    left.default_socket_timeout === right.default_socket_timeout &&
    left.error_reporting === right.error_reporting &&
    left.display_errors === right.display_errors
  );
}

function getPHPActionLabel(state?: string | null) {
  switch (state) {
    case "installing":
      return "Installing...";
    case "removing":
      return "Removing...";
    case "starting":
      return "Starting...";
    case "stopping":
      return "Stopping...";
    case "restarting":
      return "Restarting...";
    default:
      return null;
  }
}

function isPHPActionState(state?: string | null) {
  return getPHPActionLabel(state) !== null;
}

function extractVersionNumber(value: string, pattern: RegExp) {
  const match = value.match(pattern);
  return match?.[1] ?? null;
}

function formatPHPVersion(status: PHPRuntimeStatus | null) {
  const actionLabel = getPHPActionLabel(status?.state);
  if (actionLabel) {
    return actionLabel;
  }

  if (!status?.php_installed) {
    return "Not installed";
  }

  const version = status.php_version?.trim();
  if (!version) {
    return "Installed";
  }

  return extractVersionNumber(version, /\bPHP\s+(\d+(?:\.\d+)+)\b/i) ?? version;
}

function getPHPServiceLabel(status: PHPRuntimeStatus | null) {
  if (!status) {
    return "Unavailable";
  }

  const actionLabel = getPHPActionLabel(status.state);
  if (actionLabel) {
    return actionLabel.replace("...", "");
  }

  if (status.service_running) {
    return "Running";
  }

  if (status.fpm_installed) {
    return "Installed";
  }

  return "Not installed";
}

function FieldError({ message }: { message?: string }) {
  if (!message) {
    return null;
  }

  return <p className="text-sm text-destructive">{message}</p>;
}

function formatValue(value?: string) {
  const trimmed = value?.trim();
  return trimmed ? trimmed : "Unavailable";
}

export function PHPSettingsDialog({
  open,
  onOpenChange,
  status,
  version,
  onStatusChange,
}: PHPSettingsDialogProps) {
  const [activeSection, setActiveSection] =
    useState<PHPSettingsSectionId>("runtime-settings");
  const [form, setForm] = useState<PHPSettingsFormState>(emptyForm);
  const [savedForm, setSavedForm] = useState<PHPSettingsFormState>(emptyForm);
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!open) {
      return;
    }

    const nextForm = toFormState(status?.settings);
    setActiveSection("runtime-settings");
    setForm(nextForm);
    setSavedForm(nextForm);
    setFieldErrors({});
    setError(null);
  }, [open, status?.version]);

  function handleFieldChange(field: keyof PHPSettingsFormState, value: string) {
    setForm((current) => ({ ...current, [field]: value }));
    setFieldErrors((current) => {
      if (!current[field]) {
        return current;
      }

      const nextErrors = { ...current };
      delete nextErrors[field];
      return nextErrors;
    });
  }

  async function handleSave() {
    if (!status || !version) {
      return;
    }

    setSaving(true);
    setError(null);
    setFieldErrors({});

    try {
      const nextStatus = await updatePHPSettings(form, version);
      onStatusChange(nextStatus);
      setSavedForm(form);
      toast.success(`PHP ${version} settings saved.`);
    } catch (saveError) {
      const phpError = saveError as PHPApiError;
      setFieldErrors(phpError.fieldErrors ?? {});
      setError(phpError.message || "PHP settings could not be saved.");
      toast.error(phpError.message || "PHP settings could not be saved.");
    } finally {
      setSaving(false);
    }
  }

  const runtimeLabel = version ? `PHP ${version}` : "PHP";
  const backgroundActionLabel = getPHPActionLabel(status?.state);
  const busy = saving || isPHPActionState(status?.state);
  const dirty = !sameFormState(form, savedForm);
  const saveDisabled = busy || !dirty || !status?.php_installed;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="h-[min(760px,calc(100vh-2rem))] grid-rows-[auto_minmax(0,1fr)_auto] gap-0 overflow-hidden p-5 pt-4 sm:max-w-5xl sm:p-6 sm:pt-5">
        <DialogHeader className="gap-0.5 pb-0 pe-12">
          <DialogTitle className="m-0 text-base leading-5">{runtimeLabel} settings</DialogTitle>
        </DialogHeader>

        <div className="mt-2 flex min-h-0 flex-col gap-4">
          <div className="flex flex-wrap items-center gap-x-3 gap-y-2 border-b border-[var(--app-border)] pb-3 text-sm">
            <div className="flex items-center gap-2">
              <span className="text-[var(--app-text-muted)]">Selected runtime</span>
              <Badge variant="outline" className="h-5 rounded-sm px-2 text-[11px]">
                {runtimeLabel}
              </Badge>
            </div>
            <div className="flex items-center gap-2">
              <span className="text-[var(--app-text-muted)]">Version</span>
              <Badge variant="outline" className="h-5 rounded-sm px-2 text-[11px]">
                {formatPHPVersion(status)}
              </Badge>
            </div>
            <div className="flex items-center gap-2">
              <span className="text-[var(--app-text-muted)]">Service</span>
              <Badge variant="outline" className="h-5 rounded-sm px-2 text-[11px]">
                {getPHPServiceLabel(status)}
              </Badge>
            </div>
          </div>

          {backgroundActionLabel ? (
            <div className="rounded-lg border border-[var(--app-border)] bg-[var(--app-surface-muted)] px-4 py-3 text-sm text-[var(--app-text-muted)]">
              <div className="flex items-center gap-2">
                <LoaderCircle className="h-4 w-4 animate-spin" />
                {backgroundActionLabel}
              </div>
            </div>
          ) : null}

          {!status?.php_installed ? (
            <div className="rounded-lg border border-[var(--app-danger)]/30 bg-[var(--app-danger-soft)] px-3 py-4 text-[13px] text-[var(--app-danger)]">
              Install the selected PHP runtime before saving settings.
            </div>
          ) : null}

          {error ? (
            <div className="rounded-lg border border-[var(--app-danger)]/30 bg-[var(--app-danger-soft)] px-3 py-4 text-[13px] text-[var(--app-danger)]">
              {error}
            </div>
          ) : null}

          <div className="grid min-h-0 flex-1 gap-4 md:grid-cols-[190px_minmax(0,1fr)]">
            <nav className="overflow-y-auto rounded-lg border border-[var(--app-border)] bg-[var(--app-bg-2)] p-2">
              <div className="space-y-1">
                {phpSettingsSections.map((section) => (
                  <button
                    key={section.id}
                    type="button"
                    className={cn(
                      "flex w-full flex-col items-start rounded-md px-3 py-2 text-left transition-colors",
                      activeSection === section.id
                        ? "bg-[var(--app-surface-muted)] text-[var(--app-text)]"
                        : "text-[var(--app-text-muted)] hover:bg-[var(--app-surface-muted)] hover:text-[var(--app-text)]"
                    )}
                    onClick={() => setActiveSection(section.id)}
                  >
                    <span className="text-sm font-medium">{section.label}</span>
                  </button>
                ))}
              </div>
            </nav>

            <div className="min-h-0 min-w-0 overflow-y-auto pr-1">
              {activeSection === "runtime-settings" ? (
                <section className="grid gap-4 md:grid-cols-2">
                  <div className="space-y-2">
                    <Label htmlFor="php_max_execution_time">Max execution time</Label>
                    <Input
                      id="php_max_execution_time"
                      value={form.max_execution_time}
                      onChange={(event) => handleFieldChange("max_execution_time", event.target.value)}
                      placeholder="60"
                      disabled={busy}
                      aria-invalid={fieldErrors.max_execution_time ? true : undefined}
                    />
                    <p className="text-xs text-[var(--app-text-muted)]">
                      Seconds. Use `0` for unlimited.
                    </p>
                    <FieldError message={fieldErrors.max_execution_time} />
                  </div>

                  <div className="space-y-2">
                    <Label htmlFor="php_max_input_time">Max input time</Label>
                    <Input
                      id="php_max_input_time"
                      value={form.max_input_time}
                      onChange={(event) => handleFieldChange("max_input_time", event.target.value)}
                      placeholder="60"
                      disabled={busy}
                      aria-invalid={fieldErrors.max_input_time ? true : undefined}
                    />
                    <p className="text-xs text-[var(--app-text-muted)]">
                      Seconds. Use `-1` for unlimited.
                    </p>
                    <FieldError message={fieldErrors.max_input_time} />
                  </div>

                  <div className="space-y-2">
                    <Label htmlFor="php_memory_limit">Memory limit</Label>
                    <Input
                      id="php_memory_limit"
                      value={form.memory_limit}
                      onChange={(event) => handleFieldChange("memory_limit", event.target.value)}
                      placeholder="256M"
                      disabled={busy}
                      aria-invalid={fieldErrors.memory_limit ? true : undefined}
                    />
                    <FieldError message={fieldErrors.memory_limit} />
                  </div>

                  <div className="space-y-2">
                    <Label htmlFor="php_post_max_size">Post max size</Label>
                    <Input
                      id="php_post_max_size"
                      value={form.post_max_size}
                      onChange={(event) => handleFieldChange("post_max_size", event.target.value)}
                      placeholder="64M"
                      disabled={busy}
                      aria-invalid={fieldErrors.post_max_size ? true : undefined}
                    />
                    <FieldError message={fieldErrors.post_max_size} />
                  </div>

                  <div className="space-y-2">
                    <Label htmlFor="php_file_uploads">File uploads</Label>
                    <Select
                      value={form.file_uploads}
                      onValueChange={(value) => handleFieldChange("file_uploads", value)}
                      disabled={busy}
                    >
                      <SelectTrigger
                        id="php_file_uploads"
                        className="w-full"
                        aria-invalid={fieldErrors.file_uploads ? true : undefined}
                      >
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="On">On</SelectItem>
                        <SelectItem value="Off">Off</SelectItem>
                      </SelectContent>
                    </Select>
                    <FieldError message={fieldErrors.file_uploads} />
                  </div>

                  <div className="space-y-2">
                    <Label htmlFor="php_upload_max_filesize">Upload max filesize</Label>
                    <Input
                      id="php_upload_max_filesize"
                      value={form.upload_max_filesize}
                      onChange={(event) => handleFieldChange("upload_max_filesize", event.target.value)}
                      placeholder="64M"
                      disabled={busy}
                      aria-invalid={fieldErrors.upload_max_filesize ? true : undefined}
                    />
                    <FieldError message={fieldErrors.upload_max_filesize} />
                  </div>

                  <div className="space-y-2">
                    <Label htmlFor="php_max_file_uploads">Max file uploads</Label>
                    <Input
                      id="php_max_file_uploads"
                      value={form.max_file_uploads}
                      onChange={(event) => handleFieldChange("max_file_uploads", event.target.value)}
                      placeholder="20"
                      disabled={busy}
                      aria-invalid={fieldErrors.max_file_uploads ? true : undefined}
                    />
                    <FieldError message={fieldErrors.max_file_uploads} />
                  </div>

                  <div className="space-y-2">
                    <Label htmlFor="php_default_socket_timeout">Default socket timeout</Label>
                    <Input
                      id="php_default_socket_timeout"
                      value={form.default_socket_timeout}
                      onChange={(event) => handleFieldChange("default_socket_timeout", event.target.value)}
                      placeholder="60"
                      disabled={busy}
                      aria-invalid={fieldErrors.default_socket_timeout ? true : undefined}
                    />
                    <FieldError message={fieldErrors.default_socket_timeout} />
                  </div>

                  <div className="space-y-2">
                    <Label htmlFor="php_display_errors">Display errors</Label>
                    <Select
                      value={form.display_errors}
                      onValueChange={(value) => handleFieldChange("display_errors", value)}
                      disabled={busy}
                    >
                      <SelectTrigger
                        id="php_display_errors"
                        className="w-full"
                        aria-invalid={fieldErrors.display_errors ? true : undefined}
                      >
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="On">On</SelectItem>
                        <SelectItem value="Off">Off</SelectItem>
                      </SelectContent>
                    </Select>
                    <FieldError message={fieldErrors.display_errors} />
                  </div>

                  <div className="space-y-2">
                    <Label htmlFor="php_error_reporting">Error reporting</Label>
                    <Select
                      value={form.error_reporting}
                      onValueChange={(value) => handleFieldChange("error_reporting", value)}
                      disabled={busy}
                    >
                      <SelectTrigger
                        id="php_error_reporting"
                        className="w-full"
                        aria-invalid={fieldErrors.error_reporting ? true : undefined}
                      >
                        <SelectValue placeholder="Select error reporting" />
                      </SelectTrigger>
                      <SelectContent>
                        {phpErrorReportingOptions.map((option) => (
                          <SelectItem key={option.value} value={option.value}>
                            {option.label}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                    <FieldError message={fieldErrors.error_reporting} />
                  </div>
                </section>
              ) : (
                <section className="space-y-4">
                  <div className="grid gap-3 sm:grid-cols-2">
                    <div className="rounded-lg border border-[var(--app-border)] px-4 py-3">
                      <div className="text-xs text-[var(--app-text-muted)]">Platform</div>
                      <div className="mt-1 break-all text-sm text-[var(--app-text)]">
                        {formatValue(status?.platform)}
                      </div>
                    </div>
                    <div className="rounded-lg border border-[var(--app-border)] px-4 py-3">
                      <div className="text-xs text-[var(--app-text-muted)]">PHP binary</div>
                      <div className="mt-1 break-all text-sm text-[var(--app-text)]">
                        {formatValue(status?.php_path)}
                      </div>
                    </div>
                    <div className="rounded-lg border border-[var(--app-border)] px-4 py-3">
                      <div className="text-xs text-[var(--app-text-muted)]">Managed config</div>
                      <div className="mt-1 break-all text-sm text-[var(--app-text)]">
                        {formatValue(status?.managed_config_file)}
                      </div>
                    </div>
                    <div className="rounded-lg border border-[var(--app-border)] px-4 py-3">
                      <div className="text-xs text-[var(--app-text-muted)]">Loaded config</div>
                      <div className="mt-1 break-all text-sm text-[var(--app-text)]">
                        {formatValue(status?.loaded_config_file)}
                      </div>
                    </div>
                  </div>

                  {status?.issues && status.issues.length > 0 ? (
                    <section className="rounded-lg border border-[var(--app-border)] bg-[var(--app-surface-muted)] px-4 py-4">
                      <h3 className="text-sm font-semibold text-[var(--app-text)]">Issues</h3>
                      <ul className="mt-3 space-y-2 text-sm text-[var(--app-text-muted)]">
                        {status.issues.map((issue) => (
                          <li key={issue}>{issue}</li>
                        ))}
                      </ul>
                    </section>
                  ) : (
                    <div className="rounded-lg border border-[var(--app-border)] bg-[var(--app-surface-muted)] px-4 py-4 text-sm text-[var(--app-text-muted)]">
                      No runtime issues reported for {runtimeLabel}.
                    </div>
                  )}
                </section>
              )}
            </div>
          </div>
        </div>

        <DialogFooter className="border-t border-[var(--app-border)] pt-4">
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)} disabled={busy}>
            Close
          </Button>
          <Button type="button" onClick={() => void handleSave()} disabled={saveDisabled}>
            {saving ? (
              <>
                <LoaderCircle className="h-4 w-4 animate-spin" />
                Saving
              </>
            ) : (
              "Save settings"
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
