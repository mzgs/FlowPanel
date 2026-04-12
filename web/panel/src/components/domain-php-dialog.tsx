import { type DomainRecord } from "@/api/domains";
import { type PHPRuntimeStatus, type PHPSettings } from "@/api/php";
import { LoaderCircle } from "@/components/icons/tabler-icons";
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
import { Textarea } from "@/components/ui/textarea";

const phpErrorReportingOptions = [
  { value: "E_ALL", label: "E_ALL" },
  { value: "E_ALL & ~E_NOTICE", label: "E_ALL & ~E_NOTICE" },
  { value: "E_ALL & ~E_DEPRECATED", label: "E_ALL & ~E_DEPRECATED" },
  {
    value: "E_ALL & ~E_NOTICE & ~E_DEPRECATED",
    label: "E_ALL & ~E_NOTICE & ~E_DEPRECATED",
  },
] as const;

type DomainPHPDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  domain: DomainRecord;
  status: PHPRuntimeStatus | null;
  availableVersions: string[];
  selectedVersion: string;
  form: PHPSettings;
  fieldErrors: Record<string, string>;
  loading: boolean;
  saving: boolean;
  error: string | null;
  dirty: boolean;
  runningAction: "install" | "start" | null;
  onVersionChange: (value: string) => void;
  onFieldChange: (field: keyof PHPSettings, value: string) => void;
  onInstall: () => void;
  onStart: () => void;
  onSave: () => void;
};

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

