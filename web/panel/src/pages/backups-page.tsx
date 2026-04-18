import { Link } from "@tanstack/react-router";
import { useEffect, useRef, useState } from "react";
import {
  createScheduledBackup,
  createBackup,
  deleteScheduledBackup,
  deleteBackup,
  fetchBackups,
  fetchScheduledBackups,
  getBackupDownloadUrl,
  importBackup,
  restoreBackup,
  type BackupRecord,
  type CreateBackupInput,
  type ScheduledBackupRecord,
} from "@/api/backups";
import { fetchSettings, type PanelSettings } from "@/api/settings";
import {
  Clock,
  Database,
  Download,
  FolderOpen,
  HardDrive,
  LoaderCircle,
  RotateCcw,
  Trash2,
  Upload,
} from "@/components/icons/tabler-icons";
import { ActionFeedbackIcon } from "@/components/action-feedback-icon";
import { ActionConfirmDialog } from "@/components/action-confirm-dialog";
import { FieldError } from "@/components/field-error";
import { PageHeader } from "@/components/page-header";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { formatBytes, formatDateTime } from "@/lib/format";
import { getErrorMessage } from "@/lib/utils";
import { toast } from "sonner";

const initialScope: CreateBackupInput = {
  include_panel_data: true,
  include_sites: true,
  include_databases: true,
  location: "local",
};
type ScheduleFormState = {
  name: string;
  schedule: string;
  include_panel_data: boolean;
  include_sites: boolean;
  include_databases: boolean;
  location: "local" | "google_drive";
};

const initialScheduleForm: ScheduleFormState = {
  name: "Nightly backup",
  schedule: "0 3 * * *",
  include_panel_data: true,
  include_sites: true,
  include_databases: true,
  location: "local",
};
const backupArchiveExtension = ".tar.gz";

function isBackupArchiveFileName(name: string) {
  const normalizedName = name.trim().toLowerCase();
  return normalizedName.endsWith(backupArchiveExtension);
}

function formatScheduledBackupScope(record: ScheduledBackupRecord) {
  const parts: string[] = [];
  if (record.include_panel_data) {
    parts.push("Panel data");
  }
  if (record.include_sites) {
    parts.push("Site files");
  }
  if (record.include_databases) {
    parts.push("Database dumps");
  }

  return parts.join(", ");
}

function formatBackupLocation(location: BackupRecord["location"]) {
  return location === "google_drive" ? "Google Drive" : "Local";
}

function getBackupKey(record: Pick<BackupRecord, "id" | "location">) {
  return `${record.location}:${record.id}`;
}

export function BackupsPage() {
  const [backups, setBackups] = useState<BackupRecord[]>([]);
  const [settings, setSettings] = useState<PanelSettings | null>(null);
  const [scheduledBackups, setScheduledBackups] = useState<
    ScheduledBackupRecord[]
  >([]);
  const [loading, setLoading] = useState(true);
  const [scheduledLoading, setScheduledLoading] = useState(true);
  const [creating, setCreating] = useState(false);
  const [scheduling, setScheduling] = useState(false);
  const [deletingBackupKey, setDeletingBackupKey] = useState<string | null>(
    null,
  );
  const [deletingScheduleId, setDeletingScheduleId] = useState<string | null>(
    null,
  );
  const [confirmDeleteRecord, setConfirmDeleteRecord] =
    useState<BackupRecord | null>(null);
  const [confirmDeleteSchedule, setConfirmDeleteSchedule] =
    useState<ScheduledBackupRecord | null>(null);
  const [confirmRestoreRecord, setConfirmRestoreRecord] =
    useState<BackupRecord | null>(null);
  const [restoringBackupKey, setRestoringBackupKey] = useState<string | null>(
    null,
  );
  const [restoredBackupKey, setRestoredBackupKey] = useState<string | null>(
    null,
  );
  const [importing, setImporting] = useState(false);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [scheduledLoadError, setScheduledLoadError] = useState<string | null>(
    null,
  );
  const [createDialogError, setCreateDialogError] = useState<string | null>(
    null,
  );
  const [createDialogOpen, setCreateDialogOpen] = useState(false);
  const [scheduleDialogOpen, setScheduleDialogOpen] = useState(false);
  const [scope, setScope] = useState<CreateBackupInput>(initialScope);
  const [scheduleForm, setScheduleForm] =
    useState<ScheduleFormState>(initialScheduleForm);
  const [scheduleFieldErrors, setScheduleFieldErrors] = useState<
    Record<string, string>
  >({});
  const [schedulerEnabled, setSchedulerEnabled] = useState(false);
  const [schedulerStarted, setSchedulerStarted] = useState(false);
  const restoredTimeoutRef = useRef<number | null>(null);
  const importInputRef = useRef<HTMLInputElement | null>(null);

  const hasSelectedScope =
    scope.include_panel_data || scope.include_sites || scope.include_databases;
  const hasScheduledScope =
    scheduleForm.include_panel_data ||
    scheduleForm.include_sites ||
    scheduleForm.include_databases;
  const googleDriveAvailable = settings?.google_drive_available ?? false;
  const googleDriveConnected = settings?.google_drive_connected ?? false;
  const canUseCreateLocation =
    scope.location === "local" ||
    (googleDriveAvailable && googleDriveConnected);
  const canUseScheduleLocation =
    scheduleForm.location === "local" ||
    (googleDriveAvailable && googleDriveConnected);
  const panelDataCheckboxId = "backup-scope-panel-data";
  const siteFilesCheckboxId = "backup-scope-site-files";
  const databaseDumpsCheckboxId = "backup-scope-database-dumps";
  const scheduleNameInputId = "scheduled-backup-name";
  const scheduleInputId = "scheduled-backup-schedule";
  const schedulePanelDataCheckboxId = "scheduled-backup-scope-panel-data";
  const scheduleSiteFilesCheckboxId = "scheduled-backup-scope-site-files";
  const scheduleDatabaseDumpsCheckboxId =
    "scheduled-backup-scope-database-dumps";

  async function loadBackups() {
    try {
      const payload = await fetchBackups();
      setBackups(payload.backups);
      setLoadError(null);
    } catch (error) {
      setLoadError(getErrorMessage(error, "Failed to load backups."));
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void loadBackups();
  }, []);

  useEffect(() => {
    let active = true;

    async function loadSettings() {
      try {
        const nextSettings = await fetchSettings();
        if (active) {
          setSettings(nextSettings);
        }
      } catch {
        if (active) {
          setSettings(null);
        }
      }
    }

    void loadSettings();

    return () => {
      active = false;
    };
  }, []);

  async function loadScheduledBackups() {
    try {
      const payload = await fetchScheduledBackups();
      setScheduledBackups(payload.schedules);
      setSchedulerEnabled(payload.enabled);
      setSchedulerStarted(payload.started);
      setScheduledLoadError(null);
    } catch (error) {
      setScheduledLoadError(
        getErrorMessage(error, "Failed to load scheduled backups."),
      );
    } finally {
      setScheduledLoading(false);
    }
  }

  useEffect(() => {
    void loadScheduledBackups();
  }, []);

  useEffect(() => {
    return () => {
      if (restoredTimeoutRef.current !== null) {
        window.clearTimeout(restoredTimeoutRef.current);
      }
    };
  }, []);

  async function handleCreateBackup() {
    if (!hasSelectedScope) {
      setCreateDialogError("Select at least one backup source.");
      return;
    }
    if (!canUseCreateLocation) {
      setCreateDialogError(
        "Connect Google Drive in Settings before using that backup location.",
      );
      return;
    }

    setCreateDialogError(null);
    setCreating(true);

    try {
      const record = await createBackup(scope);
      setBackups((current) => [
        record,
        ...current.filter(
          (item) => getBackupKey(item) !== getBackupKey(record),
        ),
      ]);
      setLoadError(null);
      setCreateDialogError(null);
      setCreateDialogOpen(false);
      toast.success(
        record.location === "google_drive"
          ? `Uploaded backup ${record.name} to Google Drive.`
          : `Created backup ${record.name}.`,
      );
    } catch (error) {
      setCreateDialogError(getErrorMessage(error, "Failed to create backup."));
    } finally {
      setCreating(false);
    }
  }

  async function handleCreateScheduledBackup() {
    if (!hasScheduledScope) {
      setScheduleFieldErrors({ scope: "Select at least one backup source." });
      return;
    }
    if (!canUseScheduleLocation) {
      setScheduleFieldErrors({
        location:
          "Connect Google Drive in Settings before using that backup location.",
      });
      return;
    }

    setScheduling(true);
    setScheduleFieldErrors({});

    try {
      const record = await createScheduledBackup(scheduleForm);
      setScheduledBackups((current) => [
        record,
        ...current.filter((item) => item.id !== record.id),
      ]);
      setScheduledLoadError(null);
      setScheduleDialogOpen(false);
      setScheduleForm(initialScheduleForm);
      toast.success(
        `Scheduled ${formatBackupLocation(record.location).toLowerCase()} backup ${record.name}.`,
      );
    } catch (error) {
      const backupError = error as Error & {
        fieldErrors?: Record<string, string>;
      };
      setScheduleFieldErrors(backupError.fieldErrors ?? {});
      toast.error(getErrorMessage(error, "Failed to schedule backup."));
    } finally {
      setScheduling(false);
    }
  }

  function handleDeleteBackup(record: BackupRecord) {
    if (deletingBackupKey !== null) {
      return;
    }

    setConfirmDeleteRecord(record);
  }

  function handleDeleteScheduledBackup(schedule: ScheduledBackupRecord) {
    if (deletingScheduleId !== null) {
      return;
    }

    setConfirmDeleteSchedule(schedule);
  }

  async function confirmDeleteBackup() {
    if (!confirmDeleteRecord) {
      return;
    }

    const record = confirmDeleteRecord;
    const backupKey = getBackupKey(record);

    setDeletingBackupKey(backupKey);

    try {
      await deleteBackup(record.id, record.location);
      setBackups((current) =>
        current.filter((item) => getBackupKey(item) !== backupKey),
      );
      toast.success(`Deleted backup ${record.name}.`);
    } catch (error) {
      toast.error(getErrorMessage(error, "Failed to delete backup."));
    } finally {
      setDeletingBackupKey(null);
      setConfirmDeleteRecord((current) =>
        current && getBackupKey(current) === backupKey ? null : current,
      );
    }
  }

  async function confirmDeleteScheduledBackup() {
    if (!confirmDeleteSchedule) {
      return;
    }

    const schedule = confirmDeleteSchedule;

    setDeletingScheduleId(schedule.id);

    try {
      await deleteScheduledBackup(schedule.id);
      setScheduledBackups((current) =>
        current.filter((item) => item.id !== schedule.id),
      );
      toast.success(`Deleted scheduled backup ${schedule.name}.`);
    } catch (error) {
      toast.error(getErrorMessage(error, "Failed to delete scheduled backup."));
    } finally {
      setDeletingScheduleId(null);
      setConfirmDeleteSchedule((current) =>
        current?.id === schedule.id ? null : current,
      );
    }
  }

  function handleRestoreBackup(record: BackupRecord) {
    if (restoringBackupKey !== null || deletingBackupKey !== null) {
      return;
    }

    setConfirmRestoreRecord(record);
  }

  async function confirmRestoreBackup() {
    if (!confirmRestoreRecord) {
      return;
    }

    const record = confirmRestoreRecord;
    const backupKey = getBackupKey(record);

    setRestoringBackupKey(backupKey);
    setRestoredBackupKey(null);

    try {
      const result = await restoreBackup(record.id, record.location);
      if (restoredTimeoutRef.current !== null) {
        window.clearTimeout(restoredTimeoutRef.current);
      }
      setRestoredBackupKey(backupKey);
      restoredTimeoutRef.current = window.setTimeout(() => {
        setRestoredBackupKey((current) =>
          current === backupKey ? null : current,
        );
        restoredTimeoutRef.current = null;
      }, 1500);
      if (result.restored_panel_database) {
        window.setTimeout(() => {
          window.location.reload();
        }, 700);
      }
    } catch (error) {
      toast.error(getErrorMessage(error, "Failed to restore backup."));
    } finally {
      setRestoringBackupKey(null);
      setConfirmRestoreRecord((current) =>
        current && getBackupKey(current) === backupKey ? null : current,
      );
    }
  }

  function handleOpenImportDialog() {
    if (importing) {
      return;
    }

    importInputRef.current?.click();
  }

  async function handleImportSelection(files: FileList | null) {
    const file = files?.[0];
    if (!file) {
      return;
    }
    if (!isBackupArchiveFileName(file.name)) {
      toast.error("Select a FlowPanel backup archive ending in .tar.gz.");
      return;
    }

    setImporting(true);

    try {
      const record = await importBackup(file);
      setBackups((current) => [
        record,
        ...current.filter(
          (item) => getBackupKey(item) !== getBackupKey(record),
        ),
      ]);
      setLoadError(null);
      toast.success(`Imported backup ${record.name}.`);
    } catch (error) {
      toast.error(getErrorMessage(error, "Failed to import backup."));
    } finally {
      setImporting(false);
    }
  }

  return (
    <>
      <PageHeader
        title="Backups"
        actions={
          <>
            <Button
              type="button"
              variant="outline"
              onClick={handleOpenImportDialog}
              disabled={importing}
            >
              {importing ? (
                <LoaderCircle className="h-4 w-4 animate-spin" />
              ) : (
                <Upload className="h-4 w-4" />
              )}
              Import backup
            </Button>
            <Button
              type="button"
              variant="outline"
              onClick={() => setScheduleDialogOpen(true)}
            >
              <Clock className="h-4 w-4" />
              Schedule backup
            </Button>
            <Button type="button" onClick={() => setCreateDialogOpen(true)}>
              <HardDrive className="h-4 w-4" />
              Create backup
            </Button>
          </>
        }
      />

      <input
        ref={importInputRef}
        type="file"
        accept={`${backupArchiveExtension},application/gzip,application/x-gzip,application/octet-stream`}
        className="hidden"
        onChange={(event) => {
          void handleImportSelection(event.target.files);
          event.target.value = "";
        }}
      />

      <Dialog
        open={createDialogOpen}
        onOpenChange={(open) => {
          setCreateDialogOpen(open);
          if (!open) {
            setCreateDialogError(null);
          }
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Create backup</DialogTitle>
            <DialogDescription>
              Select which FlowPanel-managed sources should be included and
              where the archive should be stored.
            </DialogDescription>
          </DialogHeader>

          <div className="grid gap-5">
            {createDialogError ? (
              <div className="rounded-lg border border-[var(--app-danger)]/30 bg-[var(--app-danger)]/8 px-3 py-2 text-sm text-[var(--app-danger)]">
                {createDialogError}
              </div>
            ) : null}

            <div className="grid gap-2">
              <Label>Backup location</Label>
              <RadioGroup
                value={scope.location ?? "local"}
                onValueChange={(value) =>
                  setScope((current) => {
                    if (createDialogError) {
                      setCreateDialogError(null);
                    }
                    return {
                      ...current,
                      location: value as "local" | "google_drive",
                    };
                  })
                }
              >
                <div className="overflow-hidden rounded-lg border border-[var(--app-border)]">
                  <label className="flex cursor-pointer items-center gap-3 px-3 py-3">
                    <RadioGroupItem value="local" id="backup-location-local" />
                    <Label
                      htmlFor="backup-location-local"
                      className="cursor-pointer text-sm text-foreground"
                    >
                      Local
                    </Label>
                  </label>

                  <label
                    className={`flex items-center gap-3 border-t px-3 py-3 ${
                      googleDriveAvailable
                        ? "cursor-pointer border-[var(--app-border)]"
                        : "cursor-not-allowed border-[var(--app-border)] opacity-60"
                    }`}
                  >
                    <RadioGroupItem
                      value="google_drive"
                      id="backup-location-google-drive"
                      disabled={!googleDriveAvailable}
                    />
                    <Label
                      htmlFor="backup-location-google-drive"
                      className="cursor-pointer text-sm text-foreground"
                    >
                      Google Drive
                    </Label>
                  </label>
                </div>
              </RadioGroup>
            </div>

            <div className="grid gap-3">
              <label
                htmlFor={panelDataCheckboxId}
                className="flex cursor-pointer items-start gap-3 rounded-lg border border-[var(--app-border)] px-3 py-3"
              >
                <Checkbox
                  id={panelDataCheckboxId}
                  checked={scope.include_panel_data}
                  onCheckedChange={(checked) =>
                    setScope((current) => {
                      if (createDialogError) {
                        setCreateDialogError(null);
                      }
                      return {
                        ...current,
                        include_panel_data: checked === true,
                      };
                    })
                  }
                  className="mt-0.5"
                />
                <div className="min-w-0">
                  <Label
                    htmlFor={panelDataCheckboxId}
                    className="cursor-pointer text-sm text-foreground"
                  >
                    <HardDrive className="h-4 w-4" />
                    Panel data
                  </Label>
                </div>
              </label>

              <label
                htmlFor={siteFilesCheckboxId}
                className="flex cursor-pointer items-start gap-3 rounded-lg border border-[var(--app-border)] px-3 py-3"
              >
                <Checkbox
                  id={siteFilesCheckboxId}
                  checked={scope.include_sites}
                  onCheckedChange={(checked) =>
                    setScope((current) => {
                      if (createDialogError) {
                        setCreateDialogError(null);
                      }
                      return {
                        ...current,
                        include_sites: checked === true,
                      };
                    })
                  }
                  className="mt-0.5"
                />
                <div className="min-w-0">
                  <Label
                    htmlFor={siteFilesCheckboxId}
                    className="cursor-pointer text-sm text-foreground"
                  >
                    <FolderOpen className="h-4 w-4" />
                    Site files
                  </Label>
                </div>
              </label>

              <label
                htmlFor={databaseDumpsCheckboxId}
                className="flex cursor-pointer items-start gap-3 rounded-lg border border-[var(--app-border)] px-3 py-3"
              >
                <Checkbox
                  id={databaseDumpsCheckboxId}
                  checked={scope.include_databases}
                  onCheckedChange={(checked) =>
                    setScope((current) => {
                      if (createDialogError) {
                        setCreateDialogError(null);
                      }
                      return {
                        ...current,
                        include_databases: checked === true,
                      };
                    })
                  }
                  className="mt-0.5"
                />
                <div className="min-w-0">
                  <Label
                    htmlFor={databaseDumpsCheckboxId}
                    className="cursor-pointer text-sm text-foreground"
                  >
                    <Database className="h-4 w-4" />
                    Database dumps
                  </Label>
                </div>
              </label>
            </div>
          </div>

          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              onClick={() => setCreateDialogOpen(false)}
              disabled={creating}
            >
              Cancel
            </Button>
            <Button
              type="button"
              onClick={() => void handleCreateBackup()}
              disabled={creating || !hasSelectedScope || !canUseCreateLocation}
            >
              {creating ? (
                <LoaderCircle className="h-4 w-4 animate-spin" />
              ) : (
                <HardDrive className="h-4 w-4" />
              )}
              {scope.location === "google_drive"
                ? "Upload backup"
                : "Create backup"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog
        open={scheduleDialogOpen}
        onOpenChange={(open) => {
          setScheduleDialogOpen(open);
          if (!open) {
            setScheduleFieldErrors({});
            setScheduleForm(initialScheduleForm);
          }
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Schedule backup</DialogTitle>
          </DialogHeader>

          <div className="grid gap-5">
            <div className="grid gap-2">
              <Label htmlFor={scheduleNameInputId}>Name</Label>
              <Input
                id={scheduleNameInputId}
                value={scheduleForm.name}
                onChange={(event) =>
                  setScheduleForm((current) => ({
                    ...current,
                    name: event.target.value,
                  }))
                }
                aria-invalid={scheduleFieldErrors.name ? true : undefined}
              />
              <FieldError message={scheduleFieldErrors.name} />
            </div>

            <div className="grid gap-2">
              <Label htmlFor={scheduleInputId}>Schedule</Label>
              <Input
                id={scheduleInputId}
                value={scheduleForm.schedule}
                onChange={(event) =>
                  setScheduleForm((current) => ({
                    ...current,
                    schedule: event.target.value,
                  }))
                }
                placeholder="0 3 * * *"
                spellCheck={false}
                aria-invalid={scheduleFieldErrors.schedule ? true : undefined}
              />
              <p className="text-sm text-muted-foreground">
                Uses a standard 5-field cron expression or a descriptor like{" "}
                <span className="font-medium text-foreground">@daily</span>.
              </p>
              <FieldError message={scheduleFieldErrors.schedule} />
            </div>

            <div className="grid gap-2">
              <Label>Backup location</Label>
              <RadioGroup
                value={scheduleForm.location}
                onValueChange={(value) =>
                  setScheduleForm((current) => ({
                    ...current,
                    location: value as "local" | "google_drive",
                  }))
                }
              >
                <label className="flex cursor-pointer items-start gap-3 rounded-lg border border-[var(--app-border)] px-3 py-3">
                  <RadioGroupItem
                    value="local"
                    id="scheduled-backup-location-local"
                    className="mt-1"
                  />
                  <div className="space-y-1">
                    <Label
                      htmlFor="scheduled-backup-location-local"
                      className="cursor-pointer text-sm text-foreground"
                    >
                      Local
                    </Label>
                    <p className="text-sm text-muted-foreground">
                      Save each scheduled archive on this server.
                    </p>
                  </div>
                </label>

                <label
                  className={`flex items-start gap-3 rounded-lg border px-3 py-3 ${
                    googleDriveAvailable
                      ? "cursor-pointer border-[var(--app-border)]"
                      : "cursor-not-allowed border-[var(--app-border)] opacity-60"
                  }`}
                >
                  <RadioGroupItem
                    value="google_drive"
                    id="scheduled-backup-location-google-drive"
                    className="mt-1"
                    disabled={!googleDriveAvailable}
                  />
                  <div className="space-y-1">
                    <Label
                      htmlFor="scheduled-backup-location-google-drive"
                      className="cursor-pointer text-sm text-foreground"
                    >
                      Google Drive
                    </Label>
                    <p className="text-sm text-muted-foreground">
                      Upload each scheduled archive to the connected Drive
                      account.
                    </p>
                  </div>
                </label>
              </RadioGroup>
              <FieldError message={scheduleFieldErrors.location} />
            </div>

            <div className="grid gap-3">
              <label
                htmlFor={schedulePanelDataCheckboxId}
                className="flex cursor-pointer items-start gap-3 rounded-lg border border-[var(--app-border)] px-3 py-3"
              >
                <Checkbox
                  id={schedulePanelDataCheckboxId}
                  checked={scheduleForm.include_panel_data}
                  onCheckedChange={(checked) =>
                    setScheduleForm((current) => ({
                      ...current,
                      include_panel_data: checked === true,
                    }))
                  }
                  className="mt-0.5"
                />
                <div className="min-w-0">
                  <Label
                    htmlFor={schedulePanelDataCheckboxId}
                    className="cursor-pointer text-sm text-foreground"
                  >
                    <HardDrive className="h-4 w-4" />
                    Panel data
                  </Label>
                </div>
              </label>

              <label
                htmlFor={scheduleSiteFilesCheckboxId}
                className="flex cursor-pointer items-start gap-3 rounded-lg border border-[var(--app-border)] px-3 py-3"
              >
                <Checkbox
                  id={scheduleSiteFilesCheckboxId}
                  checked={scheduleForm.include_sites}
                  onCheckedChange={(checked) =>
                    setScheduleForm((current) => ({
                      ...current,
                      include_sites: checked === true,
                    }))
                  }
                  className="mt-0.5"
                />
                <div className="min-w-0">
                  <Label
                    htmlFor={scheduleSiteFilesCheckboxId}
                    className="cursor-pointer text-sm text-foreground"
                  >
                    <FolderOpen className="h-4 w-4" />
                    Site files
                  </Label>
                </div>
              </label>

              <label
                htmlFor={scheduleDatabaseDumpsCheckboxId}
                className="flex cursor-pointer items-start gap-3 rounded-lg border border-[var(--app-border)] px-3 py-3"
              >
                <Checkbox
                  id={scheduleDatabaseDumpsCheckboxId}
                  checked={scheduleForm.include_databases}
                  onCheckedChange={(checked) =>
                    setScheduleForm((current) => ({
                      ...current,
                      include_databases: checked === true,
                    }))
                  }
                  className="mt-0.5"
                />
                <div className="min-w-0">
                  <Label
                    htmlFor={scheduleDatabaseDumpsCheckboxId}
                    className="cursor-pointer text-sm text-foreground"
                  >
                    <Database className="h-4 w-4" />
                    Database dumps
                  </Label>
                </div>
              </label>
            </div>

            <FieldError message={scheduleFieldErrors.scope} />
          </div>

          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              onClick={() => setScheduleDialogOpen(false)}
              disabled={scheduling}
            >
              Cancel
            </Button>
            <Button
              type="button"
              onClick={() => void handleCreateScheduledBackup()}
              disabled={
                scheduling || !hasScheduledScope || !canUseScheduleLocation
              }
            >
              {scheduling ? (
                <LoaderCircle className="h-4 w-4 animate-spin" />
              ) : (
                <Clock className="h-4 w-4" />
              )}
              {scheduleForm.location === "google_drive"
                ? "Schedule upload"
                : "Schedule backup"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <ActionConfirmDialog
        open={confirmRestoreRecord !== null}
        onOpenChange={(open) => {
          if (!open && restoringBackupKey === null) {
            setConfirmRestoreRecord(null);
          }
        }}
        title="Restore backup"
        desc={
          confirmRestoreRecord
            ? `Restore backup "${confirmRestoreRecord.name}"? This overwrites the matching panel files, site files, and databases contained in the archive.`
            : "Restore this backup?"
        }
        confirmText="Restore backup"
        isLoading={
          confirmRestoreRecord !== null &&
          restoringBackupKey === getBackupKey(confirmRestoreRecord)
        }
        handleConfirm={() => {
          void confirmRestoreBackup();
        }}
        className="sm:max-w-md"
      />

      <ActionConfirmDialog
        open={confirmDeleteRecord !== null}
        onOpenChange={(open) => {
          if (!open && deletingBackupKey === null) {
            setConfirmDeleteRecord(null);
          }
        }}
        title="Delete backup"
        desc={
          confirmDeleteRecord
            ? `Delete backup "${confirmDeleteRecord.name}"?`
            : "Delete this backup?"
        }
        confirmText="Delete backup"
        destructive
        isLoading={
          confirmDeleteRecord !== null &&
          deletingBackupKey === getBackupKey(confirmDeleteRecord)
        }
        handleConfirm={() => {
          void confirmDeleteBackup();
        }}
        className="sm:max-w-md"
      />

      <ActionConfirmDialog
        open={confirmDeleteSchedule !== null}
        onOpenChange={(open) => {
          if (!open && deletingScheduleId === null) {
            setConfirmDeleteSchedule(null);
          }
        }}
        title="Delete scheduled backup"
        desc={
          confirmDeleteSchedule
            ? `Delete scheduled backup "${confirmDeleteSchedule.name}"?`
            : "Delete this scheduled backup?"
        }
        confirmText="Delete schedule"
        destructive
        isLoading={
          confirmDeleteSchedule !== null &&
          deletingScheduleId === confirmDeleteSchedule.id
        }
        handleConfirm={() => {
          void confirmDeleteScheduledBackup();
        }}
        className="sm:max-w-md"
      />

      <div className="px-4 pb-6 sm:px-6 lg:px-8">
        <section className="overflow-hidden rounded-xl border border-[var(--app-border)] bg-[var(--app-bg-2)]">
          {loadError ? (
            <div className="border-b border-[var(--app-border)] px-4 py-3 text-sm text-[var(--app-danger)]">
              {loadError}
            </div>
          ) : null}

          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead className="w-[140px]">Location</TableHead>
                <TableHead className="w-[180px]">Created</TableHead>
                <TableHead className="w-[120px]">Size</TableHead>
                <TableHead className="w-[220px] text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {loading ? (
                <TableRow>
                  <TableCell
                    colSpan={5}
                    className="h-40 text-center text-sm text-muted-foreground"
                  >
                    Loading backups...
                  </TableCell>
                </TableRow>
              ) : backups.length === 0 ? (
                <TableRow>
                  <TableCell
                    colSpan={5}
                    className="h-40 text-center text-sm text-muted-foreground"
                  >
                    No backups created yet.
                  </TableCell>
                </TableRow>
              ) : (
                backups.map((backup) => {
                  const backupKey = getBackupKey(backup);
                  const deleting = deletingBackupKey === backupKey;
                  const restoring = restoringBackupKey === backupKey;
                  const restored = restoredBackupKey === backupKey;

                  return (
                    <TableRow key={backupKey}>
                      <TableCell className="font-medium text-foreground">
                        {backup.name}
                      </TableCell>
                      <TableCell className="text-sm text-muted-foreground">
                        {formatBackupLocation(backup.location)}
                      </TableCell>
                      <TableCell className="text-sm text-muted-foreground">
                        {formatDateTime(backup.created_at)}
                      </TableCell>
                      <TableCell className="text-sm text-muted-foreground">
                        {formatBytes(backup.size)}
                      </TableCell>
                      <TableCell>
                        <div className="flex justify-end gap-2">
                          <Button
                            type="button"
                            variant="outline"
                            size="icon"
                            onClick={() => handleRestoreBackup(backup)}
                            disabled={restoring || deleting}
                            aria-label={`Restore ${backup.name}`}
                            title={`Restore ${backup.name}`}
                          >
                            <ActionFeedbackIcon
                              busy={restoring}
                              done={restored}
                              icon={RotateCcw}
                              className="h-4 w-4"
                            />
                          </Button>
                          <Button
                            type="button"
                            variant="outline"
                            size="icon"
                            asChild
                          >
                            <a
                              href={getBackupDownloadUrl(
                                backup.id,
                                backup.location,
                              )}
                              aria-label={`Download ${backup.name}`}
                              title={`Download ${backup.name}`}
                            >
                              <Download className="h-4 w-4" />
                            </a>
                          </Button>
                          <Button
                            type="button"
                            variant="destructive"
                            size="icon"
                            onClick={() => handleDeleteBackup(backup)}
                            disabled={deleting}
                            aria-label={`Delete ${backup.name}`}
                            title={`Delete ${backup.name}`}
                          >
                            {deleting ? (
                              <LoaderCircle className="h-4 w-4 animate-spin" />
                            ) : (
                              <Trash2 className="h-4 w-4" />
                            )}
                          </Button>
                        </div>
                      </TableCell>
                    </TableRow>
                  );
                })
              )}
            </TableBody>
          </Table>
        </section>

        <section className="mt-6 overflow-hidden rounded-xl border border-[var(--app-border)] bg-[var(--app-bg-2)]">
          <div className="flex flex-col gap-3 border-b border-[var(--app-border)] px-4 py-4 sm:flex-row sm:items-center sm:justify-between">
            <div className="space-y-1">
              <h2 className="text-base font-semibold text-foreground">
                Scheduled backups
              </h2>
              <p className="text-sm text-muted-foreground">
                Recurring backup jobs created from this page.
              </p>
            </div>
            <Button type="button" variant="outline" asChild>
              <Link to="/cron">Open cron</Link>
            </Button>
          </div>

          {scheduledLoadError ? (
            <div className="border-b border-[var(--app-border)] px-4 py-3 text-sm text-[var(--app-danger)]">
              {scheduledLoadError}
            </div>
          ) : !scheduledLoading && !schedulerEnabled ? (
            <div className="border-b border-[var(--app-border)] px-4 py-3 text-sm text-muted-foreground">
              Cron scheduling is disabled. Saved backup schedules will not run
              until{" "}
              <span className="font-medium text-foreground">
                FLOWPANEL_CRON_ENABLED
              </span>{" "}
              is enabled.
            </div>
          ) : !scheduledLoading && !schedulerStarted ? (
            <div className="border-b border-[var(--app-border)] px-4 py-3 text-sm text-muted-foreground">
              Cron scheduling is enabled but the scheduler has not started yet.
            </div>
          ) : null}

          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead className="w-[180px]">Schedule</TableHead>
                <TableHead className="w-[140px]">Location</TableHead>
                <TableHead>Scope</TableHead>
                <TableHead className="w-[180px]">Created</TableHead>
                <TableHead className="w-[120px] text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {scheduledLoading ? (
                <TableRow>
                  <TableCell
                    colSpan={6}
                    className="h-32 text-center text-sm text-muted-foreground"
                  >
                    Loading scheduled backups...
                  </TableCell>
                </TableRow>
              ) : scheduledBackups.length === 0 ? (
                <TableRow>
                  <TableCell
                    colSpan={6}
                    className="h-32 text-center text-sm text-muted-foreground"
                  >
                    No scheduled backups yet.
                  </TableCell>
                </TableRow>
              ) : (
                scheduledBackups.map((backupSchedule) => (
                  <TableRow key={backupSchedule.id}>
                    <TableCell className="font-medium text-foreground">
                      {backupSchedule.name}
                    </TableCell>
                    <TableCell className="font-mono text-sm text-muted-foreground">
                      {backupSchedule.schedule}
                    </TableCell>
                    <TableCell className="text-sm text-muted-foreground">
                      {formatBackupLocation(backupSchedule.location)}
                    </TableCell>
                    <TableCell className="text-sm text-muted-foreground">
                      {formatScheduledBackupScope(backupSchedule)}
                    </TableCell>
                    <TableCell className="text-sm text-muted-foreground">
                      {formatDateTime(backupSchedule.created_at)}
                    </TableCell>
                    <TableCell>
                      <div className="flex justify-end gap-2">
                        <Button
                          type="button"
                          variant="destructive"
                          size="icon"
                          onClick={() =>
                            handleDeleteScheduledBackup(backupSchedule)
                          }
                          disabled={deletingScheduleId === backupSchedule.id}
                          aria-label={`Delete ${backupSchedule.name}`}
                          title={`Delete ${backupSchedule.name}`}
                        >
                          {deletingScheduleId === backupSchedule.id ? (
                            <LoaderCircle className="h-4 w-4 animate-spin" />
                          ) : (
                            <Trash2 className="h-4 w-4" />
                          )}
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </section>
      </div>
    </>
  );
}