function FieldError({ message }: { message?: string }) {
  if (!message) {
    return null;
  }

  return <p className="text-sm text-destructive">{message}</p>;
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

export function DomainPHPDialog({
  open,
  onOpenChange,
  domain,
  status,
  availableVersions,
  selectedVersion,
  form,
  fieldErrors,
  loading,
  saving,
  error,
  dirty,
  runningAction,
  onVersionChange,
  onFieldChange,
  onInstall,
  onStart,
  onSave,
}: DomainPHPDialogProps) {
  const backgroundActionLabel = getPHPActionLabel(status?.state);
  const busy = saving || runningAction !== null || isPHPActionState(status?.state);
  const installDisabled = busy;
  const startDisabled = busy || !status?.start_available;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="gap-4 sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>{domain.hostname} PHP</DialogTitle>
        </DialogHeader>

        <section className="grid gap-3 border-b border-[var(--app-border)] pb-4 sm:grid-cols-3">
          <div className="sm:col-span-3">
            <Label htmlFor="domain_php_version">PHP runtime</Label>
            <Select
              value={selectedVersion}
              onValueChange={onVersionChange}
              disabled={busy || availableVersions.length === 0}
            >
              <SelectTrigger
                id="domain_php_version"
                className="mt-2 w-full"
                aria-invalid={fieldErrors.php_version ? true : undefined}
              >
                <SelectValue placeholder="Select a PHP version" />
              </SelectTrigger>
              <SelectContent>
                {availableVersions.map((version) => (
                  <SelectItem key={version} value={version}>
                    PHP {version}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <p className="mt-2 text-xs text-[var(--app-text-muted)]">
              The selected runtime is assigned to this domain. Per-domain PHP settings stay the same across versions.
            </p>
            <FieldError message={fieldErrors.php_version} />
          </div>
          <div className="rounded-lg border border-[var(--app-border)] bg-[var(--app-surface-muted)] px-4 py-3">
            <div className="text-xs text-[var(--app-text-muted)]">PHP version</div>
            <div className="mt-1 text-sm font-medium text-[var(--app-text)]">
              {formatPHPVersion(status)}
            </div>
          </div>
          <div className="rounded-lg border border-[var(--app-border)] bg-[var(--app-surface-muted)] px-4 py-3">
            <div className="text-xs text-[var(--app-text-muted)]">PHP-FPM</div>
            <div className="mt-1 text-sm font-medium text-[var(--app-text)]">
              {getPHPServiceLabel(status)}
            </div>
          </div>
          <div className="rounded-lg border border-[var(--app-border)] bg-[var(--app-surface-muted)] px-4 py-3">
            <div className="text-xs text-[var(--app-text-muted)]">Platform</div>
            <div className="mt-1 text-sm font-medium text-[var(--app-text)]">
              {status?.platform || "Unknown"}
            </div>
          </div>
        </section>

        <div className="flex flex-wrap items-center gap-2">
          {backgroundActionLabel ? (
            <Button type="button" variant="outline" disabled>
              <LoaderCircle className="h-4 w-4 animate-spin" />
              {backgroundActionLabel}
            </Button>
          ) : null}
          {status?.install_available ? (
            <Button type="button" onClick={onInstall} disabled={installDisabled}>
              {runningAction === "install" ? (
                <LoaderCircle className="h-4 w-4 animate-spin" />
              ) : null}
              {status.install_label ?? "Install PHP"}
            </Button>
          ) : null}
          {status?.start_available ? (
            <Button type="button" onClick={onStart} disabled={startDisabled}>
              {runningAction === "start" ? (
                <LoaderCircle className="h-4 w-4 animate-spin" />
              ) : null}
              {status.start_label ?? "Start PHP-FPM"}
            </Button>
            ) : null}
          </div>

        <section className="grid gap-4 border-b border-[var(--app-border)] pb-4 md:grid-cols-2">
          <div className="space-y-2">
            <Label htmlFor="php_max_execution_time">Max execution time</Label>
            <Input
              id="php_max_execution_time"
              value={form.max_execution_time ?? ""}
              onChange={(event) => onFieldChange("max_execution_time", event.target.value)}
              placeholder="60"
              disabled={busy}
              aria-invalid={fieldErrors.max_execution_time ? true : undefined}
            />
            <p className="text-xs text-[var(--app-text-muted)]">Seconds. Use `0` for unlimited.</p>
            <FieldError message={fieldErrors.max_execution_time} />
          </div>

          <div className="space-y-2">
            <Label htmlFor="php_max_input_time">Max input time</Label>
            <Input
              id="php_max_input_time"
              value={form.max_input_time ?? ""}
              onChange={(event) => onFieldChange("max_input_time", event.target.value)}
              placeholder="60"
              disabled={busy}
              aria-invalid={fieldErrors.max_input_time ? true : undefined}
            />
            <p className="text-xs text-[var(--app-text-muted)]">Seconds. Use `-1` for unlimited.</p>
            <FieldError message={fieldErrors.max_input_time} />
          </div>

          <div className="space-y-2">
            <Label htmlFor="php_memory_limit">Memory limit</Label>
            <Input
              id="php_memory_limit"
              value={form.memory_limit ?? ""}
              onChange={(event) => onFieldChange("memory_limit", event.target.value)}
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
              value={form.post_max_size ?? ""}
              onChange={(event) => onFieldChange("post_max_size", event.target.value)}
              placeholder="64M"
              disabled={busy}
              aria-invalid={fieldErrors.post_max_size ? true : undefined}
            />
            <FieldError message={fieldErrors.post_max_size} />
          </div>

          <div className="space-y-2">
            <Label htmlFor="php_file_uploads">File uploads</Label>
            <Select
              value={form.file_uploads ?? "On"}
              onValueChange={(value) => onFieldChange("file_uploads", value)}
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
              value={form.upload_max_filesize ?? ""}
              onChange={(event) => onFieldChange("upload_max_filesize", event.target.value)}
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
              value={form.max_file_uploads ?? ""}
              onChange={(event) => onFieldChange("max_file_uploads", event.target.value)}
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
              value={form.default_socket_timeout ?? ""}
              onChange={(event) => onFieldChange("default_socket_timeout", event.target.value)}
              placeholder="60"
              disabled={busy}
              aria-invalid={fieldErrors.default_socket_timeout ? true : undefined}
            />
            <FieldError message={fieldErrors.default_socket_timeout} />
          </div>

          <div className="space-y-2">
            <Label htmlFor="php_display_errors">Display errors</Label>
            <Select
              value={form.display_errors ?? "Off"}
              onValueChange={(value) => onFieldChange("display_errors", value)}
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
              value={form.error_reporting ?? ""}
              onValueChange={(value) => onFieldChange("error_reporting", value)}
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

          <div className="space-y-2 md:col-span-2">
            <Label htmlFor="php_disable_functions">Disabled functions</Label>
            <Textarea
              id="php_disable_functions"
              value={form.disable_functions ?? ""}
              onChange={(event) => onFieldChange("disable_functions", event.target.value)}
              placeholder="exec,shell_exec,system"
              disabled={busy}
              aria-invalid={fieldErrors.disable_functions ? true : undefined}
              className="min-h-24"
            />
            <p className="text-xs text-[var(--app-text-muted)]">
              Comma-separated PHP function names to disable.
            </p>
            <FieldError message={fieldErrors.disable_functions} />
          </div>
        </section>

        {loading ? (
          <div className="rounded-lg border border-[var(--app-border)] bg-[var(--app-surface-muted)] px-4 py-6 text-sm text-[var(--app-text-muted)]">
            <div className="flex items-center gap-2">
              <LoaderCircle className="h-4 w-4 animate-spin" />
              Loading PHP settings...
            </div>
          </div>
        ) : (
          <>
            {error ? (
              <div className="rounded-lg border border-[var(--app-danger)]/30 bg-[var(--app-danger-soft)] px-3 py-4 text-[13px] text-[var(--app-danger)]">
                {error}
              </div>
            ) : null}

            {(status?.managed_config_file || status?.loaded_config_file) ? (
              <section className="grid gap-3 lg:grid-cols-2">
                <div>
                  <div className="text-xs text-[var(--app-text-muted)]">Managed config</div>
                  <div className="mt-1 break-all text-[11px] text-[var(--app-text)]">
                    {status?.managed_config_file || "Unavailable"}
                  </div>
                </div>
                <div>
                  <div className="text-xs text-[var(--app-text-muted)]">Loaded config</div>
                  <div className="mt-1 break-all text-[11px] text-[var(--app-text)]">
                    {status?.loaded_config_file || "Unavailable"}
                  </div>
                </div>
              </section>
            ) : null}

            {status?.issues && status.issues.length > 0 ? (
              <section className="rounded-lg border border-[var(--app-border)] bg-[var(--app-surface-muted)] px-4 py-4">
                <h3 className="text-sm font-semibold text-[var(--app-text)]">Issues</h3>
                <ul className="mt-3 space-y-2 text-sm text-[var(--app-text-muted)]">
                  {status.issues.map((issue) => (
                    <li key={issue}>{issue}</li>
                  ))}
                </ul>
              </section>
            ) : null}
          </>
        )}

        <DialogFooter className="border-t border-[var(--app-border)] pt-4">
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)} disabled={busy}>
            Close
          </Button>
          <Button type="button" onClick={onSave} disabled={busy || !dirty}>
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
